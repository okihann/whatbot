package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "tiktok",
		Description: "Downloads a video or photo slideshow from a TikTok link.",
		Category:    "downloader",
		Execute:     executeTiktok,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "tt",
		Description: "Alias for /tiktok.",
		Category:    "downloader",
		Execute:     executeTiktok,
	})
}

type tikwmResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Title    string   `json:"title"`
		Duration int      `json:"duration"`
		Play     string   `json:"play"`
		HDPlay   string   `json:"hdplay"`
		Music    string   `json:"music"`
		Images   []string `json:"images"`
	} `json:"data"`
}

func isTikTokURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "tiktok.com")
}

func fetchTikWM(ctx context.Context, tiktokURL string) (*tikwmResponse, error) {
	endpoint := "https://www.tikwm.com/api/?hd=1&url=" + url.QueryEscape(tiktokURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", utils.BrowserUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result tikwmResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("tikwm error: %s", result.Msg)
	}
	return &result, nil
}

func executeTiktok(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawInput := strings.TrimSpace(strings.Join(args, " "))
	targetURL := utils.ExtractURL(rawInput)
	if targetURL == "" || !isTikTokURL(targetURL) {
		utils.SendReply(client, msg.Info.Chat, "❌ Please provide a valid TikTok link.")
		return
	}

	go downloadTikTokURL(client, msg.Info.Chat, targetURL)
}

func downloadTikTokURL(client *whatsmeow.Client, chatJID waTypes.JID, targetURL string) {
	utils.SendReply(client, chatJID, "🔍 Fetching TikTok media, please wait...")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Try TikWM first: it is fast, watermark-free, and handles both videos AND
	// photo slideshows (which yt-dlp does not support).
	if tw, err := fetchTikWM(ctx, targetURL); err == nil && tw != nil {
		title := strings.TrimSpace(tw.Data.Title)
		if title == "" {
			title = "TikTok"
		}

		// Case 1: TikTok Photo Slideshow.
		if len(tw.Data.Images) > 0 {
			if err := sendTikTokSlideshow(ctx, client, chatJID, title, tw.Data.Images, tw.Data.Music); err == nil {
				return
			} else {
				fmt.Printf("[TIKTOK] Slideshow send failed: %v\n", err)
			}
		}

		// Case 2: TikTok Video (prefer HD, fallback to standard play).
		videoURL := tw.Data.HDPlay
		if videoURL == "" {
			videoURL = tw.Data.Play
		}
		if videoURL != "" {
			if err := downloadAndSendTikTokDirect(ctx, client, chatJID, videoURL, title, uint32(tw.Data.Duration)); err == nil {
				return
			} else {
				fmt.Printf("[TIKTOK] TikWM video download failed (%v), falling back to yt-dlp...\n", err)
			}
		}
	} else if err != nil {
		fmt.Printf("[TIKTOK] TikWM API unavailable (%v), falling back to yt-dlp...\n", err)
	}

	// Fallback: yt-dlp video download + H.264 compatibility check.
	downloadTikTokViaYtDlp(ctx, client, chatJID, targetURL)
}

func sendTikTokSlideshow(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, title string, images []string, musicURL string) error {
	sent := 0
	for i, imgURL := range images {
		data, ct, err := utils.DownloadHTTPBytes(ctx, imgURL, nil)
		if err != nil {
			continue
		}
		caption := ""
		if i == 0 {
			caption = fmt.Sprintf("🖼️ *[ TIKTOK SLIDESHOW ]* (%d photos)\n\n%s", len(images), title)
		}
		if _, err := utils.SendImageData(ctx, client, chatJID, data, ct, caption); err == nil {
			sent++
		}
	}
	if sent == 0 {
		return fmt.Errorf("failed to download slideshow images")
	}

	// Send background audio if available.
	if musicURL != "" {
		if audioBytes, _, err := utils.DownloadHTTPBytes(ctx, musicURL, nil); err == nil && len(audioBytes) > 0 {
			_, _ = utils.SendAudioData(ctx, client, chatJID, audioBytes, "audio/mpeg", 0, false)
		}
	}
	return nil
}

func downloadAndSendTikTokDirect(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, videoURL, title string, duration uint32) error {
	rawBytes, _, err := utils.DownloadHTTPBytes(ctx, videoURL, nil)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "whatbot-tt-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	rawPath := filepath.Join(workDir, "raw.mp4")
	outPath := filepath.Join(workDir, "out.mp4")
	if err := os.WriteFile(rawPath, rawBytes, 0644); err != nil {
		return err
	}
	if err := utils.EnsureWhatsAppVideo(rawPath, outPath); err != nil {
		outPath = rawPath
	}

	finalBytes, err := os.ReadFile(outPath)
	if err != nil {
		return err
	}

	thumb, seconds := utils.ExtractVideoMeta(outPath)
	if seconds == 0 {
		seconds = duration
	}
	_, err = utils.SendVideoData(ctx, client, chatJID, finalBytes, title, seconds, thumb)
	return err
}

func downloadTikTokViaYtDlp(ctx context.Context, client *whatsmeow.Client, chatJID waTypes.JID, targetURL string) {
	videoInfo, err := utils.GetVideoInfo(targetURL, "best")
	if err != nil {
		fmt.Printf("[TIKTOK] yt-dlp info error: %v\n", err)
		utils.SendReply(client, chatJID, "⚠️ Failed to fetch TikTok details. The video may be private or the link is invalid.")
		return
	}

	title := videoInfo.Title
	if title == "" {
		title = "TikTok Video"
	}

	workDir, err := os.MkdirTemp("", "whatbot-tt-yt-*")
	if err != nil {
		utils.SendReply(client, chatJID, "⚠️ Could not prepare temporary directory.")
		return
	}
	defer os.RemoveAll(workDir)

	outputFile := filepath.Join(workDir, fmt.Sprintf("tiktok_%d.mp4", time.Now().UnixNano()))
	if err := utils.DownloadVideo(targetURL, outputFile, "best"); err != nil {
		fmt.Printf("[TIKTOK] yt-dlp download error: %v\n", err)
		utils.SendReply(client, chatJID, "⚠️ TikTok video download failed.")
		return
	}

	videoData, err := os.ReadFile(outputFile)
	if err != nil {
		utils.SendReply(client, chatJID, "⚠️ Failed to read downloaded video file.")
		return
	}

	thumb, seconds := utils.ExtractVideoMeta(outputFile)
	if seconds == 0 && videoInfo.Duration > 0 {
		seconds = uint32(videoInfo.Duration)
	}

	if _, err := utils.SendVideoData(ctx, client, chatJID, videoData, title, seconds, thumb); err != nil {
		fmt.Printf("[TIKTOK] Send video error: %v\n", err)
		utils.SendReply(client, chatJID, "⚠️ Failed to upload TikTok video to WhatsApp.")
	}
}
