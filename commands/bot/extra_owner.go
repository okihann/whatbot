package botcmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	cmds := []types.Command{
		{Name: "broadcast", Aliases: []string{"bcgc"}, Category: "owner", OwnerOnly: true, Description: "Broadcast a text announcement to all joined groups. Usage: .broadcast <text>", Execute: cmdBroadcast},
		{Name: "react", Category: "owner", OwnerOnly: true, Description: "React to a replied message with an emoji. Usage: .react 🔥", Execute: cmdReact},
		{Name: "block", Category: "owner", OwnerOnly: true, Description: "Block a WhatsApp user. Usage: .block @user|phone", Execute: cmdBlockAction(events.BlocklistChangeActionBlock)},
		{Name: "unblock", Category: "owner", OwnerOnly: true, Description: "Unblock a WhatsApp user. Usage: .unblock @user|phone", Execute: cmdBlockAction(events.BlocklistChangeActionUnblock)},
		{Name: "listblock", Category: "owner", OwnerOnly: true, Description: "List all blocked WhatsApp contacts.", Execute: cmdListBlock},
		{Name: "kirim", Category: "owner", OwnerOnly: true, Description: "Send a text message directly to a phone/JID. Usage: .kirim <phone> <message>", Execute: cmdKirim},
		{Name: "public", Category: "owner", OwnerOnly: true, Description: "Disable self-mode so everyone can use the bot.", Execute: cmdPublicMode},
		{Name: "addpoint", Aliases: []string{"addbalance"}, Category: "owner", OwnerOnly: true, Description: "Add coins to a user. Usage: .addpoint <phone|@user> <amount>", Execute: cmdAdjustCoins(1)},
		{Name: "delpoint", Aliases: []string{"delbalance"}, Category: "owner", OwnerOnly: true, Description: "Subtract coins from a user. Usage: .delpoint <phone|@user> <amount>", Execute: cmdAdjustCoins(-1)},
		{Name: "resetpoint", Category: "owner", OwnerOnly: true, Description: "Reset a user's coin balance to 0. Usage: .resetpoint <phone|@user>", Execute: cmdResetCoins},
		{Name: "addexp", Category: "owner", OwnerOnly: true, Description: "Add XP to a user. Usage: .addexp <phone|@user> <xp>", Execute: cmdAdjustXP(1)},
		{Name: "delexp", Category: "owner", OwnerOnly: true, Description: "Subtract XP from a user. Usage: .delexp <phone|@user> <xp>", Execute: cmdAdjustXP(-1)},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdBroadcast(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "📢 Usage: `.broadcast <message>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	groups, err := client.GetJoinedGroups(ctx)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch joined groups: "+err.Error())
		return
	}
	sent := 0
	for _, g := range groups {
		if _, err := utils.SendReply(client, g.JID, "📢 *Broadcast*\n\n"+text); err == nil {
			sent++
		}
		time.Sleep(600 * time.Millisecond)
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Broadcast delivered to `%d/%d` groups.", sent, len(groups)))
}

func cmdReact(client *whatsmeow.Client, msg *events.Message, args []string) {
	emoji := strings.TrimSpace(strings.Join(args, ""))
	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || ctxInfo.GetStanzaID() == "" {
		utils.SendReply(client, msg.Info.Chat, "😀 Reply to a message with `.react 🔥`")
		return
	}
	var senderJID waTypes.JID
	if p := ctxInfo.GetParticipant(); p != "" {
		senderJID, _ = waTypes.ParseJID(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.SendMessage(ctx, msg.Info.Chat, client.BuildReaction(msg.Info.Chat, senderJID, ctxInfo.GetStanzaID(), emoji))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Reaction failed: "+err.Error())
	}
}

func resolveOwnerCmdTarget(client *whatsmeow.Client, msg *events.Message, raw string) string {
	if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil {
		if len(ctxInfo.GetMentionedJID()) > 0 {
			if j, err := waTypes.ParseJID(ctxInfo.GetMentionedJID()[0]); err == nil {
				return utils.ResolvePhoneNumber(client, j, waTypes.EmptyJID)
			}
		}
		if p := ctxInfo.GetParticipant(); p != "" {
			if j, err := waTypes.ParseJID(p); err == nil {
				return utils.ResolvePhoneNumber(client, j, waTypes.EmptyJID)
			}
		}
	}
	return utils.NormalizeTarget(raw)
}

func cmdBlockAction(action events.BlocklistChangeAction) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		target := resolveOwnerCmdTarget(client, msg, strings.Join(args, ""))
		if target == "" {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.block <phone|@user>` or `.unblock <phone|@user>`")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.UpdateBlocklist(ctx, waTypes.NewJID(target, waTypes.DefaultUserServer), action, "")
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Blocklist update failed: "+err.Error())
			return
		}
		if action == events.BlocklistChangeActionBlock {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🚫 Blocked `%s`.", target))
		} else {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Unblocked `%s`.", target))
		}
	}
}

func cmdListBlock(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bl, err := client.GetBlocklist(ctx, "")
	if err != nil || bl == nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not load blocklist.")
		return
	}
	if len(bl.Items) == 0 {
		utils.SendReply(client, msg.Info.Chat, "✅ Blocklist is empty.")
		return
	}
	var lines []string
	for i, item := range bl.Items {
		user := item.PN.ToNonAD().User
		if user == "" {
			user = item.LID.ToNonAD().User
		}
		lines = append(lines, fmt.Sprintf("%d. `%s`", i+1, user))
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🚫 *Blocked Contacts (%d)*\n\n%s", len(lines), strings.Join(lines, "\n")))
}

func cmdKirim(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.SplitN(strings.TrimSpace(strings.Join(args, " ")), " ", 2)
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "✉️ Usage: `.kirim <phone_or_jid> <message>`")
		return
	}
	rawTarget := strings.TrimSpace(parts[0])
	var targetJID waTypes.JID
	if strings.Contains(rawTarget, "@") {
		j, err := waTypes.ParseJID(rawTarget)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid JID.")
			return
		}
		targetJID = j
	} else {
		num := utils.NormalizeTarget(rawTarget)
		if num == "" {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid phone number.")
			return
		}
		targetJID = waTypes.NewJID(num, waTypes.DefaultUserServer)
	}
	if _, err := utils.SendReply(client, targetJID, strings.TrimSpace(parts[1])); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed sending message: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Message sent to `"+targetJID.String()+"`.")
}

func cmdPublicMode(client *whatsmeow.Client, msg *events.Message, args []string) {
	cmdToggleTenantBool("self")(client, msg, []string{"off"})
}

func cmdAdjustCoins(sign int64) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		fields := strings.Fields(strings.Join(args, " "))
		if len(fields) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.addpoint <phone|@user> <amount>` / `.delpoint <phone|@user> <amount>`")
			return
		}
		target := resolveOwnerCmdTarget(client, msg, fields[0])
		amt, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
		if target == "" || err != nil || amt <= 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid target or amount.")
			return
		}
		store := db.GetBotStore()
		if store == nil {
			return
		}
		newBal, _ := store.AddBalance(target, sign*amt, "Owner adjustment")
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💰 Updated balance for `%s`: `%+d` → *%d* coins.", target, sign*amt, newBal))
	}
}

func cmdResetCoins(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := resolveOwnerCmdTarget(client, msg, strings.Join(args, ""))
	if target == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.resetpoint <phone|@user>`")
		return
	}
	store := db.GetBotStore()
	if store == nil {
		return
	}
	u, _ := store.GetUser(target)
	u.Balance = 0
	_ = store.SaveUser(u)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗑️ Reset balance for `%s` to `0` coins.", target))
}

func cmdAdjustXP(sign int64) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		fields := strings.Fields(strings.Join(args, " "))
		if len(fields) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.addexp <phone|@user> <xp>` / `.delexp <phone|@user> <xp>`")
			return
		}
		target := resolveOwnerCmdTarget(client, msg, fields[0])
		xp, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
		if target == "" || err != nil || xp <= 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid target or XP.")
			return
		}
		store := db.GetBotStore()
		if store == nil {
			return
		}
		u, _ := store.GetUser(target)
		u.XP += sign * xp
		if u.XP < 0 {
			u.XP = 0
		}
		u.Level = int(u.XP/250) + 1
		_ = store.SaveUser(u)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⭐ Updated XP for `%s`: `%+d` → *%d XP* (Lv.%d).", target, sign*xp, u.XP, u.Level))
	}
}
