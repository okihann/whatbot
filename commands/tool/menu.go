package tool

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"bot/handlers"
	"bot/session"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	menuSessionType = "menu"
	menuPageSize    = 3
)

type menuState struct {
	Categories []string
	Page       int
}

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "menu",
		Description: "Category menu with tappable buttons.",
		Category:    "main",
		Execute:     executeMenu,
	})
	handlers.RegisterInteractiveSessionHandler(handleMenuInteraction)
}

func executeMenu(client *whatsmeow.Client, msg *events.Message, args []string) {
	cats := menuCategories()
	if len(cats) == 0 {
		utils.SendReply(client, msg.Info.Chat, "No commands registered yet.")
		return
	}
	sendMenuPage(client, msg, &menuState{Categories: cats})
}

func sendMenuPage(client *whatsmeow.Client, msg *events.Message, st *menuState) {
	start := st.Page * menuPageSize
	if start >= len(st.Categories) {
		utils.SendReply(client, msg.Info.Chat, "No more categories.")
		return
	}

	remaining := len(st.Categories) - start
	show := menuPageSize
	if remaining > menuPageSize {
		show = menuPageSize - 1 // reserve the last slot for "More"
	}

	var buttons []utils.QuickReply
	for _, c := range st.Categories[start : start+show] {
		buttons = append(buttons, utils.QuickReply{ID: "cat:" + c, Text: categoryLabel(c)})
	}
	if remaining > menuPageSize {
		buttons = append(buttons, utils.QuickReply{
			ID:   fmt.Sprintf("menu:page:%d", st.Page+1),
			Text: "More ▸",
		})
	}

	body := "Tap a category, or reply with its number:\n\n" + utils.RenderButtonsAsText(buttons)
	title := "📋 Main Menu"
	if st.Page > 0 {
		title = fmt.Sprintf("📋 Main Menu (page %d)", st.Page+1)
	}

	resp, err := utils.SendButtons(client, msg.Info.Chat, title, body, "Auto-closes in 30s", buttons)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, title+"\n\n"+body)
		return
	}

	session.StoreInteractiveSession(resp.ID, &session.InteractiveSession{
		Type:   menuSessionType,
		UserID: msg.Info.Sender.ToNonAD().String(),
		Data:   st,
	})
}

func handleMenuInteraction(client *whatsmeow.Client, msg *events.Message) bool {
	if id := utils.ExtractButtonID(msg.Message); id != "" {
		switch {
		case strings.HasPrefix(id, "cat:"):
			sendCategoryMenu(client, msg, strings.TrimPrefix(id, "cat:"))
			return true
		case strings.HasPrefix(id, "menu:page:"):
			page, err := strconv.Atoi(strings.TrimPrefix(id, "menu:page:"))
			if err != nil {
				return true
			}
			// Categories are derived from the registry, so rebuild rather than
			// depending on the button message still having a live session.
			sendMenuPage(client, msg, &menuState{Categories: menuCategories(), Page: page})
			return true
		}
	}

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

	st, ok := sess.Data.(*menuState)
	if !ok {
		return false
	}

	choice, err := strconv.Atoi(strings.TrimSpace(utils.GetTextFromMessage(msg.Message)))
	if err != nil || choice < 1 {
		return false
	}

	start := st.Page * menuPageSize
	remaining := len(st.Categories) - start
	show := menuPageSize
	if remaining > menuPageSize {
		show = menuPageSize - 1
	}
	if choice > show {
		return false
	}

	session.ClearInteractiveSession(ctxInfo.GetStanzaID())
	sendCategoryMenu(client, msg, st.Categories[start+choice-1])
	return true
}

func sendCategoryMenu(client *whatsmeow.Client, msg *events.Message, category string) {
	var names []string
	for _, c := range handlers.Commands {
		if c.Category == category {
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Category '%s' has no commands.", category))
		return
	}
	sort.Strings(names)

	var b strings.Builder
	fmt.Fprintf(&b, "╭─「 %s %s Menu 」\n", categoryEmoji(category), handlers.Title(category))
	for _, n := range names {
		fmt.Fprintf(&b, "│ ◦ /%s\n", n)
	}
	b.WriteString("╰────")
	utils.SendReply(client, msg.Info.Chat, b.String())
}

// menuCategories lists user-facing categories in a stable, friendly order.
func menuCategories() []string {
	preferred := []string{"downloader", "search", "tools", "media", "anime"}

	seen := map[string]bool{}
	for _, c := range handlers.Commands {
		if c.Category != "" && c.Category != "owner" && c.Category != "main" {
			seen[c.Category] = true
		}
	}

	var out []string
	for _, c := range preferred {
		if seen[c] {
			out = append(out, c)
			delete(seen, c)
		}
	}

	rest := make([]string, 0, len(seen))
	for c := range seen {
		rest = append(rest, c)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func categoryLabel(category string) string {
	return categoryEmoji(category) + " " + handlers.Title(category)
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
