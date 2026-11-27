package handlers

import (
	"bot/config"
	db "bot/databases"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// This will be set by the main package at startup
var botStartTime time.Time

func BotStartTime() time.Time {
	return botStartTime
}

// Commands holds all registered bot commands keyed by primary name and lowercase aliases.
var Commands = make(map[string]types.Command)

// PrimaryCommands tracks canonical command names (excluding aliases) for accurate counts.
var PrimaryCommands = make(map[string]types.Command)

// Telemetry counters for .MsgStats / .Stats.
var (
	TotalMessagesHandled atomic.Int64
	TotalCommandsHandled atomic.Int64
	pacingMu             sync.Mutex
	lastCmdByChat        = make(map[string]time.Time)
)

type ReactionFunc func(client *whatsmeow.Client, msg *events.Message) bool
type InteractiveSessionFunc func(client *whatsmeow.Client, msg *events.Message) bool
type MessageMiddlewareFunc func(client *whatsmeow.Client, msg *events.Message) bool
type GroupParticipantFunc func(client *whatsmeow.Client, evt *events.GroupInfo)
type CallHandlerFunc func(client *whatsmeow.Client, evt *events.CallOffer)

var (
	reactionHandlers           []ReactionFunc
	interactiveSessionHandlers []InteractiveSessionFunc
	messageMiddlewares         []MessageMiddlewareFunc
	groupParticipantHandlers   []GroupParticipantFunc
	callHandlers               []CallHandlerFunc
)

func RegisterReactionHandler(handler ReactionFunc) {
	reactionHandlers = append(reactionHandlers, handler)
}

func RegisterInteractiveSessionHandler(handler InteractiveSessionFunc) {
	interactiveSessionHandlers = append(interactiveSessionHandlers, handler)
}

// RegisterMessageMiddleware registers a hook run on every live message before command parsing.
// Returning true stops further command processing (e.g. muted/spam/banned user).
func RegisterMessageMiddleware(handler MessageMiddlewareFunc) {
	messageMiddlewares = append(messageMiddlewares, handler)
}

func RegisterGroupParticipantHandler(handler GroupParticipantFunc) {
	groupParticipantHandlers = append(groupParticipantHandlers, handler)
}

func RegisterCallHandler(handler CallHandlerFunc) {
	callHandlers = append(callHandlers, handler)
}

func HandleGroupParticipant(client *whatsmeow.Client, evt *events.GroupInfo) {
	for _, h := range groupParticipantHandlers {
		h(client, evt)
	}
}

func HandleCallOffer(client *whatsmeow.Client, evt *events.CallOffer) {
	for _, h := range callHandlers {
		h(client, evt)
	}
}

// categoryEmojis maps category names to emojis for a more visual menu.
var categoryEmojis = map[string]string{
	"main": "📋", "general": "📋", "bot": "🤖", "downloader": "📥", "search": "🔎",
	"ai": "🧠", "game": "🎮", "minigame": "🕹️", "rpg": "⚔️", "xp": "🌟",
	"sticker": "🖼️", "media": "🖼️", "fun": "😂", "anime": "🌸", "animanga": "🌸",
	"waifu": "💐", "group": "👥", "group_admin": "🛡️", "group_security": "🔒",
	"tools": "🛠️", "audio": "🎵", "audiofx": "🎛️", "music": "🎧", "youtube": "▶️",
	"profile": "👤", "owner": "👑", "info": "ℹ️", "sound": "🔊",
}

// SetStartTime allows the main package to set the bot's start time.
func SetStartTime(t time.Time) {
	botStartTime = t
}

// RegisterCommand adds a new command (and any lowercase aliases) to the registry.
func RegisterCommand(cmd types.Command) {
	name := strings.ToLower(strings.TrimSpace(cmd.Name))
	if name == "" {
		return
	}
	cmd.Name = name
	Commands[name] = cmd
	PrimaryCommands[name] = cmd
	for _, alias := range cmd.Aliases {
		a := strings.ToLower(strings.TrimSpace(alias))
		if a != "" && a != name {
			Commands[a] = cmd
		}
	}
	fmt.Printf("Registered command: %s\n", cmd.Name)
}

// ActivePrefixes returns the accepted command prefixes for this message context
// (always supporting both "." and "/" by default, plus custom tenant/group/user prefixes).
func ActivePrefixes(client *whatsmeow.Client, msg *events.Message, conf *config.Config) []string {
	seen := map[string]bool{}
	var prefixes []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			prefixes = append(prefixes, p)
		}
	}

	if store := db.GetBotStore(); store != nil {
		if msg != nil {
			senderNum := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
			if senderNum != "" {
				if u, err := store.GetUser(senderNum); err == nil && u != nil && u.CustomPrefix != "" {
					add(u.CustomPrefix)
				}
			}
			if msg.Info.IsGroup {
				if gcfg, err := store.GetGroupConfig(msg.Info.Chat.ToNonAD().String()); err == nil && gcfg != nil && gcfg.Prefix != "" {
					add(gcfg.Prefix)
				}
			}
		}
		if client != nil && client.Store != nil && client.Store.ID != nil {
			if tenant, err := store.GetTenant(client.Store.ID.ToNonAD().User); err == nil && tenant != nil && tenant.Prefix != "" {
				add(tenant.Prefix)
			}
		}
	}

	if conf != nil && conf.Settings.Prefix != "" {
		add(conf.Settings.Prefix)
	}
	add(".")
	add("/")
	return prefixes
}

// MatchPrefix strips the first matching prefix from text.
func MatchPrefix(text string, prefixes []string) (matchedPrefix, rest string, ok bool) {
	for _, p := range prefixes {
		if strings.HasPrefix(text, p) {
			return p, strings.TrimPrefix(text, p), true
		}
	}
	return "", "", false
}

// IsBotOrTenantOwner returns true if the sender is the bot itself, a global owner,
// or the registered buyer-owner of this linked bot tenant.
func IsBotOrTenantOwner(client *whatsmeow.Client, msg *events.Message, conf *config.Config) bool {
	if utils.IsOwner(client, msg, conf) {
		return true
	}
	store := db.GetBotStore()
	if store == nil || client == nil || client.Store == nil || client.Store.ID == nil || msg == nil {
		return false
	}
	tenant, err := store.GetTenant(client.Store.ID.ToNonAD().User)
	if err != nil || tenant == nil || tenant.OwnerJID == "" {
		return false
	}
	senderNum := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	return senderNum != "" && utils.NormalizeTarget(tenant.OwnerJID) == senderNum
}

// IsGroupAdmin checks whether targetJID is an admin/superadmin of groupJID.
func IsGroupAdmin(client *whatsmeow.Client, groupJID, targetJID waTypes.JID) (isAdmin, isBotAdmin bool) {
	if client == nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, groupJID)
	if err != nil || gi == nil {
		return false, false
	}
	targetUser := targetJID.ToNonAD().User
	botUser := ""
	if client.Store != nil && client.Store.ID != nil {
		botUser = client.Store.ID.ToNonAD().User
	}
	botLID := ""
	if client.Store != nil && client.Store.LID.User != "" {
		botLID = client.Store.LID.ToNonAD().User
	}
	for _, p := range gi.Participants {
		u := p.JID.ToNonAD().User
		pn := p.PhoneNumber.ToNonAD().User
		admin := p.IsAdmin || p.IsSuperAdmin
		if !admin {
			continue
		}
		if targetUser != "" && (u == targetUser || pn == targetUser) {
			isAdmin = true
		}
		if (botUser != "" && (u == botUser || pn == botUser)) || (botLID != "" && u == botLID) {
			isBotAdmin = true
		}
	}
	return isAdmin, isBotAdmin
}

// HandleCommand processes reactions, middleware, interactive sessions, and text commands.
func HandleCommand(client *whatsmeow.Client, msg *events.Message) {
	TotalMessagesHandled.Add(1)

	conf, _ := config.LoadConfig()
	isOwner := IsBotOrTenantOwner(client, msg, conf)

	// Per-bot tenant AutoRead / SelfMode / GroupMode enforcement.
	var tenant *db.BotTenant
	if store := db.GetBotStore(); store != nil && client != nil && client.Store != nil && client.Store.ID != nil {
		tenant, _ = store.GetTenant(client.Store.ID.ToNonAD().User)
		if tenant != nil {
			if tenant.AutoRead && !msg.Info.IsFromMe {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = client.MarkRead(ctx, []waTypes.MessageID{msg.Info.ID}, msg.Info.Timestamp, msg.Info.Chat, msg.Info.Sender)
				}()
			}
			if tenant.SelfMode && !isOwner {
				return
			}
			if tenant.GroupMode && !msg.Info.IsGroup && !isOwner {
				return
			}
		}
	}

	// 1. Check if the message is a REACTION.
	if msg.Message.ReactionMessage != nil {
		for _, handler := range reactionHandlers {
			if handler(client, msg) {
				return
			}
		}
		return
	}

	// 2. Run registered message middlewares (group security, AFK, activity/XP, auto-respon).
	for _, mw := range messageMiddlewares {
		if mw(client, msg) {
			return
		}
	}

	// 3. Check if the message is part of an INTERACTIVE SESSION (menu, chat games, etc.).
	for _, handler := range interactiveSessionHandlers {
		if handler(client, msg) {
			return
		}
	}

	// 4. Parse text command.
	text := strings.TrimSpace(utils.GetTextFromMessage(msg.Message))
	if text == "" {
		return
	}
	jid := msg.Info.Chat

	if strings.HasPrefix(text, "->") {
		if cmd, ok := Commands["eval"]; ok {
			if !isOwner {
				utils.SendReply(client, jid, "⛔ Access Denied: This command is restricted to the bot owner.")
				return
			}
			argsString := strings.TrimSpace(strings.TrimPrefix(text, "->"))
			cmd.Execute(client, msg, strings.Fields(argsString))
			TotalCommandsHandled.Add(1)
			return
		}
	}

	prefixes := ActivePrefixes(client, msg, conf)
	_, commandAndArgs, matched := MatchPrefix(text, prefixes)
	if !matched {
		return
	}
	commandAndArgs = strings.TrimSpace(commandAndArgs)
	if commandAndArgs == "" {
		return
	}

	parts := strings.SplitN(commandAndArgs, " ", 2)
	commandName := strings.ToLower(parts[0])

	var args []string
	if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
		args = []string{strings.TrimSpace(parts[1])}
	}

	cmd, ok := Commands[commandName]
	if !ok {
		return
	}

	// Owner check
	if (cmd.Category == "owner" || cmd.OwnerOnly) && !isOwner {
		utils.SendReply(client, jid, "⛔ Access Denied: This command is restricted to the bot owner.")
		return
	}

	// Group-only check
	if cmd.GroupOnly && !msg.Info.IsGroup {
		utils.SendReply(client, jid, "👥 This command can only be used inside a group.")
		return
	}

	// Per-chat pacing guard
	if tenant != nil && tenant.PacingMS > 0 && !isOwner {
		chatKey := jid.ToNonAD().String()
		pacingMu.Lock()
		last := lastCmdByChat[chatKey]
		minGap := time.Duration(tenant.PacingMS) * time.Millisecond
		if time.Since(last) < minGap {
			pacingMu.Unlock()
			return
		}
		lastCmdByChat[chatKey] = time.Now()
		pacingMu.Unlock()
	}

	// Group checks: Banned chat, AdminOnly, disabled categories/commands, admin/bot-admin, NSFW
	if msg.Info.IsGroup {
		groupID := jid.ToNonAD().String()
		var gcfg *db.GroupConfig
		if store := db.GetBotStore(); store != nil {
			gcfg, _ = store.GetGroupConfig(groupID)
		}

		if gcfg != nil && gcfg.Banned && !isOwner {
			return
		}

		senderJID := msg.Info.Sender
		senderAdmin, botAdmin := false, false
		needAdminCheck := cmd.AdminOnly || cmd.BotAdmin || (gcfg != nil && gcfg.AdminOnly)
		if needAdminCheck {
			senderAdmin, botAdmin = IsGroupAdmin(client, jid, senderJID)
			// Fallback: resolve phone number JID if LID didn't match directly
			if !senderAdmin {
				if pn := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt); pn != "" {
					sa, ba := IsGroupAdmin(client, jid, waTypes.NewJID(pn, waTypes.DefaultUserServer))
					if sa {
						senderAdmin = true
					}
					if ba {
						botAdmin = true
					}
				}
			}
		}

		if !isOwner {
			if gcfg != nil {
				if gcfg.AdminOnly && !senderAdmin {
					return
				}
				if gcfg.DisabledCategories[cmd.Category] || gcfg.DisabledCommands[cmd.Name] {
					return
				}
			}
			// Legacy JSON group settings compatibility
			if legacySettings, err := db.LoadGroupSettings(jid.String()); err == nil && legacySettings != nil {
				if legacySettings.DisabledCategories[cmd.Category] || legacySettings.DisabledCommands[cmd.Name] {
					return
				}
			}
			if cmd.AdminOnly && !senderAdmin {
				utils.SendReply(client, jid, "🛡️ Group Admin required for this command.")
				return
			}
		}

		if cmd.BotAdmin && !botAdmin {
			utils.SendReply(client, jid, "⚠️ Please make the bot a Group Admin first.")
			return
		}

		if cmd.NSFW && !isOwner {
			allowed := false
			if gcfg != nil && gcfg.NSFWEnabled {
				allowed = true
			}
			if !allowed {
				utils.SendReply(client, jid, "🔞 NSFW is disabled in this group. An admin can enable it with `.nsfw on`.")
				return
			}
		}
	} else if cmd.NSFW && !isOwner {
		if tenant == nil || !tenant.NSFWEnabled {
			utils.SendReply(client, jid, "🔞 NSFW is disabled on this bot. Owner can enable it with `.nsfw on`.")
			return
		}
	}

	TotalCommandsHandled.Add(1)
	cmd.Execute(client, msg, args)
}

// formatDuration formats the uptime into a readable string.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if days > 0 {
		return fmt.Sprintf("%d days, %02d:%02d:%02d", days, h, m, s)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// GenerateHelpMenu constructs the final help message string.
func GenerateHelpMenu(conf *config.Config, categories map[string][]string, requestedCategory, userName string) (string, []string) {
	var orderedCategories []string

	if requestedCategory == "" {
		for name := range categories {
			orderedCategories = append(orderedCategories, name)
		}
		sort.Strings(orderedCategories)

		var menuListBuilder strings.Builder
		for i, name := range orderedCategories {
			emoji, ok := categoryEmojis[strings.ToLower(name)]
			if !ok {
				emoji = "📁"
			}
			menuListBuilder.WriteString(fmt.Sprintf("%d. %s %s Menu\n", i+1, emoji, Title(name)))
		}

		loc, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		uptime := formatDuration(time.Since(botStartTime))

		replacer := strings.NewReplacer(
			"${botName}", conf.Settings.BotName,
			"${userName}", userName,
			"${platform}", runtime.GOOS,
			"${uptime}", uptime,
			"${date}", now.Format("02/01/2006"),
			"${time}", now.Format("15:04:05 MST"),
			"${totalCommands}", fmt.Sprintf("%d", len(PrimaryCommands)),
			"${readMore}", strings.Repeat("‎", 8192),
			"${menuList}", strings.TrimSpace(menuListBuilder.String()),
			"${categoryCount}", fmt.Sprintf("%d", len(orderedCategories)),
		)

		finalMenu := replacer.Replace(conf.HelpMenuTemplate.MainMenu)
		return strings.TrimSpace(finalMenu), orderedCategories
	}

	var builder strings.Builder
	commands, ok := categories[requestedCategory]
	if !ok {
		return fmt.Sprintf("Category '%s' not found.", requestedCategory), nil
	}
	sort.Strings(commands)

	emoji, ok := categoryEmojis[strings.ToLower(requestedCategory)]
	if !ok {
		emoji = "📁"
	}

	var commandListBuilder strings.Builder
	for _, name := range commands {
		commandListBuilder.WriteString(fmt.Sprintf("│ ◦ %s%s\n", conf.Settings.Prefix, name))
	}

	replacer := strings.NewReplacer(
		"${emoji}", emoji,
		"${categoryName}", Title(requestedCategory),
		"${commandList}", strings.TrimSuffix(commandListBuilder.String(), "\n"),
	)
	categoryMenu := replacer.Replace(conf.HelpMenuTemplate.CategoryMenu)
	builder.WriteString(categoryMenu)
	return builder.String(), nil
}

// Title capitalizes the first letter of a string.
func Title(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
