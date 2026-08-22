package owner

import (
	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "disableallgroup",
		Description: "Disables all features in every group. (Owner only)",
		Category:    "owner",
		Execute:     executeDisableAllGroup,
	})
}

// executeDisableAllGroup disables all categories and welcome messages in every group.
func executeDisableAllGroup(client *whatsmeow.Client, msg *events.Message, args []string) {
	// 1. Load config and verify the sender is the owner.
	conf, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Error loading config for disableallgroup:", err)
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

	utils.SendReply(client, msg.Info.Chat, "Processing... Disabling all features in all groups. This may take a moment.")

	// 2. Get all unique command categories.
	allCategories := make(map[string]bool)
	for _, cmd := range handlers.Commands {
		allCategories[cmd.Category] = true
	}

	// 3. Get all groups the bot is a member of.
	groups, err := client.GetJoinedGroups(context.Background())
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "Error: Could not retrieve group list.")
		return
	}

	var updatedCount int
	var errorCount int

	// 4. Iterate through each group and update its settings.
	for _, group := range groups {
		groupID := group.JID.String()
		settings, err := db.LoadGroupSettings(groupID)
		if err != nil {
			fmt.Printf("Could not load settings for group %s: %v\n", groupID, err)
			errorCount++
			continue
		}

		// Disable welcome message.
		settings.WelcomeEnabled = false

		// Disable all command categories.
		for category := range allCategories {
			settings.DisabledCategories[category] = true
		}

		// Save the modified settings.
		if err := db.SaveGroupSettings(groupID, settings); err != nil {
			fmt.Printf("Could not save settings for group %s: %v\n", groupID, err)
			errorCount++
			continue
		}
		updatedCount++
	}

	// 5. Send a confirmation message.
	reply := fmt.Sprintf("✅ Task complete.\n\nSuccessfully disabled all features in *%d* groups.\nFailed to update *%d* groups (see server logs for details).", updatedCount, errorCount)
	utils.SendReply(client, msg.Info.Chat, reply)
}
