package tool

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

const smemeUsage = "⚠️ *Sticker maker*\n\n" +
	"• `/smeme some text` → brat-style sticker\n" +
	"• Reply to a photo or sticker with `/smeme TOP | BOTTOM` → meme sticker\n" +
	"• Reply to a photo, GIF, or video with `/sticker` → normal/animated sticker\n" +
	"• Reply to a video or combine media+audio with `/sa` → sound sticker"

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "smeme",
		Description: "Text-only makes a brat sticker; reply to a photo/sticker makes a meme sticker.",
		Category:    "media",
		Execute:     executeStickerMeme,
	})
}

func resolveImageOrStickerSource(client *whatsmeow.Client, msg *events.Message) (*waE2E.Message, []byte) {
	core := utils.UnwrapMessage(msg.Message)
	if isImageLike(core) {
		return core, nil
	}

	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil {
		return nil, nil
	}

	botID := ""
	if client.Store != nil && client.Store.ID != nil {
		botID = client.Store.ID.ToNonAD().User
	}
	if cached, ok := utils.DefaultMessageCache.Get(botID, ctxInfo.GetStanzaID()); ok && cached != nil {
		if isImageLike(cached.Message) && (len(cached.MediaData) > 0 || utils.HasDownloadableMedia(cached.Message)) {
			return cached.Message, cached.MediaData
		}
	}

	quoted := utils.UnwrapMessage(ctxInfo.QuotedMessage)
	if isImageLike(quoted) {
		return quoted, nil
	}
	return nil, nil
}

func isImageLike(msg *waE2E.Message) bool {
	if msg == nil {
		return false
	}
	if msg.ImageMessage != nil || msg.StickerMessage != nil {
		return true
	}
	if msg.DocumentMessage != nil && strings.HasPrefix(strings.ToLower(msg.DocumentMessage.GetMimetype()), "image/") {
		return true
	}
	return false
}

func executeStickerMeme(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, smemeUsage)
		return
	}

	srcMsg, preCached := resolveImageOrStickerSource(client, msg)
	if srcMsg == nil {
		// Text-only -> brat sticker.
		go executeBratSticker(client, msg, text)
		return
	}

	parts := strings.SplitN(text, "|", 2)
	top := strings.TrimSpace(parts[0])
	bottom := ""
	if len(parts) > 1 {
		bottom = strings.TrimSpace(parts[1])
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		data := preCached
		if len(data) == 0 {
			var err error
			data, _, _, err = utils.DownloadMedia(ctx, client, srcMsg)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ Failed to download the image: "+err.Error())
				return
			}
		}

		pngData, err := renderMemegen(data, top, bottom)
		if err != nil {
			fmt.Printf("[SMEME] memegen failed (%v), using local renderer fallback\n", err)
			pngData, err = renderMemePNG(data, top, bottom)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ Could not render that meme: "+err.Error())
				return
			}
		}

		if err := sendStickerPNG(client, msg, pngData); err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Failed to send the sticker: "+err.Error())
		}
	}()
}
