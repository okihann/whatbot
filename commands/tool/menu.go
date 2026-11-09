package tool

import (
	"bot/handlers"
	"bot/session"
	"bot/types"
	"bot/utils"
	"fmt"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

const menuSessionType = "menu"

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "menu",
		Description: "Interactive menu with tappable category buttons.",
		Category:    "main",
		Execute:     executeMenu,
	})
	handlers.RegisterInteractiveSessionHandler(handleMenuInteraction)
}

// executeMenu sends an interactive message with up to 3 category buttons and
// registers a session so we can handle both button taps and numeric replies.
func executeMenu(client *whatsmeow.Client, msg *events.Message, args []string) {
	categories := listCategories()
	if len(categories) == 0 {
		utils.SendReply(client, msg.Info.Chat, "No commands registered yet.")
		return
	}

	shown := categories
	if len(shown) > 3 {
		shown = shown[:3]
	}

	buttons := make([]utils.QuickReply, 0, len(shown))
	for _, c := range shown {
		buttons = append(buttons, utils.QuickReply{
			ID:   "cat:" + c,
			Text: handlers.Title(c),
		})
	}

	body := "Tap a category to see its commands.\n\nReply with a number as a fallback:\n" + utils.RenderButtonsAsText(buttons)

	resp, err := utils.SendButtons(client, msg.Info.Chat, "📋 Main Menu", body, "Auto-closes in 30s", buttons)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "📋 *Main Menu*\n\n"+body)
		return
	}

	session.StoreInteractiveSession(resp.ID, &session.InteractiveSession{
		Type:   menuSessionType,
		UserID: msg.Info.Sender.ToNonAD().String(),
		Data:   shown,
	})
}

// handleMenuInteraction resolves both button taps and plain numeric replies.
func handleMenuInteraction(client *whatsmeow.Client, msg *events.Message) bool {
	// 1. Button tap: arrives as an interactive/button response.
	if id := utils.ExtractButtonID(msg.Message); strings.HasPrefix(id, "cat:") {
		cat := strings.TrimPrefix(id, "cat:")
		sendCategoryMenu(client, msg, cat)
		return true
	}

	// 2. Numeric reply to the menu message.
	ctxInfo := msg.Message.GetExtendedTextMessage().GetContextInfo()
	if ctxInfo == nil || ctxInfo.StanzaID == nil {
		return false
	}
	sess, ok := session.GetInteractiveSession(ctxInfo.GetStanzaID())
	if !ok || sess.Type != menuSessionType {
		return false
	}
	if sess.UserID != msg.Info.Sender.ToNonAD().String() {
		return true
	}

	text := strings.TrimSpace(utils.GetTextFromMessage(msg.Message))
	var choice int
	if _, err := fmt.Sscanf(text, "%d", &choice); err != nil || choice < 1 {
		return false
	}
	cats, _ := sess.Data.([]string)
	if choice > len(cats) {
		return false
	}

	session.ClearInteractiveSession(ctxInfo.GetStanzaID())
	sendCategoryMenu(client, msg, cats[choice-1])
	return true
}

func sendCategoryMenu(client *whatsmeow.Client, msg *events.Message, category string) {
	var cmds []string
	for _, c := range handlers.Commands {
		if c.Category == category {
			cmds = append(cmds, c.Name)
		}
	}
	if len(cmds) == 0 {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Category '%s' has no commands.", category))
		return
	}
	sort.Strings(cmds)

	var b strings.Builder
	fmt.Fprintf(&b, "╭─「 %s %s Menu 」\n", categoryEmoji(category), handlers.Title(category))
	for _, name := range cmds {
		fmt.Fprintf(&b, "│ ◦ /%s\n", name)
	}
	b.WriteString("╰────")
	utils.SendReply(client, msg.Info.Chat, b.String())
}

func listCategories() []string {
	seen := map[string]struct{}{}
	for _, c := range handlers.Commands {
		if c.Category == "" || c.Category == "owner" {
			continue
		}
		seen[c.Category] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func categoryEmoji(category string) string {
	switch category {
	case "downloader":
		return "📥"
	case "search":
		return "🔎"
	case "tools":
		return "🛠️"
	case "anime":
		return "🌸"
	case "media":
		return "🖼️"
	case "main":
		return "📋"
	default:
		return "📁"
	}
}
