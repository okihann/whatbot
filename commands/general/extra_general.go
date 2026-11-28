package general

import (
	"fmt"
	"strconv"
	"strings"

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
	cmds := []types.Command{
		{Name: "ai", Aliases: []string{"ask", "gpt"}, Category: "general", Description: "Ask the configured AI assistant anything. Usage: .ai <question>", Execute: cmdAskAI},
		{Name: "transfer", Aliases: []string{"tf"}, Category: "general", Description: "Transfer coins to another user. Usage: .transfer @user <amount>", Execute: cmdTransfer},
		{Name: "report", Category: "general", Description: "Send a bug report or suggestion to the bot owner. Usage: .report <message>", Execute: cmdReport},
		{Name: "changelog", Category: "general", Description: "Show recent NopeBot updates and highlights.", Execute: cmdChangelog},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdAskAI(client *whatsmeow.Client, msg *events.Message, args []string) {
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		utils.SendReply(client, msg.Info.Chat, "🤖 Usage: `.ai <question>`")
		return
	}
	if len(ai.AppConfig.Providers) == 0 {
		utils.SendReply(client, msg.Info.Chat, "⚠️ No AI provider configured in `.env` yet (set `OPENAI_BASE_URL` / `GEMINI_BASE_URL` etc.).")
		return
	}
	var modelName string
	for _, p := range ai.AppConfig.Providers {
		if len(p.APIKeys) > 0 {
			modelName = p.Name + "/gpt-4o-mini"
			if strings.EqualFold(p.Name, "gemini") {
				modelName = p.Name + "/gemini-2.0-flash"
			}
			break
		}
	}
	if modelName == "" {
		utils.SendReply(client, msg.Info.Chat, "⚠️ No AI API keys found in `.env`.")
		return
	}

	sess := &types.AISession{
		Model: modelName,
		Messages: []types.ChatMessage{
			{Role: "system", Content: "You are NopeBot, a concise and helpful WhatsApp assistant."},
			{Role: "user", Content: prompt},
		},
	}
	var reasoning, searchOut strings.Builder
	resp, err := ai.GenerateAIResponseWithTrace(sess, &reasoning, &searchOut)
	if err != nil || strings.TrimSpace(resp.Content) == "" {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ AI request failed: %v", err))
		return
	}
	utils.SendReply(client, msg.Info.Chat, "🤖 *NopeBot AI*\n\n"+strings.TrimSpace(resp.Content))
}

func cmdTransfer(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, "💸 Usage: `.transfer @user <amount>` or `.transfer 628xxx <amount>`")
		return
	}
	var targetPhone string
	if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil && len(ctxInfo.GetMentionedJID()) > 0 {
		if j, err := waTypes.ParseJID(ctxInfo.GetMentionedJID()[0]); err == nil {
			targetPhone = utils.ResolvePhoneNumber(client, j, waTypes.EmptyJID)
		}
	}
	if targetPhone == "" {
		targetPhone = utils.NormalizeTarget(fields[0])
	}
	amt, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if targetPhone == "" || err != nil || amt <= 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Invalid target or amount. Usage: `.transfer @user 500`")
		return
	}

	sender := userKey(client, msg)
	if sender == targetPhone {
		utils.SendReply(client, msg.Info.Chat, "😅 You cannot transfer coins to yourself.")
		return
	}
	store := db.GetBotStore()
	if store == nil {
		return
	}
	u, err := store.GetUser(sender)
	if err != nil || u.Balance < amt {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Insufficient balance! You only have `%d` coins.", u.Balance))
		return
	}
	_, _ = store.AddBalance(sender, -amt, "Transfer to "+targetPhone)
	_, _ = store.AddBalance(targetPhone, amt, "Transfer from "+sender)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💸 *Transfer Successful!*\n• *To:* `%s`\n• *Amount:* `%d` coins", targetPhone, amt))
}

func cmdReport(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "📣 Usage: `.report <bug or suggestion>`")
		return
	}
	conf, _ := config.LoadConfig()
	sender := userKey(client, msg)
	delivered := false
	if conf != nil {
		for _, raw := range conf.Settings.OwnerJIDs {
			num := utils.NormalizeTarget(raw)
			if num == "" {
				continue
			}
			_, err := utils.SendReply(client, waTypes.NewJID(num, waTypes.DefaultUserServer),
				fmt.Sprintf("📣 *User Report*\n• *From:* `%s` (%s)\n• *Chat:* `%s`\n\n%s", sender, msg.Info.PushName, msg.Info.Chat.ToNonAD().String(), text),
			)
			if err == nil {
				delivered = true
			}
		}
	}
	if !delivered {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Owner contact is not configured yet.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "✅ Your report has been forwarded to the bot owner. Thank you!")
}

func cmdChangelog(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat,
		"📋 *NopeBot Changelog*\n\n"+
			"• 9 unified command categories with full EN/ID bilingual `.menu`\n"+
			"• Native LID WhatsApp Status posting (reshareable + My Status)\n"+
			"• Hardened Owner-Only Web UI with 6-digit WhatsApp OTP & Contacts\n"+
			"• View-Once-only `.auto-get` capture & Anti-Delete\n"+
			"• Live Real-World Market Tracker, Alerts & AI Analysis\n"+
			"• Expanded Fun, Ramalan, Trivia, Stickers, Makers, Tools & Group Store Lists",
	)
}
