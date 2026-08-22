package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "get",
		Description: "Downloads the replied-to media and sends it to your private chat.",
		Category:    "tools",
		Execute:     executeGet,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "fetch",
		Description: "Alias for /get.",
		Category:    "tools",
		Execute:     executeGet,
	})
}

// unwrapMessage remains the same.
func unwrapMessage(msg *waE2E.Message) *waE2E.Message {
	if msg.EphemeralMessage != nil {
		return unwrapMessage(msg.EphemeralMessage.Message)
	}
	if msg.ViewOnceMessage != nil {
		return unwrapMessage(msg.ViewOnceMessage.Message)
	}
	if msg.ViewOnceMessageV2 != nil {
		return unwrapMessage(msg.ViewOnceMessageV2.Message)
	}
	return msg
}

func executeGet(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctxInfo := msg.Message.GetExtendedTextMessage().GetContextInfo()
	if ctxInfo == nil || ctxInfo.QuotedMessage == nil {
		utils.SendReply(client, msg.Info.Chat, "Please reply to a media message to use this command.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, "Processing media, please wait...")

	quotedMsg := unwrapMessage(ctxInfo.QuotedMessage)

	var data []byte
	var err error
	var mediaType whatsmeow.MediaType

	if quotedMsg.ImageMessage != nil {
		mediaType = whatsmeow.MediaImage
		data, err = client.Download(context.Background(), quotedMsg.ImageMessage)
	} else if quotedMsg.VideoMessage != nil {
		mediaType = whatsmeow.MediaVideo
		data, err = client.Download(context.Background(), quotedMsg.VideoMessage)
	} else if quotedMsg.AudioMessage != nil {
		mediaType = whatsmeow.MediaAudio
		data, err = client.Download(context.Background(), quotedMsg.AudioMessage)
	} else if quotedMsg.StickerMessage != nil {
		mediaType = whatsmeow.MediaImage
		data, err = client.Download(context.Background(), quotedMsg.StickerMessage)
	} else if quotedMsg.DocumentMessage != nil {
		mediaType = whatsmeow.MediaDocument
		data, err = client.Download(context.Background(), quotedMsg.DocumentMessage)
	} else {
		utils.SendReply(client, msg.Info.Chat, "The replied-to message does not contain any downloadable media.")
		return
	}

	if err != nil {
		fmt.Printf("Failed to download media: %v\n", err)
		utils.SendReply(client, msg.Info.Chat, "Sorry, I couldn't download that media.")
		return
	}

	uploadedMedia, err := client.Upload(context.Background(), data, mediaType)
	if err != nil {
		fmt.Printf("Failed to upload media: %v\n", err)
		utils.SendReply(client, msg.Info.Chat, "Sorry, I couldn't process the media to send it.")
		return
	}

	var finalMessage waE2E.Message
	caption := "Here is the media you requested!"

	if quotedMsg.ImageMessage != nil {
		finalMessage.ImageMessage = &waE2E.ImageMessage{
			URL: proto.String(uploadedMedia.URL), DirectPath: proto.String(uploadedMedia.DirectPath), MediaKey: uploadedMedia.MediaKey,
			Mimetype: proto.String("image/jpeg"), FileEncSHA256: uploadedMedia.FileEncSHA256, FileSHA256: uploadedMedia.FileSHA256,
			FileLength: proto.Uint64(uploadedMedia.FileLength), Caption: &caption,
		}
	} else if quotedMsg.VideoMessage != nil {
		finalMessage.VideoMessage = &waE2E.VideoMessage{
			URL: proto.String(uploadedMedia.URL), DirectPath: proto.String(uploadedMedia.DirectPath), MediaKey: uploadedMedia.MediaKey,
			Mimetype: proto.String("video/mp4"), FileEncSHA256: uploadedMedia.FileEncSHA256, FileSHA256: uploadedMedia.FileSHA256,
			FileLength: proto.Uint64(uploadedMedia.FileLength), Caption: &caption,
		}
	} else if quotedMsg.AudioMessage != nil {
		finalMessage.AudioMessage = &waE2E.AudioMessage{
			URL: proto.String(uploadedMedia.URL), DirectPath: proto.String(uploadedMedia.DirectPath), MediaKey: uploadedMedia.MediaKey,
			Mimetype: proto.String("audio/ogg; codecs=opus"), FileEncSHA256: uploadedMedia.FileEncSHA256, FileSHA256: uploadedMedia.FileSHA256,
			FileLength: proto.Uint64(uploadedMedia.FileLength), PTT: proto.Bool(true),
		}
	} else if quotedMsg.StickerMessage != nil {
		finalMessage.StickerMessage = &waE2E.StickerMessage{
			URL: proto.String(uploadedMedia.URL), DirectPath: proto.String(uploadedMedia.DirectPath), MediaKey: uploadedMedia.MediaKey,
			Mimetype: proto.String("image/webp"), FileEncSHA256: uploadedMedia.FileEncSHA256, FileSHA256: uploadedMedia.FileSHA256,
			FileLength: proto.Uint64(uploadedMedia.FileLength),
		}
	} else if quotedMsg.DocumentMessage != nil {
		finalMessage.DocumentMessage = &waE2E.DocumentMessage{
			URL: proto.String(uploadedMedia.URL), DirectPath: proto.String(uploadedMedia.DirectPath), MediaKey: uploadedMedia.MediaKey,
			Mimetype: quotedMsg.DocumentMessage.Mimetype, FileName: quotedMsg.DocumentMessage.FileName, FileEncSHA256: uploadedMedia.FileEncSHA256,
			FileSHA256: uploadedMedia.FileSHA256, FileLength: proto.Uint64(uploadedMedia.FileLength),
		}
	}

	var recipientJID waTypes.JID
	if msg.Info.IsFromMe {
		recipientJID = *client.Store.ID
	} else {
		recipientJID = msg.Info.Sender
	}

	// --- DEFINITIVE FIX: Convert the JID to its non-device form ---
	bareRecipientJID := recipientJID.ToNonAD()

	_, err = client.SendMessage(context.Background(), bareRecipientJID, &finalMessage)
	if err != nil {
		fmt.Printf("Failed to send media privately: %v\n", err)
		utils.SendReply(client, msg.Info.Chat, "I couldn't send the media to your private chat.")
		return
	}

	if recipientJID != msg.Info.Chat {
		utils.SendReply(client, msg.Info.Chat, "✅ Media sent to your private chat!")
	}
}
