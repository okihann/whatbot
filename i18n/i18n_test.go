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

func TestCategoryNormalizationAndBilingualCatalog(t *testing.T) {
	cases := map[string]string{
		"main":           CatGeneral,
		"profile":        CatGeneral,
		"confess":        CatGeneral,
		"downloader":     CatMedia,
		"youtube":        CatMedia,
		"music":          CatMedia,
		"sticker":        CatMedia,
		"audiofx":        CatMedia,
		"search":         CatTools,
		"fun":            CatGames,
		"game":           CatGames,
		"minigame":       CatGames,
		"animanga":       CatAnime,
		"waifu":          CatAnime,
		"group_admin":    CatGroup,
		"group_security": CatGroup,
		"bot":            CatOwner,
		"pemilik":        CatOwner,
		"unduhan":        CatMedia,
		"alat":           CatTools,
	}
	for raw, want := range cases {
		if got := NormalizeCategory(raw); got != want {
			t.Errorf("NormalizeCategory(%q) = %q, want %q", raw, got, want)
		}
	}

	if CategoryTitle(LangID, CatMedia) != "Unduhan & Media" || CategoryTitle(LangEN, CatMedia) != "Downloader & Media" {
		t.Fatalf("unexpected localized category titles for media")
	}

	enDesc := CommandDescription(LangEN, "auto-get", "")
	idDesc := CommandDescription(LangID, "auto-get", "")
	if enDesc == "" || idDesc == "" || enDesc == idDesc {
		t.Fatalf("expected distinct bilingual descriptions for auto-get, got en=%q id=%q", enDesc, idDesc)
	}
}
