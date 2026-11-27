package botcmd

import (
	"testing"

	"bot/handlers"
)

func TestJadiBotAndAllBotCommandsAreLockedToOwner(t *testing.T) {
	for _, name := range []string{"jadibot", "scanbot", "lockjadibot", "bot", "nsfw", "gostats", "msgstats"} {
		cmd, ok := handlers.Commands[name]
		if !ok {
			t.Fatalf("expected command %q to be registered", name)
		}
		if !cmd.OwnerOnly {
			t.Fatalf("expected %q to have OwnerOnly=true (locked)", name)
		}
	}
	for _, cmd := range handlers.PrimaryCommands {
		if cmd.Category == "bot" && !cmd.OwnerOnly {
			t.Fatalf("expected all 'bot' category commands to have OwnerOnly=true, but %q has OwnerOnly=false", cmd.Name)
		}
	}
}
