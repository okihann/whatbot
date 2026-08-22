package owner

import (
	"bot/config"
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "removeowner",
		Description: "Removes an owner by replying to their message. (Bot account only)",
		Category:    "owner",
		Execute:     executeRemoveOwner,
	})
}

func executeRemoveOwner(client *whatsmeow.Client, msg *events.Message, args []string) {
	// 1. Verify this command is sent from the bot's own account for security.
	if !msg.Info.IsFromMe {
		// Silently ignore if not from the bot's number
		return
	}

	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Error loading config for removeowner:", err)
		return
	}

	// 2. Check if the command is a reply to a message.
	ctxInfo := msg.Message.GetExtendedTextMessage().GetContextInfo()
	if ctxInfo == nil || ctxInfo.Participant == nil {
		utils.SendReply(client, msg.Info.Chat, "Please reply to a user's message to remove them as an owner.")
		return
	}

	// 3. Resolve the replied-to user's JID, handling LIDs correctly.
	var ownerToRemoveJID waTypes.JID
	participantJIDString := *ctxInfo.Participant

	parsedJID, err := waTypes.ParseJID(participantJIDString)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "The replied-to user has an invalid ID.")
		return
	}

	if parsedJID.Server == "lid" {
		resolvedJID, err := client.Store.LIDs.GetPNForLID(context.Background(), parsedJID)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "Could not resolve the user's ID to remove them.")
			return
		}
		ownerToRemoveJID = resolvedJID
	} else {
		ownerToRemoveJID = parsedJID
	}

	ownerToRemoveJIDString := cutJID(ownerToRemoveJID.String())

	// 4. Find the owner in the list and remove them.
	var newOwnerList []string
	found := false
	for _, ownerJID := range conf.Settings.OwnerJIDs {
		if ownerJID == ownerToRemoveJIDString {
			found = true
			// Skip adding this JID to the new list, effectively removing them.
		} else {
			newOwnerList = append(newOwnerList, ownerJID)
		}
	}

	if !found {
		utils.SendReply(client, msg.Info.Chat, "That user is not currently an owner.")
		return
	}

	// 5. Update the config with the new, smaller list.
	conf.Settings.OwnerJIDs = newOwnerList
	if err := config.SaveConfig(conf); err != nil {
		fmt.Println("Failed to save config:", err)
		utils.SendReply(client, msg.Info.Chat, "An error occurred while trying to save the updated owner list.")
		return
	}

	// 6. Send a confirmation message.
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Success! %s has been removed as an owner.", ownerToRemoveJIDString))
}
