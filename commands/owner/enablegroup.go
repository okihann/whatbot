package owner

import (
	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	//"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	//waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "enablegroup",
		Description: "Enables all features for the current group. (Owner only)",
		Category:    "owner",
		Execute:     executeEnableGroup,
	})
}

// executeEnableGroup enables all categories and the welcome message in the current group.
func executeEnableGroup(client *whatsmeow.Client, msg *events.Message, args []string) {
	// 1. Ensure this is a group chat.
	if !msg.Info.IsGroup {
		utils.SendReply(client, msg.Info.Chat, "This command can only be used in groups.")
		return
	}

	// 2. Load config and verify the sender is the owner.
	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Error loading config for enablegroup:", err)
		return
	}

	isOwner := msg.Info.IsFromMe

	if !isOwner {
		// Note: We are using Sender.String() here, not SenderAlt, for broad compatibility.
		// cutJID is not needed for these commands.
		senderJID := cutJID(msg.Info.SenderAlt.String())
		for _, ownerJID := range conf.Settings.OwnerJIDs {
			if senderJID == ownerJID {
				isOwner = true
				break
			}
		}
	}

	if !isOwner {
		return
	}

	// 3. Load the settings for the current group.
	groupID := msg.Info.Chat.String()
	settings, err := db.LoadGroupSettings(groupID)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "Error: Could not load settings for this group.")
		return
	}

	// 4. Enable the welcome message.
	settings.WelcomeEnabled = true

	// 5. Clear all disabled categories and commands for this group.
	settings.DisabledCategories = make(map[string]bool)
	settings.DisabledCommands = make(map[string]bool)

	// 6. Save the updated settings.
	if err := db.SaveGroupSettings(groupID, settings); err != nil {
		utils.SendReply(client, msg.Info.Chat, "Error: Could not save the updated settings.")
		return
	}

	// 7. Send a confirmation message.
	utils.SendReply(client, msg.Info.Chat, "✅ All features have been re-enabled for this group.")
}
