package tool

import (
	"context"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "sticker",
		Description: "Turns a replied-to image into a WebP sticker.",
		Category:    "media",
		Execute:     executeSticker,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "s",
		Description: "Alias for /sticker.",
		Category:    "media",
		Execute:     executeSticker,
	})
}

func executeSticker(client *whatsmeow.Client, msg *events.Message, args []string) {
	media := utils.GetMediaMessage(msg.Message)
	if media == nil {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Reply to an image with `/sticker`.")
		return
	}

	data, err := client.Download(context.Background(), media)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download that media.")
		return
	}

	if err := sendStickerPNG(client, msg, data); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not convert that to a sticker.")
	}
}
