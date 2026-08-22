package owner

import (
	"bot/config"
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context" // <-- ADD THIS IMPORT
	"fmt"

	//"strings" // <-- ADD THIS IMPORT

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types" // <-- ADD THIS IMPORT
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "addowner",
		Description: "Adds a new owner by replying to their message. (Owner only)",
		Category:    "owner",
		Execute:     executeAddOwner,
	})
}

func executeAddOwner(client *whatsmeow.Client, msg *events.Message, args []string) {
	// 1. Verify the user of this command is an owner
	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Error loading config for addowner:", err)
		return
	}

	if !msg.Info.IsFromMe {
		return // Silently ignore if not an owner
	}

	// 2. Check if the command is a reply to a message
	ctxInfo := msg.Message.GetExtendedTextMessage().GetContextInfo()
	if ctxInfo == nil || ctxInfo.Participant == nil {
		utils.SendReply(client, msg.Info.Chat, "Please reply to a user's message to make them an owner.")
		return
	}

	// 3. Resolve the replied-to user's JID, handling LIDs correctly
	var newOwnerJID waTypes.JID
	participantJIDString := *ctxInfo.Participant

	// Manually parse the string to a JID struct to check its server
	parsedJID, err := waTypes.ParseJID(participantJIDString)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "The replied-to user has an invalid ID.")
		return
	}

	if parsedJID.Server == "lid" {
		resolvedJID, err := client.Store.LIDs.GetPNForLID(context.Background(), parsedJID)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "Could not resolve the user's ID. They may need to message the bot directly first.")
			return
		}
		newOwnerJID = resolvedJID
	} else {
		newOwnerJID = parsedJID
	}

	newOwnerJIDString := cutJID(newOwnerJID.String())

	// 4. Check if the user is already an owner
	for _, ownerJID := range conf.Settings.OwnerJIDs {
		if newOwnerJIDString == ownerJID {
			utils.SendReply(client, msg.Info.Chat, "That user is already an owner.")
			return
		}
	}

	// 5. Add the new owner and save the config
	conf.Settings.OwnerJIDs = append(conf.Settings.OwnerJIDs, newOwnerJIDString)
	if err := config.SaveConfig(conf); err != nil {
		fmt.Println("Failed to save config:", err)
		utils.SendReply(client, msg.Info.Chat, "An error occurred while trying to save the new owner.")
		return
	}

	// 6. Send a confirmation message
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Success! %s has been added as an owner.", newOwnerJIDString))
}
