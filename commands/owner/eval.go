package owner

import (
	"bot/config"
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func cutJID(jid string) string {
	if strings.Contains(jid, ":") {
		return strings.Split(jid, ":")[0]
	}
	return strings.Split(jid, "@")[0]
}

func init() {
	// This command is special and will be handled by a custom prefix in the main handler.
	// We still register it to show up in the help menu.
	handlers.RegisterCommand(types.Command{
		Name:        "eval",
		Description: "Owner-only command to evaluate message properties.",
		Category:    "owner", // Moved to an "owner" category
		Execute:     executeEval,
	})
}

// executeEval is now a powerful, owner-only debugging tool.
func executeEval(client *whatsmeow.Client, msg *events.Message, args []string) {
	// 1. Load config and check if the sender is the owner.
	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Error loading config for eval:", err)
		return
	}

	// The full JID includes the server, e.g., "6281234567890@s.whatsapp.net"
	isOwner := msg.Info.IsFromMe // Always true if the bot sends it.

	if !isOwner {
		// If not FromMe, check both Sender and Chat against the owner list.
		senderJID := cutJID(msg.Info.SenderAlt.String())
		chatJID := cutJID(msg.Info.Chat.String())
		for _, ownerJID := range conf.Settings.OwnerJIDs {
			if senderJID == ownerJID || chatJID == ownerJID {
				isOwner = true
				break
			}
		}
	}

	if !isOwner {
		return // Silently ignore if not an owner
	}

	// 2. Get the code to "evaluate".
	if len(args) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Please provide a property to evaluate. E.g., `-> msg.Info.Chat`")
		return
	}
	evalCode := strings.Join(args, " ")

	var result string

	// 3. Use a switch statement to safely "evaluate" the requested property.
	switch evalCode {
	case "msg.Info.Chat":
		result = msg.Info.Chat.String()
	case "msg.Info.Sender":
		result = msg.Info.Sender.String()
	case "msg.Info.PushName":
		result = msg.Info.PushName
	case "msg.Info.ID":
		result = msg.Info.ID
	case "msg.Info.Timestamp":
		result = msg.Info.Timestamp.String()
	default:
		result = fmt.Sprintf("Unknown property: `%s`", evalCode)
	}

	// 4. Send the result.
	reply := fmt.Sprintf("Result:\n`%s`", result)
	utils.SendReply(client, msg.Info.Chat, reply)
}
