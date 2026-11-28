package tool

import (
	"bot/handlers"
	"bot/i18n"
	"strings"
	"testing"
)

func TestHelpAndMenuRegistrationAndRendering(t *testing.T) {
	for _, name := range []string{"help", "menu", "sa", "soundsticker", "dl", "download", "s", "sticker", "tt", "tiktok", "yt", "ytmp4", "play", "ytmp3", "pin", "pinterest", "fetch", "get", "sz", "shazam"} {
		if _, ok := handlers.Commands[name]; !ok {
			t.Fatalf("expected command or alias %q to be registered", name)
		}
	}

	userCats := menuCategories(false)
	for _, c := range userCats {
		if c == i18n.CatOwner || c == "bot" {
			t.Fatalf("non-owner menu must not expose %q category", c)
		}
	}
	// Verify canonical AND legacy/Indonesian category queries match properly.
	for _, query := range []string{"general", "main", "umum", "media", "downloader", "unduhan", "tools", "search", "alat", "anime"} {
		if _, ok := matchCategory(userCats, query); !ok {
			t.Errorf("expected query %q to match user menuCategories, got %v", query, userCats)
		}
	}

	ownerCats := menuCategories(true)
	for _, q := range []string{"owner", "bot", "pemilik"} {
		if got, ok := matchCategory(ownerCats, q); !ok || got != i18n.CatOwner {
			t.Errorf("expected query %q to match 'owner' in owner menuCategories, got %q (ok=%v)", q, got, ok)
		}
	}

	// English menu rendering
	enMain := renderMainMenuLang(i18n.LangEN, "NopeBot", ".", "Alice", userCats)
	for _, want := range []string{"*NopeBot*", "Alice", "General & Profile", "Downloader & Media", "Tools & Search", "English (EN)", "*0.* 📜 Show All Commands"} {
		if !strings.Contains(enMain, want) {
			t.Errorf("EN main menu missing %q:\n%s", want, enMain)
		}
	}

	// Indonesian menu rendering
	idMain := renderMainMenuLang(i18n.LangID, "NopeBot", ".", "Budi", userCats)
	for _, want := range []string{"*NopeBot*", "Budi", "Umum & Profil", "Unduhan & Media", "Alat & Pencarian", "Bahasa Indonesia (ID)", "*0.* 📜 Tampilkan Semua Perintah"} {
		if !strings.Contains(idMain, want) {
			t.Errorf("ID main menu missing %q:\n%s", want, idMain)
		}
	}

	// Category menu rendering in EN & ID
	enMedia := renderCategoryMenuLang(i18n.LangEN, ".", "media")
	for _, want := range []string{"DOWNLOADER & MEDIA", "`.soundsticker`", "`.sticker`", "`.smeme`", "`.shazam`", "`.dl`", "`.tiktok`"} {
		if !strings.Contains(enMedia, want) {
			t.Errorf("EN media category menu missing %q:\n%s", want, enMedia)
		}
	}
	idMedia := renderCategoryMenuLang(i18n.LangID, ".", "media")
	for _, want := range []string{"UNDUHAN & MEDIA", "`.sticker`", "Ubah foto, GIF, atau video pendek menjadi stiker WebP."} {
		if !strings.Contains(idMedia, want) {
			t.Errorf("ID media category menu missing %q:\n%s", want, idMedia)
		}
	}

	allText := renderAllCommandsLang(i18n.LangEN, ".", userCats)
	if !strings.Contains(allText, "`.dl`") || !strings.Contains(allText, "`.get`") || !strings.Contains(allText, "`.pinterest`") {
		t.Errorf("renderAllCommands missing expected commands")
	}

	// Alias lookup detail in both languages
	enDetail := renderCommandDetailLang(i18n.LangEN, ".", handlers.Commands["sa"])
	if !strings.Contains(enDetail, "`.soundsticker`") || !strings.Contains(enDetail, "Downloader & Media") || !strings.Contains(enDetail, "`sa`") {
		t.Errorf("unexpected EN command detail:\n%s", enDetail)
	}
	idDetail := renderCommandDetailLang(i18n.LangID, ".", handlers.Commands["sa"])
	if !strings.Contains(idDetail, "Info Perintah") || !strings.Contains(idDetail, "Unduhan & Media") || !strings.Contains(idDetail, "Stiker Bersuara") {
		t.Errorf("unexpected ID command detail:\n%s", idDetail)
	}
}
