package tool

import (
	"bot/handlers"
	"bot/session"
	"bot/types"
	"bot/utils"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func init() {
	// MP4 Commands
	handlers.RegisterCommand(types.Command{Name: "ytmp4", Category: "downloader", Execute: executeYtmp4})
	handlers.RegisterCommand(types.Command{Name: "yt", Category: "downloader", Execute: executeYtmp4})

	// MP3 Commands
	handlers.RegisterCommand(types.Command{Name: "ytmp3", Category: "downloader", Execute: executeYtmp3})
	handlers.RegisterCommand(types.Command{Name: "play", Category: "downloader", Execute: executeYtmp3})

	// Interceptor
	handlers.RegisterInteractiveSessionHandler(handleYTInteraction)
}

func executeYtmp4(client *whatsmeow.Client, msg *events.Message, args []string) {
	processYTCommand(client, msg, args, "ytmp4")
}

func executeYtmp3(client *whatsmeow.Client, msg *events.Message, args []string) {
	processYTCommand(client, msg, args, "ytmp3")
}

func processYTCommand(client *whatsmeow.Client, msg *events.Message, args []string, targetCmd string) {
	senderID := msg.Info.Sender.ToNonAD().String()
	fullArgString := strings.Join(args, " ")

	if fullArgString == "" {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Please provide a YouTube link or a search query.")
		return
	}

	// 1. Direct Link Bypass
	if strings.Contains(fullArgString, "youtube.com") || strings.Contains(fullArgString, "youtu.be") {
		if targetCmd == "ytmp4" {
			go downloadDirectYTVideo(client, msg, fullArgString)
		} else {
			go downloadDirectYTAudio(client, msg, fullArgString)
		}
		return
	}

	// 2. Search & Anti-Spam Lock
	if !session.AcquireLock(senderID, "youtube") {
		utils.SendReply(client, msg.Info.Chat, "⏳ You already have an active YouTube search menu! Please wait 30 seconds for it to close.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, "🔍 Searching YouTube...")

	go func() {
		ytDlpPath, cleanup, err := utils.GetYtDlpPathForVersionCheck()
		if err != nil {
			return
		}
		defer cleanup()

		cmdArgs := []string{fmt.Sprintf("ytsearch30:%s", fullArgString), "--dump-json", "--flat-playlist", "--no-warnings"}
		out, err := exec.Command(ytDlpPath, cmdArgs...).CombinedOutput()
		if err != nil {
			session.ReleaseLock(senderID, "youtube")
			utils.SendReply(client, msg.Info.Chat, "❌ Search failed.")
			return
		}

		var results []utils.YTSearchResult
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			var data utils.YTSearchResult
			if err := json.Unmarshal([]byte(line), &data); err == nil && data.URL != "" {
				results = append(results, data)
			}
		}

		if len(results) == 0 {
			session.ReleaseLock(senderID, "youtube")
			utils.SendReply(client, msg.Info.Chat, "❌ No results found.")
			return
		}

		sess := &session.InteractiveSession{
			Type:      "youtube",
			Query:     fullArgString,
			Page:      0,
			Data:      results,
			UserID:    senderID,
			TargetCmd: targetCmd, // Remembers if they want MP4 or MP3!
		}

		SendYTPage(client, msg.Info.Chat, sess)
	}()
}

// --- DIRECT DOWNLOADERS ---

func downloadDirectYTVideo(client *whatsmeow.Client, msg *events.Message, videoURL string) {
	utils.SendReply(client, msg.Info.Chat, "  Fetching video details...")

	// Bumped back to 1080p because Documents have a 100MB+ limit!
	videoInfo, err := utils.GetVideoInfo(videoURL, "1080")
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "  Failed to fetch video details.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("  *[ YOUTUBE VIDEO ]*\n\n*Title:* %s\n\nProcessing high-quality video document...", videoInfo.Title))

	tempDir := "temp"
	os.MkdirAll(tempDir, 0755)
	outputFile := filepath.Join(tempDir, fmt.Sprintf("video_%d.mp4", time.Now().Unix()))
	defer os.Remove(outputFile)

	if err := utils.DownloadVideo(videoURL, outputFile, "1080"); err != nil {
		fmt.Printf("  DOWNLOAD ERROR: %v\n", err)
		utils.SendReply(client, msg.Info.Chat, "  Video download failed.")
		return
	}

	videoData, err := os.ReadFile(outputFile)
	if err != nil {
		fmt.Printf("  FILE READ ERROR: %v\n", err)
		utils.SendReply(client, msg.Info.Chat, "  Failed to read downloaded video file.")
		return
	}

	// Upload as a Document to bypass strict video size limits
	uploaded, err := client.Upload(context.Background(), videoData, whatsmeow.MediaDocument)
	if err != nil {
		fmt.Printf("  UPLOAD ERROR: %v\n", err)
		utils.SendReply(client, msg.Info.Chat, "  Failed to upload document to WhatsApp servers.")
		return
	}

	// Construct a Document Message
	docMsg := &waE2E.DocumentMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String("video/mp4"),
		FileName:      proto.String(videoInfo.Title + ".mp4"), // Ensures it has a playable file extension
		FileSHA256:    uploaded.FileSHA256,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileLength:    proto.Uint64(uploaded.FileLength),
		Caption:       proto.String(videoInfo.Title),
	}

	_, err = client.SendMessage(context.Background(), msg.Info.Chat, &waE2E.Message{DocumentMessage: docMsg})
	if err != nil {
		fmt.Printf("  SEND MESSAGE ERROR: %v\n", err)
		utils.SendReply(client, msg.Info.Chat, "  Failed to send document message.")
	}
}

func downloadDirectYTAudio(client *whatsmeow.Client, msg *events.Message, videoURL string) {
	utils.SendReply(client, msg.Info.Chat, "⏳ Fetching audio details...")

	videoInfo, err := utils.GetVideoInfo(videoURL, "best")
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to fetch audio details.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎧 *[ YOUTUBE AUDIO ]*\n\n*Title:* %s\n\nProcessing MP3...", videoInfo.Title))

	ytDlpPath, ytCleanup, _ := utils.GetYtDlpPathForVersionCheck()
	defer ytCleanup()
	ffmpegPath, ffCleanup, _ := utils.GetFFmpegPath()
	defer ffCleanup()

	tempDir := "temp"
	os.Mkdir(tempDir, 0755)
	outputFile := filepath.Join(tempDir, fmt.Sprintf("audio_%d.mp3", time.Now().Unix()))
	defer os.Remove(outputFile)

	args := []string{
		"-f", "bestaudio",
		"-x", "--audio-format", "mp3",
		"-o", outputFile,
		"--ffmpeg-location", ffmpegPath,
		"--no-warnings", videoURL,
	}

	if err := exec.Command(ytDlpPath, args...).Run(); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Audio download failed.")
		return
	}

	audioData, err := os.ReadFile(outputFile)
	if err != nil {
		return
	}

	uploaded, err := client.Upload(context.Background(), audioData, whatsmeow.MediaAudio)
	if err != nil {
		fmt.Printf("\n❌ UPLOAD ERROR: Failed to upload video to WhatsApp: %v\n", err)
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to upload the video to WhatsApp servers. Check the console for details.")
		return
	}

	audioMsg := &waE2E.AudioMessage{
		URL: proto.String(uploaded.URL), DirectPath: proto.String(uploaded.DirectPath),
		MediaKey: uploaded.MediaKey, Mimetype: proto.String("audio/mpeg"),
		FileSHA256: uploaded.FileSHA256, FileEncSHA256: uploaded.FileEncSHA256,
		FileLength: proto.Uint64(uploaded.FileLength),
        Seconds: proto.Uint32(uint32(videoInfo.Duration)),
	}
	client.SendMessage(context.Background(), msg.Info.Chat, &waE2E.Message{AudioMessage: audioMsg})
}

// --- INTERACTIVE MENU & INTERCEPTOR ---

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

	text := fmt.Sprintf("🔍 *YouTube Search: %s*\nFormat: %s | Page %d\n\n", sess.Query, formatType, sess.Page+1)
	for i := start; i < end; i++ {
		text += fmt.Sprintf("*%d.* %s (%02d:%02d)\n", (i-start)+1, results[i].Title, int(results[i].Duration)/60, int(results[i].Duration)%60)
	}
	text += "\nReply with a number *(1-10)* to download.\nReply with *+* for next page, *-* for previous.\n_(Closes in 30s)_"

	sentMsg, err := client.SendMessage(context.Background(), chatJID, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text)},
	})
	if err == nil {
		session.StoreInteractiveSession(sentMsg.ID, sess)
	}
}

func handleYTInteraction(client *whatsmeow.Client, msg *events.Message) bool {
	ctxInfo := msg.Message.GetExtendedTextMessage().GetContextInfo()
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

			// CLEAR THE SESSION (Releases memory, unlocks the user, and stops the timer)
			session.ClearInteractiveSession(*ctxInfo.StanzaID)

			if sess.TargetCmd == "ytmp4" {
				go downloadDirectYTVideo(client, msg, targetVideo.URL)
			} else {
				go downloadDirectYTAudio(client, msg, targetVideo.URL)
			}
		}
		return true
	}

	return false
}
