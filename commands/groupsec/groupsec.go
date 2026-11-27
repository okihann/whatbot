package groupsec

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

var (
	linkRegex    = regexp.MustCompile(`(?i)(https?://\S+|chat\.whatsapp\.com/\S+|wa\.me/\S+|t\.me/\S+)`)
	spamMu       sync.Mutex
	spamTracker  = map[string][]time.Time{}
	kudetaMu     sync.Mutex
	demoteEvents = map[string][]time.Time{}
)

func init() {
	handlers.RegisterMessageMiddleware(groupSecurityMiddleware)
	handlers.RegisterGroupParticipantHandler(handleAntiKudetaAndBot)

	cmds := []types.Command{
		{Name: "antilink", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Block links in group. Usage: .antilink on|off|delete|warn|kick", Execute: cmdAntiLink},
		{Name: "antimedia", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Delete media sent by non-admins. Usage: .antimedia on|off", Execute: cmdToggleSec("antimedia")},
		{Name: "antibot", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Toggle AntiBot guard. Usage: .antibot on|off", Execute: cmdToggleSec("antibot")},
		{Name: "antikudeta", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Auto-demote mass-demoters in the group. Usage: .antikudeta on|off", Execute: cmdToggleSec("antikudeta")},
		{Name: "badword", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Manage group badword filter. Usage: .badword on|off|add <word>|del <word>|list", Execute: cmdBadWord},

		{Name: "warn", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Warn a member (auto-kicks at warn limit). Usage: .warn @user [reason]", Execute: cmdWarn},
		{Name: "warnings", Category: "group_security", GroupOnly: true, Description: "Check warnings for yourself or a mentioned member.", Execute: cmdWarnings},
		{Name: "listwarning", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "List all warned members in this group.", Execute: cmdListWarning},
		{Name: "resetwarn", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Reset warnings for a member. Usage: .resetwarn @user", Execute: cmdResetWarn},

		{Name: "mutemember", Category: "group_security", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Mute a member (auto-deletes their messages). Usage: .mutemember @user [minutes]", Execute: cmdMuteMember},
		{Name: "unmutemember", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Unmute a member. Usage: .unmutemember @user", Execute: cmdUnmuteMember},
		{Name: "listmute", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "List currently muted members in this group.", Execute: cmdListMute},

		{Name: "banmember", Category: "group_security", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Ban and kick a member from this group. Usage: .banmember @user", Execute: cmdBanMember(true)},
		{Name: "unbanmember", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "Unban a member in this group. Usage: .unbanmember <phone>", Execute: cmdBanMember(false)},
		{Name: "listbanmember", Category: "group_security", GroupOnly: true, AdminOnly: true, Description: "List banned members in this group.", Execute: cmdListBanMember},

		{Name: "banchat", Category: "group_security", GroupOnly: true, OwnerOnly: true, Description: "Mute bot responses in this group.", Execute: cmdBanChat(true)},
		{Name: "unbanchat", Category: "group_security", GroupOnly: true, OwnerOnly: true, Description: "Re-enable bot responses in this group.", Execute: cmdBanChat(false)},
		{Name: "banlist", Category: "group_security", OwnerOnly: true, Description: "List all banned groups.", Execute: cmdBanList},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func deleteViolatingMessage(client *whatsmeow.Client, msg *events.Message) {
	if client == nil || msg == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	revoke := client.BuildRevoke(msg.Info.Chat, msg.Info.Sender, msg.Info.ID)
	_, _ = client.SendMessage(ctx, msg.Info.Chat, revoke)
}

// groupSecurityMiddleware enforces group security rules on live messages.
// Returns true if the message violated a rule and should stop further processing.
func groupSecurityMiddleware(client *whatsmeow.Client, msg *events.Message) bool {
	if msg == nil || !msg.Info.IsGroup || msg.Info.IsFromMe {
		return false
	}
	store := db.GetBotStore()
	if store == nil {
		return false
	}
	gid := msg.Info.Chat.ToNonAD().String()
	cfg, err := store.GetGroupConfig(gid)
	if err != nil || cfg == nil {
		return false
	}

	conf, _ := config.LoadConfig()
	isOwner := handlers.IsBotOrTenantOwner(client, msg, conf)
	senderNum := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)

	// AntiVO applies to any view-once media in group if enabled
	if cfg.AntiVO && utils.IsViewOnceMessage(msg, nil) {
		core := utils.UnwrapMessage(msg.Message)
		if core != nil && utils.HasDownloadableMedia(core) {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_ = utils.ForwardPermanentMedia(ctx, client, msg.Info.Chat, core, nil, "👁️ *Anti-ViewOnce Unlocked*", "👁️ *Anti-ViewOnce Unlocked*")
			}()
		}
	}

	// AutoRespon keyword triggers
	text := strings.TrimSpace(utils.GetTextFromMessage(msg.Message))
	if text != "" && len(cfg.AutoRespon) > 0 {
		if reply, ok := cfg.AutoRespon[strings.ToLower(text)]; ok && reply != "" {
			utils.SendReply(client, msg.Info.Chat, reply)
		}
	}

	if isOwner {
		return false
	}

	// Check member mute/ban state
	if senderNum != "" {
		if m, err := store.GetGroupMember(gid, senderNum); err == nil && m != nil {
			if m.Banned {
				deleteViolatingMessage(client, msg)
				return true
			}
			if m.MutedUntil == -1 || (m.MutedUntil > 0 && time.Now().Unix() < m.MutedUntil) {
				deleteViolatingMessage(client, msg)
				return true
			}
		}
	}

	// Check if sender is admin before applying non-admin filters
	hasFilters := cfg.AntiLink || cfg.AntiMedia || cfg.AntiSpam || cfg.AntiHideTag || cfg.AntiBug || cfg.AntiSW || cfg.AntiTagSW || (cfg.BadWordEnabled && len(cfg.BadWords) > 0)
	if !hasFilters {
		return false
	}
	isAdmin, botAdmin := handlers.IsGroupAdmin(client, msg.Info.Chat, msg.Info.Sender)
	if isAdmin {
		return false
	}

	// AntiBug: block excessively huge payloads (> 15,000 runes)
	if cfg.AntiBug && len([]rune(text)) > 15000 {
		deleteViolatingMessage(client, msg)
		if botAdmin {
			_, _ = client.UpdateGroupParticipants(context.Background(), msg.Info.Chat, []waTypes.JID{msg.Info.Sender}, whatsmeow.ParticipantChangeRemove)
		}
		utils.SendReply(client, msg.Info.Chat, "🛡️ *Anti-Bug:* Blocked oversized payload.")
		return true
	}

	// AntiSpam: > 6 messages in 6 seconds
	if cfg.AntiSpam && senderNum != "" {
		key := gid + ":" + senderNum
		now := time.Now()
		spamMu.Lock()
		hist := spamTracker[key]
		filtered := hist[:0]
		for _, ts := range hist {
			if now.Sub(ts) < 6*time.Second {
				filtered = append(filtered, ts)
			}
		}
		filtered = append(filtered, now)
		spamTracker[key] = filtered
		spamCount := len(filtered)
		spamMu.Unlock()
		if spamCount > 6 {
			deleteViolatingMessage(client, msg)
			if spamCount == 7 {
				utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚠️ *Anti-Spam:* Please slow down, `%s`!", senderNum))
			}
			return true
		}
	}

	// AntiHideTag: > 10 mentions by a non-admin
	if cfg.AntiHideTag {
		if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil && len(ctxInfo.GetMentionedJID()) >= 10 {
			deleteViolatingMessage(client, msg)
			utils.SendReply(client, msg.Info.Chat, "🛡️ *Anti-HideTag:* Mass mentions by non-admins are blocked.")
			return true
		}
	}

	// AntiSW / AntiTagSW
	if cfg.AntiTagSW && msg.Message.GetGroupStatusMentionMessage() != nil {
		deleteViolatingMessage(client, msg)
		utils.SendReply(client, msg.Info.Chat, "🛡️ *Anti-TagSW:* Group status mention tags are disabled.")
		return true
	}
	if cfg.AntiSW {
		if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil && ctxInfo.GetRemoteJID() == "status@broadcast" {
			deleteViolatingMessage(client, msg)
			return true
		}
	}

	// AntiMedia
	if cfg.AntiMedia && utils.HasDownloadableMedia(utils.UnwrapMessage(msg.Message)) {
		deleteViolatingMessage(client, msg)
		return true
	}

	// AntiLink
	if cfg.AntiLink && text != "" && linkRegex.MatchString(text) {
		deleteViolatingMessage(client, msg)
		switch cfg.AntiLinkAction {
		case "kick":
			if botAdmin {
				_, _ = client.UpdateGroupParticipants(context.Background(), msg.Info.Chat, []waTypes.JID{msg.Info.Sender}, whatsmeow.ParticipantChangeRemove)
			}
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🚫 *Anti-Link:* Removed `%s` for posting a link.", senderNum))
		case "warn":
			issueWarn(client, msg.Info.Chat, msg.Info.Sender, senderNum, "Anti-Link violation", cfg, botAdmin)
		default:
			utils.SendReply(client, msg.Info.Chat, "🚫 *Anti-Link:* Links are not allowed in this group.")
		}
		return true
	}

	// BadWord filter
	if cfg.BadWordEnabled && text != "" && len(cfg.BadWords) > 0 {
		lower := strings.ToLower(text)
		for _, w := range cfg.BadWords {
			if w != "" && strings.Contains(lower, strings.ToLower(w)) {
				deleteViolatingMessage(client, msg)
				utils.SendReply(client, msg.Info.Chat, "🤬 *BadWord Filter:* Message removed.")
				return true
			}
		}
	}

	return false
}

func handleAntiKudetaAndBot(client *whatsmeow.Client, evt *events.GroupInfo) {
	store := db.GetBotStore()
	if store == nil || client == nil || evt == nil {
		return
	}
	gid := evt.JID.ToNonAD().String()
	cfg, _ := store.GetGroupConfig(gid)
	if cfg == nil {
		return
	}

	// AntiKudeta: if someone demotes >= 2 admins within 30s, demote the actor
	if cfg.AntiKudeta && len(evt.Demote) > 0 && evt.Sender != nil {
		actor := evt.Sender.ToNonAD()
		if client.Store.ID != nil && actor.User == client.Store.ID.ToNonAD().User {
			return
		}
		key := gid + ":" + actor.User
		now := time.Now()
		kudetaMu.Lock()
		hist := demoteEvents[key]
		kept := hist[:0]
		for _, ts := range hist {
			if now.Sub(ts) < 30*time.Second {
				kept = append(kept, ts)
			}
		}
		for range evt.Demote {
			kept = append(kept, now)
		}
		demoteEvents[key] = kept
		count := len(kept)
		kudetaMu.Unlock()

		if count >= 2 {
			_, _ = client.UpdateGroupParticipants(context.Background(), evt.JID, []waTypes.JID{actor}, whatsmeow.ParticipantChangeDemote)
			utils.SendReply(client, evt.JID, fmt.Sprintf("🚨 *Anti-Kudeta Triggered!* Demoted `%s` for mass-demoting admins.", actor.User))
		}
	}
}

func issueWarn(client *whatsmeow.Client, groupJID, targetJID waTypes.JID, targetNum, reason string, cfg *db.GroupConfig, botAdmin bool) {
	store := db.GetBotStore()
	gid := groupJID.ToNonAD().String()
	m, _ := store.GetGroupMember(gid, targetNum)
	m.Warns++
	_ = store.SaveGroupMember(m)
	limit := cfg.WarnLimit
	if limit <= 0 {
		limit = 3
	}
	if m.Warns >= limit {
		m.Warns = 0
		_ = store.SaveGroupMember(m)
		if botAdmin {
			_, _ = client.UpdateGroupParticipants(context.Background(), groupJID, []waTypes.JID{targetJID}, whatsmeow.ParticipantChangeRemove)
			utils.SendReply(client, groupJID, fmt.Sprintf("⛔ `%s` reached `%d/%d` warnings (%s) and was removed!", targetNum, limit, limit, reason))
		} else {
			utils.SendReply(client, groupJID, fmt.Sprintf("⚠️ `%s` reached `%d/%d` warnings (%s). Make bot admin to auto-kick.", targetNum, limit, limit, reason))
		}
		return
	}
	utils.SendReply(client, groupJID, fmt.Sprintf("⚠️ Warning `%d/%d` for `%s` — Reason: _%s_", m.Warns, limit, targetNum, reason))
}

func cmdAntiLink(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	switch arg {
	case "on", "enable", "1":
		cfg.AntiLink = true
	case "off", "disable", "0":
		cfg.AntiLink = false
	case "delete", "warn", "kick":
		cfg.AntiLink = true
		cfg.AntiLinkAction = arg
	default:
		cfg.AntiLink = !cfg.AntiLink
	}
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔗 AntiLink is now *%v* (action: `%s`).", cfg.AntiLink, cfg.AntiLinkAction))
}

func cmdToggleSec(field string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		store := db.GetBotStore()
		cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
		arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
		apply := func(cur bool) bool {
			if arg == "on" || arg == "1" {
				return true
			}
			if arg == "off" || arg == "0" {
				return false
			}
			return !cur
		}
		var state bool
		switch field {
		case "antimedia":
			cfg.AntiMedia = apply(cfg.AntiMedia)
			state = cfg.AntiMedia
		case "antibot":
			cfg.AntiBot = apply(cfg.AntiBot)
			state = cfg.AntiBot
		case "antikudeta":
			cfg.AntiKudeta = apply(cfg.AntiKudeta)
			state = cfg.AntiKudeta
		}
		_ = store.SaveGroupConfig(cfg)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔒 `%s` is now *%v*.", field, state))
	}
}

func cmdBadWord(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	parts := strings.SplitN(strings.TrimSpace(strings.Join(args, " ")), " ", 2)
	sub := strings.ToLower(parts[0])
	switch sub {
	case "on":
		cfg.BadWordEnabled = true
	case "off":
		cfg.BadWordEnabled = false
	case "add":
		if len(parts) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.badword add <word>`")
			return
		}
		word := strings.ToLower(strings.TrimSpace(parts[1]))
		cfg.BadWords = append(cfg.BadWords, word)
		cfg.BadWordEnabled = true
	case "del", "remove":
		if len(parts) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.badword del <word>`")
			return
		}
		word := strings.ToLower(strings.TrimSpace(parts[1]))
		kept := cfg.BadWords[:0]
		for _, w := range cfg.BadWords {
			if strings.ToLower(w) != word {
				kept = append(kept, w)
			}
		}
		cfg.BadWords = kept
	default:
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🤬 *BadWord Filter:* `%v`\nWords (%d): `%s`", cfg.BadWordEnabled, len(cfg.BadWords), strings.Join(cfg.BadWords, "`, `")))
		return
	}
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ BadWord updated (enabled=`%v`, total `%d` words).", cfg.BadWordEnabled, len(cfg.BadWords)))
}

func cmdWarn(client *whatsmeow.Client, msg *events.Message, args []string) {
	targets := utils.ExtractTargetJIDs(msg, args)
	if len(targets) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.warn @user [reason]` or reply to a message.")
		return
	}
	reason := strings.TrimSpace(strings.Join(args, " "))
	if reason == "" {
		reason = "Rule violation"
	}
	cfg, _ := db.GetBotStore().GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	_, botAdmin := handlers.IsGroupAdmin(client, msg.Info.Chat, msg.Info.Sender)
	t := targets[0]
	issueWarn(client, msg.Info.Chat, t, utils.ResolvePhoneNumber(client, t, waTypes.EmptyJID), reason, cfg, botAdmin)
}

func cmdWarnings(client *whatsmeow.Client, msg *events.Message, args []string) {
	targets := utils.ExtractTargetJIDs(msg, args)
	num := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	if len(targets) > 0 {
		num = utils.ResolvePhoneNumber(client, targets[0], waTypes.EmptyJID)
	}
	gid := msg.Info.Chat.ToNonAD().String()
	m, _ := db.GetBotStore().GetGroupMember(gid, num)
	cfg, _ := db.GetBotStore().GetGroupConfig(gid)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚠️ Warnings for `%s`: *%d/%d*", num, m.Warns, cfg.WarnLimit))
}

func cmdListWarning(client *whatsmeow.Client, msg *events.Message, args []string) {
	members, _ := db.GetBotStore().ListGroupMembers(msg.Info.Chat.ToNonAD().String())
	var lines []string
	for _, m := range members {
		if m.Warns > 0 {
			lines = append(lines, fmt.Sprintf("• `%s` — %d warn(s)", m.UserJID, m.Warns))
		}
	}
	if len(lines) == 0 {
		utils.SendReply(client, msg.Info.Chat, "✅ No members currently have warnings.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "⚠️ *Warned Members:*\n"+strings.Join(lines, "\n"))
}

func cmdResetWarn(client *whatsmeow.Client, msg *events.Message, args []string) {
	targets := utils.ExtractTargetJIDs(msg, args)
	if len(targets) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.resetwarn @user`")
		return
	}
	num := utils.ResolvePhoneNumber(client, targets[0], waTypes.EmptyJID)
	gid := msg.Info.Chat.ToNonAD().String()
	m, _ := db.GetBotStore().GetGroupMember(gid, num)
	m.Warns = 0
	_ = db.GetBotStore().SaveGroupMember(m)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Warnings cleared for `%s`.", num))
}

func cmdMuteMember(client *whatsmeow.Client, msg *events.Message, args []string) {
	targets := utils.ExtractTargetJIDs(msg, args)
	if len(targets) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.mutemember @user [minutes]`")
		return
	}
	minutes := 0
	for _, tok := range strings.Fields(strings.Join(args, " ")) {
		if n, err := strconv.Atoi(tok); err == nil && n > 0 && n <= 43200 {
			minutes = n
		}
	}
	num := utils.ResolvePhoneNumber(client, targets[0], waTypes.EmptyJID)
	gid := msg.Info.Chat.ToNonAD().String()
	m, _ := db.GetBotStore().GetGroupMember(gid, num)
	if minutes > 0 {
		m.MutedUntil = time.Now().Add(time.Duration(minutes) * time.Minute).Unix()
	} else {
		m.MutedUntil = -1
	}
	_ = db.GetBotStore().SaveGroupMember(m)
	if minutes > 0 {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔇 Muted `%s` for `%d` minute(s).", num, minutes))
	} else {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔇 Muted `%s` indefinitely.", num))
	}
}

func cmdUnmuteMember(client *whatsmeow.Client, msg *events.Message, args []string) {
	targets := utils.ExtractTargetJIDs(msg, args)
	if len(targets) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.unmutemember @user`")
		return
	}
	num := utils.ResolvePhoneNumber(client, targets[0], waTypes.EmptyJID)
	gid := msg.Info.Chat.ToNonAD().String()
	m, _ := db.GetBotStore().GetGroupMember(gid, num)
	m.MutedUntil = 0
	_ = db.GetBotStore().SaveGroupMember(m)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔊 Unmuted `%s`.", num))
}

func cmdListMute(client *whatsmeow.Client, msg *events.Message, args []string) {
	members, _ := db.GetBotStore().ListGroupMembers(msg.Info.Chat.ToNonAD().String())
	now := time.Now().Unix()
	var lines []string
	for _, m := range members {
		if m.MutedUntil == -1 {
			lines = append(lines, fmt.Sprintf("• `%s` (permanent)", m.UserJID))
		} else if m.MutedUntil > now {
			lines = append(lines, fmt.Sprintf("• `%s` (%s left)", m.UserJID, time.Until(time.Unix(m.MutedUntil, 0)).Round(time.Minute)))
		}
	}
	if len(lines) == 0 {
		utils.SendReply(client, msg.Info.Chat, "✅ No muted members.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "🔇 *Muted Members:*\n"+strings.Join(lines, "\n"))
}

func cmdBanMember(ban bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		targets := utils.ExtractTargetJIDs(msg, args)
		if len(targets) == 0 {
			utils.SendReply(client, msg.Info.Chat, "Usage: mention/reply user or pass phone number.")
			return
		}
		gid := msg.Info.Chat.ToNonAD().String()
		t := targets[0]
		num := utils.ResolvePhoneNumber(client, t, waTypes.EmptyJID)
		m, _ := db.GetBotStore().GetGroupMember(gid, num)
		m.Banned = ban
		_ = db.GetBotStore().SaveGroupMember(m)
		if ban {
			_, _ = client.UpdateGroupParticipants(context.Background(), msg.Info.Chat, []waTypes.JID{t}, whatsmeow.ParticipantChangeRemove)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🚫 Banned & removed `%s`.", num))
		} else {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Unbanned `%s`.", num))
		}
	}
}

func cmdListBanMember(client *whatsmeow.Client, msg *events.Message, args []string) {
	members, _ := db.GetBotStore().ListGroupMembers(msg.Info.Chat.ToNonAD().String())
	var lines []string
	for _, m := range members {
		if m.Banned {
			lines = append(lines, "• `"+m.UserJID+"`")
		}
	}
	if len(lines) == 0 {
		utils.SendReply(client, msg.Info.Chat, "✅ No banned members in this group.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "🚫 *Banned Members:*\n"+strings.Join(lines, "\n"))
}

func cmdBanChat(ban bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		store := db.GetBotStore()
		cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
		cfg.Banned = ban
		_ = store.SaveGroupConfig(cfg)
		if ban {
			utils.SendReply(client, msg.Info.Chat, "🚫 This group has been banned from using the bot.")
		} else {
			utils.SendReply(client, msg.Info.Chat, "✅ This group has been unbanned.")
		}
	}
}

func cmdBanList(client *whatsmeow.Client, msg *events.Message, args []string) {
	cfgs, _ := db.GetBotStore().ListAllGroupConfigs()
	var lines []string
	for _, c := range cfgs {
		if c.Banned {
			lines = append(lines, "• `"+c.GroupJID+"`")
		}
	}
	if len(lines) == 0 {
		utils.SendReply(client, msg.Info.Chat, "✅ No banned groups.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "🚫 *Banned Groups:*\n"+strings.Join(lines, "\n"))
}
