package tool

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"bot/config"
	"bot/handlers"
	"bot/session"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

const menuSessionType = "menu"

type menuState struct {
	Categories []string
	IncludeOwn bool
}

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "help",
		Description: "Shows the interactive command menu. Usage: `/help [category|command|all]`",
		Category:    "main",
		Execute:     executeMenu,
	})
	handlers.RegisterCommand(types.Command{
		Name:        "menu",
		Description: "Alias for /help.",
		Category:    "main",
		Execute:     executeMenu,
	})
	handlers.RegisterInteractiveSessionHandler(handleMenuInteraction)
}

func executeMenu(client *whatsmeow.Client, msg *events.Message, args []string) {
	conf, _ := config.LoadConfig()
	prefix := "/"
	botName := "Whatbot"
	if conf != nil {
		if conf.Settings.Prefix != "" {
			prefix = conf.Settings.Prefix
		}
		if conf.Settings.BotName != "" {
			botName = conf.Settings.BotName
		}
	}

	isOwner := utils.IsOwner(client, msg, conf)
	cats := menuCategories(isOwner)
	if len(cats) == 0 {
		utils.SendReply(client, msg.Info.Chat, "No commands registered yet.")
		return
	}

	query := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	query = strings.TrimPrefix(query, prefix)

	if query != "" {
		if query == "all" {
			utils.SendReply(client, msg.Info.Chat, renderAllCommands(prefix, cats))
			return
		}
		// Match a category name (exact or prefix).
		if cat, ok := matchCategory(cats, query); ok {
			utils.SendReply(client, msg.Info.Chat, renderCategoryMenu(prefix, cat))
			return
		}
		// Match a specific command name.
		if cmd, ok := handlers.Commands[query]; ok {
			if cmd.Category == "owner" && !isOwner {
				utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚠️ Category or command `%s` not found.", query))
				return
			}
			utils.SendReply(client, msg.Info.Chat, renderCommandDetail(prefix, cmd))
			return
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚠️ Unknown category or command `%s`.\nUse `%shelp` to view all categories or `%shelp all` to list all commands.", query, prefix, prefix))
		return
	}

	// Main interactive menu (rendered as clean formatted text so it works
	// identically on Android, iOS, Desktop, and Web).
	userName := strings.TrimSpace(msg.Info.PushName)
	if userName == "" {
		userName = "there"
	}

	text := renderMainMenu(botName, prefix, userName, cats)
	resp, err := utils.SendReply(client, msg.Info.Chat, text)
	if err != nil {
		return
	}

	session.StoreInteractiveSession(resp.ID, &session.InteractiveSession{
		Type:   menuSessionType,
		UserID: msg.Info.Sender.ToNonAD().String(),
		Data: &menuState{
			Categories: cats,
			IncludeOwn: isOwner,
		},
	})
}

func handleMenuInteraction(client *whatsmeow.Client, msg *events.Message) bool {
	conf, _ := config.LoadConfig()
	prefix := "/"
	if conf != nil && conf.Settings.Prefix != "" {
		prefix = conf.Settings.Prefix
	}

	// Support legacy button taps if any older menu message is still tapped.
	if id := utils.ExtractButtonID(msg.Message); id != "" {
		if strings.HasPrefix(id, "cat:") {
			cat := strings.TrimPrefix(id, "cat:")
			if cat == "owner" && !utils.IsOwner(client, msg, conf) {
				return true
			}
			utils.SendReply(client, msg.Info.Chat, renderCategoryMenu(prefix, cat))
			return true
		}
	}

	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || ctxInfo.StanzaID == nil {
		return false
	}

	targetID := ctxInfo.GetStanzaID()
	sess, ok := session.GetInteractiveSession(targetID)
	if !ok || sess.Type != menuSessionType {
		return false
	}

	st, ok := sess.Data.(*menuState)
	if !ok {
		return false
	}

	replyText := strings.ToLower(strings.TrimSpace(utils.GetTextFromMessage(msg.Message)))
	if replyText == "" {
		return false
	}

	// Allow replying "all" or "0" to list every command.
	if replyText == "all" || replyText == "0" {
		session.ResetSessionTimer(targetID)
		utils.SendReply(client, msg.Info.Chat, renderAllCommands(prefix, st.Categories))
		return true
	}

	// Allow replying with a category name directly.
	if cat, found := matchCategory(st.Categories, replyText); found {
		session.ResetSessionTimer(targetID)
		utils.SendReply(client, msg.Info.Chat, renderCategoryMenu(prefix, cat))
		return true
	}

	choice, err := strconv.Atoi(replyText)
	if err != nil || choice < 1 || choice > len(st.Categories) {
		return false
	}

	// Keep the session alive (refresh timer) so users can inspect multiple
	// categories from the same menu message.
	session.ResetSessionTimer(targetID)
	chosen := st.Categories[choice-1]
	if chosen == "owner" && !utils.IsOwner(client, msg, conf) {
		utils.SendReply(client, msg.Info.Chat, "⛔ Access Denied: Owner commands are restricted to the bot owner.")
		return true
	}
	utils.SendReply(client, msg.Info.Chat, renderCategoryMenu(prefix, chosen))
	return true
}

func renderMainMenu(botName, prefix, userName string, cats []string) string {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	uptime := formatMenuUptime(time.Since(handlers.BotStartTime()))

	totalCmds := 0
	catCounts := make(map[string]int, len(cats))
	allowed := make(map[string]bool, len(cats))
	for _, c := range cats {
		allowed[c] = true
	}
	for _, cmd := range handlers.PrimaryCommands {
		if allowed[cmd.Category] {
			catCounts[cmd.Category]++
			totalCmds++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "╭─〔 *%s* 〕\n", botName)
	fmt.Fprintf(&b, "│ User: %s\n", userName)
	fmt.Fprintf(&b, "│ Runtime: %s\n", uptime)
	fmt.Fprintf(&b, "│ Prefix: `%s` or `.` / `/`\n", prefix)
	fmt.Fprintf(&b, "│ Total Cmd: %d\n", totalCmds)
	fmt.Fprintf(&b, "│ Platform: %s • %s\n", runtime.GOOS, now.Format("02/01 15:04"))
	b.WriteString("╰──────────\n\n")

	b.WriteString("╭─〔 *CATEGORIES* 〕\n")
	for i, c := range cats {
		fmt.Fprintf(&b, "│ *%d.* %s %s (%d)\n", i+1, categoryEmoji(c), formatCategoryTitle(c), catCounts[c])
	}
	b.WriteString("│ *0.* 📜 Show All Commands\n")
	b.WriteString("╰──────────\n\n")
	fmt.Fprintf(&b, "Reply with *1-%d* or *0*, or run `%smenu <category>`, `%smenu <cmd>`, or `%smenu all`.", len(cats), prefix, prefix, prefix)

	return b.String()
}

func renderCategoryMenu(prefix, category string) string {
	var cmds []types.Command
	for _, c := range handlers.PrimaryCommands {
		if c.Category == category {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return fmt.Sprintf("Category '%s' has no commands.", category)
	}
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name < cmds[j].Name
	})

	var b strings.Builder
	fmt.Fprintf(&b, "╭─〔 *%s %s* 〕\n", categoryEmoji(category), strings.ToUpper(formatCategoryTitle(category)))
	for _, c := range cmds {
		if desc := strings.TrimSpace(c.Description); desc != "" {
			fmt.Fprintf(&b, "│ `%s%s` — %s\n", prefix, c.Name, desc)
		} else {
			fmt.Fprintf(&b, "│ `%s%s`\n", prefix, c.Name)
		}
	}
	b.WriteString("╰──────────")
	return b.String()
}

func renderAllCommands(prefix string, cats []string) string {
	var b strings.Builder
	b.WriteString("📜 *All Commands*\n\n")
	for i, c := range cats {
		if i > 0 {
			b.WriteString("\n\n")
		}
		var cmds []types.Command
		for _, cmd := range handlers.PrimaryCommands {
			if cmd.Category == c {
				cmds = append(cmds, cmd)
			}
		}
		sort.Slice(cmds, func(a, j int) bool { return cmds[a].Name < cmds[j].Name })
		fmt.Fprintf(&b, "╭─〔 *%s %s* 〕\n", categoryEmoji(c), strings.ToUpper(formatCategoryTitle(c)))
		for _, cmd := range cmds {
			fmt.Fprintf(&b, "│ `%s%s`\n", prefix, cmd.Name)
		}
		b.WriteString("╰──────────")
	}
	return b.String()
}

func renderCommandDetail(prefix string, cmd types.Command) string {
	desc := strings.TrimSpace(cmd.Description)
	if desc == "" {
		desc = "No description available."
	}
	extra := ""
	if len(cmd.Aliases) > 0 {
		extra += fmt.Sprintf("\n• *Aliases:* `%s`", strings.Join(cmd.Aliases, "`, `"))
	}
	if cmd.Usage != "" {
		extra += fmt.Sprintf("\n• *Usage:* `%s`", cmd.Usage)
	}
	return fmt.Sprintf(
		"ℹ️ *Command Info*\n\n• *Command:* `%s%s`\n• *Category:* %s %s\n• *Description:* %s%s",
		prefix, cmd.Name, categoryEmoji(cmd.Category), formatCategoryTitle(cmd.Category), desc, extra,
	)
}

func formatCategoryTitle(cat string) string {
	parts := strings.Split(strings.ReplaceAll(cat, "_", " "), " ")
	for i, p := range parts {
		parts[i] = handlers.Title(p)
	}
	return strings.Join(parts, " ")
}

func matchCategory(cats []string, query string) (string, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	qNorm := strings.ReplaceAll(q, " ", "_")
	for _, c := range cats {
		if strings.ToLower(c) == q || strings.ToLower(c) == qNorm {
			return c, true
		}
	}
	for _, c := range cats {
		if (strings.HasPrefix(strings.ToLower(c), q) || strings.HasPrefix(strings.ToLower(c), qNorm)) && len(q) >= 3 {
			return c, true
		}
	}
	return "", false
}

// menuCategories lists categories in a stable, friendly order, including
// "owner" only when includeOwner is true.
func menuCategories(includeOwner bool) []string {
	preferred := []string{
		"general", "main", "profile", "bot",
		"group_admin", "group_security",
		"downloader", "youtube", "music",
		"sticker", "media", "audiofx",
		"tools", "search",
		"animanga", "anime", "waifu",
		"fun", "game", "minigame",
	}
	if includeOwner {
		preferred = append(preferred, "owner")
	}

	seen := map[string]bool{}
	for _, c := range handlers.PrimaryCommands {
		if c.Category == "" {
			continue
		}
		if c.Category == "owner" && !includeOwner {
			continue
		}
		seen[c.Category] = true
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

func formatMenuUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if days > 0 {
		return fmt.Sprintf("%dd %02d:%02d:%02d", days, h, m, s)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func categoryEmoji(category string) string {
	switch strings.ToLower(category) {
	case "bot":
		return "🤖"
	case "general", "main":
		return "📋"
	case "profile":
		return "👤"
	case "group_admin":
		return "🛡️"
	case "group_security":
		return "🔒"
	case "downloader":
		return "📥"
	case "youtube":
		return "▶️"
	case "music":
		return "🎧"
	case "sticker", "media":
		return "🖼️"
	case "audiofx":
		return "🎛️"
	case "tools":
		return "🛠️"
	case "search":
		return "🔎"
	case "animanga", "anime":
		return "🌸"
	case "waifu":
		return "💐"
	case "fun":
		return "😂"
	case "game":
		return "🎮"
	case "minigame":
		return "🕹️"
	case "owner":
		return "👑"
	default:
		return "📁"
	}
}
