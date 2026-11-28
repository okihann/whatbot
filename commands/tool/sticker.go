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

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "sticker",
		Aliases:     []string{"s"},
		Description: "Convert a photo, GIF, or short video into a WebP sticker.",
		Category:    "media",
		Execute:     executeSticker,
	})
}

func parseStickerArgs(args []string) (crop bool, packName, author string) {
	packName = utils.DefaultStickerPackName
	author = utils.DefaultStickerAuthor

	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		return crop, packName, author
	}

	// Check for leading/standalone "crop" flag.
	fields := strings.Fields(raw)
	if len(fields) > 0 && strings.EqualFold(fields[0], "crop") {
		crop = true
		raw = strings.TrimSpace(strings.TrimPrefix(raw, fields[0]))
	}
	if raw == "" {
		return crop, packName, author
	}

	if strings.Contains(raw, "|") {
		parts := strings.SplitN(raw, "|", 2)
		if p := strings.TrimSpace(parts[0]); p != "" {
			packName = p
		}
		if a := strings.TrimSpace(parts[1]); a != "" {
			author = a
		}
	} else {
		packName = raw
	}
	return crop, packName, author
}

func resolveVisualStickerSource(client *whatsmeow.Client, msg *events.Message) (src *waE2E.Message, preCached []byte, isVideo bool) {
	check := func(m *waE2E.Message) (ok bool, video bool) {
		if m == nil {
			return false, false
		}
		if m.ImageMessage != nil || m.StickerMessage != nil {
			return true, false
		}
		if m.VideoMessage != nil {
			return true, true
		}
		if m.DocumentMessage != nil {
			mime := strings.ToLower(m.DocumentMessage.GetMimetype())
			if strings.HasPrefix(mime, "image/gif") || strings.HasPrefix(mime, "video/") {
				return true, true
			}
			if strings.HasPrefix(mime, "image/") {
				return true, false
			}
		}
		return false, false
	}

	core := utils.UnwrapMessage(msg.Message)
	if ok, vid := check(core); ok {
		return core, nil, vid
	}

	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil {
		return nil, nil, false
	}

	botID := ""
	if client.Store != nil && client.Store.ID != nil {
		botID = client.Store.ID.ToNonAD().User
	}
	if cached, found := utils.DefaultMessageCache.Get(botID, ctxInfo.GetStanzaID()); found && cached != nil {
		if ok, vid := check(cached.Message); ok && (len(cached.MediaData) > 0 || utils.HasDownloadableMedia(cached.Message)) {
			return cached.Message, cached.MediaData, vid
		}
	}

	quoted := utils.UnwrapMessage(ctxInfo.QuotedMessage)
	if ok, vid := check(quoted); ok {
		return quoted, nil, vid
	}
	return nil, nil, false
}

func executeSticker(client *whatsmeow.Client, msg *events.Message, args []string) {
	srcMsg, preCached, isVideo := resolveVisualStickerSource(client, msg)
	if srcMsg == nil {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Send or reply to a photo, GIF, or short video with `/sticker` (or `/s`).\nOptional: `/s crop` or `/s Pack | Author`")
		return
	}

	crop, packName, author := parseStickerArgs(args)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		data := preCached
		if len(data) == 0 {
			var err error
			data, _, _, err = utils.DownloadMedia(ctx, client, srcMsg)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ Failed to download that media.")
				return
			}
		}

		var (
			webpData   []byte
			isAnimated bool
			err        error
		)
		if isVideo || (srcMsg.StickerMessage != nil && srcMsg.StickerMessage.GetIsAnimated()) {
			isAnimated = true
			if srcMsg.StickerMessage != nil && srcMsg.StickerMessage.GetIsAnimated() {
				// Re-stamp EXIF directly on existing animated WebP sticker.
				webpData = utils.InjectWebPEXIF(data, utils.BuildStickerEXIF(packName, author, []string{"✨"}))
			} else {
				webpData, err = utils.ConvertVideoToAnimatedWebP(data, 7, crop, packName, author)
			}
		} else {
			webpData, err = utils.ConvertImageToWebP(data, crop, packName, author)
		}

		if err != nil {
			fmt.Printf("[STICKER] Conversion error: %v\n", err)
			utils.SendReply(client, msg.Info.Chat, "❌ Could not convert that media into a sticker.")
			return
		}

		if _, err := utils.SendSticker(ctx, client, msg.Info.Chat, webpData, isAnimated, msg); err != nil {
			fmt.Printf("[STICKER] Send error: %v\n", err)
			utils.SendReply(client, msg.Info.Chat, "❌ Failed to send sticker to WhatsApp.")
		}
	}()
}
