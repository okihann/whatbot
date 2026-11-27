package tool

import (
	"bot/handlers"
	"bot/session"
	"bot/types"
	"bot/utils"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const inlineVideoMaxBytes = 16 * 1024 * 1024 // 16 MB safe inline video limit; larger sent as Document

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "ytmp4",
		Description: "Search or download a YouTube video as MP4.",
		Category:    "downloader",
		Execute:     executeYtmp4,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "yt",
		Description: "Alias for /ytmp4.",
		Category:    "downloader",
		Execute:     executeYtmp4,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "ytmp3",
		Description: "Search or download YouTube audio as MP3.",
		Category:    "downloader",
		Execute:     executeYtmp3,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "play",
		Description: "Alias for /ytmp3.",
		Category:    "downloader",
		Execute:     executeYtmp3,
	})

	handlers.RegisterInteractiveSessionHandler(handleYTInteraction)
}

func executeYtmp4(client *whatsmeow.Client, msg *events.Message, args []string) {
	processYTCommand(client, msg, args, "ytmp4")
}

func executeYtmp3(client *whatsmeow.Client, msg *events.Message, args []string) {
	processYTCommand(client, msg, args, "ytmp3")
}

func isYouTubeLink(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "youtube.com") || strings.Contains(lower, "youtu.be")
}

func processYTCommand(client *whatsmeow.Client, msg *events.Message, args []string, targetCmd string) {
	senderID := msg.Info.Sender.ToNonAD().String()
	fullArgString := strings.TrimSpace(strings.Join(args, " "))

	if fullArgString == "" {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Please provide a YouTube link or a search query.")
		return
	}

	// 1. Direct YouTube URL (handles extra share text around the link too).
	if extracted := utils.ExtractURL(fullArgString); extracted != "" && isYouTubeLink(extracted) {
		if targetCmd == "ytmp4" {
			go downloadDirectYTVideo(client, msg.Info.Chat, extracted)
		} else {
			go downloadDirectYTAudio(client, msg.Info.Chat, extracted)
		}
		return
	}

	// 2. Interactive YouTube search.
	if !session.AcquireLock(senderID, "youtube") {
		utils.SendReply(client, msg.Info.Chat, "⏳ You already have an active YouTube search menu! Please wait 30 seconds for it to close.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, "🔍 Searching YouTube...")

	go func() {
		results, err := utils.SearchYouTube(fullArgString, 20)
		if err != nil {
			fmt.Printf("[YOUTUBE] Search failed: %v\n", err)
			session.ReleaseLock(senderID, "youtube")
			utils.SendReply(client, msg.Info.Chat, "❌ YouTube search failed.")
			return
		}

		if len(results) == 0 {
			session.ReleaseLock(senderID, "youtube")
			utils.SendReply(client, msg.Info.Chat, "❌ No YouTube results found.")
			return
		}

		sess := &session.InteractiveSession{
			Type:      "youtube",
			Query:     fullArgString,
			Page:      0,
			Data:      results,
			UserID:    senderID,
			TargetCmd: targetCmd,
		}

		SendYTPage(client, msg.Info.Chat, sess)
	}()
}

func formatYTDlError(prefix string, err error) string {
	if errors.Is(err, utils.ErrYouTubeBotCheck) {
		return prefix + "\n\n⚠️ YouTube blocked this server's IP (bot check). Configure `cookiesFile` in `config.json` (or place a `cookies.txt` file in the bot folder) to authenticate downloads."
	}
	return prefix
}

func downloadDirectYTVideo(client *whatsmeow.Client, chatJID waTypes.JID, videoURL string) {
	utils.SendReply(client, chatJID, "⏳ Fetching YouTube video details...")

	videoInfo, err := utils.GetVideoInfo(videoURL, "1080")
	if err != nil {
		fmt.Printf("[YOUTUBE] Info error: %v\n", err)
		utils.SendReply(client, chatJID, formatYTDlError("❌ Failed to fetch video details.", err))
		return
	}

	title := videoInfo.Title
	if title == "" {
		title = "YouTube Video"
	}

	utils.SendReply(client, chatJID, fmt.Sprintf("🎬 *[ YOUTUBE VIDEO ]*\n\n*Title:* %s\n\n⏳ Downloading and preparing video...", title))

	workDir, err := os.MkdirTemp("", "nopebot-yt-*")
	if err != nil {
		utils.SendReply(client, chatJID, "❌ Could not create temporary workspace.")
		return
	}
	defer os.RemoveAll(workDir)

	outputFile := filepath.Join(workDir, fmt.Sprintf("video_%d.mp4", time.Now().UnixNano()))
	if err := utils.DownloadVideo(videoURL, outputFile, "1080"); err != nil {
		fmt.Printf("[YOUTUBE] Video download error: %v\n", err)
		utils.SendReply(client, chatJID, formatYTDlError("❌ Video download failed.", err))
		return
	}

	videoData, err := os.ReadFile(outputFile)
	if err != nil {
		fmt.Printf("[YOUTUBE] File read error: %v\n", err)
		utils.SendReply(client, chatJID, "❌ Failed to read downloaded video file.")
		return
	}

	ctx := context.Background()
	if len(videoData) <= inlineVideoMaxBytes {
		thumb, seconds := utils.ExtractVideoMeta(outputFile)
		if seconds == 0 && videoInfo.Duration > 0 {
			seconds = uint32(videoInfo.Duration)
		}
		if _, err := utils.SendVideoData(ctx, client, chatJID, videoData, title, seconds, thumb); err != nil {
			fmt.Printf("[YOUTUBE] Video send error: %v\n", err)
			utils.SendReply(client, chatJID, "❌ Failed to send video to WhatsApp.")
		}
		return
	}

	// Videos larger than 16MB are sent as DocumentMessage to bypass WhatsApp's
	// stricter inline video size limit while keeping full quality.
	fileName := utils.SanitizeFilename(title) + ".mp4"
	if _, err := utils.SendDocumentData(ctx, client, chatJID, videoData, "video/mp4", fileName, title); err != nil {
		fmt.Printf("[YOUTUBE] Document send error: %v\n", err)
		utils.SendReply(client, chatJID, "❌ Failed to send video document to WhatsApp.")
	}
}

func downloadDirectYTAudio(client *whatsmeow.Client, chatJID waTypes.JID, videoURL string) {
	utils.SendReply(client, chatJID, "⏳ Fetching YouTube audio details...")

	videoInfo, err := utils.GetVideoInfo(videoURL, "best")
	if err != nil {
		fmt.Printf("[YOUTUBE] Audio info error: %v\n", err)
		utils.SendReply(client, chatJID, formatYTDlError("❌ Failed to fetch audio details.", err))
		return
	}

	title := videoInfo.Title
	if title == "" {
		title = "YouTube Audio"
	}

	utils.SendReply(client, chatJID, fmt.Sprintf("🎧 *[ YOUTUBE AUDIO ]*\n\n*Title:* %s\n\n⏳ Downloading and converting to MP3...", title))

	workDir, err := os.MkdirTemp("", "nopebot-yta-*")
	if err != nil {
		utils.SendReply(client, chatJID, "❌ Could not create temporary workspace.")
		return
	}
	defer os.RemoveAll(workDir)

	outputFile := filepath.Join(workDir, fmt.Sprintf("audio_%d.mp3", time.Now().UnixNano()))
	if err := utils.DownloadAudioMP3(videoURL, outputFile); err != nil {
		fmt.Printf("[YOUTUBE] Audio download error: %v\n", err)
		utils.SendReply(client, chatJID, formatYTDlError("❌ Audio download failed.", err))
		return
	}

	audioData, err := os.ReadFile(outputFile)
	if err != nil {
		fmt.Printf("[YOUTUBE] Audio read error: %v\n", err)
		utils.SendReply(client, chatJID, "❌ Failed to read downloaded audio file.")
		return
	}

	if _, err := utils.SendAudioData(context.Background(), client, chatJID, audioData, "audio/mpeg", uint32(videoInfo.Duration), false); err != nil {
		fmt.Printf("[YOUTUBE] Audio send error: %v\n", err)
		utils.SendReply(client, chatJID, "❌ Failed to upload audio to WhatsApp.")
	}
}

func formatYTDuration(seconds float64) string {
	total := int(seconds)
	if total <= 0 {
		return "LIVE"
	}
	if total >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", total/3600, (total%3600)/60, total%60)
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func SendYTPage(client *whatsmeow.Client, chatJID waTypes.JID, sess *session.InteractiveSession) {
	results := sess.Data.([]utils.YTSearchResult)
	start := sess.Page * 10
	end := start + 10
	if end > len(results) {
		end = len(results)
	}

	formatType := "MP4 VIDEO"
	if sess.TargetCmd == "ytmp3" {
		formatType = "MP3 AUDIO"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🔍 *YouTube Search: %s*\nFormat: %s | Page %d\n\n", sess.Query, formatType, sess.Page+1)
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "*%d.* %s (%s)\n", (i-start)+1, results[i].Title, formatYTDuration(results[i].Duration))
	}
	b.WriteString("\nReply with a number *(1-10)* to download.\nReply with *+* for next page, *-* for previous.\n_(Closes in 30s)_")

	sentMsg, err := client.SendMessage(context.Background(), chatJID, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(b.String())},
	})
	if err != nil {
		session.ReleaseLock(sess.UserID, "youtube")
		return
	}
	session.StoreInteractiveSession(sentMsg.ID, sess)
}

func handleYTInteraction(client *whatsmeow.Client, msg *events.Message) bool {
	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || ctxInfo.StanzaID == nil {
		return false
	}

	sess, found := session.GetInteractiveSession(*ctxInfo.StanzaID)
	if !found || sess.Type != "youtube" {
		return false
	}

	if sess.UserID != msg.Info.Sender.ToNonAD().String() {
		utils.SendReply(client, msg.Info.Chat, "⚠️ This is not your search session!")
		return true
	}

	userText := strings.ToLower(strings.TrimSpace(utils.GetTextFromMessage(msg.Message)))
	results := sess.Data.([]utils.YTSearchResult)

	if userText == "+" {
		session.ResetSessionTimer(*ctxInfo.StanzaID)
		if (sess.Page+1)*10 < len(results) {
			sess.Page++
			SendYTPage(client, msg.Info.Chat, sess)
		} else {
			utils.SendReply(client, msg.Info.Chat, "⚠️ You are on the last page.")
		}
		return true
	}

	if userText == "-" {
		session.ResetSessionTimer(*ctxInfo.StanzaID)
		if sess.Page > 0 {
			sess.Page--
			SendYTPage(client, msg.Info.Chat, sess)
		} else {
			utils.SendReply(client, msg.Info.Chat, "⚠️ You are on the first page.")
		}
		return true
	}

	var selection int
	if _, err := fmt.Sscanf(userText, "%d", &selection); err == nil && selection >= 1 && selection <= 10 {
		realIndex := (sess.Page * 10) + (selection - 1)
		if realIndex < len(results) {
			targetVideo := results[realIndex]
			session.ClearInteractiveSession(*ctxInfo.StanzaID)

			if sess.TargetCmd == "ytmp4" {
				go downloadDirectYTVideo(client, msg.Info.Chat, targetVideo.URL)
			} else {
				go downloadDirectYTAudio(client, msg.Info.Chat, targetVideo.URL)
			}
		}
		return true
	}

	return false
}
