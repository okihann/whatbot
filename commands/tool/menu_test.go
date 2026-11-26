package tool

import (
	"bot/handlers"
	"strings"
	"testing"
)

func TestHelpAndMenuRegistrationAndRendering(t *testing.T) {
	for _, name := range []string{"help", "menu"} {
		if _, ok := handlers.Commands[name]; !ok {
			t.Fatalf("expected command %q to be registered", name)
		}
	}

	userCats := menuCategories(false)
	for _, c := range userCats {
		if c == "owner" {
			t.Fatalf("non-owner menu must not expose owner category")
		}
	}
	// Verify all user categories (including previously skipped 'tools') are present.
	for _, required := range []string{"downloader", "search", "media", "tools", "anime", "main"} {
		if _, ok := matchCategory(userCats, required); !ok {
			t.Errorf("expected category %q in user menuCategories, got %v", required, userCats)
		}
	}

	ownerCats := menuCategories(true)
	if _, ok := matchCategory(ownerCats, "owner"); !ok {
		t.Errorf("expected 'owner' category in owner menuCategories, got %v", ownerCats)
	}

	mainText := renderMainMenu("Whatbot", "/", "Alice", userCats)
	for _, want := range []string{"*Whatbot*", "Alice", "Downloader", "Tools", "Media", "*0.* 📜 Show All Commands"} {
		if !strings.Contains(mainText, want) {
			t.Errorf("main menu missing %q:\n%s", want, mainText)
		}
	}

	catText := renderCategoryMenu("/", "media")
	for _, want := range []string{"`/sa`", "`/sticker`", "`/smeme`", "`/shazam`"} {
		if !strings.Contains(catText, want) {
			t.Errorf("media category menu missing %q:\n%s", want, catText)
		}
	}

	allText := renderAllCommands("/", userCats)
	if !strings.Contains(allText, "`/dl`") || !strings.Contains(allText, "`/get`") || !strings.Contains(allText, "`/pinterest`") {
		t.Errorf("renderAllCommands missing expected commands")
	}

	detail := renderCommandDetail("/", handlers.Commands["sa"])
	if !strings.Contains(detail, "`/sa`") || !strings.Contains(detail, "Media") {
		t.Errorf("unexpected command detail:\n%s", detail)
	}
}
