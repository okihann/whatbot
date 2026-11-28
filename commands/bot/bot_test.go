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
		if !cmd.OwnerOnly || cmd.Category != "owner" {
			t.Fatalf("expected %q to have OwnerOnly=true and Category='owner', got owner=%v cat=%q", name, cmd.OwnerOnly, cmd.Category)
		}
	}
	for _, cmd := range handlers.PrimaryCommands {
		if cmd.Category == "owner" && !cmd.OwnerOnly {
			t.Fatalf("expected all 'owner' category commands to have OwnerOnly=true, but %q has OwnerOnly=false", cmd.Name)
		}
	}
}
