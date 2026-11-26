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

func init() {
	// This command is special and will be handled by a custom prefix in the main handler.
	// We still register it to show up in the help menu.
	handlers.RegisterCommand(types.Command{
		Name:        "eval",
		Description: "Owner-only command to evaluate message properties.",
		Category:    "owner",
		Execute:     executeEval,
	})
}

// executeEval is an owner-only debugging tool.
func executeEval(client *whatsmeow.Client, msg *events.Message, args []string) {
	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Error loading config for eval:", err)
		return
	}

	if !utils.IsOwner(client, msg, conf) {
		return
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
