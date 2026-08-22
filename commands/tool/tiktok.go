package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "tiktok",
		Description: "Downloads a video from a TikTok link.",
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

func executeTiktok(client *whatsmeow.Client, msg *events.Message, args []string) {
	// The full message content is passed as the first argument
	fullArgString := ""
	if len(args) > 0 {
		fullArgString = args[0]
	}

	if fullArgString == "" || !strings.Contains(fullArgString, "tiktok.com") {
		utils.SendReply(client, msg.Info.Chat, "❌ Please provide a valid TikTok link.")
		return
	}

	// Launch the entire process in a goroutine to keep the bot responsive
	go func(url string, chatJID waTypes.JID) {
		utils.SendReply(client, chatJID, "🔍 Fetching TikTok details, please wait...")

		// Use the generic GetVideoInfo function, it works for TikTok too
		videoInfo, err := utils.GetVideoInfo(url, "best")
		if err != nil {
			fmt.Println("Error getting TikTok info:", err)
			utils.SendReply(client, chatJID, "⚠️ Failed to fetch TikTok details. The video may be private or the link is invalid.")
			return
		}

		title := videoInfo.Title
		if title == "" {
			title = "TikTok Video"
		}

		var fileSize int64
		if videoInfo.Filesize > 0 {
			fileSize = videoInfo.Filesize
		} else {
			fileSize = videoInfo.FilesizeApprox
		}
		sizeMB := float64(fileSize) / (1024 * 1024)

		infoMessage := fmt.Sprintf(
			`🎬 *[ TIKTOK VIDEO ]*

• *Title:* %s
• *Size:* %.2f MB

⌛ Please wait while your video is being downloaded...`, title, sizeMB)

		utils.SendReply(client, chatJID, infoMessage)

		tempDir := "temp"
		if _, err := os.Stat(tempDir); os.IsNotExist(err) {
			os.Mkdir(tempDir, 0755)
		}
		outputFile := filepath.Join(tempDir, fmt.Sprintf("tiktok_%d.mp4", time.Now().Unix()))
		defer os.Remove(outputFile)

		// Use the generic DownloadVideo function
		err = utils.DownloadVideo(url, outputFile, "best")
		if err != nil {
			fmt.Println("Error downloading TikTok video:", err)
			utils.SendReply(client, chatJID, "⚠️ Video download failed.")
			return
		}

		if _, err := os.Stat(outputFile); os.IsNotExist(err) {
			utils.SendReply(client, chatJID, "⚠️ Video download failed or the file is empty.")
			return
		}

		videoData, err := os.ReadFile(outputFile)
		if err != nil {
			utils.SendReply(client, chatJID, "⚠️ Failed to read downloaded video file.")
			return
		}

		uploaded, err := client.Upload(context.Background(), videoData, whatsmeow.MediaVideo)
		if err != nil {
			utils.SendReply(client, chatJID, "⚠️ Failed to upload video to WhatsApp.")
			return
		}

		videoMsg := &waE2E.VideoMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String("video/mp4"),
			FileSHA256:    uploaded.FileSHA256,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Caption:       proto.String(title),
            Seconds:       proto.Uint32(uint32(videoInfo.Duration)),
		}

		_, err = client.SendMessage(context.Background(), chatJID, &waE2E.Message{VideoMessage: videoMsg})
		if err != nil {
			fmt.Println("Error sending TikTok video message:", err)
		}

	}(fullArgString, msg.Info.Chat)
}
