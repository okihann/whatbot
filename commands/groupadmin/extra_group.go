package groupadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type groupVote struct {
	Topic   string
	Up      map[string]bool
	Down    map[string]bool
	Created time.Time
}

var (
	voteMu     sync.Mutex
	groupVotes = map[string]*groupVote{}
)

func init() {
	cmds := []types.Command{
		// Group Rules
		{Name: "rules", Category: "group", GroupOnly: true, Description: "Show the rules for this group.", Execute: cmdRules},
		{Name: "setrules", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Set custom rules for this group. Usage: .setrules <text>", Execute: cmdSetRules},
		{Name: "delrules", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Delete the custom rules for this group.", Execute: cmdDelRules},

		// Group Voting & Native Polls
		{Name: "vote", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Start a vote session in this group. Usage: .vote <topic>", Execute: cmdVoteStart},
		{Name: "upvote", Category: "group", GroupOnly: true, Description: "Vote YES (upvote) in the active group vote.", Execute: cmdVoteCast(true)},
		{Name: "devote", Category: "group", GroupOnly: true, Description: "Vote NO (devote) in the active group vote.", Execute: cmdVoteCast(false)},
		{Name: "cekvote", Category: "group", GroupOnly: true, Description: "Check live tally of the active group vote.", Execute: cmdVoteCheck},
		{Name: "delvote", Category: "group", GroupOnly: true, AdminOnly: true, Description: "End and delete the active group vote.", Execute: cmdVoteDelete},
		{Name: "poll", Category: "group", GroupOnly: true, Description: "Create a native WhatsApp Poll. Usage: .poll Question | Option 1 | Option 2", Execute: cmdPoll},

		// Store / Custom Group Lists & Order Status
		{Name: "list", Category: "group", GroupOnly: true, Description: "View group store/info list or a specific item. Usage: .list [key]", Execute: cmdGroupList},
		{Name: "addlist", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Add an item to this group's list. Usage: .addlist key | response", Execute: cmdAddGroupList},
		{Name: "updatelist", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Update an existing item in this group's list. Usage: .updatelist key | new response", Execute: cmdAddGroupList},
		{Name: "dellist", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Delete an item from this group's list. Usage: .dellist <key>", Execute: cmdDelGroupList},
		{Name: "resetlist", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Clear all items from this group's list.", Execute: cmdResetGroupList},
		{Name: "proses", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Send a formatted 'Order In Progress' status card (reply to buyer).", Execute: cmdOrderStatus("⏳ *PESANAN DIPROSES*", "Pesanan sedang diproses oleh admin. Mohon ditunggu ya!")},
		{Name: "done", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Send a formatted 'Order Completed' status card (reply to buyer).", Execute: cmdOrderStatus("✅ *PESANAN SELESAI*", "Transaksi berhasil & selesai. Terima kasih telah berbelanja!")},
		{Name: "batal", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Send a formatted 'Order Cancelled' status card (reply to buyer).", Execute: cmdOrderStatus("❌ *PESANAN DIBATALKAN*", "Pesanan telah dibatalkan oleh admin.")},

		// Utilities & Tags & Birthdays
		{Name: "listadmin", Category: "group", GroupOnly: true, Description: "List all admins of this group.", Execute: cmdListAdmin},
		{Name: "kickme", Category: "group", GroupOnly: true, BotAdmin: true, Description: "Remove yourself from this group.", Execute: cmdKickMe},
		{Name: "group", Category: "group", GroupOnly: true, AdminOnly: true, BotAdmin: true, Description: "Open or close the group chat. Usage: .group <open|close>", Execute: cmdGroupToggle},
		{Name: "ultah", Category: "group", GroupOnly: true, Description: "View saved member birthdays in this group.", Execute: cmdUltah},
		{Name: "setultah", Category: "group", GroupOnly: true, Description: "Save your birthday for this group. Usage: .setultah DD-MM", Execute: cmdSetUltah},
		{Name: "delultah", Category: "group", GroupOnly: true, Description: "Delete your saved birthday in this group.", Execute: cmdDelUltah},
		{Name: "tag", Category: "group", GroupOnly: true, Description: "Mention members in a saved custom tag group. Usage: .tag <group_name>", Execute: cmdCustomTag},
		{Name: "settag", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Create/update a custom tag group. Usage: .settag <name> @user1 @user2", Execute: cmdSetCustomTag},
		{Name: "deltag", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Delete a custom tag group. Usage: .deltag <name>", Execute: cmdDelCustomTag},
		{Name: "listtag", Category: "group", GroupOnly: true, Description: "List all custom tag groups saved in this group.", Execute: cmdListCustomTag},
		{Name: "savekontak", Category: "group", GroupOnly: true, AdminOnly: true, Description: "Export all members of this group as a .vcf vCard contacts file.", Execute: cmdSaveKontak},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func loadJSONMap(key string) map[string]string {
	out := map[string]string{}
	store := db.GetBotStore()
	if store == nil {
		return out
	}
	if val, ok, err := store.GetKV(key); err == nil && ok && val != "" {
		_ = json.Unmarshal([]byte(val), &out)
	}
	return out
}

func saveJSONMap(key string, m map[string]string) {
	store := db.GetBotStore()
	if store == nil {
		return
	}
	if raw, err := json.Marshal(m); err == nil {
		_ = store.SetKV(key, string(raw))
	}
}

func cmdRules(client *whatsmeow.Client, msg *events.Message, args []string) {
	store := db.GetBotStore()
	if store == nil {
		return
	}
	val, ok, _ := store.GetKV("rules:" + msg.Info.Chat.ToNonAD().String())
	if !ok || strings.TrimSpace(val) == "" {
		utils.SendReply(client, msg.Info.Chat, "📜 No rules configured for this group yet. Admins can set them with `.setrules <text>`.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "📜 *Group Rules*\n\n"+val)
}

func cmdSetRules(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.setrules <group rules text>`")
		return
	}
	if store := db.GetBotStore(); store != nil {
		_ = store.SetKV("rules:"+msg.Info.Chat.ToNonAD().String(), text)
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Group rules updated! View them anytime with `.rules`.")
}

func cmdDelRules(client *whatsmeow.Client, msg *events.Message, args []string) {
	if store := db.GetBotStore(); store != nil {
		_ = store.SetKV("rules:"+msg.Info.Chat.ToNonAD().String(), "")
	}
	utils.SendReply(client, msg.Info.Chat, "🗑️ Group rules cleared.")
}

func cmdVoteStart(client *whatsmeow.Client, msg *events.Message, args []string) {
	topic := strings.TrimSpace(strings.Join(args, " "))
	if topic == "" {
		utils.SendReply(client, msg.Info.Chat, "🗳️ Usage: `.vote <topic>`")
		return
	}
	gid := msg.Info.Chat.ToNonAD().String()
	voteMu.Lock()
	groupVotes[gid] = &groupVote{
		Topic:   topic,
		Up:      map[string]bool{},
		Down:    map[string]bool{},
		Created: time.Now(),
	}
	voteMu.Unlock()
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗳️ *Group Vote Started!*\n\n*Topic:* %s\n\n• `.upvote` — Vote YES 👍\n• `.devote` — Vote NO 👎\n• `.cekvote` — View tally\n• `.delvote` — End vote", topic))
}

func cmdVoteCast(isUp bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		gid := msg.Info.Chat.ToNonAD().String()
		uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
		voteMu.Lock()
		v, ok := groupVotes[gid]
		if !ok || v == nil {
			voteMu.Unlock()
			utils.SendReply(client, msg.Info.Chat, "ℹ️ No active vote in this group. Start one with `.vote <topic>`.")
			return
		}
		if isUp {
			v.Up[uid] = true
			delete(v.Down, uid)
		} else {
			v.Down[uid] = true
			delete(v.Up, uid)
		}
		up, down, topic := len(v.Up), len(v.Down), v.Topic
		voteMu.Unlock()
		choice := "YES 👍"
		if !isUp {
			choice = "NO 👎"
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗳️ Voted *%s* on _%s_\n👍 `%d` | 👎 `%d`", choice, topic, up, down))
	}
}

func cmdVoteCheck(client *whatsmeow.Client, msg *events.Message, args []string) {
	gid := msg.Info.Chat.ToNonAD().String()
	voteMu.Lock()
	v, ok := groupVotes[gid]
	if !ok || v == nil {
		voteMu.Unlock()
		utils.SendReply(client, msg.Info.Chat, "ℹ️ No active vote in this group.")
		return
	}
	up, down, topic := len(v.Up), len(v.Down), v.Topic
	voteMu.Unlock()
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗳️ *Vote Tally*\n*Topic:* %s\n\n👍 *Upvote:* `%d`\n👎 *Devote:* `%d`", topic, up, down))
}

func cmdVoteDelete(client *whatsmeow.Client, msg *events.Message, args []string) {
	gid := msg.Info.Chat.ToNonAD().String()
	voteMu.Lock()
	v, ok := groupVotes[gid]
	delete(groupVotes, gid)
	voteMu.Unlock()
	if !ok || v == nil {
		utils.SendReply(client, msg.Info.Chat, "ℹ️ No active vote to delete.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏁 *Vote Closed*\n*Topic:* %s\nFinal: 👍 `%d` vs 👎 `%d`", v.Topic, len(v.Up), len(v.Down)))
}

func cmdPoll(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.Split(strings.Join(args, " "), "|")
	if len(parts) < 3 {
		utils.SendReply(client, msg.Info.Chat, "📊 Usage: `.poll Question | Option 1 | Option 2 [| Option 3...]`")
		return
	}
	question := strings.TrimSpace(parts[0])
	var opts []string
	for _, p := range parts[1:] {
		if o := strings.TrimSpace(p); o != "" {
			opts = append(opts, o)
		}
	}
	if question == "" || len(opts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "❌ Provide a question and at least 2 options separated by `|`.")
		return
	}
	if len(opts) > 12 {
		opts = opts[:12]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := client.SendMessage(ctx, msg.Info.Chat, client.BuildPollCreation(question, opts, 1))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to create poll: "+err.Error())
	}
}

func cmdGroupList(client *whatsmeow.Client, msg *events.Message, args []string) {
	key := "glist:" + msg.Info.Chat.ToNonAD().String()
	m := loadJSONMap(key)
	q := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if q != "" {
		if val, ok := m[q]; ok {
			utils.SendReply(client, msg.Info.Chat, val)
			return
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Item `%s` not found. Run `.list` to see all items.", q))
		return
	}
	if len(m) == 0 {
		utils.SendReply(client, msg.Info.Chat, "📋 No list items saved in this group yet.\nAdmins can add items with `.addlist key | response`.")
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("📋 *Group List*\n\n")
	for i, k := range keys {
		fmt.Fprintf(&b, "%d. `%s`\n", i+1, k)
	}
	b.WriteString("\n_Type `.list <name>` to view details._")
	utils.SendReply(client, msg.Info.Chat, b.String())
}

func cmdAddGroupList(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.SplitN(strings.Join(args, " "), "|", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.addlist key | response text`")
		return
	}
	itemKey := strings.ToLower(strings.TrimSpace(parts[0]))
	itemVal := strings.TrimSpace(parts[1])
	storeKey := "glist:" + msg.Info.Chat.ToNonAD().String()
	m := loadJSONMap(storeKey)
	m[itemKey] = itemVal
	saveJSONMap(storeKey, m)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Saved list item `%s`.", itemKey))
}

func cmdDelGroupList(client *whatsmeow.Client, msg *events.Message, args []string) {
	itemKey := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if itemKey == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.dellist <key>`")
		return
	}
	storeKey := "glist:" + msg.Info.Chat.ToNonAD().String()
	m := loadJSONMap(storeKey)
	if _, ok := m[itemKey]; !ok {
		utils.SendReply(client, msg.Info.Chat, "❌ Item not found.")
		return
	}
	delete(m, itemKey)
	saveJSONMap(storeKey, m)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗑️ Deleted list item `%s`.", itemKey))
}

func cmdResetGroupList(client *whatsmeow.Client, msg *events.Message, args []string) {
	saveJSONMap("glist:"+msg.Info.Chat.ToNonAD().String(), map[string]string{})
	utils.SendReply(client, msg.Info.Chat, "🗑️ Cleared all list items in this group.")
}

func cmdOrderStatus(title, defaultNote string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		note := strings.TrimSpace(strings.Join(args, " "))
		if note == "" {
			note = defaultNote
		}
		buyer := ""
		var mentions []string
		if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil && ctxInfo.GetParticipant() != "" {
			if j, err := waTypes.ParseJID(ctxInfo.GetParticipant()); err == nil {
				buyer = "\n• *Pemesan:* @" + j.ToNonAD().User
				mentions = append(mentions, j.ToNonAD().String())
			}
		}
		loc, _ := time.LoadLocation("Asia/Jakarta")
		if loc == nil {
			loc = time.UTC
		}
		text := fmt.Sprintf("%s\n\n• *Waktu:* %s WIB%s\n• *Catatan:* %s", title, time.Now().In(loc).Format("02 Jan 2006 15:04"), buyer, note)
		if len(mentions) > 0 {
			utils.SendMentionReply(client, msg.Info.Chat, text, mentions)
			return
		}
		utils.SendReply(client, msg.Info.Chat, text)
	}
}

func cmdListAdmin(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil || gi == nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not load group info.")
		return
	}
	var mentions []string
	var lines []string
	for _, p := range gi.Participants {
		if p.IsAdmin || p.IsSuperAdmin {
			role := "Admin"
			if p.IsSuperAdmin {
				role = "Creator"
			}
			j := p.JID.ToNonAD()
			mentions = append(mentions, j.String())
			lines = append(lines, fmt.Sprintf("• @%s (%s)", j.User, role))
		}
	}
	utils.SendMentionReply(client, msg.Info.Chat, fmt.Sprintf("🛡️ *Group Admins (%d)*\n\n%s", len(lines), strings.Join(lines, "\n")), mentions)
}

func cmdKickMe(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.UpdateGroupParticipants(ctx, msg.Info.Chat, []waTypes.JID{msg.Info.Sender.ToNonAD()}, whatsmeow.ParticipantChangeRemove)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not remove you (you might be group creator): "+err.Error())
	}
}

func cmdGroupToggle(client *whatsmeow.Client, msg *events.Message, args []string) {
	mode := strings.ToLower(strings.TrimSpace(strings.Join(args, "")))
	switch mode {
	case "open", "buka":
		cmdOpenClose(false)(client, msg, nil)
	case "close", "tutup":
		cmdOpenClose(true)(client, msg, nil)
	default:
		utils.SendReply(client, msg.Info.Chat, "Usage: `.group open` or `.group close`")
	}
}

func cmdUltah(client *whatsmeow.Client, msg *events.Message, args []string) {
	m := loadJSONMap("ultah:" + msg.Info.Chat.ToNonAD().String())
	if len(m) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🎂 No birthdays saved yet. Add yours with `.setultah DD-MM`!")
		return
	}
	var lines []string
	for uid, val := range m {
		lines = append(lines, fmt.Sprintf("• `%s` — %s", uid, val))
	}
	sort.Strings(lines)
	utils.SendReply(client, msg.Info.Chat, "🎂 *Group Birthdays*\n\n"+strings.Join(lines, "\n"))
}

func cmdSetUltah(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "🎂 Usage: `.setultah DD-MM` (e.g. `.setultah 17-08`)")
		return
	}
	key := "ultah:" + msg.Info.Chat.ToNonAD().String()
	m := loadJSONMap(key)
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	label := raw
	if msg.Info.PushName != "" {
		label = fmt.Sprintf("%s (%s)", raw, msg.Info.PushName)
	}
	m[uid] = label
	saveJSONMap(key, m)
	utils.SendReply(client, msg.Info.Chat, "✅ Birthday saved: `"+label+"`")
}

func cmdDelUltah(client *whatsmeow.Client, msg *events.Message, args []string) {
	key := "ultah:" + msg.Info.Chat.ToNonAD().String()
	m := loadJSONMap(key)
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	delete(m, uid)
	saveJSONMap(key, m)
	utils.SendReply(client, msg.Info.Chat, "🗑️ Your birthday entry has been removed.")
}

func cmdSetCustomTag(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🏷️ Usage: `.settag <name> @user1 @user2`")
		return
	}
	tagName := strings.ToLower(fields[0])
	var jids []string
	if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil {
		jids = append(jids, ctxInfo.GetMentionedJID()...)
	}
	if len(jids) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Mention at least one member to add to tag `"+tagName+"`.")
		return
	}
	key := "gtag:" + msg.Info.Chat.ToNonAD().String()
	m := loadJSONMap(key)
	m[tagName] = strings.Join(jids, ",")
	saveJSONMap(key, m)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Saved tag `%s` with `%d` member(s). Call it with `.tag %s`.", tagName, len(jids), tagName))
}

func cmdCustomTag(client *whatsmeow.Client, msg *events.Message, args []string) {
	tagName := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if tagName == "" {
		utils.SendReply(client, msg.Info.Chat, "🏷️ Usage: `.tag <name>` (see `.listtag`)")
		return
	}
	m := loadJSONMap("gtag:" + msg.Info.Chat.ToNonAD().String())
	raw, ok := m[tagName]
	if !ok || raw == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ Tag `"+tagName+"` not found.")
		return
	}
	mentions := strings.Split(raw, ",")
	var tags []string
	for _, jStr := range mentions {
		if j, err := waTypes.ParseJID(jStr); err == nil {
			tags = append(tags, "@"+j.ToNonAD().User)
		}
	}
	utils.SendMentionReply(client, msg.Info.Chat, fmt.Sprintf("🏷️ *Tag: %s*\n%s", tagName, strings.Join(tags, " ")), mentions)
}

func cmdDelCustomTag(client *whatsmeow.Client, msg *events.Message, args []string) {
	tagName := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if tagName == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.deltag <name>`")
		return
	}
	key := "gtag:" + msg.Info.Chat.ToNonAD().String()
	m := loadJSONMap(key)
	delete(m, tagName)
	saveJSONMap(key, m)
	utils.SendReply(client, msg.Info.Chat, "🗑️ Deleted tag `"+tagName+"`.")
}

func cmdListCustomTag(client *whatsmeow.Client, msg *events.Message, args []string) {
	m := loadJSONMap("gtag:" + msg.Info.Chat.ToNonAD().String())
	if len(m) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🏷️ No custom tags in this group yet.")
		return
	}
	var lines []string
	for k, v := range m {
		lines = append(lines, fmt.Sprintf("• `%s` (%d members)", k, len(strings.Split(v, ","))))
	}
	sort.Strings(lines)
	utils.SendReply(client, msg.Info.Chat, "🏷️ *Custom Tag Groups*\n\n"+strings.Join(lines, "\n"))
}

func cmdSaveKontak(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil || gi == nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch group participants.")
		return
	}
	var vcf strings.Builder
	count := 0
	for i, p := range gi.Participants {
		num := utils.ResolvePhoneNumber(client, p.JID, p.PhoneNumber)
		if num == "" {
			continue
		}
		count++
		fmt.Fprintf(&vcf, "BEGIN:VCARD\nVERSION:3.0\nFN:%s #%d\nTEL;TYPE=CELL:+%s\nEND:VCARD\n", gi.Name, i+1, num)
	}
	if count == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ No phone numbers available to export.")
		return
	}
	fileName := utils.SanitizeFilename(gi.Name) + "_contacts.vcf"
	_, _ = utils.SendDocumentData(ctx, client, msg.Info.Chat, []byte(vcf.String()), "text/vcard", fileName, fmt.Sprintf("📇 Exported `%d` group contacts.", count))
}
