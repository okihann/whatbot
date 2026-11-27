package groupadmin

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/i18n"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func init() {
	handlers.RegisterGroupParticipantHandler(handleWelcomeGoodbye)

	cmds := []types.Command{
		// Member & metadata moderation
		{Name: "add", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Add member(s) to the group. Usage: .add 628xxx", Execute: cmdAdd},
		{Name: "remove", Aliases: []string{"kick"}, Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Remove member(s) from the group. Usage: .remove @user", Execute: cmdRemove},
		{Name: "promote", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Promote member(s) to group admin.", Execute: cmdPromote},
		{Name: "demote", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Demote admin(s) to regular member.", Execute: cmdDemote},
		{Name: "open", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Open group chat for all participants.", Execute: cmdOpenClose(false)},
		{Name: "close", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Close group chat (admins only can send).", Execute: cmdOpenClose(true)},
		{Name: "linkgc", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Get the group invite link.", Execute: cmdLinkGC(false)},
		{Name: "revoke", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Reset and generate a new group invite link.", Execute: cmdLinkGC(true)},
		{Name: "setsubject", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Change the group name/subject.", Execute: cmdSetSubject},
		{Name: "setdesc", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Change the group description.", Execute: cmdSetDesc},
		{Name: "setppgc", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Change group photo (reply to an image).", Execute: cmdSetPPGC},
		{Name: "groupinfo", Aliases: []string{"info"}, Category: "group_admin", GroupOnly: true, Description: "Display group details, admins, and member count.", Execute: cmdGroupInfo},
		{Name: "groupid", Category: "group_admin", GroupOnly: true, Description: "Show the current group JID.", Execute: cmdGroupID},
		{Name: "cekbotadmin", Category: "group_admin", GroupOnly: true, Description: "Check whether the bot is an admin in this group.", Execute: cmdCekBotAdmin},
		{Name: "cektanpagrup", Category: "group_admin", OwnerOnly: true, Description: "List active chats that are not groups.", Execute: cmdCekTanpaGrup},
		{Name: "joinreq", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "List or approve/reject pending join requests. Usage: .joinreq [approve|reject all|@phone]", Execute: cmdJoinReq},
		{Name: "leavegroup", Aliases: []string{"left"}, Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Make the bot leave this group.", Execute: cmdLeaveGroup},
		{Name: "syncgroup", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Refresh group metadata from WhatsApp.", Execute: cmdSyncGroup},

		// Messaging, tagging, pinning & disappearing
		{Name: "hidetag", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Send an announcement mentioning all members silently.", Execute: cmdHideTag},
		{Name: "tagall", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Mention all group members visibly.", Execute: cmdTagAll},
		{Name: "delete", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Delete a replied message in the group.", Execute: cmdDeleteMsg},
		{Name: "pinmsg", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Pin a replied message in chat.", Execute: cmdPinMsg(true)},
		{Name: "unpinmsg", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Unpin a replied message in chat.", Execute: cmdPinMsg(false)},
		{Name: "ephemeral", Category: "group_admin", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Set disappearing messages timer. Usage: .ephemeral <off|24h|7d|90d>", Execute: cmdEphemeral},

		// Welcome / Goodbye
		{Name: "welcome", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Toggle welcome & goodbye messages. Usage: .welcome on|off", Execute: cmdWelcomeToggle},
		{Name: "setwelcome", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Set custom welcome message (@user, {group}, {count}).", Execute: cmdSetWelcome},
		{Name: "setgoodbye", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Set custom goodbye message (@user, {group}, {count}).", Execute: cmdSetGoodbye},
		{Name: "simulatewelcome", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Preview the group welcome message.", Execute: cmdSimulateWelcome(true)},
		{Name: "simulategoodbye", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Preview the group goodbye message.", Execute: cmdSimulateWelcome(false)},

		// Group settings & automation
		{Name: "groupsettings", Aliases: []string{"groupstatus"}, Category: "group_admin", GroupOnly: true, Description: "Show all active settings and security toggles for this group.", Execute: cmdGroupSettings},
		{Name: "groupprefix", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Set custom group command prefix. Usage: .groupprefix <symbol|reset>", Execute: cmdGroupPrefix},
		{Name: "adminonly", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Restrict bot commands in this group to admins only. Usage: .adminonly on|off", Execute: cmdToggleGroupFlag("adminonly")},
		{Name: "cmd", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Enable or disable a command or category. Usage: .cmd <on|off> <name>", Execute: cmdToggleCmd},
		{Name: "autorespon", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Manage keyword auto-responses. Usage: .autorespon add <key>|<reply> | del <key> | list", Execute: cmdAutoRespon},
		{Name: "schedulegroup", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Schedule daily auto open/close. Usage: .schedulegroup <HH:MM> <HH:MM> | off", Execute: cmdScheduleGroup},
		{Name: "adzan", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Toggle prayer time reminder city. Usage: .adzan <city|off>", Execute: cmdAdzan},
		{Name: "settimezone", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Set group timezone (e.g. Asia/Jakarta, Asia/Makassar, Asia/Jayapura).", Execute: cmdSetTimezone},
		{Name: "listtimezone", Category: "group_admin", Description: "Show supported common timezones.", Execute: cmdListTimezone},
		{Name: "setlang", Aliases: []string{"bahasa"}, Category: "general", Description: "Set Indonesian (id) or English (en) language for yourself or this group. Usage: .setlang <id|en> [group]", Execute: cmdSetLang},
		{Name: "debugpriority", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Inspect active prefixes and middleware status for this group.", Execute: cmdDebugPriority},

		// Attendance & activity
		{Name: "attendance", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Start/view/clear group roll call. Usage: .attendance [start <title>|list|clear]", Execute: cmdAttendance},
		{Name: "present", Category: "group_admin", GroupOnly: true, Description: "Mark yourself present in the active group attendance.", Execute: cmdPresent},
		{Name: "sider", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "List inactive/silent members (no messages in last 7 days).", Execute: cmdSider},
		{Name: "listaktif", Aliases: []string{"topactive"}, Category: "group_admin", GroupOnly: true, Description: "Show the most active members in this group.", Execute: cmdTopActive},
		{Name: "gstats", Category: "group_admin", GroupOnly: true, Description: "Show group message and member activity statistics.", Execute: cmdGStats},

		// Anti-feature toggles (enforced in groupsec middleware)
		{Name: "antivo", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Auto-repost view-once media in this group. Usage: .antivo on|off", Execute: cmdToggleGroupFlag("antivo")},
		{Name: "antihidetag", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Block non-admin mass mention hidetags. Usage: .antihidetag on|off", Execute: cmdToggleGroupFlag("antihidetag")},
		{Name: "antispam", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Toggle flood/spam protection. Usage: .antispam on|off", Execute: cmdToggleGroupFlag("antispam")},
		{Name: "antibug", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Toggle oversized/virtex bug message protection. Usage: .antibug on|off", Execute: cmdToggleGroupFlag("antibug")},
		{Name: "anticall", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Toggle call protection flag for this group. Usage: .anticall on|off", Execute: cmdToggleGroupFlag("anticall")},
		{Name: "antisw", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Block status/story forwards in group. Usage: .antisw on|off", Execute: cmdToggleGroupFlag("antisw")},
		{Name: "antitagsw", Category: "group_admin", GroupOnly: true, AdminOnly: true, Description: "Block group status mention tags. Usage: .antitagsw on|off", Execute: cmdToggleGroupFlag("antitagsw")},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func formatTemplate(tpl string, userTag, groupName string, count int) string {
	r := strings.NewReplacer(
		"@user", userTag,
		"{user}", userTag,
		"{group}", groupName,
		"{count}", strconv.Itoa(count),
	)
	return r.Replace(tpl)
}

func handleWelcomeGoodbye(client *whatsmeow.Client, evt *events.GroupInfo) {
	store := db.GetBotStore()
	if store == nil || client == nil || evt == nil {
		return
	}
	cfg, err := store.GetGroupConfig(evt.JID.ToNonAD().String())
	if err != nil || cfg == nil {
		return
	}
	if !cfg.WelcomeEnabled && !cfg.GoodbyeEnabled {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	gi, _ := client.GetGroupInfo(ctx, evt.JID)
	groupName := evt.JID.User
	count := 0
	if gi != nil {
		groupName = gi.Name
		count = len(gi.Participants)
	}

	if cfg.WelcomeEnabled && len(evt.Join) > 0 {
		for _, j := range evt.Join {
			tag := "@" + j.ToNonAD().User
			txt := formatTemplate(cfg.WelcomeMessage, tag, groupName, count)
			_, _ = utils.SendMentionReply(client, evt.JID, txt, []string{j.ToNonAD().String()})
		}
	}
	if cfg.GoodbyeEnabled && len(evt.Leave) > 0 {
		for _, j := range evt.Leave {
			tag := "@" + j.ToNonAD().User
			txt := formatTemplate(cfg.GoodbyeMessage, tag, groupName, count)
			_, _ = utils.SendMentionReply(client, evt.JID, txt, []string{j.ToNonAD().String()})
		}
	}
}

func updateParticipants(client *whatsmeow.Client, msg *events.Message, args []string, action whatsmeow.ParticipantChange, label string) {
	targets := utils.ExtractTargetJIDs(msg, args)
	if len(targets) == 0 {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Usage: reply/mention user(s) or pass phone number(s) to %s.", label))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := client.UpdateGroupParticipants(ctx, msg.Info.Chat, targets, action)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to %s: %v", label, err))
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Successfully performed *%s* on %d user(s).", label, len(targets)))
}

func cmdAdd(client *whatsmeow.Client, msg *events.Message, args []string) {
	updateParticipants(client, msg, args, whatsmeow.ParticipantChangeAdd, "add")
}

func cmdRemove(client *whatsmeow.Client, msg *events.Message, args []string) {
	updateParticipants(client, msg, args, whatsmeow.ParticipantChangeRemove, "remove")
}

func cmdPromote(client *whatsmeow.Client, msg *events.Message, args []string) {
	updateParticipants(client, msg, args, whatsmeow.ParticipantChangePromote, "promote")
}

func cmdDemote(client *whatsmeow.Client, msg *events.Message, args []string) {
	updateParticipants(client, msg, args, whatsmeow.ParticipantChangeDemote, "demote")
}

func cmdOpenClose(announce bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.SetGroupAnnounce(ctx, msg.Info.Chat, announce); err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Failed: "+err.Error())
			return
		}
		if announce {
			utils.SendReply(client, msg.Info.Chat, "🔒 Group is now *CLOSED* (admins only).")
		} else {
			utils.SendReply(client, msg.Info.Chat, "🔓 Group is now *OPEN* (all members can send messages).")
		}
	}
}

func cmdLinkGC(reset bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		link, err := client.GetGroupInviteLink(ctx, msg.Info.Chat, reset)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Failed to fetch invite link: "+err.Error())
			return
		}
		if reset {
			utils.SendReply(client, msg.Info.Chat, "🔄 *Group Invite Link Reset:*\n"+link)
		} else {
			utils.SendReply(client, msg.Info.Chat, "🔗 *Group Invite Link:*\n"+link)
		}
	}
}

func cmdSetSubject(client *whatsmeow.Client, msg *events.Message, args []string) {
	name := strings.TrimSpace(strings.Join(args, " "))
	if name == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setsubject <new group name>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.SetGroupName(ctx, msg.Info.Chat, name); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Group name updated to *"+name+"*.")
}

func cmdSetDesc(client *whatsmeow.Client, msg *events.Message, args []string) {
	desc := strings.TrimSpace(strings.Join(args, " "))
	if desc == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setdesc <new description>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.SetGroupDescription(ctx, msg.Info.Chat, desc); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Group description updated.")
}

func cmdSetPPGC(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := utils.ExtractQuotedOrOwnMessage(msg)
	if target == nil || target.GetImageMessage() == nil {
		utils.SendReply(client, msg.Info.Chat, "📷 Reply to an image with `.setppgc`.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	data, _, _, err := utils.DownloadMedia(ctx, client, target)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Download failed: "+err.Error())
		return
	}
	if _, err := client.SetGroupPhoto(ctx, msg.Info.Chat, data); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to set group photo: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Group photo updated.")
}

func cmdGroupInfo(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed: "+err.Error())
		return
	}
	admins := 0
	for _, p := range gi.Participants {
		if p.IsAdmin || p.IsSuperAdmin {
			admins++
		}
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"👥 *%s*\n• JID: `%s`\n• Members: `%d` (`%d` admins)\n• Locked Info: `%v` | Admins-Only Chat: `%v`\n• Created: `%s`\n• Description:\n%s",
		gi.Name, gi.JID.String(), len(gi.Participants), admins, gi.IsLocked, gi.IsAnnounce,
		gi.GroupCreated.Format("02 Jan 2006"), gi.Topic,
	))
}

func cmdGroupID(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "🆔 Group JID: `"+msg.Info.Chat.ToNonAD().String()+"`")
}

func cmdCekBotAdmin(client *whatsmeow.Client, msg *events.Message, args []string) {
	_, botAdmin := handlers.IsGroupAdmin(client, msg.Info.Chat, msg.Info.Sender)
	if botAdmin {
		utils.SendReply(client, msg.Info.Chat, "✅ Bot *is* an admin in this group.")
	} else {
		utils.SendReply(client, msg.Info.Chat, "❌ Bot is *not* an admin in this group.")
	}
}

func cmdCekTanpaGrup(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	groups, _ := client.GetJoinedGroups(ctx)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("ℹ️ Bot is currently in `%d` groups.", len(groups)))
}

func cmdJoinReq(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reqs, err := client.GetGroupRequestParticipants(ctx, msg.Info.Chat)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to fetch join requests: "+err.Error())
		return
	}
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) >= 2 && (strings.EqualFold(fields[0], "approve") || strings.EqualFold(fields[0], "reject")) {
		action := whatsmeow.ParticipantChangeApprove
		if strings.EqualFold(fields[0], "reject") {
			action = whatsmeow.ParticipantChangeReject
		}
		var targets []waTypes.JID
		if strings.EqualFold(fields[1], "all") {
			for _, r := range reqs {
				targets = append(targets, r.JID)
			}
		} else {
			targets = utils.ExtractTargetJIDs(msg, fields[1:])
		}
		if len(targets) == 0 {
			utils.SendReply(client, msg.Info.Chat, "⚠️ No matching pending requests.")
			return
		}
		if _, err := client.UpdateGroupRequestParticipants(ctx, msg.Info.Chat, targets, action); err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Action failed: "+err.Error())
			return
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ %sd %d request(s).", fields[0], len(targets)))
		return
	}
	if len(reqs) == 0 {
		utils.SendReply(client, msg.Info.Chat, "✅ No pending join requests.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📩 *Pending Join Requests (%d)*\n", len(reqs))
	for i, r := range reqs {
		fmt.Fprintf(&b, "%d. `%s`\n", i+1, r.JID.ToNonAD().User)
	}
	b.WriteString("\nUse `.joinreq approve all` or `.joinreq reject all`.")
	utils.SendReply(client, msg.Info.Chat, b.String())
}

func cmdLeaveGroup(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "👋 Leaving group...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = client.LeaveGroup(ctx, msg.Info.Chat)
}

func cmdSyncGroup(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Sync failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔄 Synced *%s* (`%d` participants).", gi.Name, len(gi.Participants)))
}

func cmdHideTag(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		if q := utils.ExtractQuotedOrOwnMessage(msg); q != nil {
			text = utils.GetTextFromMessage(q)
		}
	}
	if text == "" {
		text = "📢 Attention everyone!"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not load group members.")
		return
	}
	mentions := make([]string, 0, len(gi.Participants))
	for _, p := range gi.Participants {
		mentions = append(mentions, p.JID.ToNonAD().String())
	}
	_, _ = utils.SendMentionReply(client, msg.Info.Chat, text, mentions)
}

func cmdTagAll(client *whatsmeow.Client, msg *events.Message, args []string) {
	header := strings.TrimSpace(strings.Join(args, " "))
	if header == "" {
		header = "📢 Tag All"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not load group members.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "*%s*\n\n", header)
	mentions := make([]string, 0, len(gi.Participants))
	for i, p := range gi.Participants {
		j := p.JID.ToNonAD()
		mentions = append(mentions, j.String())
		fmt.Fprintf(&b, "%d. @%s\n", i+1, j.User)
	}
	_, _ = utils.SendMentionReply(client, msg.Info.Chat, strings.TrimSpace(b.String()), mentions)
}

func cmdDeleteMsg(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || ctxInfo.GetStanzaID() == "" {
		utils.SendReply(client, msg.Info.Chat, "↩️ Reply to the message you want to delete.")
		return
	}
	senderJID := waTypes.EmptyJID
	if p := ctxInfo.GetParticipant(); p != "" {
		senderJID, _ = waTypes.ParseJID(p)
	}
	revoke := client.BuildRevoke(msg.Info.Chat, senderJID, waTypes.MessageID(ctxInfo.GetStanzaID()))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.SendMessage(ctx, msg.Info.Chat, revoke); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Delete failed: "+err.Error())
	}
}

func cmdPinMsg(pin bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		ctxInfo := utils.ExtractContextInfo(msg.Message)
		if ctxInfo == nil || ctxInfo.GetStanzaID() == "" {
			utils.SendReply(client, msg.Info.Chat, "↩️ Reply to a message to pin/unpin it.")
			return
		}
		pinType := waE2E.PinInChatMessage_PIN_FOR_ALL
		if !pin {
			pinType = waE2E.PinInChatMessage_UNPIN_FOR_ALL
		}
		nowMS := time.Now().UnixMilli()
		key := &waCommon.MessageKey{
			RemoteJID: proto.String(msg.Info.Chat.ToNonAD().String()),
			ID:        proto.String(ctxInfo.GetStanzaID()),
			FromMe:    proto.Bool(false),
		}
		if p := ctxInfo.GetParticipant(); p != "" {
			key.Participant = proto.String(p)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.SendMessage(ctx, msg.Info.Chat, &waE2E.Message{
			PinInChatMessage: &waE2E.PinInChatMessage{
				Key:               key,
				Type:              &pinType,
				SenderTimestampMS: &nowMS,
			},
		})
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Pin action failed: "+err.Error())
			return
		}
		if pin {
			utils.SendReply(client, msg.Info.Chat, "📌 Message pinned.")
		} else {
			utils.SendReply(client, msg.Info.Chat, "📌 Message unpinned.")
		}
	}
}

func cmdEphemeral(client *whatsmeow.Client, msg *events.Message, args []string) {
	arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	var dur time.Duration
	switch arg {
	case "off", "0":
		dur = whatsmeow.DisappearingTimerOff
	case "24h", "1d":
		dur = whatsmeow.DisappearingTimer24Hours
	case "7d":
		dur = whatsmeow.DisappearingTimer7Days
	case "90d":
		dur = whatsmeow.DisappearingTimer90Days
	default:
		utils.SendReply(client, msg.Info.Chat, "Usage: `.ephemeral <off|24h|7d|90d>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.SetDisappearingTimer(ctx, msg.Info.Chat, dur, time.Now()); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Disappearing message timer set to `"+arg+"`.")
}

func cmdWelcomeToggle(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	on := !cfg.WelcomeEnabled
	if arg == "on" || arg == "1" {
		on = true
	} else if arg == "off" || arg == "0" {
		on = false
	}
	cfg.WelcomeEnabled = on
	cfg.GoodbyeEnabled = on
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Welcome & Goodbye messages are now *%v*.", on))
}

func cmdSetWelcome(client *whatsmeow.Client, msg *events.Message, args []string) {
	val := strings.TrimSpace(strings.Join(args, " "))
	if val == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setwelcome Welcome @user to {group}! Members: {count}`")
		return
	}
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	cfg.WelcomeMessage = val
	cfg.WelcomeEnabled = true
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, "✅ Welcome message saved & enabled.")
}

func cmdSetGoodbye(client *whatsmeow.Client, msg *events.Message, args []string) {
	val := strings.TrimSpace(strings.Join(args, " "))
	if val == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setgoodbye Goodbye @user from {group}!`")
		return
	}
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	cfg.GoodbyeMessage = val
	cfg.GoodbyeEnabled = true
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, "✅ Goodbye message saved & enabled.")
}

func cmdSimulateWelcome(welcome bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		cfg, _ := db.GetBotStore().GetGroupConfig(msg.Info.Chat.ToNonAD().String())
		gi, _ := client.GetGroupInfo(context.Background(), msg.Info.Chat)
		name := msg.Info.Chat.User
		count := 1
		if gi != nil {
			name = gi.Name
			count = len(gi.Participants)
		}
		tpl := cfg.WelcomeMessage
		if !welcome {
			tpl = cfg.GoodbyeMessage
		}
		tag := "@" + msg.Info.Sender.ToNonAD().User
		_, _ = utils.SendMentionReply(client, msg.Info.Chat, formatTemplate(tpl, tag, name, count), []string{msg.Info.Sender.ToNonAD().String()})
	}
}

func cmdGroupSettings(client *whatsmeow.Client, msg *events.Message, args []string) {
	cfg, _ := db.GetBotStore().GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"⚙️ *Group Settings*\n• Welcome/Goodbye: `%v` / `%v`\n• AdminOnly: `%v` | NSFW: `%v`\n• Prefix: `%s` | TZ: `%s` | Lang: `%s`\n• AntiLink: `%v` (%s) | AntiMedia: `%v`\n• AntiSpam: `%v` | AntiVO: `%v` | AntiHideTag: `%v`\n• AntiBot: `%v` | AntiKudeta: `%v` | AntiBug: `%v`\n• AntiSW: `%v` | AntiTagSW: `%v` | BadWord: `%v` (%d words)\n• Schedule: Open `%s` / Close `%s` | Adzan: `%v` (%s)\n• Warn Limit: `%d` | Banned: `%v`",
		cfg.WelcomeEnabled, cfg.GoodbyeEnabled, cfg.AdminOnly, cfg.NSFWEnabled,
		cfg.Prefix, cfg.Timezone, cfg.Lang,
		cfg.AntiLink, cfg.AntiLinkAction, cfg.AntiMedia,
		cfg.AntiSpam, cfg.AntiVO, cfg.AntiHideTag,
		cfg.AntiBot, cfg.AntiKudeta, cfg.AntiBug,
		cfg.AntiSW, cfg.AntiTagSW, cfg.BadWordEnabled, len(cfg.BadWords),
		cfg.ScheduleOpen, cfg.ScheduleClose, cfg.AdzanEnabled, cfg.AdzanCity,
		cfg.WarnLimit, cfg.Banned,
	))
}

func cmdGroupPrefix(client *whatsmeow.Client, msg *events.Message, args []string) {
	val := strings.TrimSpace(strings.Join(args, " "))
	if val == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.groupprefix <symbol|reset>`")
		return
	}
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	if strings.EqualFold(val, "reset") {
		cfg.Prefix = ""
	} else {
		cfg.Prefix = val
	}
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Group prefix set to `%s`.", val))
}

func cmdToggleGroupFlag(flag string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		store := db.GetBotStore()
		cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
		arg := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
		apply := func(cur bool) bool {
			if arg == "on" || arg == "1" || arg == "enable" {
				return true
			}
			if arg == "off" || arg == "0" || arg == "disable" {
				return false
			}
			return !cur
		}
		var state bool
		switch flag {
		case "adminonly":
			cfg.AdminOnly = apply(cfg.AdminOnly)
			state = cfg.AdminOnly
		case "antivo":
			cfg.AntiVO = apply(cfg.AntiVO)
			state = cfg.AntiVO
		case "antihidetag":
			cfg.AntiHideTag = apply(cfg.AntiHideTag)
			state = cfg.AntiHideTag
		case "antispam":
			cfg.AntiSpam = apply(cfg.AntiSpam)
			state = cfg.AntiSpam
		case "antibug":
			cfg.AntiBug = apply(cfg.AntiBug)
			state = cfg.AntiBug
		case "anticall":
			cfg.AntiCall = apply(cfg.AntiCall)
			state = cfg.AntiCall
		case "antisw":
			cfg.AntiSW = apply(cfg.AntiSW)
			state = cfg.AntiSW
		case "antitagsw":
			cfg.AntiTagSW = apply(cfg.AntiTagSW)
			state = cfg.AntiTagSW
		}
		_ = store.SaveGroupConfig(cfg)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ `%s` is now *%v* in this group.", flag, state))
	}
}

func cmdToggleCmd(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.cmd <on|off> <command_or_category>`")
		return
	}
	enable := strings.EqualFold(fields[0], "on") || strings.EqualFold(fields[0], "enable")
	target := strings.ToLower(fields[1])
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	if cmd, ok := handlers.Commands[target]; ok {
		cfg.DisabledCommands[cmd.Name] = !enable
	} else {
		cfg.DisabledCategories[target] = !enable
	}
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ `%s` set to enabled=`%v` for this group.", target, enable))
}

func cmdAutoRespon(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	raw := strings.TrimSpace(strings.Join(args, " "))
	parts := strings.SplitN(raw, " ", 2)
	sub := strings.ToLower(parts[0])
	switch sub {
	case "add":
		if len(parts) < 2 || !strings.Contains(parts[1], "|") {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.autorespon add keyword|reply message`")
			return
		}
		kv := strings.SplitN(parts[1], "|", 2)
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.TrimSpace(kv[1])
		cfg.AutoRespon[k] = v
		_ = store.SaveGroupConfig(cfg)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Auto-response added for `%s`.", k))
	case "del", "remove":
		if len(parts) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.autorespon del <keyword>`")
			return
		}
		k := strings.ToLower(strings.TrimSpace(parts[1]))
		delete(cfg.AutoRespon, k)
		_ = store.SaveGroupConfig(cfg)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗑️ Removed auto-response `%s`.", k))
	default:
		if len(cfg.AutoRespon) == 0 {
			utils.SendReply(client, msg.Info.Chat, "🤖 No auto-responses configured. Use `.autorespon add key|reply`.")
			return
		}
		var b strings.Builder
		b.WriteString("🤖 *Group Auto-Responses:*\n")
		for k, v := range cfg.AutoRespon {
			fmt.Fprintf(&b, "• `%s` → %s\n", k, v)
		}
		utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
	}
}

func cmdScheduleGroup(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 1 && strings.EqualFold(fields[0], "off") {
		cfg.ScheduleOpen = ""
		cfg.ScheduleClose = ""
		_ = store.SaveGroupConfig(cfg)
		utils.SendReply(client, msg.Info.Chat, "✅ Group open/close schedule disabled.")
		return
	}
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.schedulegroup <open HH:MM> <close HH:MM>` or `.schedulegroup off`")
		return
	}
	cfg.ScheduleOpen = fields[0]
	cfg.ScheduleClose = fields[1]
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⏰ Scheduled group auto-open at `%s` and auto-close at `%s` (%s).", cfg.ScheduleOpen, cfg.ScheduleClose, cfg.Timezone))
}

func cmdAdzan(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	city := strings.TrimSpace(strings.Join(args, " "))
	if city == "" || strings.EqualFold(city, "off") {
		cfg.AdzanEnabled = false
		_ = store.SaveGroupConfig(cfg)
		utils.SendReply(client, msg.Info.Chat, "🕌 Adzan reminder disabled.")
		return
	}
	cfg.AdzanCity = city
	cfg.AdzanEnabled = true
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🕌 Adzan reminder enabled for *%s* (%s).", city, cfg.Timezone))
}

func cmdSetTimezone(client *whatsmeow.Client, msg *events.Message, args []string) {
	tz := strings.TrimSpace(strings.Join(args, " "))
	if _, err := time.LoadLocation(tz); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Invalid IANA timezone. Use `.listtimezone` to see examples.")
		return
	}
	store := db.GetBotStore()
	cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
	cfg.Timezone = tz
	_ = store.SaveGroupConfig(cfg)
	utils.SendReply(client, msg.Info.Chat, "✅ Group timezone set to `"+tz+"`.")
}

func cmdListTimezone(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "🌍 *Common Timezones*\n• `Asia/Jakarta` (WIB / UTC+7)\n• `Asia/Makassar` (WITA / UTC+8)\n• `Asia/Jayapura` (WIT / UTC+9)\n• `Asia/Singapore` (UTC+8)\n• `Asia/Tokyo` (UTC+9)\n• `UTC` (UTC+0)\n• `America/New_York` | `Europe/London`")
}

func cmdSetLang(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(strings.Join(args, " "))))
	if len(fields) == 0 || (fields[0] != "id" && fields[0] != "en") {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "lang.usage"))
		return
	}
	lang := fields[0]
	label := "Bahasa Indonesia (id)"
	if lang == "en" {
		label = "English (en)"
	}
	store := db.GetBotStore()
	if store == nil {
		return
	}

	if msg.Info.IsGroup && len(fields) >= 2 && (fields[1] == "group" || fields[1] == "grup") {
		conf, _ := config.LoadConfig()
		isAdmin, _ := handlers.IsGroupAdmin(client, msg.Info.Chat, msg.Info.Sender)
		if !isAdmin && !handlers.IsBotOrTenantOwner(client, msg, conf) {
			utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "err.admin_only"))
			return
		}
		cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
		cfg.Lang = lang
		_ = store.SaveGroupConfig(cfg)
		utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "lang.set_group", label))
		return
	}

	// Default: update caller's personal language preference (and group language if admin in group)
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	if uid != "" {
		u, _ := store.GetUser(uid)
		u.Lang = lang
		_ = store.SaveUser(u)
	}
	if msg.Info.IsGroup {
		conf, _ := config.LoadConfig()
		isAdmin, _ := handlers.IsGroupAdmin(client, msg.Info.Chat, msg.Info.Sender)
		if isAdmin || handlers.IsBotOrTenantOwner(client, msg, conf) {
			cfg, _ := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String())
			cfg.Lang = lang
			_ = store.SaveGroupConfig(cfg)
			utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "lang.set_group", label))
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "lang.set_user", label))
}

func cmdDebugPriority(client *whatsmeow.Client, msg *events.Message, args []string) {
	prefixes := handlers.ActivePrefixes(client, msg, nil)
	isAdmin, botAdmin := handlers.IsGroupAdmin(client, msg.Info.Chat, msg.Info.Sender)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🔧 *Debug Priority*\n• Active Prefixes: `%s`\n• Sender Admin: `%v`\n• Bot Admin: `%v`",
		strings.Join(prefixes, " "), isAdmin, botAdmin,
	))
}

func cmdAttendance(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	gid := msg.Info.Chat.ToNonAD().String()
	raw := strings.TrimSpace(strings.Join(args, " "))
	parts := strings.SplitN(raw, " ", 2)
	sub := strings.ToLower(parts[0])

	switch sub {
	case "clear", "reset":
		_ = store.ClearAttendance(gid)
		utils.SendReply(client, msg.Info.Chat, "🧹 Attendance cleared.")
	case "start", "new":
		title := "Daily Attendance"
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			title = strings.TrimSpace(parts[1])
		}
		rec := &db.AttendanceRecord{
			GroupJID: gid,
			Title:    title,
			DateKey:  time.Now().Format("02 Jan 2006"),
			Present:  map[string]string{},
		}
		_ = store.SaveAttendance(rec)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📋 *Attendance Started: %s* (%s)\nType `.present` to check in!", rec.Title, rec.DateKey))
	default:
		rec, _ := store.GetAttendance(gid)
		if rec == nil {
			utils.SendReply(client, msg.Info.Chat, "📋 No active attendance. Admin can start one with `.attendance start <title>`.")
			return
		}
		var b strings.Builder
		fmt.Fprintf(&b, "📋 *%s* (%s)\n*Present (%d):*\n", rec.Title, rec.DateKey, len(rec.Present))
		i := 1
		for uid, name := range rec.Present {
			fmt.Fprintf(&b, "%d. %s (`%s`)\n", i, name, uid)
			i++
		}
		b.WriteString("\nType `.present` to check in.")
		utils.SendReply(client, msg.Info.Chat, b.String())
	}
}

func cmdPresent(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	gid := msg.Info.Chat.ToNonAD().String()
	rec, _ := store.GetAttendance(gid)
	if rec == nil {
		utils.SendReply(client, msg.Info.Chat, "⚠️ No active attendance session in this group.")
		return
	}
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	name := strings.TrimSpace(msg.Info.PushName)
	if name == "" {
		name = uid
	}
	rec.Present[uid] = name
	_ = store.SaveAttendance(rec)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ *%s* marked present! Total present: `%d`.", name, len(rec.Present)))
}

func cmdSider(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to fetch group info.")
		return
	}
	members, _ := db.GetBotStore().ListGroupMembers(msg.Info.Chat.ToNonAD().String())
	activeMap := map[string]int64{}
	for _, m := range members {
		activeMap[m.UserJID] = m.LastActive
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	var siders []string
	for _, p := range gi.Participants {
		u := p.PhoneNumber.ToNonAD().User
		if u == "" {
			u = p.JID.ToNonAD().User
		}
		if last := activeMap[u]; last < cutoff {
			siders = append(siders, u)
		}
	}
	if len(siders) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🎉 Everyone has been active in the last 7 days!")
		return
	}
	if len(siders) > 40 {
		siders = siders[:40]
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("👻 *Silent Members / Siders (%d shown):*\n• `%s`", len(siders), strings.Join(siders, "`\n• `")))
}

func cmdTopActive(client *whatsmeow.Client, msg *events.Message, args []string) {
	members, _ := db.GetBotStore().ListGroupMembers(msg.Info.Chat.ToNonAD().String())
	if len(members) == 0 {
		utils.SendReply(client, msg.Info.Chat, "📊 No activity recorded in this group yet.")
		return
	}
	sort.Slice(members, func(i, j int) bool { return members[i].MsgCount > members[j].MsgCount })
	if len(members) > 15 {
		members = members[:15]
	}
	var b strings.Builder
	b.WriteString("🔥 *Top Active Members*\n\n")
	for i, m := range members {
		fmt.Fprintf(&b, "%d. `%s` — *%d* msgs\n", i+1, m.UserJID, m.MsgCount)
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdGStats(client *whatsmeow.Client, msg *events.Message, args []string) {
	members, _ := db.GetBotStore().ListGroupMembers(msg.Info.Chat.ToNonAD().String())
	var total int64
	warns := 0
	for _, m := range members {
		total += m.MsgCount
		warns += m.Warns
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📊 *Group Statistics*\n• Tracked Active Members: `%d`\n• Total Recorded Messages: `%d`\n• Active Warnings Issued: `%d`", len(members), total, warns))
}

// StartGroupScheduler runs a lightweight 1-minute ticker executing daily
// ScheduleGroup auto open/close rules across all connected clients.
func StartGroupScheduler(stopCh <-chan struct{}, getClients func() []*whatsmeow.Client) {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		lastFired := map[string]string{}
		for {
			select {
			case <-stopCh:
				return
			case now := <-ticker.C:
				store := db.GetBotStore()
				if store == nil {
					continue
				}
				cfgs, _ := store.ListAllGroupConfigs()
				clients := getClients()
				if len(clients) == 0 {
					continue
				}
				for _, cfg := range cfgs {
					if cfg.ScheduleOpen == "" && cfg.ScheduleClose == "" {
						continue
					}
					loc, err := time.LoadLocation(cfg.Timezone)
					if err != nil {
						loc = time.UTC
					}
					hhmm := now.In(loc).Format("15:04")
					dayKey := now.In(loc).Format("2006-01-02") + ":" + hhmm
					if lastFired[cfg.GroupJID] == dayKey {
						continue
					}
					gid, err := waTypes.ParseJID(cfg.GroupJID)
					if err != nil {
						continue
					}
					if cfg.ScheduleOpen == hhmm {
						lastFired[cfg.GroupJID] = dayKey
						_ = clients[0].SetGroupAnnounce(context.Background(), gid, false)
						_, _ = utils.SendReply(clients[0], gid, "🔓 *Scheduled Open:* Group is now open!")
					} else if cfg.ScheduleClose == hhmm {
						lastFired[cfg.GroupJID] = dayKey
						_ = clients[0].SetGroupAnnounce(context.Background(), gid, true)
						_, _ = utils.SendReply(clients[0], gid, "🔒 *Scheduled Close:* Group is now closed (admins only).")
					}
				}
			}
		}
	}()
}
