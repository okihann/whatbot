package handlers

import (
	"bot/config"
	"bot/session"
	"bot/types"
	"bot/utils"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	db "bot/databases" // Added 'db' alias for your Group Feature Check

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// This will be set by the main package at startup
var botStartTime time.Time

func BotStartTime() time.Time {
	return botStartTime
}

// Commands holds all registered bot commands
var Commands = make(map[string]types.Command)

// --- REACTION HANDLING SYSTEM ---
type ReactionFunc func(client *whatsmeow.Client, msg *events.Message) bool

var reactionHandlers []ReactionFunc

func RegisterReactionHandler(handler ReactionFunc) {
	reactionHandlers = append(reactionHandlers, handler)
}

// --- END REACTION HANDLING SYSTEM ---

// --- INTERACTIVE SESSION HANDLING ---
type InteractiveSessionFunc func(client *whatsmeow.Client, msg *events.Message) bool

var interactiveSessionHandlers []InteractiveSessionFunc

func RegisterInteractiveSessionHandler(handler InteractiveSessionFunc) {
	interactiveSessionHandlers = append(interactiveSessionHandlers, handler)
}

// --- END INTERACTIVE SESSION HANDLING ---

// categoryEmojis maps category names to emojis for a more visual menu.
var categoryEmojis = map[string]string{
	"main": "📋", "downloader": "📥", "search": "🔎", "ai": "🤖",
	"game": "🎮", "rpg": "⚔️", "xp": "🌟", "sticker": "🖼️",
	"kerang": "🐚", "quotes": "📜", "fun": "😂", "anime": "🌸",
	"group": "👥", "premium": "💎", "nsfw": "🔞", "internet": "🌐",
	"genshin": "✨", "news": "📰", "tools": "🛠️", "primbon": "🔮",
	"nulis": "✍️", "audio": "🎵", "maker": "🎨", "database": "💾",
	"quran": "📖", "owner": "👑", "info": "ℹ️", "sound": "🔊",
	"general": "⚙️",
}

// SetStartTime allows the main package to set the bot's start time.
func SetStartTime(t time.Time) {
	botStartTime = t
}

// RegisterCommand adds a new command to the registry
func RegisterCommand(cmd types.Command) {
	Commands[cmd.Name] = cmd
	fmt.Printf("Registered command: %s\n", cmd.Name)
}

// HandleCommand now processes reactions, interactive sessions, and text commands.
func HandleCommand(client *whatsmeow.Client, msg *events.Message) {
	// 1. Check if the message is a REACTION.
	if msg.Message.ReactionMessage != nil {
		for _, handler := range reactionHandlers {
			if handler(client, msg) {
				return
			}
		}
		return
	}

	// 2. Check if the message is part of an INTERACTIVE SESSION (like registration).
	for _, handler := range interactiveSessionHandlers {
		if handler(client, msg) {
			return
		}
	}

	// 3. If not a reaction or session, process as a potential COMMAND.
	text := utils.GetTextFromMessage(msg.Message)
	jid := msg.Info.Chat
    
	if strings.HasPrefix(text, "->") {
		// Find the 'eval' command specifically
		if cmd, ok := Commands["eval"]; ok {
			// Trim the prefix and any leading space
			argsString := strings.TrimSpace(strings.TrimPrefix(text, "->"))
			// Split the rest of the string into arguments
			args := strings.Fields(argsString)
			cmd.Execute(client, msg, args)
			return // Stop processing
		}
	}

	// Interactive Help Menu Logic
	ctxInfo := msg.Message.GetExtendedTextMessage().GetContextInfo()
	if ctxInfo != nil && ctxInfo.StanzaID != nil {
		quotedMsgID := ctxInfo.GetStanzaID()
		if helpSession, found := session.GetHelpSession(jid.String()); found && helpSession.MessageID == quotedMsgID {
			choice, err := strconv.Atoi(text)
			if err == nil && choice > 0 && choice <= len(helpSession.Categories) {
				chosenCategory := helpSession.Categories[choice-1]
				session.ClearHelpSession(jid.String())
				conf, _ := config.LoadConfig()
				categories := make(map[string][]string)
				seen := make(map[string]bool)
				for _, cmd := range Commands {
					if !seen[cmd.Name] {
						categories[cmd.Category] = append(categories[cmd.Category], cmd.Name)
						seen[cmd.Name] = true
					}
				}
				categoryMenuText, _ := GenerateHelpMenu(conf, categories, chosenCategory, msg.Info.PushName)
				utils.SendReply(client, jid, categoryMenuText)
				return
			}
		}
	}

	// Standard Command Logic
	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Could not load config for command handling:", err)
		return
	}
	prefix := conf.Settings.Prefix
	if !strings.HasPrefix(text, prefix) {
		return
	}

	// *** MODIFIED THIS SECTION FOR MULTI-LINE ARGUMENTS ***
	commandAndArgs := strings.TrimPrefix(text, prefix)
	parts := strings.SplitN(commandAndArgs, " ", 2)
	commandName := strings.ToLower(parts[0])

	var args []string
	if len(parts) > 1 {
		// The rest of the message is a single argument
		args = []string{parts[1]}
	} else {
		args = []string{}
	}
	// *** END OF MODIFICATION ***
	if cmd, ok := Commands[commandName]; ok {
		// --- 1. GLOBAL OWNER IDENTIFICATION ---
		isOwner := false
		if msg.Info.IsFromMe {
			isOwner = true
		} else {
			senderNum := msg.Info.Sender.ToNonAD().User
			for _, ownerNum := range conf.Settings.OwnerJIDs {
				if senderNum == ownerNum {
					isOwner = true
					break
				}
			}
		}

		// --- 2. OWNER COMMAND GATEKEEPER ---
		if cmd.Category == "owner" && !isOwner {
			utils.SendReply(client, jid, "⛔ Access Denied: This command is restricted to the bot owner.")
			return
		}

		// --- 3. GROUP FEATURE CHECK (WITH OWNER BYPASS) ---
		if msg.Info.IsGroup {
			// ONLY block the command if the user is NOT an owner
			if !isOwner {
				settings, err := db.LoadGroupSettings(jid.String())
				if err != nil {
					return 
				}
				if settings.DisabledCategories[cmd.Category] {
					return 
				}
				if settings.DisabledCommands[cmd.Name] {
					return 
				}
			}
		}

		cmd.Execute(client, msg, args)
	}
}

// --- HELP MENU LOGIC ---
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
		// --- Main Menu ---
		for name := range categories {
			orderedCategories = append(orderedCategories, name)
		}
		sort.Strings(orderedCategories)

		var menuListBuilder strings.Builder
		for i, name := range orderedCategories {
			emoji, ok := categoryEmojis[strings.ToLower(name)]
			if !ok {
				emoji = "📁" // Default emoji
			}
			menuListBuilder.WriteString(fmt.Sprintf("%d. %s %s Menu\n", i+1, emoji, Title(name)))
		}

		uniqueCmds := make(map[string]struct{})
		for _, cmd := range Commands {
			uniqueCmds[cmd.Name] = struct{}{}
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
			"${totalCommands}", fmt.Sprintf("%d", len(uniqueCmds)),
			"${readMore}", strings.Repeat("‎", 8192),
			"${menuList}", strings.TrimSpace(menuListBuilder.String()),
			"${categoryCount}", fmt.Sprintf("%d", len(orderedCategories)),
		)

		finalMenu := replacer.Replace(conf.HelpMenuTemplate.MainMenu)
		return strings.TrimSpace(finalMenu), orderedCategories

	} else {
		// --- Category Specific Menu ---
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