package botcmd

import (
	"testing"

	"bot/handlers"
)

func TestJadiBotIsLockedToOwner(t *testing.T) {
	for _, name := range []string{"jadibot", "scanbot", "lockjadibot"} {
		cmd, ok := handlers.Commands[name]
		if !ok {
			t.Fatalf("expected command %q to be registered", name)
		}
		if !cmd.OwnerOnly {
			t.Fatalf("expected %q to have OwnerOnly=true (locked)", name)
		}
	}
}
