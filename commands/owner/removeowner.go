package owner

import (
	"bot/config"
	"bot/handlers"
	"bot/types"
	"bot/utils"
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
	if !msg.Info.IsFromMe {
		return
	}

	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Error loading config for removeowner:", err)
		return
	}

	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || ctxInfo.GetParticipant() == "" {
		utils.SendReply(client, msg.Info.Chat, "Please reply to a user's message to remove them as an owner.")
		return
	}

	parsedJID, err := waTypes.ParseJID(ctxInfo.GetParticipant())
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "The replied-to user has an invalid ID.")
		return
	}

	ownerToRemove := utils.ResolvePhoneNumber(client, parsedJID, waTypes.EmptyJID)
	if ownerToRemove == "" {
		utils.SendReply(client, msg.Info.Chat, "Could not resolve the user's phone number.")
		return
	}

	var newOwnerList []string
	found := false
	for _, ownerNum := range conf.Settings.OwnerJIDs {
		if utils.NormalizeTarget(ownerNum) == ownerToRemove {
			found = true
			continue
		}
		newOwnerList = append(newOwnerList, ownerNum)
	}

	if !found {
		utils.SendReply(client, msg.Info.Chat, "That user is not currently an owner.")
		return
	}

	conf.Settings.OwnerJIDs = newOwnerList
	if err := config.SaveConfig(conf); err != nil {
		fmt.Println("Failed to save config:", err)
		utils.SendReply(client, msg.Info.Chat, "An error occurred while trying to save the updated owner list.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Success! %s has been removed as an owner.", ownerToRemove))
}
