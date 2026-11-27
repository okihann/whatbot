package db

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBotStoreFullLifecycle(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenBotStore(filepath.Join(dir, "botdata.db"))
	if err != nil {
		t.Fatalf("OpenBotStore: %v", err)
	}
	defer store.Close()

	if got := strings.ToLower(store.Status()["journal_mode"]); got != "wal" {
		t.Fatalf("expected WAL journal_mode, got %q", got)
	}

	// Tenants
	if err := store.SaveTenant(&BotTenant{
		AccountID:  "628111",
		OwnerJID:   "628222",
		BotName:    "BuyerBot",
		SelfMode:   true,
		RejectCall: true,
		PacingMS:   500,
	}); err != nil {
		t.Fatalf("SaveTenant: %v", err)
	}
	tenant, err := store.GetTenant("628111")
	if err != nil || tenant.BotName != "BuyerBot" || !tenant.SelfMode || tenant.PacingMS != 500 {
		t.Fatalf("unexpected tenant: %+v err=%v", tenant, err)
	}

	// Users, XP, Balance, Redeem
	lvl, leveled := store.AddUserActivity("628333", "Alice", 150)
	if !leveled || lvl != 2 {
		t.Fatalf("expected level 2 leveledUp=true, got lvl=%d leveled=%v", lvl, leveled)
	}
	bal, err := store.AddBalance("628333", 250, "daily")
	if err != nil || bal != 350 {
		t.Fatalf("expected balance 350, got %d err=%v", bal, err)
	}
	hist, _ := store.GetBalanceHistory("628333", 5)
	if len(hist) != 1 || hist[0].Delta != 250 {
		t.Fatalf("unexpected balance history: %+v", hist)
	}

	// Group config & member state
	cfg, _ := store.GetGroupConfig("120363@g.us")
	cfg.AntiLink = true
	cfg.BadWords = []string{"spamword"}
	cfg.BadWordEnabled = true
	if err := store.SaveGroupConfig(cfg); err != nil {
		t.Fatalf("SaveGroupConfig: %v", err)
	}
	cfg2, _ := store.GetGroupConfig("120363@g.us")
	if !cfg2.AntiLink || !cfg2.BadWordEnabled || len(cfg2.BadWords) != 1 {
		t.Fatalf("unexpected reloaded GroupConfig: %+v", cfg2)
	}
	store.TouchGroupMember("120363@g.us", "628333")
	store.TouchGroupMember("120363@g.us", "628333")
	m, _ := store.GetGroupMember("120363@g.us", "628333")
	if m.MsgCount != 2 {
		t.Fatalf("expected msgCount=2, got %d", m.MsgCount)
	}

	// Game stats & leaderboard
	_ = store.RecordGameResult("628333", "120363@g.us", "wordle", true, false, 40)
	_ = store.RecordGameResult("628333", "120363@g.us", "wordle", true, false, 40)
	lb, _ := store.GameLeaderboard("120363@g.us", 5)
	if len(lb) != 1 || lb[0].Points != 80 || lb[0].Wins != 2 || lb[0].Best != 2 {
		t.Fatalf("unexpected leaderboard: %+v", lb)
	}
}
