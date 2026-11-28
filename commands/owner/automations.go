package owner

import (
	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"
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
		Name:        "anti-delete",
		Description: "Recovers deleted messages and forwards them to your private chat. (Owner only)",
		Category:    "owner",
		Execute:     executeAntiDelete,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "auto-get",
		Description: "Automatically saves view-once media from targets to your private chat. (Owner only)",
		Category:    "owner",
		Execute:     executeAutoGet,
	})
}

func isTargeted(targets []string, num string) bool {
	cleanNum := utils.NormalizeTarget(num)
	for _, t := range targets {
		cleanTarget := utils.NormalizeTarget(t)
		if cleanTarget == "" {
			continue
		}
		if cleanTarget == "all" {
			return true
		}
		if cleanNum != "" && (cleanNum == cleanTarget || strings.HasPrefix(cleanNum, cleanTarget)) {
			return true
		}
	}
	return false
}

func hasExactTarget(targets []string, target string) bool {
	want := utils.NormalizeTarget(target)
	for _, t := range targets {
		if utils.NormalizeTarget(t) == want {
			return true
		}
	}
	return false
}

func manageAutomation(client *whatsmeow.Client, msg *events.Message, args []string, feature string) {
	conf, err := config.LoadConfig()
	if err != nil {
		return
	}
	if !utils.IsOwner(client, msg, conf) {
		utils.SendReply(client, msg.Info.Chat, "🚫 Access Denied: This command is restricted to the bot owner.")
		return
	}

	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Usage: `/%s [on|off|add|remove|list] [number/all]`", feature))
		return
	}

	parts := strings.Fields(args[0])
	action := strings.ToLower(parts[0])
	target := ""
	if len(parts) > 1 {
		target = utils.NormalizeTarget(strings.Join(parts[1:], ""))
	}

	if client.Store == nil || client.Store.ID == nil {
		return
	}
	botID := client.Store.ID.ToNonAD().User
	autoConf := db.GetAutoConfig(botID)

	isAntiDelete := feature == "anti-delete"
	getTargets := func() []string {
		if isAntiDelete {
			return autoConf.AntiDeleteTargets
		}
		return autoConf.AutoGetTargets
	}
	setTargets := func(list []string) {
		if isAntiDelete {
			autoConf.AntiDeleteTargets = list
		} else {
			autoConf.AutoGetTargets = list
		}
	}

	switch action {
	case "list":
		enabled := autoConf.AutoGetOn
		title := "Auto-Get"
		if isAntiDelete {
			enabled = autoConf.AntiDeleteOn
			title = "Anti-Delete"
		}
		targets := getTargets()
		targetDisplay := "(none — run `/" + feature + " add all` or `/" + feature + " add 628...`)"
		if len(targets) > 0 {
			targetDisplay = strings.Join(targets, ", ")
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📋 *%s Status*\n*Global Switch:* %v\n*Targets:* %s", title, enabled, targetDisplay))
		return

	case "on":
		if isAntiDelete {
			autoConf.AntiDeleteOn = true
		} else {
			autoConf.AutoGetOn = true
		}
		reply := fmt.Sprintf("✅ *%s* is now ON.", feature)
		if len(getTargets()) == 0 {
			reply += fmt.Sprintf("\n\n⚠️ No targets configured yet! Add one with:\n`/%s add all` or `/%s add 628...`", feature, feature)
		}
		utils.SendReply(client, msg.Info.Chat, reply)

	case "off":
		if isAntiDelete {
			autoConf.AntiDeleteOn = false
		} else {
			autoConf.AutoGetOn = false
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ *%s* is now OFF.", feature))

	case "add":
		if target == "" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Please provide a number to add. (e.g. `/%s add all` or `/%s add 628...`)", feature, feature))
			return
		}
		targets := getTargets()
		if !hasExactTarget(targets, target) {
			setTargets(append(targets, target))
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Added `%s` to *%s* targets.", target, feature))

	case "remove":
		if target == "" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Please provide a target to remove. (e.g. `/%s remove all` or `/%s remove 628...`)", feature, feature))
			return
		}
		var newList []string
		removed := false
		for _, v := range getTargets() {
			if utils.NormalizeTarget(v) == target {
				removed = true
				continue
			}
			newList = append(newList, v)
		}
		if newList == nil {
			newList = []string{}
		}
		setTargets(newList)
		if removed {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗑️ Removed `%s` from *%s* targets.", target, feature))
		} else {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚠️ `%s` was not found in *%s* targets.", target, feature))
		}

	default:
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Unknown action: `%s`\nUsage: `/%s [on|off|add|remove|list] [number/all]`", action, feature))
		return
	}

	if err := db.SaveAutoConfig(); err != nil {
		fmt.Printf("[AUTOMATIONS] Failed to save automation config: %v\n", err)
	}
}

func executeAntiDelete(client *whatsmeow.Client, msg *events.Message, args []string) {
	manageAutomation(client, msg, args, "anti-delete")
}

func executeAutoGet(client *whatsmeow.Client, msg *events.Message, args []string) {
	manageAutomation(client, msg, args, "auto-get")
}

// CacheOnlyMessage stores an incoming message in the shared cache without
// triggering automations (used for recent offline messages delivered on boot).
func CacheOnlyMessage(client *whatsmeow.Client, msg *events.Message) {
	if msg == nil || msg.Message == nil || client == nil || client.Store == nil || client.Store.ID == nil {
		return
	}
	coreMsg := utils.UnwrapMessage(msg.Message)
	if coreMsg == nil || isRevokeMessage(msg, coreMsg) {
		return
	}
	botID := client.Store.ID.ToNonAD().User
	if msg.Info.ID != "" {
		utils.DefaultMessageCache.Put(botID, msg.Info.ID, &utils.CachedMessage{
			Info:       msg.Info,
			Message:    coreMsg,
			IsViewOnce: utils.IsViewOnceMessage(msg, nil),
			StoredAt:   time.Now(),
		})
	}
}

// HandleUnavailableViewOnce records when WhatsApp blocks a view-once message
// with <unavailable type="view_once"> (typical when paired as a Web client)
// and sends a best-effort peer resend request to the primary phone.
func HandleUnavailableViewOnce(client *whatsmeow.Client, evt *events.UndecryptableMessage) {
	if evt == nil || client == nil || client.Store == nil || client.Store.ID == nil {
		return
	}
	if !evt.IsUnavailable || evt.UnavailableType != events.UnavailableTypeViewOnce {
		return
	}
	botID := client.Store.ID.ToNonAD().User
	utils.DefaultMessageCache.MarkUnavailableViewOnce(botID, evt.Info.ID, evt.Info)
	fmt.Printf("⚠️ [VIEW-ONCE] WhatsApp sent <unavailable type=\"view_once\"> for %s (account %s). Re-pair via QR to register as Android companion.\n", evt.Info.ID, botID)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := client.SendPeerMessage(ctx, client.BuildUnavailableMessageRequest(evt.Info.Chat, evt.Info.Sender, evt.Info.ID))
		if err != nil {
			fmt.Printf("  [VIEW-ONCE] Primary phone resend request for %s failed: %v\n", evt.Info.ID, err)
		}
	}()
}

func isRevokeMessage(evt *events.Message, coreMsg *waE2E.Message) bool {
	if coreMsg != nil && coreMsg.GetProtocolMessage() != nil &&
		coreMsg.GetProtocolMessage().GetType() == waE2E.ProtocolMessage_REVOKE {
		return true
	}
	return evt != nil && (evt.Info.Edit == waTypes.EditAttributeSenderRevoke || evt.Info.Edit == waTypes.EditAttributeAdminRevoke)
}

func resolveChatLabel(client *whatsmeow.Client, info waTypes.MessageInfo) string {
	if info.Chat.Server == waTypes.BroadcastServer {
		return "WhatsApp Status"
	}
	if info.IsGroup {
		if client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if groupInfo, err := client.GetGroupInfo(ctx, info.Chat); err == nil && groupInfo != nil && groupInfo.Name != "" {
				return fmt.Sprintf("Group (%s)", groupInfo.Name)
			}
		}
		return fmt.Sprintf("Group (%s)", info.Chat.String())
	}
	return "Private Chat"
}

func HandleAutomations(client *whatsmeow.Client, msg *events.Message) {
	if msg == nil || msg.Message == nil || client == nil || client.Store == nil || client.Store.ID == nil {
		return
	}

	ownerJID := client.Store.ID.ToNonAD()
	botID := ownerJID.User
	autoConf := db.GetAutoConfig(botID)
	coreMsg := utils.UnwrapMessage(msg.Message)
	if coreMsg == nil {
		return
	}

	// 1. Handle message revocations (Anti-Delete).
	if isRevokeMessage(msg, coreMsg) {
		handleRevoke(client, msg, coreMsg, ownerJID, botID, autoConf)
		return
	}

	isViewOnce := utils.IsViewOnceMessage(msg, nil)

	// 2. Cache the incoming message for anti-delete, auto-get, and /get.
	if msg.Info.ID != "" {
		utils.DefaultMessageCache.Put(botID, msg.Info.ID, &utils.CachedMessage{
			Info:       msg.Info,
			Message:    coreMsg,
			IsViewOnce: isViewOnce,
			StoredAt:   time.Now(),
		})

		// Eagerly capture view-once media bytes on arrival so opening or
		// deleting it seconds later never causes /get or anti-delete to fail.
		if isViewOnce && utils.HasDownloadableMedia(coreMsg) {
			fileLen := utils.MediaFileLength(coreMsg)
			if fileLen == 0 || fileLen <= utils.MaxCachedViewOnceBytes {
				go precacheViewOnceMedia(client, botID, msg.Info.ID, coreMsg)
			}
		}
	}

	// 3. Inspect quoted messages (recovers view-once messages revealed via reply
	// without ever clobbering a complete original cached message).
	if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil && ctxInfo.QuotedMessage != nil && ctxInfo.GetStanzaID() != "" {
		handleQuotedAutomation(client, msg, ctxInfo, ownerJID, botID, autoConf)
	}

	// 4. Direct Auto-Get:
	//    - Only capture view-once media (in both DMs and Groups) when targeted;
	//      regular DM media and stickers are ignored.
	if autoConf.AutoGetOn && !msg.Info.IsFromMe && msg.Info.Chat.Server != waTypes.BroadcastServer {
		if shouldAutoGetMedia(coreMsg, isViewOnce) {
			senderNum := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
			if isTargeted(autoConf.AutoGetTargets, senderNum) && utils.DefaultMessageCache.TryMarkAutoGet(botID, msg.Info.ID) {
				chatLabel := resolveChatLabel(client, msg.Info)
				go processAutoGet(client, botID, msg.Info.ID, coreMsg, ownerJID, senderNum, chatLabel, true)
			}
		}
	}
}

func shouldAutoGetMedia(coreMsg *waE2E.Message, isViewOnce bool) bool {
	return isViewOnce && utils.IsMediaMessage(coreMsg)
}

func precacheViewOnceMedia(client *whatsmeow.Client, botID, msgID string, coreMsg *waE2E.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, _, _, err := utils.DownloadMedia(ctx, client, coreMsg)
	if err != nil {
		fmt.Printf("  [VIEW-ONCE CACHE] Pre-download skipped for %s: %v\n", msgID, err)
		return
	}
	utils.DefaultMessageCache.SetMediaData(botID, msgID, data)
}

func handleRevoke(
	client *whatsmeow.Client,
	revokeEvt *events.Message,
	coreMsg *waE2E.Message,
	ownerJID waTypes.JID,
	botID string,
	autoConf *db.AutomationConfig,
) {
	if !autoConf.AntiDeleteOn {
		return
	}

	targetID := coreMsg.GetProtocolMessage().GetKey().GetID()
	if targetID == "" {
		targetID = revokeEvt.Info.MsgMetaInfo.TargetID
	}
	if targetID == "" {
		return
	}

	cached, found := utils.DefaultMessageCache.Get(botID, targetID)
	if !found || cached == nil || cached.Message == nil {
		return
	}

	senderNum := utils.ResolvePhoneNumber(client, cached.Info.Sender, cached.Info.SenderAlt)
	if senderNum == "" {
		senderNum = utils.ResolvePhoneNumber(client, revokeEvt.Info.Sender, revokeEvt.Info.SenderAlt)
	}
	if !isTargeted(autoConf.AntiDeleteTargets, senderNum) {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		chatName := resolveChatLabel(client, cached.Info)
		voBadge := ""
		if cached.IsViewOnce {
			voBadge = " (View-Once)"
		}
		alertHeader := fmt.Sprintf("🚨 *Anti-Delete Triggered*%s\n*Target:* %s\n*Chat:* %s", voBadge, senderNum, chatName)

		recoveredMsg := utils.UnwrapMessage(cached.Message)
		if utils.IsMediaMessage(recoveredMsg) {
			origCap := utils.MediaCaption(recoveredMsg)
			caption := alertHeader
			if origCap != "" {
				caption += "\n\n_Recovered Caption:_\n" + origCap
			}
			if err := utils.ForwardPermanentMedia(ctx, client, ownerJID, recoveredMsg, cached.MediaData, caption, alertHeader); err != nil {
				fmt.Printf("  [ANTI-DELETE] Failed to forward media (id=%s): %v\n", targetID, err)
				_, _ = utils.SendReply(client, ownerJID, alertHeader+"\n\n_(Media could not be recovered from WhatsApp servers.)_")
			}
			return
		}

		originalText := utils.GetTextFromMessage(recoveredMsg)
		if originalText == "" {
			originalText = "_[Unsupported or empty message]_"
		}
		finalText := alertHeader + "\n\n_Recovered Message:_\n" + originalText
		if _, err := client.SendMessage(ctx, ownerJID, &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(finalText),
			},
		}); err != nil {
			fmt.Printf("  [ANTI-DELETE] Failed to send recovered text (id=%s): %v\n", targetID, err)
		}
	}()
}

func handleQuotedAutomation(
	client *whatsmeow.Client,
	msg *events.Message,
	ctxInfo *waE2E.ContextInfo,
	ownerJID waTypes.JID,
	botID string,
	autoConf *db.AutomationConfig,
) {
	quotedID := ctxInfo.GetStanzaID()
	quotedCore := utils.UnwrapMessage(ctxInfo.QuotedMessage)
	if quotedCore == nil {
		return
	}
	quotedIsViewOnce := utils.IsViewOnceMessage(nil, ctxInfo.QuotedMessage)

	var quotedSenderJID, quotedSenderAlt waTypes.JID
	if p := ctxInfo.GetParticipant(); p != "" {
		quotedSenderJID, _ = waTypes.ParseJID(p)
	} else if !msg.Info.IsGroup {
		if msg.Info.IsFromMe {
			quotedSenderJID = msg.Info.Chat
			quotedSenderAlt = msg.Info.RecipientAlt
		} else {
			quotedSenderJID = msg.Info.Sender
			quotedSenderAlt = msg.Info.SenderAlt
		}
	} else {
		quotedSenderJID = msg.Info.Sender
		quotedSenderAlt = msg.Info.SenderAlt
	}

	utils.DefaultMessageCache.PutQuotedIfBetter(botID, quotedID, &utils.CachedMessage{
		Info: waTypes.MessageInfo{
			MessageSource: waTypes.MessageSource{
				Sender:    quotedSenderJID,
				SenderAlt: quotedSenderAlt,
				Chat:      msg.Info.Chat,
				IsGroup:   msg.Info.IsGroup,
			},
			ID:        quotedID,
			Timestamp: msg.Info.Timestamp,
		},
		Message:    quotedCore,
		IsViewOnce: quotedIsViewOnce,
		StoredAt:   time.Now(),
	})

	if !quotedIsViewOnce {
		if cached, ok := utils.DefaultMessageCache.Get(botID, quotedID); ok && cached != nil && cached.IsViewOnce {
			quotedIsViewOnce = true
		}
	}

	if !autoConf.AutoGetOn || !shouldAutoGetMedia(quotedCore, quotedIsViewOnce) {
		return
	}

	quotedSenderNum := utils.ResolvePhoneNumber(client, quotedSenderJID, quotedSenderAlt)
	if !isTargeted(autoConf.AutoGetTargets, quotedSenderNum) {
		return
	}
	if !utils.DefaultMessageCache.TryMarkAutoGet(botID, quotedID) {
		return
	}

	chatLabel := resolveChatLabel(client, msg.Info)
	go processAutoGet(client, botID, quotedID, quotedCore, ownerJID, quotedSenderNum, chatLabel, quotedIsViewOnce)
}

func processAutoGet(
	client *whatsmeow.Client,
	botID, msgID string,
	coreMsg *waE2E.Message,
	ownerJID waTypes.JID,
	senderNum, chatLabel string,
	isViewOnce bool,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var preDownloaded []byte
	if cached, ok := utils.DefaultMessageCache.Get(botID, msgID); ok && cached != nil {
		if len(cached.MediaData) > 0 {
			preDownloaded = cached.MediaData
		}
		if !utils.HasDownloadableMedia(coreMsg) && utils.HasDownloadableMedia(cached.Message) {
			coreMsg = cached.Message
		}
	}

	voBadge := ""
	if isViewOnce {
		voBadge = " (View-Once)"
	}
	header := fmt.Sprintf("📥 *Auto-Get*%s\n*From:* %s\n*Chat:* %s", voBadge, senderNum, chatLabel)
	caption := header
	if origCaption := utils.MediaCaption(coreMsg); origCaption != "" {
		caption += "\n\n_Original Caption:_\n" + origCaption
	}

	if err := utils.ForwardPermanentMedia(ctx, client, ownerJID, coreMsg, preDownloaded, caption, header); err != nil {
		fmt.Printf("  [AUTO-GET] Failed to save media (id=%s): %v\n", msgID, err)
		// Allow a later reply with valid media keys to retry.
		utils.DefaultMessageCache.UnmarkAutoGet(botID, msgID)
	}
}
