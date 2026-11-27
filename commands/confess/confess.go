package confess

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	db "bot/databases"
	"bot/handlers"
	"bot/i18n"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

var (
	cdMu       sync.Mutex
	lastSentAt = map[string]time.Time{}
)

func init() {
	// Two-way anonymous reply relay when a user replies directly to a confession DM
	handlers.RegisterInteractiveSessionHandler(handleDirectConfessReply)

	cmds := []types.Command{
		{Name: "confess", Aliases: []string{"menfess", "fess"}, Category: "confess", Description: "Send an anonymous confession via DM. Usage: .confess <phone> | <message> [| alias]", Execute: cmdConfess},
		{Name: "balasconfess", Aliases: []string{"replyconfess", "balasfess"}, Category: "confess", Description: "Reply anonymously to a confession thread. Usage: .balasconfess <CF-XXXX> <reply>", Execute: cmdBalasConfess},
		{Name: "stopconfess", Aliases: []string{"closeconfess"}, Category: "confess", Description: "Close an active confession thread. Usage: .stopconfess <CF-XXXX>", Execute: cmdStopConfess},
		{Name: "blockconfess", Category: "confess", Description: "Anonymously block the sender of a confession thread. Usage: .blockconfess <CF-XXXX>", Execute: cmdBlockConfess},
		{Name: "confessmode", Category: "confess", Description: "Enable or disable receiving anonymous confessions. Usage: .confessmode on|off", Execute: cmdConfessMode},
		{Name: "confessinbox", Category: "confess", Description: "List your recent anonymous confession threads.", Execute: cmdConfessInbox},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func newConfessID() string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return "CF-" + strings.ToUpper(hex.EncodeToString(b[:]))
}

func cleanPhone(raw string) string {
	s := utils.NormalizeTarget(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	if strings.HasPrefix(s, "08") {
		s = "62" + s[1:]
	}
	if len(s) < 8 || s == "all" {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

func cmdConfess(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	if store == nil {
		return
	}
	raw := strings.TrimSpace(strings.Join(args, " "))
	parts := strings.Split(raw, "|")
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.usage"))
		return
	}

	targetPhone := cleanPhone(parts[0])
	if targetPhone == "" {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.invalid_num"))
		return
	}
	body := strings.TrimSpace(parts[1])
	if body == "" {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.usage"))
		return
	}
	if len(body) > 1500 {
		body = body[:1500]
	}
	alias := "Anonim"
	if i18n.ResolveLang(client, msg) == i18n.LangEN {
		alias = "Anonymous"
	}
	if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
		alias = strings.TrimSpace(parts[2])
	}

	senderPhone := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	if senderPhone == targetPhone {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.self"))
		return
	}

	// If triggered in a group and bot is admin, delete the trigger message for privacy
	if msg.Info.IsGroup {
		if _, botAdmin := handlers.IsGroupAdmin(client, msg.Info.Chat, msg.Info.Sender); botAdmin {
			revoke := client.BuildRevoke(msg.Info.Chat, msg.Info.Sender, msg.Info.ID)
			_, _ = client.SendMessage(context.Background(), msg.Info.Chat, revoke)
		}
	}

	// Cooldown check (30s)
	cdMu.Lock()
	if last, ok := lastSentAt[senderPhone]; ok && time.Since(last) < 30*time.Second {
		rem := int((30*time.Second - time.Since(last)).Seconds()) + 1
		cdMu.Unlock()
		utils.SendReply(client, utils.ResolvePrivateChatJID(client, msg), i18n.TMsg(client, msg, "confess.cooldown", rem))
		return
	}
	lastSentAt[senderPhone] = time.Now()
	cdMu.Unlock()

	// Check opt-out & anonymous blocklist
	if targetUser, err := store.GetUser(targetPhone); err == nil && targetUser != nil && targetUser.ConfessOptOut {
		utils.SendReply(client, utils.ResolvePrivateChatJID(client, msg), i18n.TMsg(client, msg, "confess.opt_out"))
		return
	}
	if store.IsConfessBlocked(targetPhone, senderPhone) {
		utils.SendReply(client, utils.ResolvePrivateChatJID(client, msg), i18n.TMsg(client, msg, "confess.blocked"))
		return
	}

	cid := newConfessID()
	targetJID := waTypes.NewJID(targetPhone, waTypes.DefaultUserServer)

	targetLang := i18n.DefaultLang
	if tu, err := store.GetUser(targetPhone); err == nil && tu != nil && (tu.Lang == i18n.LangID || tu.Lang == i18n.LangEN) {
		targetLang = tu.Lang
	}
	dmText := i18n.T(targetLang, "confess.dm_card", cid, alias, body, cid, cid, cid)
	sent, err := utils.SendReply(client, targetJID, dmText)
	if err != nil {
		utils.SendReply(client, utils.ResolvePrivateChatJID(client, msg), "❌ Failed to deliver confession DM: "+err.Error())
		return
	}

	rec := &db.ConfessionRecord{
		ID:            cid,
		SenderJID:     senderPhone,
		TargetJID:     targetPhone,
		Alias:         alias,
		Message:       body,
		Status:        "open",
		LastTargetMsg: sent.ID,
	}
	_ = store.SaveConfession(rec)

	// Confirm privately to sender (never leak in group)
	senderDM := utils.ResolvePrivateChatJID(client, msg)
	confirmResp, _ := utils.SendReply(client, senderDM, i18n.TMsg(client, msg, "confess.delivered", cid, alias))
	if confirmResp.ID != "" {
		rec.LastSenderMsg = confirmResp.ID
		_ = store.SaveConfession(rec)
	}
}

func relayConfessReply(client *whatsmeow.Client, msg *events.Message, rec *db.ConfessionRecord, replyText string) {
	store := db.GetBotStore()
	caller := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	if rec == nil || (caller != rec.TargetJID && caller != rec.SenderJID) {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.not_found", ""))
		return
	}
	if rec.Status != "open" {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.closed", rec.ID))
		return
	}

	var destPhone, senderLabel string
	isTargetReplying := caller == rec.TargetJID
	if isTargetReplying {
		destPhone = rec.SenderJID
		senderLabel = "Penerima / Recipient"
	} else {
		destPhone = rec.TargetJID
		senderLabel = rec.Alias
		if store.IsConfessBlocked(rec.TargetJID, rec.SenderJID) {
			utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.blocked"))
			return
		}
	}

	destLang := i18n.DefaultLang
	if du, err := store.GetUser(destPhone); err == nil && du != nil && (du.Lang == i18n.LangID || du.Lang == i18n.LangEN) {
		destLang = du.Lang
	}
	destJID := waTypes.NewJID(destPhone, waTypes.DefaultUserServer)
	sent, err := utils.SendReply(client, destJID, i18n.T(destLang, "confess.reply_dm", rec.ID, senderLabel, replyText, rec.ID))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Relay failed: "+err.Error())
		return
	}
	if isTargetReplying {
		rec.LastSenderMsg = sent.ID
	} else {
		rec.LastTargetMsg = sent.ID
	}
	_ = store.SaveConfession(rec)
	utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.reply_sent", rec.ID))
}

func handleDirectConfessReply(client *whatsmeow.Client, msg *events.Message) bool {
	if msg == nil || msg.Info.IsFromMe || msg.Info.IsGroup {
		return false
	}
	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || ctxInfo.GetStanzaID() == "" {
		return false
	}
	text := strings.TrimSpace(utils.GetTextFromMessage(msg.Message))
	if text == "" || strings.HasPrefix(text, ".") || strings.HasPrefix(text, "/") {
		return false
	}
	store := db.GetBotStore()
	if store == nil {
		return false
	}
	rec, err := store.GetConfessionByMsgID(ctxInfo.GetStanzaID())
	if err != nil || rec == nil {
		return false
	}
	relayConfessReply(client, msg, rec, text)
	return true
}

func cmdBalasConfess(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.SplitN(strings.TrimSpace(strings.Join(args, " ")), " ", 2)
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.balasconfess <CF-XXXX> <reply text>`")
		return
	}
	rec, _ := db.GetBotStore().GetConfession(parts[0])
	if rec == nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.not_found", parts[0]))
		return
	}
	relayConfessReply(client, msg, rec, strings.TrimSpace(parts[1]))
}

func cmdStopConfess(client *whatsmeow.Client, msg *events.Message, args []string) {
	id := strings.TrimSpace(strings.Join(args, " "))
	if id == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.stopconfess <CF-XXXX>`")
		return
	}
	store := db.GetBotStore()
	rec, _ := store.GetConfession(id)
	caller := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	if rec == nil || (caller != rec.SenderJID && caller != rec.TargetJID) {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.not_found", id))
		return
	}
	rec.Status = "closed"
	_ = store.SaveConfession(rec)
	utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.stopped", rec.ID))
}

func cmdBlockConfess(client *whatsmeow.Client, msg *events.Message, args []string) {
	id := strings.TrimSpace(strings.Join(args, " "))
	if id == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.blockconfess <CF-XXXX>`")
		return
	}
	store := db.GetBotStore()
	rec, _ := store.GetConfession(id)
	caller := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	if rec == nil || caller != rec.TargetJID {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.not_found", id))
		return
	}
	_ = store.BlockConfessSender(rec.TargetJID, rec.SenderJID)
	rec.Status = "closed"
	_ = store.SaveConfession(rec)
	utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.blocked_ok", rec.ID))
}

func cmdConfessMode(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	u, _ := store.GetUser(uid)
	arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	switch arg {
	case "on", "1", "enable":
		u.ConfessOptOut = false
	case "off", "0", "disable":
		u.ConfessOptOut = true
	default:
		u.ConfessOptOut = !u.ConfessOptOut
	}
	_ = store.SaveUser(u)
	if u.ConfessOptOut {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.mode_off"))
	} else {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "confess.mode_on"))
	}
}

func cmdConfessInbox(client *whatsmeow.Client, msg *events.Message, args []string) {
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	recs, _ := db.GetBotStore().ListUserConfessions(uid, 10)
	if len(recs) == 0 {
		utils.SendReply(client, msg.Info.Chat, "💌 Inbox confess kamu masih kosong.")
		return
	}
	var b strings.Builder
	b.WriteString("💌 *Confess Inbox*\n\n")
	for _, r := range recs {
		role := "Masuk / Received"
		if r.SenderJID == uid {
			role = "Terkirim / Sent"
		}
		preview := r.Message
		if len(preview) > 50 {
			preview = preview[:50] + "..."
		}
		fmt.Fprintf(&b, "• `#%s` [%s | %s] — *%s*: _%s_\n", r.ID, role, r.Status, r.Alias, preview)
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}
