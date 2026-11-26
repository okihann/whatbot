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
		Name:        "addowner",
		Description: "Adds a new owner by replying to their message. (Owner only)",
		Category:    "owner",
		Execute:     executeAddOwner,
	})
}

func executeAddOwner(client *whatsmeow.Client, msg *events.Message, args []string) {
	if !msg.Info.IsFromMe {
		return
	}

	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Error loading config for addowner:", err)
		return
	}

	ctxInfo := utils.ExtractContextInfo(msg.Message)
	if ctxInfo == nil || ctxInfo.GetParticipant() == "" {
		utils.SendReply(client, msg.Info.Chat, "Please reply to a user's message to make them an owner.")
		return
	}

	parsedJID, err := waTypes.ParseJID(ctxInfo.GetParticipant())
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "The replied-to user has an invalid ID.")
		return
	}

	newOwnerNum := utils.ResolvePhoneNumber(client, parsedJID, waTypes.EmptyJID)
	if newOwnerNum == "" {
		utils.SendReply(client, msg.Info.Chat, "Could not resolve the user's phone number.")
		return
	}

	for _, ownerNum := range conf.Settings.OwnerJIDs {
		if utils.NormalizeTarget(ownerNum) == newOwnerNum {
			utils.SendReply(client, msg.Info.Chat, "That user is already an owner.")
			return
		}
	}

	conf.Settings.OwnerJIDs = append(conf.Settings.OwnerJIDs, newOwnerNum)
	if err := config.SaveConfig(conf); err != nil {
		fmt.Println("Failed to save config:", err)
		utils.SendReply(client, msg.Info.Chat, "An error occurred while trying to save the new owner.")
		return
	}

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Success! %s has been added as an owner.", newOwnerNum))
}
