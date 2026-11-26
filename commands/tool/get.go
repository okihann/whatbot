package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "get",
		Description: "Downloads the replied-to media (including view-once) and sends it to your private chat.",
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

func executeGet(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || (ctxInfo.QuotedMessage == nil && ctxInfo.GetStanzaID() == "") {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Please reply to a media or view-once message to use `/get`.")
		return
	}

	botID := ""
	if client.Store != nil && client.Store.ID != nil {
		botID = client.Store.ID.ToNonAD().User
	}

	quotedID := ctxInfo.GetStanzaID()
	quotedMsg := utils.UnwrapMessage(ctxInfo.QuotedMessage)
	cached, hasCached := utils.DefaultMessageCache.Get(botID, quotedID)

	var candidates []*waE2E.Message
	var preDownloaded []byte
	var isViewOnce bool
	var unavailableVO bool

	if hasCached && cached != nil {
		if len(cached.MediaData) > 0 {
			preDownloaded = cached.MediaData
		}
		if cached.IsViewOnce {
			isViewOnce = true
		}
		if cached.UnavailableViewOnce {
			unavailableVO = true
		}
	}
	if utils.IsViewOnceMessage(nil, ctxInfo.QuotedMessage) {
		isViewOnce = true
	}

	// Prefer candidates that actually carry downloadable URL / DirectPath.
	if hasCached && cached != nil && utils.HasDownloadableMedia(cached.Message) {
		candidates = append(candidates, cached.Message)
	}
	if utils.HasDownloadableMedia(quotedMsg) {
		candidates = append(candidates, quotedMsg)
	}

	// If neither has URL/DirectPath, check whether we still have pre-cached bytes.
	if len(candidates) == 0 && len(preDownloaded) > 0 {
		if hasCached && cached != nil && utils.IsMediaMessage(cached.Message) {
			candidates = append(candidates, cached.Message)
		} else if utils.IsMediaMessage(quotedMsg) {
			candidates = append(candidates, quotedMsg)
		}
	}

	if len(candidates) == 0 {
		if isViewOnce || unavailableVO {
			fmt.Printf("[GET] View-once media unavailable (bot=%s id=%s unavailableVO=%v hasCached=%v quotedHasMedia=%v quotedHasURL=%v)\n",
				botID, quotedID, unavailableVO, hasCached, utils.IsMediaMessage(quotedMsg), utils.HasDownloadableMedia(quotedMsg))
			utils.SendReply(client, msg.Info.Chat,
				"❌ WhatsApp blocked this view-once media on this linked device and stripped its media keys from the reply.\n\n"+
					"To fix this for future view-once messages, remove & re-pair this account via QR (`remove <id>` then `add` in the bot console) so WhatsApp registers it as an Android companion device.")
			return
		}
		utils.SendReply(client, msg.Info.Chat, "⚠️ The replied-to message does not contain any downloadable media.")
		return
	}

	recipientJID := utils.ResolvePrivateChatJID(client, msg)
	chatJID := msg.Info.Chat

	utils.SendReply(client, chatJID, "⏳ Processing media, please wait...")

	go func() {
		ctx := context.Background()
		var lastErr error

		for i, candidate := range candidates {
			origCaption := utils.MediaCaption(candidate)
			caption := "Here is the media you requested!"
			if isViewOnce {
				caption = "👁️ *[View-Once Recovered]*\nHere is the media you requested!"
			}
			if origCaption != "" {
				caption += "\n\n_Original Caption:_\n" + origCaption
			}

			var bytesToUse []byte
			if i == 0 && len(preDownloaded) > 0 {
				bytesToUse = preDownloaded
			}

			err := utils.ForwardPermanentMedia(ctx, client, recipientJID, candidate, bytesToUse, caption, caption)
			if err == nil {
				if recipientJID.ToNonAD() != chatJID.ToNonAD() {
					utils.SendReply(client, chatJID, "✅ Media sent to your private chat!")
				}
				return
			}
			lastErr = err
		}

		fmt.Printf("[GET] Failed to recover media (bot=%s id=%s candidates=%d preCached=%dB): %v\n",
			botID, quotedID, len(candidates), len(preDownloaded), lastErr)
		utils.SendReply(client, chatJID, fmt.Sprintf("❌ Sorry, I couldn't download or recover that media (%v).", lastErr))
	}()
}
