package i18n

import (
	"strings"
	"testing"
)

func TestBilingualTranslations(t *testing.T) {
	idLock := T(LangID, "err.jadibot_locked")
	enLock := T(LangEN, "err.jadibot_locked")
	if !strings.Contains(idLock, "Dikunci") || !strings.Contains(enLock, "Locked") {
		t.Fatalf("unexpected lock strings: id=%q en=%q", idLock, enLock)
	}

	formatted := T(LangID, "confess.delivered", "CF-1234", "Rahasia")
	if !strings.Contains(formatted, "#CF-1234") || !strings.Contains(formatted, "Rahasia") {
		t.Fatalf("unexpected formatted confess string: %q", formatted)
	}

	// Unknown language falls back to DefaultLang (Indonesian)
	if got := T("fr", "err.owner_only"); got != T(DefaultLang, "err.owner_only") {
		t.Fatalf("expected fallback to default lang, got %q", got)
	}
}
