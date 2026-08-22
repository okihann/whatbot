package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"fmt"
	"os/exec"
	//"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{
		Name:        "ytversion",
		Description: "Checks the embedded version of yt-dlp. (Owner only)",
		Category:    "owner",
		Execute:     executeYtVersion,
	})
}

// executeYtVersion runs yt-dlp --version
func executeYtVersion(client *whatsmeow.Client, msg *events.Message, args []string) {
	// A new, temporary function to get the path
	ytDlpPath, cleanup, err := utils.GetYtDlpPathForVersionCheck()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Failed to get yt-dlp path: %v", err))
		return
	}
	defer cleanup()

	cmd := exec.Command(ytDlpPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Failed to run yt-dlp --version: %v\nOutput: %s", err, string(output)))
		return
	}

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Embedded yt-dlp version:\n`%s`", string(output)))
}
