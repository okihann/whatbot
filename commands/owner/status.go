package owner

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "status",
		Description: "Uploads a status (Video/Image/Audio/Docs) or deletes a status.",
		Category:    "owner",
		Execute:     executeStatus,
	})
}

func executeStatus(client *whatsmeow.Client, msg *events.Message, args []string) {
	// --- PROPER DELETION LOGIC ---
	fullText := ""
	if len(args) > 0 {
		fullText = strings.TrimSpace(args[0])
	}

	// --- 1. PROPER DELETION LOGIC ---
	if strings.HasPrefix(strings.ToLower(fullText), "delete") {
		// Split the text into separate words to extract the ID
		parts := strings.Fields(fullText)
		
		if len(parts) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Please provide the status ID to delete. E.g., `/status delete 12345ABC...`")
			return
		}
		
		statusID := parts[1] // The actual ID is the second word
		statusJID := waTypes.NewJID("status", "broadcast")
		ownJID := client.Store.ID.ToNonAD()

		// Build the exact revocation protocol message
		revokeMsg := client.BuildRevoke(statusJID, ownJID, statusID)

		_, err := client.SendMessage(context.Background(), statusJID, revokeMsg)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to delete status: %v", err))
		} else {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Status `%s` deleted globally!", statusID))
		}
		return
	}

	caption := strings.Join(args, " ")
	statusJID := waTypes.NewJID("status", "broadcast")

	var imgMsg *waE2E.ImageMessage
	var vidMsg *waE2E.VideoMessage
	var audioMsg *waE2E.AudioMessage
	var docMsg *waE2E.DocumentMessage
	var statusMsg *waE2E.Message

	if msg.Message.GetImageMessage() != nil {
		imgMsg = msg.Message.GetImageMessage()
	} else if msg.Message.GetVideoMessage() != nil {
		vidMsg = msg.Message.GetVideoMessage()
	} else if quoted := msg.Message.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage(); quoted != nil {
		if quoted.GetImageMessage() != nil {
			imgMsg = quoted.GetImageMessage()
		} else if quoted.GetVideoMessage() != nil {
			vidMsg = quoted.GetVideoMessage()
		} else if quoted.GetAudioMessage() != nil {
			audioMsg = quoted.GetAudioMessage()
		} else if quoted.GetDocumentMessage() != nil {
			docMsg = quoted.GetDocumentMessage() // Intercept quoted documents
		}
	}

	if imgMsg != nil {
		utils.SendReply(client, msg.Info.Chat, "Processing image status...")
		data, err := client.Download(context.Background(), imgMsg)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "Failed to download image.")
			return
		}
		uploaded, err := client.Upload(context.Background(), data, whatsmeow.MediaImage)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "Failed to upload image.")
			return
		}

		statusMsg = &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      imgMsg.Mimetype,
				FileSHA256:    uploaded.FileSHA256,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Caption:       proto.String(caption),
			},
		}
	} else if vidMsg != nil {
		utils.SendReply(client, msg.Info.Chat, "Processing video status...")
		data, err := client.Download(context.Background(), vidMsg)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "Failed to download video.")
			return
		}
		uploaded, err := client.Upload(context.Background(), data, whatsmeow.MediaVideo)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "Failed to upload video.")
			return
		}

		statusMsg = &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      vidMsg.Mimetype,
				FileSHA256:    uploaded.FileSHA256,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Caption:       proto.String(caption),
				Seconds:       vidMsg.Seconds,
				GifPlayback:   vidMsg.GifPlayback,
				JPEGThumbnail: vidMsg.JPEGThumbnail,
			},
		}
	} else if audioMsg != nil {
		utils.SendReply(client, msg.Info.Chat, "Processing audio status...")
		data, err := client.Download(context.Background(), audioMsg)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "Failed to download audio.")
			return
		}
		uploaded, err := client.Upload(context.Background(), data, whatsmeow.MediaAudio)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "Failed to upload audio.")
			return
		}

		statusMsg = &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				Mimetype:      audioMsg.Mimetype,
				FileSHA256:    uploaded.FileSHA256,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
				Seconds:       audioMsg.Seconds,
				PTT:           proto.Bool(true),
			},
		}
	} else if docMsg != nil {
		// --- SMART DOCUMENT CONVERSION ---
		mime := docMsg.GetMimetype()
		
		if strings.HasPrefix(mime, "video/") {
			utils.SendReply(client, msg.Info.Chat, "Converting video document to status and generating thumbnail...")
			data, err := client.Download(context.Background(), docMsg)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "Failed to download document.")
				return
			}

			// --- THE FIX: GENERATE THUMBNAIL AND DURATION ---
			tempDir := "temp"
			os.MkdirAll(tempDir, 0755)
			
			// Save the video temporarily to memory
			tempPath := filepath.Join(tempDir, fmt.Sprintf("status_vid_%d.mp4", time.Now().UnixNano()))
			os.WriteFile(tempPath, data, 0644)
			defer os.Remove(tempPath) // Guarantees the file is deleted immediately after

			// 1. Extract a fast thumbnail using ffmpeg
			thumbCmd := exec.Command("ffmpeg", "-i", tempPath, "-ss", "00:00:00", "-vframes", "1", "-vf", "scale=320:-1", "-f", "image2", "-")
			jpegThumb, _ := thumbCmd.Output()

			// 2. Extract the exact duration using ffprobe
			var seconds uint32
			durCmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", tempPath)
			if durBytes, err := durCmd.Output(); err == nil {
				durFloat, _ := strconv.ParseFloat(strings.TrimSpace(string(durBytes)), 64)
				seconds = uint32(durFloat)
			}
			// ------------------------------------------------
			
			// Upload as MediaVideo instead of Document
			uploaded, err := client.Upload(context.Background(), data, whatsmeow.MediaVideo)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "Failed to upload as video status. (File may exceed 16MB limit)")
				return
			}

			statusMsg = &waE2E.Message{
				VideoMessage: &waE2E.VideoMessage{
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mime),
					FileSHA256:    uploaded.FileSHA256,
					FileEncSHA256: uploaded.FileEncSHA256,
					FileLength:    proto.Uint64(uploaded.FileLength),
					Caption:       proto.String(caption),
					JPEGThumbnail: jpegThumb,             // <-- Thumbnail fixed!
					Seconds:       proto.Uint32(seconds), // <-- Duration fixed!
				},
			}
		} else if strings.HasPrefix(mime, "image/") {
			utils.SendReply(client, msg.Info.Chat, "Converting image document to status...")
			data, err := client.Download(context.Background(), docMsg)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "Failed to download document.")
				return
			}
			
			uploaded, err := client.Upload(context.Background(), data, whatsmeow.MediaImage)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "Failed to upload as image status.")
				return
			}

			statusMsg = &waE2E.Message{
				ImageMessage: &waE2E.ImageMessage{
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mime),
					FileSHA256:    uploaded.FileSHA256,
					FileEncSHA256: uploaded.FileEncSHA256,
					FileLength:    proto.Uint64(uploaded.FileLength),
					Caption:       proto.String(caption),
				},
			}
		} else {
			utils.SendReply(client, msg.Info.Chat, "❌ Unsupported document type. Status must be an image, video, or audio file.")
			return
		}
	} else {
		if caption == "" {
			utils.SendReply(client, msg.Info.Chat, "Please provide text, a media reply, or use `/status delete <id>`.")
			return
		}
        
		statusMsg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:           proto.String(caption),
				BackgroundArgb: proto.Uint32(0xFF5D5D5D),
			},
		}
	}

	resp, err := client.SendMessage(context.Background(), statusJID, statusMsg)
	
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to send status: %v", err))
	} else {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Status successfully posted!\n*ID:* `%s`\n_(To revoke globally, reply to me with: /status delete %s)_", resp.ID, resp.ID))
	}
}
