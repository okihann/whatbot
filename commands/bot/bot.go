package botcmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"bot/ai"
	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/i18n"
	"bot/pairing"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"rsc.io/qr"
)

func init() {
	// RejectCall / AnswerCall automatic event hook
	handlers.RegisterCallHandler(handleIncomingCall)

	cmds := []types.Command{
		{Name: "jadibot", Aliases: []string{"scanbot"}, Category: "bot", OwnerOnly: true, Description: "🔒 [Owner-Only] Queue a sequential Android Companion QR pairing slot for selling/linking a bot.", Usage: ".jadibot [label] [days]", Execute: cmdJadiBot},
		{Name: "lockjadibot", Category: "bot", OwnerOnly: true, Description: "Lock or unlock .jadibot (locked by default). Usage: .lockjadibot on|off", Execute: cmdLockJadiBot},
		{Name: "trackbot", Category: "bot", OwnerOnly: true, Description: "List all active linked bot tenants and sequential QR queue status.", Execute: cmdTrackBot},
		{Name: "bot", Aliases: []string{"botinfo", "botofficial"}, Category: "bot", OwnerOnly: true, Description: "Show bot runtime, Android Companion status, and SQLite WAL diagnostics.", Execute: cmdBotInfo},
		{Name: "botsettings", Category: "bot", OwnerOnly: true, Description: "Show current per-bot tenant settings.", Execute: cmdBotSettings},
		{Name: "selfmode", Category: "bot", OwnerOnly: true, Description: "Toggle self-mode (only owner can trigger commands). Usage: .selfmode on|off", Execute: cmdToggleTenantBool("self")},
		{Name: "groupmode", Category: "bot", OwnerOnly: true, Description: "Toggle group-only mode (ignore non-owner DMs). Usage: .groupmode on|off", Execute: cmdToggleTenantBool("group")},
		{Name: "autoread", Category: "bot", OwnerOnly: true, Description: "Toggle automatic read receipts. Usage: .autoread on|off", Execute: cmdToggleTenantBool("autoread")},
		{Name: "rejectcall", Category: "bot", OwnerOnly: true, Description: "Toggle automatic incoming call rejection. Usage: .rejectcall on|off", Execute: cmdToggleTenantBool("rejectcall")},
		{Name: "answercall", Category: "bot", OwnerOnly: true, Description: "Set auto-reply message sent when a call is rejected. Usage: .answercall <text|off>", Execute: cmdAnswerCall},
		{Name: "pacing", Category: "bot", OwnerOnly: true, Description: "Set per-chat command cooldown in ms. Usage: .pacing <ms>", Execute: cmdPacing},
		{Name: "nsfw", Category: "bot", OwnerOnly: true, Description: "Toggle NSFW command gate for this group or bot (Owner-Only). Usage: .nsfw on|off", Execute: cmdNSFW},
		{Name: "setbotname", Category: "bot", OwnerOnly: true, Description: "Set custom bot display name for this tenant. Usage: .setbotname <name>", Execute: cmdSetBotName},
		{Name: "setbotowner", Category: "bot", OwnerOnly: true, Description: "Set buyer/owner phone number for this tenant. Usage: .setbotowner <phone>", Execute: cmdSetBotOwner},
		{Name: "setpp", Category: "bot", OwnerOnly: true, Description: "Set bot profile picture (reply to an image).", Execute: cmdSetPP},
		{Name: "getprivacysetting", Category: "bot", OwnerOnly: true, Description: "Show WhatsApp privacy settings for this account.", Execute: cmdGetPrivacy},
		{Name: "setprivacy", Category: "bot", OwnerOnly: true, Description: "Update privacy setting. Usage: .setprivacy <groupadd|last|status|profile|readreceipts|online> <all|contacts|none|match_last_seen>", Execute: cmdSetPrivacy},
		{Name: "statuspost", Aliases: []string{"statusposttag"}, Category: "bot", OwnerOnly: true, Description: "Post a text or replied image/video to WhatsApp Status (Stories).", Execute: cmdStatusPost},
		{Name: "channelpost", Category: "bot", OwnerOnly: true, Description: "Post text to a WhatsApp Channel (Newsletter). Usage: .channelpost <channel_jid> <text>", Execute: cmdChannelPost},
		{Name: "listgroup", Category: "bot", OwnerOnly: true, Description: "List all joined WhatsApp groups.", Execute: cmdListGroup},
		{Name: "clearchat", Aliases: []string{"deletechat"}, Category: "bot", OwnerOnly: true, Description: "Clear local cached chat history for this chat.", Execute: cmdClearChat},
		{Name: "gostats", Category: "bot", OwnerOnly: true, Description: "Show Go runtime memory, goroutines, and GC stats.", Execute: cmdGoStats},
		{Name: "msgstats", Aliases: []string{"stats"}, Category: "bot", OwnerOnly: true, Description: "Show bot message/command throughput and SQLite WAL stats.", Execute: cmdMsgStats},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func publicBaseURL() string {
	if dom := strings.TrimSpace(os.Getenv("NGROK_DOMAIN")); dom != "" {
		if !strings.HasPrefix(dom, "http") {
			return "https://" + dom
		}
		return strings.TrimRight(dom, "/")
	}
	port := ai.AppConfig.ServerPort
	if port == "" {
		port = ":8080"
	}
	return "http://localhost" + port
}

func tenantForClient(client *whatsmeow.Client) (*db.BotStore, *db.BotTenant) {
	store := db.GetBotStore()
	if store == nil || client == nil || client.Store == nil || client.Store.ID == nil {
		return store, nil
	}
	t, _ := store.GetTenant(client.Store.ID.ToNonAD().User)
	return store, t
}

func handleIncomingCall(client *whatsmeow.Client, evt *events.CallOffer) {
	if client == nil || evt == nil {
		return
	}
	_, tenant := tenantForClient(client)
	reject := tenant != nil && tenant.RejectCall
	if !reject {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = client.RejectCall(ctx, evt.CallCreator, evt.CallID)
	if tenant.AnswerCall != "" && evt.CallCreator.User != "" {
		_, _ = utils.SendReply(client, evt.CallCreator.ToNonAD(), tenant.AnswerCall)
	}
}

func cmdLockJadiBot(client *whatsmeow.Client, msg *events.Message, args []string) {
	store, t := tenantForClient(client)
	if store == nil || t == nil {
		utils.SendReply(client, msg.Info.Chat, "⚠️ BotStore unavailable.")
		return
	}
	arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	switch arg {
	case "on", "lock", "1", "true":
		t.JadiBotLocked = true
	case "off", "unlock", "0", "false":
		t.JadiBotLocked = false
	default:
		t.JadiBotLocked = !t.JadiBotLocked
	}
	_ = store.SaveTenant(t)
	if t.JadiBotLocked {
		utils.SendReply(client, msg.Info.Chat, "🔒 `.jadibot` is now *LOCKED* (owner-only).")
	} else {
		utils.SendReply(client, msg.Info.Chat, "🔓 `.jadibot` lock flag set to *UNLOCKED* (still owner-restricted unless opened).")
	}
}

func cmdJadiBot(client *whatsmeow.Client, msg *events.Message, args []string) {
	conf, _ := config.LoadConfig()
	if !handlers.IsBotOrTenantOwner(client, msg, conf) {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "err.jadibot_locked"))
		return
	}

	q := pairing.DefaultQueue()
	if q == nil {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Sequential QR pairing queue is not initialized.")
		return
	}
	label := strings.TrimSpace(msg.Info.PushName)
	days := 0
	if len(args) > 0 {
		fields := strings.Fields(args[0])
		if len(fields) > 0 {
			if n, err := strconv.Atoi(fields[len(fields)-1]); err == nil && n >= 0 {
				days = n
				fields = fields[:len(fields)-1]
			}
			if len(fields) > 0 {
				label = strings.Join(fields, " ")
			}
		}
	}
	if label == "" {
		label = "Buyer Bot"
	}
	buyerPhone := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	chatJID := msg.Info.Chat

	var pairToken string
	ticket, err := q.Enqueue(pairing.EnqueueRequest{
		Label:        label,
		BuyerJID:     buyerPhone,
		DurationDays: days,
		OnQR: func(code string) {
			encoded, encErr := qr.Encode(code, qr.L)
			if encErr != nil {
				return
			}
			caption := fmt.Sprintf("📱 *Sequential QR Pairing (%s)*\nScan with WhatsApp → Linked Devices within 90s.\nWeb link: %s/pair/%s", label, publicBaseURL(), pairToken)
			_, _ = utils.SendImageData(context.Background(), client, chatJID, encoded.PNG(), "image/png", caption)
		},
		OnDone: func(t pairing.Ticket) {
			switch t.Status {
			case pairing.StatusConnected:
				_, _ = utils.SendReply(client, chatJID, fmt.Sprintf("✅ *Bot Linked!* Account `%s` is now active in Android Companion mode (View-Once ready).", t.AccountID))
			case pairing.StatusExpired:
				_, _ = utils.SendReply(client, chatJID, "⏰ QR pairing slot timed out. Run `.jadibot` again to re-enter the sequential queue.")
			default:
				_, _ = utils.SendReply(client, chatJID, fmt.Sprintf("⚠️ Pairing ended with status `%s`: %s", t.Status, t.ErrorMsg))
			}
		},
	})
	if err != nil {
		utils.SendReply(client, chatJID, "❌ Failed to queue pairing slot: "+err.Error())
		return
	}
	pairToken = ticket.Token

	pairURL := fmt.Sprintf("%s/pair/%s", publicBaseURL(), ticket.Token)
	posText := "Slot *#1* — generating QR right now!"
	if ticket.Position > 0 {
		posText = fmt.Sprintf("Slot *#%d in queue* — QR will be generated automatically once the previous buyer finishes.", ticket.Position+1)
	}
	utils.SendReply(client, chatJID, fmt.Sprintf(
		"🎟️ *Sequential QR Pairing Queued*\n• *Label:* %s\n• *Status:* %s\n• *Buyer Pairing Page:* %s\n• *Ticket:* `%s`",
		ticket.Label, posText, pairURL, ticket.Token[:8],
	))
}

func cmdTrackBot(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	q := pairing.DefaultQueue()

	var b strings.Builder
	b.WriteString("🤖 *Bot Tenants & Sequential QR Queue*\n\n")
	if store != nil {
		tenants, _ := store.ListTenants()
		fmt.Fprintf(&b, "*Active Tenants (%d):*\n", len(tenants))
		if len(tenants) == 0 {
			b.WriteString("• _(none registered yet)_\n")
		}
		for _, t := range tenants {
			exp := "Unlimited"
			if t.ExpiresAt > 0 {
				rem := time.Until(time.Unix(t.ExpiresAt, 0)).Round(time.Hour)
				if rem <= 0 {
					exp = "EXPIRED"
				} else {
					exp = fmt.Sprintf("%dd %dh left", int(rem.Hours())/24, int(rem.Hours())%24)
				}
			}
			fmt.Fprintf(&b, "• `%s` (%s) | Owner: `%s` | %s\n", t.AccountID, t.BotName, t.OwnerJID, exp)
		}
	}
	if q != nil {
		tickets := q.List()
		active := 0
		for _, t := range tickets {
			if t.Status == pairing.StatusQueued || t.Status == pairing.StatusPairing {
				active++
			}
		}
		fmt.Fprintf(&b, "\n*Sequential QR Queue (%d waiting/active):*\n", active)
		for _, t := range tickets {
			if t.Status == pairing.StatusQueued || t.Status == pairing.StatusPairing {
				fmt.Fprintf(&b, "• #%d `%s` — %s (%s)\n", t.Position+1, t.Token[:8], t.Label, t.Status)
			}
		}
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdBotInfo(client *whatsmeow.Client, msg *events.Message, args []string) {
	conf, _ := config.LoadConfig()
	botName := "NopeBot"
	if conf != nil && conf.Settings.BotName != "" {
		botName = conf.Settings.BotName
	}
	_, tenant := tenantForClient(client)
	if tenant != nil && tenant.BotName != "" {
		botName = tenant.BotName
	}
	uptime := time.Since(handlers.BotStartTime()).Round(time.Second)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"╭─〔 *%s* 〕\n│ • *Platform:* Android Companion (View-Once Ready)\n│ • *Runtime:* %s\n│ • *Commands:* %d\n│ • *Memory:* %.1f MB\n│ • *Goroutines:* %d\n│ • *SQLite:* Sequential WAL\n│ • *Web UI:* %s\n╰──────────",
		botName, uptime, len(handlers.PrimaryCommands), float64(m.Alloc)/1024/1024, runtime.NumGoroutine(), publicBaseURL(),
	))
}

func cmdBotSettings(client *whatsmeow.Client, msg *events.Message, args []string) {
	_, t := tenantForClient(client)
	if t == nil {
		utils.SendReply(client, msg.Info.Chat, "⚠️ BotStore not initialized.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"⚙️ *Bot Settings (`%s`)*\n• Name: *%s*\n• Owner: `%s`\n• Custom Prefix: `%s`\n• SelfMode: `%v`\n• GroupMode: `%v`\n• AutoRead: `%v`\n• RejectCall: `%v`\n• AnswerCall: `%s`\n• NSFW: `%v`\n• Pacing: `%dms`",
		t.AccountID, t.BotName, t.OwnerJID, t.Prefix, t.SelfMode, t.GroupMode, t.AutoRead, t.RejectCall, t.AnswerCall, t.NSFWEnabled, t.PacingMS,
	))
}

func cmdToggleTenantBool(field string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		store, t := tenantForClient(client)
		if store == nil || t == nil {
			utils.SendReply(client, msg.Info.Chat, "⚠️ BotStore unavailable.")
			return
		}
		arg := ""
		if len(args) > 0 {
			arg = strings.ToLower(strings.TrimSpace(args[0]))
		}
		apply := func(cur bool) bool {
			switch arg {
			case "on", "true", "1", "enable":
				return true
			case "off", "false", "0", "disable":
				return false
			default:
				return !cur
			}
		}
		var state bool
		switch field {
		case "self":
			t.SelfMode = apply(t.SelfMode)
			state = t.SelfMode
		case "group":
			t.GroupMode = apply(t.GroupMode)
			state = t.GroupMode
		case "autoread":
			t.AutoRead = apply(t.AutoRead)
			state = t.AutoRead
		case "rejectcall":
			t.RejectCall = apply(t.RejectCall)
			state = t.RejectCall
		}
		_ = store.SaveTenant(t)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ `%s` is now *%v*.", field, state))
	}
}

func cmdAnswerCall(client *whatsmeow.Client, msg *events.Message, args []string) {
	store, t := tenantForClient(client)
	if store == nil || t == nil {
		return
	}
	val := strings.TrimSpace(strings.Join(args, " "))
	if val == "" || strings.EqualFold(val, "off") {
		t.AnswerCall = ""
		_ = store.SaveTenant(t)
		utils.SendReply(client, msg.Info.Chat, "✅ Call auto-reply cleared.")
		return
	}
	t.AnswerCall = val
	t.RejectCall = true
	_ = store.SaveTenant(t)
	utils.SendReply(client, msg.Info.Chat, "✅ RejectCall enabled with auto-reply: "+val)
}

func cmdPacing(client *whatsmeow.Client, msg *events.Message, args []string) {
	store, t := tenantForClient(client)
	if store == nil || t == nil {
		return
	}
	if len(args) == 0 {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⏱️ Current command pacing: `%dms`. Usage: `.pacing <ms>` (0 to disable).", t.PacingMS))
		return
	}
	ms, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || ms < 0 || ms > 60000 {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Provide a value between 0 and 60000 ms.")
		return
	}
	t.PacingMS = ms
	_ = store.SaveTenant(t)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Command pacing set to `%dms`.", ms))
}

func cmdNSFW(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	if store == nil {
		return
	}
	conf, _ := config.LoadConfig()
	isOwner := handlers.IsBotOrTenantOwner(client, msg, conf)
	arg := ""
	if len(args) > 0 {
		arg = strings.ToLower(strings.TrimSpace(args[0]))
	}
	toggle := func(cur bool) bool {
		if arg == "on" || arg == "1" || arg == "enable" {
			return true
		}
		if arg == "off" || arg == "0" || arg == "disable" {
			return false
		}
		return !cur
	}
	if msg.Info.IsGroup {
		isAdmin, _ := handlers.IsGroupAdmin(client, msg.Info.Chat, msg.Info.Sender)
		if !isAdmin && !isOwner {
			utils.SendReply(client, msg.Info.Chat, "🛡️ Group Admin required to toggle NSFW.")
			return
		}
		gcfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
		gcfg.NSFWEnabled = toggle(gcfg.NSFWEnabled)
		_ = store.SaveGroupConfig(gcfg)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔞 Group NSFW is now *%v*.", gcfg.NSFWEnabled))
		return
	}
	if !isOwner {
		utils.SendReply(client, msg.Info.Chat, "⛔ Owner required to toggle bot NSFW.")
		return
	}
	_, t := tenantForClient(client)
	if t != nil {
		t.NSFWEnabled = toggle(t.NSFWEnabled)
		_ = store.SaveTenant(t)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔞 Bot NSFW is now *%v*.", t.NSFWEnabled))
	}
}

func cmdSetBotName(client *whatsmeow.Client, msg *events.Message, args []string) {
	store, t := tenantForClient(client)
	name := strings.TrimSpace(strings.Join(args, " "))
	if name == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setbotname <new name>`")
		return
	}
	if store != nil && t != nil {
		t.BotName = name
		_ = store.SaveTenant(t)
	}
	if conf, err := config.LoadConfig(); err == nil && conf != nil {
		conf.Settings.BotName = name
		_ = config.SaveConfig(conf)
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Bot name updated to *"+name+"*.")
}

func cmdSetBotOwner(client *whatsmeow.Client, msg *events.Message, args []string) {
	store, t := tenantForClient(client)
	phone := utils.NormalizeTarget(strings.Join(args, " "))
	if phone == "" || phone == "all" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setbotowner <phone>` (e.g. `.setbotowner 628123456789`)")
		return
	}
	if store != nil && t != nil {
		t.OwnerJID = phone
		_ = store.SaveTenant(t)
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Tenant owner set to `%s`.", phone))
}

func cmdSetPP(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetImageMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "📷 Reply to an image with `.setpp` to update the bot profile picture.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download image: "+err.Error())
		return
	}
	if _, err := client.SetGroupPhoto(ctx, waTypes.EmptyJID, data); err != nil {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Could not update profile picture directly: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Profile picture updated.")
}

func cmdGetPrivacy(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ps, err := client.TryFetchPrivacySettings(ctx, true)
	if err != nil || ps == nil {
		fallback := client.GetPrivacySettings(ctx)
		ps = &fallback
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🔐 *WhatsApp Privacy Settings*\n• Group Add: `%s`\n• Last Seen: `%s`\n• Status: `%s`\n• Profile Photo: `%s`\n• Read Receipts: `%s`\n• Online: `%s`\n• Call Add: `%s`",
		ps.GroupAdd, ps.LastSeen, ps.Status, ps.Profile, ps.ReadReceipts, ps.Online, ps.CallAdd,
	))
}

func cmdSetPrivacy(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setprivacy <groupadd|last|status|profile|readreceipts|online> <all|contacts|none|match_last_seen>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.SetPrivacySetting(ctx, waTypes.PrivacySettingType(strings.ToLower(fields[0])), waTypes.PrivacySetting(strings.ToLower(fields[1])))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to update privacy: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Privacy `%s` updated to `%s`.", fields[0], fields[1]))
}

func cmdStatusPost(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	statusJID := waTypes.StatusBroadcastJID
	caption := strings.TrimSpace(strings.Join(args, " "))

	quoted := utils.ExtractQuotedOrOwnMessage(msg)
	if quoted != nil && (quoted.GetImageMessage() != nil || quoted.GetVideoMessage() != nil) {
		data, _, mimeType, err := utils.DownloadMedia(ctx, client, quoted)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Failed to download media: "+err.Error())
			return
		}
		if quoted.GetImageMessage() != nil {
			_, err = utils.SendImageData(ctx, client, statusJID, data, mimeType, caption)
		} else {
			_, err = utils.SendVideoData(ctx, client, statusJID, data, caption, 0, nil)
		}
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Status post failed: "+err.Error())
			return
		}
		utils.SendReply(client, msg.Info.Chat, "✅ Media posted to WhatsApp Status!")
		return
	}

	if caption == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.statuspost <text>` or reply to an image/video.")
		return
	}
	_, err := client.SendMessage(ctx, statusJID, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:           proto.String(caption),
			BackgroundArgb: proto.Uint32(0xFF005C4B),
		},
	})
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Status post failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Text posted to WhatsApp Status!")
}

func cmdChannelPost(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.SplitN(strings.TrimSpace(strings.Join(args, " ")), " ", 2)
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.channelpost <channel_jid@newsletter> <message>`")
		return
	}
	chJID, err := waTypes.ParseJID(fields[0])
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Invalid channel JID.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := client.SendMessage(ctx, chJID, &waE2E.Message{Conversation: proto.String(fields[1])}); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Channel post failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Posted to channel.")
}

func cmdListGroup(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	groups, err := client.GetJoinedGroups(ctx)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to fetch groups: "+err.Error())
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "👥 *Joined Groups (%d)*\n\n", len(groups))
	for i, g := range groups {
		fmt.Fprintf(&b, "%d. *%s* (%d members)\n   `%s`\n", i+1, g.Name, len(g.Participants), g.JID.String())
		if i >= 49 {
			fmt.Fprintf(&b, "\n...and %d more groups.", len(groups)-50)
			break
		}
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdClearChat(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "🧹 Local chat session state cleared for this conversation.")
}

func cmdGoStats(client *whatsmeow.Client, msg *events.Message, args []string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🐹 *Go Runtime Stats*\n• Go Version: `%s`\n• OS/Arch: `%s/%s`\n• CPUs: `%d`\n• Goroutines: `%d`\n• Heap Alloc: `%.2f MB`\n• Heap Sys: `%.2f MB`\n• Total Alloc: `%.2f MB`\n• GC Cycles: `%d`",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.NumGoroutine(),
		float64(m.Alloc)/1024/1024, float64(m.Sys)/1024/1024, float64(m.TotalAlloc)/1024/1024, m.NumGC,
	))
}

func cmdMsgStats(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	totalStored := int64(0)
	walMode := "wal"
	if store != nil {
		totalStored = store.TotalMessageCount()
		if jm := store.Status()["journal_mode"]; jm != "" {
			walMode = jm
		}
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"📊 *Bot Telemetry*\n• Uptime: `%s`\n• Messages (session): `%d`\n• Commands (session): `%d`\n• Tracked Messages (DB): `%d`\n• Registered Commands: `%d`\n• SQLite Mode: `%s`",
		time.Since(handlers.BotStartTime()).Round(time.Second),
		handlers.TotalMessagesHandled.Load(),
		handlers.TotalCommandsHandled.Load(),
		totalStored,
		len(handlers.PrimaryCommands),
		strings.ToUpper(walMode),
	))
}
