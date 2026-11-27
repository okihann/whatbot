package general

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"bot/ai"
	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	// Middleware: AFK auto-clear / mention notification + passive XP & activity tracking
	handlers.RegisterMessageMiddleware(userActivityAndAFKMiddleware)

	cmds := []types.Command{
		// GENERAL
		{Name: "about", Category: "general", Description: "About NopeBot.", Execute: cmdAbout},
		{Name: "ping", Category: "general", Description: "Check bot response latency.", Execute: cmdPing},
		{Name: "owner", Category: "general", Description: "Show bot owner contact.", Execute: cmdOwner},
		{Name: "sc", Category: "general", Description: "Show repository/source info.", Execute: cmdSC},
		{Name: "join", Category: "general", OwnerOnly: true, Description: "Join a WhatsApp group via invite link. Usage: .join <link>", Execute: cmdJoin},
		{Name: "limit", Category: "general", Description: "Show your usage limit & status.", Execute: cmdLimit},
		{Name: "cekbenefit", Aliases: []string{"donatur"}, Category: "general", Description: "Show bot feature perks.", Execute: cmdBenefit},
		{Name: "premium", Category: "general", OwnerOnly: true, Description: "Grant local premium days to a user (no payment gateway). Usage: .premium <phone> <days>", Execute: cmdGrantPremium},
		{Name: "balance", Category: "general", Description: "Check your coin balance.", Execute: cmdBalance},
		{Name: "balancehistory", Category: "general", Description: "Show your recent balance transactions.", Execute: cmdBalanceHistory},
		{Name: "daily", Category: "general", Description: "Claim your daily coin & XP reward.", Execute: cmdDaily},
		{Name: "redeem", Category: "general", Description: "Redeem a promo code. Usage: .redeem <CODE>", Execute: cmdRedeem},
		{Name: "createredeem", Category: "owner", OwnerOnly: true, Description: "Create a redeem code. Usage: .createredeem <CODE> <coins> <xp> <maxUses>", Execute: cmdCreateRedeem},
		{Name: "top", Category: "general", Description: "Top users by balance, xp, or messages. Usage: .top [balance|xp|msg]", Execute: cmdTop},

		// PROFILE
		{Name: "profile", Category: "profile", Description: "View your (or mentioned user's) profile.", Execute: cmdProfile},
		{Name: "level", Category: "profile", Description: "View your XP and level progress.", Execute: cmdLevel},
		{Name: "afk", Category: "profile", Description: "Set AFK status with optional reason. Usage: .afk [reason]", Execute: cmdAFK},
		{Name: "cekpremium", Category: "profile", Description: "Check your local premium status.", Execute: cmdCekPremium},
		{Name: "getpp", Category: "profile", Description: "Fetch profile picture of yourself or a mentioned/replied user.", Execute: cmdGetPP},
		{Name: "setname", Category: "profile", Description: "Set your profile display name. Usage: .setname <name>", Execute: cmdSetName},
		{Name: "setage", Category: "profile", Description: "Set your profile age. Usage: .setage <number>", Execute: cmdSetAge},
		{Name: "setgender", Category: "profile", Description: "Set your profile gender. Usage: .setgender <male|female|other>", Execute: cmdSetGender},
		{Name: "setbio", Category: "profile", Description: "Set your profile bio. Usage: .setbio <text>", Execute: cmdSetBio},
		{Name: "setprefix", Category: "profile", Description: "Set a personal command prefix. Usage: .setprefix <symbol|reset>", Execute: cmdSetPrefix},
		{Name: "linkaccount", Aliases: []string{"weblogin"}, Category: "profile", Description: "Get your WhatsApp Web UI / Arcade link.", Execute: cmdWebLogin},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func userKey(client *whatsmeow.Client, msg *events.Message) string {
	return utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
}

func userActivityAndAFKMiddleware(client *whatsmeow.Client, msg *events.Message) bool {
	store := db.GetBotStore()
	if store == nil || msg == nil || msg.Info.IsFromMe {
		return false
	}
	sender := userKey(client, msg)
	if sender == "" {
		return false
	}

	// Track group member activity for .Sider / .TopActive / .ListAktif
	if msg.Info.IsGroup {
		store.TouchGroupMember(msg.Info.Chat.ToNonAD().String(), sender)
	}

	// Clear sender's AFK if they sent a message (except if they are running .afk)
	text := strings.TrimSpace(utils.GetTextFromMessage(msg.Message))
	lowerText := strings.ToLower(text)
	if !strings.HasPrefix(lowerText, ".afk") && !strings.HasPrefix(lowerText, "/afk") {
		if u, err := store.GetUser(sender); err == nil && u != nil && u.AFKSince > 0 {
			dur := time.Since(time.Unix(u.AFKSince, 0)).Round(time.Second)
			u.AFKReason = ""
			u.AFKSince = 0
			_ = store.SaveUser(u)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("👋 Welcome back *%s*! You were AFK for `%s`.", msg.Info.PushName, dur))
		}
	}

	// Notify if someone mentioned or replied to an AFK user
	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo != nil {
		checked := map[string]bool{}
		var targets []string
		for _, m := range ctxInfo.GetMentionedJID() {
			if j, err := waTypes.ParseJID(m); err == nil {
				if num := utils.ResolvePhoneNumber(client, j, waTypes.EmptyJID); num != "" && !checked[num] {
					checked[num] = true
					targets = append(targets, num)
				}
			}
		}
		if p := ctxInfo.GetParticipant(); p != "" {
			if j, err := waTypes.ParseJID(p); err == nil {
				if num := utils.ResolvePhoneNumber(client, j, waTypes.EmptyJID); num != "" && !checked[num] {
					checked[num] = true
					targets = append(targets, num)
				}
			}
		}
		for _, num := range targets {
			if num == sender {
				continue
			}
			if tu, err := store.GetUser(num); err == nil && tu != nil && tu.AFKSince > 0 {
				dur := time.Since(time.Unix(tu.AFKSince, 0)).Round(time.Second)
				reason := tu.AFKReason
				if reason == "" {
					reason = "No reason"
				}
				name := tu.Name
				if name == "" {
					name = num
				}
				utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💤 *%s* is currently AFK (%s ago)\nReason: _%s_", name, dur, reason))
			}
		}
	}

	// Passive XP gain per message
	store.AddUserActivity(sender, msg.Info.PushName, 5)
	return false
}

func cmdAbout(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🤖 *NopeBot — Android Companion Edition*\nFast Go + whatsmeow multi-device bot with sequential WAL SQLite, View-Once capture, Anti-Delete, Sound Stickers, Sequential QR Queue, and %d commands.\nUse `.menu` or `/help` to explore!",
		len(handlers.PrimaryCommands),
	))
}

func cmdPing(client *whatsmeow.Client, msg *events.Message, args []string) {
	start := time.Now()
	msgAge := time.Since(msg.Info.Timestamp).Round(time.Millisecond)
	if msgAge < 0 {
		msgAge = 0
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏓 *Pong!*\n• Processing: `%s`\n• Delivery delta: `%s`\n• Uptime: `%s`", time.Since(start).Round(time.Microsecond), msgAge, time.Since(handlers.BotStartTime()).Round(time.Second)))
}

func cmdOwner(client *whatsmeow.Client, msg *events.Message, args []string) {
	conf, _ := config.LoadConfig()
	var owners []string
	if conf != nil {
		owners = append(owners, conf.Settings.OwnerJIDs...)
	}
	if store := db.GetBotStore(); store != nil && client.Store.ID != nil {
		if t, err := store.GetTenant(client.Store.ID.ToNonAD().User); err == nil && t != nil && t.OwnerJID != "" {
			owners = append(owners, t.OwnerJID+" (Tenant Owner)")
		}
	}
	if len(owners) == 0 {
		utils.SendReply(client, msg.Info.Chat, "👑 No owner number configured yet.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "👑 *Bot Owner(s):*\n• "+strings.Join(owners, "\n• "))
}

func cmdSC(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "📦 *NopeBot (Go / whatsmeow)*\nBuilt with sequential WAL SQLite, Android Companion View-Once support, and embedded WhatsApp Web UI.")
}

func cmdJoin(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.join https://chat.whatsapp.com/<invite_code>`")
		return
	}
	code := raw
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		code = raw[idx+1:]
	}
	code = strings.Split(code, "?")[0]
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	gid, err := client.JoinGroupWithLink(ctx, code)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not join group: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Joined group: `"+gid.String()+"`")
}

func cmdLimit(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	u, _ := store.GetUser(userKey(client, msg))
	prem := "Free (Unlimited local build)"
	if u != nil && u.PremiumUntil > time.Now().Unix() {
		prem = "Premium until " + time.Unix(u.PremiumUntil, 0).Format("02 Jan 2006")
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚡ *Account Limit*\n• Status: *%s*\n• Balance: `%d` coins\n• Messages: `%d`", prem, u.Balance, u.MsgCount))
}

func cmdBenefit(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "🌟 *NopeBot Benefits*\n• Unlimited downloaders, stickers, audio FX & tools\n• Android Companion View-Once recovery & Anti-Delete\n• Full Group Admin & Security suite\n• Interactive chat games & Web Arcade leaderboards")
}

func cmdGrantPremium(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.premium <phone> <days>`")
		return
	}
	phone := utils.NormalizeTarget(fields[0])
	days, err := strconv.Atoi(fields[1])
	if err != nil || days <= 0 {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Days must be a positive integer.")
		return
	}
	store := db.GetBotStore()
	u, _ := store.GetUser(phone)
	base := time.Now()
	if u.PremiumUntil > base.Unix() {
		base = time.Unix(u.PremiumUntil, 0)
	}
	u.PremiumUntil = base.Add(time.Duration(days) * 24 * time.Hour).Unix()
	_ = store.SaveUser(u)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💎 Granted `%d` premium days to `%s` (expires %s).", days, phone, time.Unix(u.PremiumUntil, 0).Format("02 Jan 2006")))
}

func cmdCekPremium(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	u, _ := store.GetUser(userKey(client, msg))
	if u != nil && u.PremiumUntil > time.Now().Unix() {
		rem := time.Until(time.Unix(u.PremiumUntil, 0)).Round(time.Hour)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💎 *Premium Active*\nExpires: %s (`%dd %dh` left)", time.Unix(u.PremiumUntil, 0).Format("02 Jan 2006"), int(rem.Hours())/24, int(rem.Hours())%24))
		return
	}
	utils.SendReply(client, msg.Info.Chat, "ℹ️ You are currently on the standard tier.")
}

func cmdBalance(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	u, _ := store.GetUser(userKey(client, msg))
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💰 *Balance:* `%d` coins\n⭐ *Level:* `%d` (`%d` XP)", u.Balance, u.Level, u.XP))
}

func cmdBalanceHistory(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	entries, _ := store.GetBalanceHistory(userKey(client, msg), 10)
	if len(entries) == 0 {
		utils.SendReply(client, msg.Info.Chat, "📜 No balance history yet. Try `.daily`!")
		return
	}
	var b strings.Builder
	b.WriteString("📜 *Recent Balance History*\n\n")
	for _, e := range entries {
		sign := "+"
		if e.Delta < 0 {
			sign = ""
		}
		fmt.Fprintf(&b, "• `%s%d` — %s (%s)\n", sign, e.Delta, e.Reason, time.Unix(e.CreatedAt, 0).Format("02 Jan 15:04"))
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdDaily(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	uid := userKey(client, msg)
	u, _ := store.GetUser(uid)
	now := time.Now()
	if u.DailyLast > 0 {
		next := time.Unix(u.DailyLast, 0).Add(24 * time.Hour)
		if now.Before(next) {
			rem := time.Until(next).Round(time.Minute)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⏳ You already claimed daily! Come back in `%s`.", rem))
			return
		}
	}
	u.DailyLast = now.Unix()
	u.XP += 50
	u.Level = int(u.XP/100) + 1
	_ = store.SaveUser(u)
	newBal, _ := store.AddBalance(uid, 250, "Daily reward")
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎁 *Daily Claimed!*\n• `+250` coins (Total: `%d`)\n• `+50` XP (Level `%d`)", newBal, u.Level))
}

func cmdCreateRedeem(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 4 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.createredeem <CODE> <coins> <xp> <maxUses>`")
		return
	}
	coins, _ := strconv.ParseInt(fields[1], 10, 64)
	xp, _ := strconv.ParseInt(fields[2], 10, 64)
	maxUses, _ := strconv.Atoi(fields[3])
	if maxUses <= 0 {
		maxUses = 1
	}
	rc := &db.RedeemCode{
		Code:          strings.ToUpper(fields[0]),
		RewardBalance: coins,
		RewardXP:      xp,
		MaxUses:       maxUses,
	}
	_ = db.GetBotStore().SaveRedeemCode(rc)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎟️ Created redeem code `%s` (+%d coins, +%d XP, max %d uses).", rc.Code, coins, xp, maxUses))
}

func cmdRedeem(client *whatsmeow.Client, msg *events.Message, args []string) {
	code := strings.ToUpper(strings.TrimSpace(strings.Join(args, " ")))
	if code == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.redeem <CODE>`")
		return
	}
	store := db.GetBotStore()
	rc, _ := store.GetRedeemCode(code)
	if rc == nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Invalid redeem code.")
		return
	}
	uid := userKey(client, msg)
	for _, used := range rc.UsedBy {
		if used == uid {
			utils.SendReply(client, msg.Info.Chat, "⚠️ You already redeemed this code.")
			return
		}
	}
	if len(rc.UsedBy) >= rc.MaxUses {
		utils.SendReply(client, msg.Info.Chat, "⚠️ This redeem code has reached its usage limit.")
		return
	}
	rc.UsedBy = append(rc.UsedBy, uid)
	_ = store.SaveRedeemCode(rc)
	if rc.RewardBalance > 0 {
		_, _ = store.AddBalance(uid, rc.RewardBalance, "Redeem "+rc.Code)
	}
	if rc.RewardXP > 0 {
		store.AddUserActivity(uid, msg.Info.PushName, rc.RewardXP)
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎉 Redeemed `%s`! Received `+%d` coins and `+%d` XP.", rc.Code, rc.RewardBalance, rc.RewardXP))
}

func cmdTop(client *whatsmeow.Client, msg *events.Message, args []string) {
	mode := "balance"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		mode = strings.ToLower(strings.TrimSpace(args[0]))
	}
	users, _ := db.GetBotStore().TopUsers(mode, 10)
	if len(users) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🏆 No leaderboard data yet.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🏆 *Top Users (%s)*\n\n", strings.ToUpper(mode))
	for i, u := range users {
		name := u.Name
		if name == "" {
			name = u.JID
		}
		fmt.Fprintf(&b, "%d. *%s* — 💰 `%d` | ⭐ Lv.%d (`%d` XP) | 💬 `%d`\n", i+1, name, u.Balance, u.Level, u.XP, u.MsgCount)
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdProfile(client *whatsmeow.Client, msg *events.Message, args []string) {
	uid := userKey(client, msg)
	if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil {
		if len(ctxInfo.GetMentionedJID()) > 0 {
			if j, err := waTypes.ParseJID(ctxInfo.GetMentionedJID()[0]); err == nil {
				if num := utils.ResolvePhoneNumber(client, j, waTypes.EmptyJID); num != "" {
					uid = num
				}
			}
		} else if p := ctxInfo.GetParticipant(); p != "" {
			if j, err := waTypes.ParseJID(p); err == nil {
				if num := utils.ResolvePhoneNumber(client, j, waTypes.EmptyJID); num != "" {
					uid = num
				}
			}
		}
	}
	u, _ := db.GetBotStore().GetUser(uid)
	name := u.Name
	if name == "" {
		name = uid
	}
	ageStr := "-"
	if u.Age > 0 {
		ageStr = strconv.Itoa(u.Age)
	}
	gender := u.Gender
	if gender == "" {
		gender = "-"
	}
	bio := u.Bio
	if bio == "" {
		bio = "-"
	}
	prefix := u.CustomPrefix
	if prefix == "" {
		prefix = ". /"
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"👤 *User Profile*\n• Name: *%s*\n• Phone: `%s`\n• Age: `%s` | Gender: `%s`\n• Bio: _%s_\n• Level: *%d* (`%d` XP)\n• Balance: `%d` coins\n• Messages: `%d`\n• Prefix: `%s`",
		name, uid, ageStr, gender, bio, u.Level, u.XP, u.Balance, u.MsgCount, prefix,
	))
}

func cmdLevel(client *whatsmeow.Client, msg *events.Message, args []string) {
	u, _ := db.GetBotStore().GetUser(userKey(client, msg))
	progress := u.XP % 100
	bars := int(progress / 10)
	barStr := strings.Repeat("█", bars) + strings.Repeat("░", 10-bars)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⭐ *Level %d*\n[%s] `%d/100` XP (Total: `%d` XP)", u.Level, barStr, progress, u.XP))
}

func cmdAFK(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	uid := userKey(client, msg)
	u, _ := store.GetUser(uid)
	reason := strings.TrimSpace(strings.Join(args, " "))
	if reason == "" {
		reason = "Busy"
	}
	u.AFKReason = reason
	u.AFKSince = time.Now().Unix()
	if u.Name == "" {
		u.Name = msg.Info.PushName
	}
	_ = store.SaveUser(u)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💤 *%s* is now AFK: _%s_", u.Name, reason))
}

func cmdGetPP(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := msg.Info.Sender.ToNonAD()
	if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil {
		if len(ctxInfo.GetMentionedJID()) > 0 {
			if j, err := waTypes.ParseJID(ctxInfo.GetMentionedJID()[0]); err == nil {
				target = j.ToNonAD()
			}
		} else if p := ctxInfo.GetParticipant(); p != "" {
			if j, err := waTypes.ParseJID(p); err == nil {
				target = j.ToNonAD()
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pp, err := client.GetProfilePictureInfo(ctx, target, &whatsmeow.GetProfilePictureParams{Preview: false})
	if err != nil || pp == nil || pp.URL == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch profile picture (may be hidden by privacy settings).")
		return
	}
	data, ct, err := utils.HTTPGetBytes(ctx, pp.URL, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to download profile picture.")
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, data, ct, "🖼️ Profile Picture")
}

func cmdSetName(client *whatsmeow.Client, msg *events.Message, args []string) {
	val := strings.TrimSpace(strings.Join(args, " "))
	if val == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setname <your name>`")
		return
	}
	u, _ := db.GetBotStore().GetUser(userKey(client, msg))
	u.Name = val
	_ = db.GetBotStore().SaveUser(u)
	utils.SendReply(client, msg.Info.Chat, "✅ Profile name set to *"+val+"*.")
}

func cmdSetAge(client *whatsmeow.Client, msg *events.Message, args []string) {
	n, err := strconv.Atoi(strings.TrimSpace(strings.Join(args, " ")))
	if err != nil || n < 5 || n > 120 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setage <5-120>`")
		return
	}
	u, _ := db.GetBotStore().GetUser(userKey(client, msg))
	u.Age = n
	_ = db.GetBotStore().SaveUser(u)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Profile age set to `%d`.", n))
}

func cmdSetGender(client *whatsmeow.Client, msg *events.Message, args []string) {
	val := strings.TrimSpace(strings.Join(args, " "))
	if val == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setgender <male|female|other>`")
		return
	}
	u, _ := db.GetBotStore().GetUser(userKey(client, msg))
	u.Gender = val
	_ = db.GetBotStore().SaveUser(u)
	utils.SendReply(client, msg.Info.Chat, "✅ Profile gender set to *"+val+"*.")
}

func cmdSetBio(client *whatsmeow.Client, msg *events.Message, args []string) {
	val := strings.TrimSpace(strings.Join(args, " "))
	if val == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setbio <text>`")
		return
	}
	u, _ := db.GetBotStore().GetUser(userKey(client, msg))
	u.Bio = val
	_ = db.GetBotStore().SaveUser(u)
	utils.SendReply(client, msg.Info.Chat, "✅ Profile bio updated.")
}

func cmdSetPrefix(client *whatsmeow.Client, msg *events.Message, args []string) {
	val := strings.TrimSpace(strings.Join(args, " "))
	if val == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setprefix <symbol|reset>`")
		return
	}
	u, _ := db.GetBotStore().GetUser(userKey(client, msg))
	if strings.EqualFold(val, "reset") {
		u.CustomPrefix = ""
	} else {
		u.CustomPrefix = val
	}
	_ = db.GetBotStore().SaveUser(u)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Personal prefix updated to `%s` (`.` and `/` also remain active).", val))
}

func cmdWebLogin(client *whatsmeow.Client, msg *events.Message, args []string) {
	base := "http://localhost" + ai.AppConfig.ServerPort
	if dom := strings.TrimSpace(os.Getenv("NGROK_DOMAIN")); dom != "" {
		if !strings.HasPrefix(dom, "http") {
			base = "https://" + dom
		} else {
			base = strings.TrimRight(dom, "/")
		}
	}
	uid := userKey(client, msg)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🌐 *NopeBot Web Links*\n• WhatsApp Web UI: %s\n• Arcade MiniGames: %s/games?user=%s",
		base, base, uid,
	))
}
