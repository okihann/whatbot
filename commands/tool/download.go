package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"
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
		Name:        "dl",
		Description: "Downloads media from Instagram, X/Twitter, Reddit, Facebook, TikTok, Pinterest, YouTube, etc.",
		Category:    "downloader",
		Execute:     executeGenericDownload,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "download",
		Description: "Alias for /dl.",
		Category:    "downloader",
		Execute:     executeGenericDownload,
	})
}

func isPinterestURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "pinterest.") || strings.Contains(lower, "pin.it")
}

func executeGenericDownload(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawInput := strings.TrimSpace(strings.Join(args, " "))
	targetURL := utils.ExtractURL(rawInput)
	if targetURL == "" || !utils.IsValidMediaURL(targetURL) {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Please provide a valid media URL.\nExample: `/dl https://...`")
		return
	}

	// Route known platforms to their specialized handlers.
	switch {
	case isPinterestURL(targetURL):
		go downloadPinterestURL(client, msg.Info.Chat, targetURL)
		return
	case isTikTokURL(targetURL):
		go downloadTikTokURL(client, msg.Info.Chat, targetURL)
		return
	case isYouTubeLink(targetURL):
		go downloadDirectYTVideo(client, msg.Info.Chat, targetURL)
		return
	}

	go downloadGenericMedia(client, msg.Info.Chat, targetURL)
}

func downloadGenericMedia(client *whatsmeow.Client, chatJID waTypes.JID, targetURL string) {
	utils.SendReply(client, chatJID, "📥 Fetching media, please wait...")

	info, err := utils.GetVideoInfo(targetURL, "1080")
	if err != nil {
		fmt.Printf("[DL] Info error for %s: %v\n", targetURL, err)
		utils.SendReply(client, chatJID, "❌ Could not fetch media from that link. It may be private or unsupported.")
		return
	}

	title := strings.TrimSpace(info.Title)
	if title == "" {
		title = "Downloaded Media"
	}

	workDir, err := os.MkdirTemp("", "nopebot-dl-cmd-*")
	if err != nil {
		utils.SendReply(client, chatJID, "❌ Could not create temporary workspace.")
		return
	}
	defer os.RemoveAll(workDir)

	outputFile := filepath.Join(workDir, fmt.Sprintf("media_%d.mp4", time.Now().UnixNano()))
	if err := utils.DownloadVideo(targetURL, outputFile, "1080"); err != nil {
		fmt.Printf("[DL] Download error for %s: %v\n", targetURL, err)
		utils.SendReply(client, chatJID, "❌ Media download failed.")
		return
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		utils.SendReply(client, chatJID, "❌ Failed to read downloaded file.")
		return
	}

	ctx := context.Background()
	if len(data) <= inlineVideoMaxBytes {
		thumb, seconds := utils.ExtractVideoMeta(outputFile)
		if seconds == 0 && info.Duration > 0 {
			seconds = uint32(info.Duration)
		}
		if _, err := utils.SendVideoData(ctx, client, chatJID, data, title, seconds, thumb); err != nil {
			fmt.Printf("[DL] Video send error: %v\n", err)
			utils.SendReply(client, chatJID, "❌ Failed to send video to WhatsApp.")
		}
		return
	}

	fileName := utils.SanitizeFilename(title) + ".mp4"
	if _, err := utils.SendDocumentData(ctx, client, chatJID, data, "video/mp4", fileName, title); err != nil {
		fmt.Printf("[DL] Document send error: %v\n", err)
		utils.SendReply(client, chatJID, "❌ Failed to send media document to WhatsApp.")
	}
}
