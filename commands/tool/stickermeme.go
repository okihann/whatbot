package tool

import (
	"context"
	"strings"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

const smemeUsage = "⚠️ *Sticker maker*\n\n" +
	"• `/smeme some text` → brat-style sticker\n" +
	"• Reply to a photo with `/smeme TOP | BOTTOM` → meme sticker\n" +
	"• Reply to a photo with `/sticker` → plain sticker"

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "smeme",
		Description: "Text-only makes a brat sticker; reply to a photo makes a meme sticker.",
		Category:    "media",
		Execute:     executeStickerMeme,
	})
}

func executeStickerMeme(client *whatsmeow.Client, msg *events.Message, args []string) {
	if len(args) == 0 {
		utils.SendReply(client, msg.Info.Chat, smemeUsage)
		return
	}

	media := utils.GetMediaMessage(msg.Message)
	if media == nil {
		// No image -> brat sticker from the text.
		executeBratSticker(client, msg, strings.Join(args, " "))
		return
	}

	parts := strings.SplitN(strings.Join(args, " "), "|", 2)
	top := strings.TrimSpace(parts[0])
	bottom := ""
	if len(parts) > 1 {
		bottom = strings.TrimSpace(parts[1])
	}

	data, err := client.Download(context.Background(), media)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download the original image.")
		return
	}

	// memegen gives nicer typography; fall back to the local renderer if the
	// upload or the render fails, so the command never hard-depends on it.
	pngData, err := renderMemegen(data, top, bottom)
	if err != nil {
		pngData, err = renderMemePNG(data, top, bottom)
	}
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not render that meme.")
		return
	}

	if err := sendStickerPNG(client, msg, pngData); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to send the sticker.")
	}
}
