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
	"bot/i18n"
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
		Aliases:     []string{"menu"},
		Description: "Open the interactive command menu. Usage: .menu [category|command|all]",
		Category:    i18n.CatGeneral,
		Execute:     executeMenu,
	})
	handlers.RegisterInteractiveSessionHandler(handleMenuInteraction)
}

func executeMenu(client *whatsmeow.Client, msg *events.Message, args []string) {
	conf, _ := config.LoadConfig()
	prefix := "."
	botName := "NopeBot"
	if conf != nil {
		if conf.Settings.Prefix != "" {
			prefix = conf.Settings.Prefix
		}
		if conf.Settings.BotName != "" {
			botName = conf.Settings.BotName
		}
	}

	lang := i18n.ResolveLang(client, msg)
	isOwner := handlers.IsBotOrTenantOwner(client, msg, conf)
	cats := menuCategories(isOwner)
	if len(cats) == 0 {
		utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "menu.no_commands"))
		return
	}

	query := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	query = strings.TrimPrefix(query, prefix)
	query = strings.TrimPrefix(query, ".")
	query = strings.TrimPrefix(query, "/")

	if query != "" {
		if query == "all" || query == "semua" {
			utils.SendReply(client, msg.Info.Chat, renderAllCommandsLang(lang, prefix, cats))
			return
		}
		// Match a category name (canonical, English, Indonesian, or legacy alias).
		if cat, ok := matchCategory(cats, query); ok {
			utils.SendReply(client, msg.Info.Chat, renderCategoryMenuLang(lang, prefix, cat))
			return
		}
		// Match a specific command name or alias.
		if cmd, ok := handlers.Commands[query]; ok {
			if (cmd.Category == i18n.CatOwner || cmd.OwnerOnly) && !isOwner {
				utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "menu.not_found", query, prefix))
				return
			}
			utils.SendReply(client, msg.Info.Chat, renderCommandDetailLang(lang, prefix, cmd))
			return
		}
		utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "menu.not_found", query, prefix))
		return
	}

	userName := strings.TrimSpace(msg.Info.PushName)
	if userName == "" {
		userName = "User"
	}

	text := renderMainMenuLang(lang, botName, prefix, userName, cats)
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
	prefix := "."
	if conf != nil && conf.Settings.Prefix != "" {
		prefix = conf.Settings.Prefix
	}
	lang := i18n.ResolveLang(client, msg)

	// Support legacy button taps if any older menu message is still tapped.
	if id := utils.ExtractButtonID(msg.Message); id != "" {
		if strings.HasPrefix(id, "cat:") {
			cat := i18n.NormalizeCategory(strings.TrimPrefix(id, "cat:"))
			if cat == i18n.CatOwner && !handlers.IsBotOrTenantOwner(client, msg, conf) {
				utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "menu.owner_denied"))
				return true
			}
			utils.SendReply(client, msg.Info.Chat, renderCategoryMenuLang(lang, prefix, cat))
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

	// Allow replying "all", "semua", or "0" to list every command.
	if replyText == "all" || replyText == "semua" || replyText == "0" {
		session.ResetSessionTimer(targetID)
		utils.SendReply(client, msg.Info.Chat, renderAllCommandsLang(lang, prefix, st.Categories))
		return true
	}

	// Allow replying with a category name (EN, ID, or legacy alias) directly.
	if cat, found := matchCategory(st.Categories, replyText); found {
		session.ResetSessionTimer(targetID)
		utils.SendReply(client, msg.Info.Chat, renderCategoryMenuLang(lang, prefix, cat))
		return true
	}

	choice, err := strconv.Atoi(replyText)
	if err != nil || choice < 1 || choice > len(st.Categories) {
		return false
	}

	// Keep the session alive so users can inspect multiple categories from the same menu.
	session.ResetSessionTimer(targetID)
	chosen := st.Categories[choice-1]
	if chosen == i18n.CatOwner && !handlers.IsBotOrTenantOwner(client, msg, conf) {
		utils.SendReply(client, msg.Info.Chat, i18n.T(lang, "menu.owner_denied"))
		return true
	}
	utils.SendReply(client, msg.Info.Chat, renderCategoryMenuLang(lang, prefix, chosen))
	return true
}

func renderMainMenu(botName, prefix, userName string, cats []string) string {
	return renderMainMenuLang(i18n.LangEN, botName, prefix, userName, cats)
}

func renderMainMenuLang(lang, botName, prefix, userName string, cats []string) string {
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

	langLabel := "English (EN)"
	if strings.EqualFold(lang, i18n.LangID) {
		langLabel = "Bahasa Indonesia (ID)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "╭─〔 *%s* 〕\n", botName)
	fmt.Fprintf(&b, "│ %s: %s\n", i18n.T(lang, "menu.user"), userName)
	fmt.Fprintf(&b, "│ %s: %s\n", i18n.T(lang, "menu.runtime"), uptime)
	fmt.Fprintf(&b, "│ %s: `%s` (. /)\n", i18n.T(lang, "menu.prefix"), prefix)
	fmt.Fprintf(&b, "│ %s: %d\n", i18n.T(lang, "menu.total_cmd"), totalCmds)
	fmt.Fprintf(&b, "│ %s: %s\n", i18n.T(lang, "menu.language"), langLabel)
	fmt.Fprintf(&b, "│ %s: %s • %s\n", i18n.T(lang, "menu.platform"), runtime.GOOS, now.Format("02/01 15:04"))
	b.WriteString("╰──────────\n\n")

	fmt.Fprintf(&b, "╭─〔 *%s* 〕\n", i18n.T(lang, "menu.categories"))
	for i, c := range cats {
		fmt.Fprintf(&b, "│ *%d.* %s %s (%d)\n", i+1, categoryEmoji(c), i18n.CategoryTitle(lang, c), catCounts[c])
	}
	fmt.Fprintf(&b, "│ *0.* 📜 %s\n", i18n.T(lang, "menu.show_all"))
	b.WriteString("╰──────────\n\n")
	b.WriteString(i18n.T(lang, "menu.footer", len(cats), prefix, prefix, prefix))

	return b.String()
}

func renderCategoryMenu(prefix, category string) string {
	return renderCategoryMenuLang(i18n.LangEN, prefix, category)
}

func renderCategoryMenuLang(lang, prefix, category string) string {
	canon := i18n.NormalizeCategory(category)
	var cmds []types.Command
	for _, c := range handlers.PrimaryCommands {
		if c.Category == canon {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return i18n.T(lang, "menu.empty_category", i18n.CategoryTitle(lang, canon))
	}
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name < cmds[j].Name
	})

	var b strings.Builder
	fmt.Fprintf(&b, "╭─〔 *%s %s* 〕\n", categoryEmoji(canon), strings.ToUpper(i18n.CategoryTitle(lang, canon)))
	for _, c := range cmds {
		desc := i18n.CommandDescription(lang, c.Name, c.Description)
		if desc != "" {
			fmt.Fprintf(&b, "│ `%s%s` — %s\n", prefix, c.Name, desc)
		} else {
			fmt.Fprintf(&b, "│ `%s%s`\n", prefix, c.Name)
		}
	}
	b.WriteString("╰──────────")
	return b.String()
}

func renderAllCommands(prefix string, cats []string) string {
	return renderAllCommandsLang(i18n.LangEN, prefix, cats)
}

func renderAllCommandsLang(lang, prefix string, cats []string) string {
	var b strings.Builder
	b.WriteString(i18n.T(lang, "menu.all_title") + "\n\n")
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
		fmt.Fprintf(&b, "╭─〔 *%s %s* 〕\n", categoryEmoji(c), strings.ToUpper(i18n.CategoryTitle(lang, c)))
		for _, cmd := range cmds {
			fmt.Fprintf(&b, "│ `%s%s`\n", prefix, cmd.Name)
		}
		b.WriteString("╰──────────")
	}
	return b.String()
}

func renderCommandDetail(prefix string, cmd types.Command) string {
	return renderCommandDetailLang(i18n.LangEN, prefix, cmd)
}

func renderCommandDetailLang(lang, prefix string, cmd types.Command) string {
	desc := i18n.CommandDescription(lang, cmd.Name, cmd.Description)
	if desc == "" {
		desc = i18n.T(lang, "menu.no_desc")
	}
	extra := ""
	if len(cmd.Aliases) > 0 {
		extra += fmt.Sprintf("\n• *%s:* `%s`", i18n.T(lang, "menu.field_aliases"), strings.Join(cmd.Aliases, "`, `"))
	}
	if cmd.Usage != "" {
		extra += fmt.Sprintf("\n• *%s:* `%s`", i18n.T(lang, "menu.field_usage"), cmd.Usage)
	}
	return fmt.Sprintf(
		"%s\n\n• *%s:* `%s%s`\n• *%s:* %s %s\n• *%s:* %s%s",
		i18n.T(lang, "menu.cmd_info"),
		i18n.T(lang, "menu.field_command"), prefix, cmd.Name,
		i18n.T(lang, "menu.field_category"), categoryEmoji(cmd.Category), i18n.CategoryTitle(lang, cmd.Category),
		i18n.T(lang, "menu.field_desc"), desc, extra,
	)
}

func formatCategoryTitle(cat string) string {
	return i18n.CategoryTitle(i18n.LangEN, cat)
}

func matchCategory(cats []string, query string) (string, bool) {
	return i18n.MatchCategory(cats, query)
}

// menuCategories lists the 9 canonical categories in display order, including
// "owner" only when includeOwner is true.
func menuCategories(includeOwner bool) []string {
	seen := map[string]bool{}
	for _, c := range handlers.PrimaryCommands {
		if c.Category == "" {
			continue
		}
		if (c.Category == i18n.CatOwner || c.OwnerOnly) && !includeOwner {
			continue
		}
		seen[c.Category] = true
	}

	var out []string
	for _, meta := range i18n.CanonicalCategoryList {
		if meta.Key == i18n.CatOwner && !includeOwner {
			continue
		}
		if seen[meta.Key] {
			out = append(out, meta.Key)
			delete(seen, meta.Key)
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
	return i18n.CategoryEmoji(category)
}
