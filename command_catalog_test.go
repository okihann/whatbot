package main

import (
	"bot/handlers"
	"bot/i18n"
	"sort"
	"testing"
)

func TestCommandCatalogIntegrity(t *testing.T) {
	canonical := make(map[string]bool, len(i18n.CanonicalCategoryList))
	for _, meta := range i18n.CanonicalCategoryList {
		canonical[meta.Key] = true
	}

	catCounts := make(map[string]int, len(canonical))
	var names []string
	for name := range handlers.PrimaryCommands {
		names = append(names, name)
	}
	sort.Strings(names)

	if len(names) == 0 {
		t.Fatal("expected primary commands to be registered")
	}

	var nonCanonical []string
	var ownerMismatch []string
	var missingEN []string
	var missingID []string
	var nilHandlers []string

	for _, name := range names {
		cmd := handlers.PrimaryCommands[name]
		if cmd.Execute == nil {
			nilHandlers = append(nilHandlers, name)
		}
		if !canonical[cmd.Category] {
			nonCanonical = append(nonCanonical, name+"="+cmd.Category)
		}
		if (cmd.Category == i18n.CatOwner) != cmd.OwnerOnly {
			ownerMismatch = append(ownerMismatch, name)
		}
		hasEN, hasID := i18n.HasBilingualCommandDescription(name)
		if !hasEN {
			missingEN = append(missingEN, name)
		}
		if !hasID {
			missingID = append(missingID, name)
		}
		catCounts[cmd.Category]++
	}

	t.Logf("PrimaryCommands=%d TotalWithAliases=%d", len(handlers.PrimaryCommands), len(handlers.Commands))
	for _, meta := range i18n.CanonicalCategoryList {
		t.Logf("  %-10s (%s): %d", meta.Key, meta.TitleEN, catCounts[meta.Key])
	}

	if len(nilHandlers) > 0 {
		t.Errorf("commands with nil handler: %v", nilHandlers)
	}
	if len(nonCanonical) > 0 {
		t.Errorf("commands with non-canonical category: %v", nonCanonical)
	}
	if len(ownerMismatch) > 0 {
		t.Errorf("commands with owner/category mismatch: %v", ownerMismatch)
	}
	if len(missingEN) > 0 {
		t.Errorf("commands missing EN catalog description (%d): %v", len(missingEN), missingEN)
	}
	if len(missingID) > 0 {
		t.Errorf("commands missing ID catalog description (%d): %v", len(missingID), missingID)
	}
}
