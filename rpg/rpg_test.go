package rpg

import (
	"path/filepath"
	"testing"
	"time"

	db "bot/databases"
)

func TestRPGEngineSQLiteRegenIncomeAndGuilds(t *testing.T) {
	dir := t.TempDir()
	store, err := db.OpenBotStore(filepath.Join(dir, "botdata.db"))
	if err != nil {
		t.Fatalf("OpenBotStore: %v", err)
	}
	defer store.Close()
	db.SetBotStore(store)
	defer db.SetBotStore(nil)

	p, err := CreateNewPlayer("628111222", "Arthur", 22)
	if err != nil {
		t.Fatalf("CreateNewPlayer: %v", err)
	}
	if p.Energy.Max != 35 || p.Currency[Yen] < 250 {
		t.Fatalf("unexpected starting player: %+v", p)
	}

	// Simulate energy drain + 10 minutes elapsed -> +5 Energy regen
	p.Energy.Current = 10
	p.LastRegenUnix = time.Now().Add(-10 * time.Minute).Unix()
	// Add active Iron Mine 2 hours old -> ~72 Iron ready to collect
	p.Buildings = append(p.Buildings, Building{
		InstanceID:       "b-1",
		ID:               "iron_mine",
		Name:             "Iron Mine",
		Level:            1,
		Status:           "active",
		Production:       Production{Resource: Iron, Rate: 36},
		MaxStorage:       500,
		LastIncomeUpdate: time.Now().Add(-2 * time.Hour).Unix(),
		DefenseBonus:     5,
	})
	if err := SavePlayer(p); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	reloaded, err := LoadPlayer("628111222")
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if reloaded.Energy.Current < 15 {
		t.Fatalf("expected regenerated energy >= 15, got %d", reloaded.Energy.Current)
	}

	collected := CollectAllIncome(reloaded)
	if collected[Iron] < 70 {
		t.Fatalf("expected >=70 iron collected after 2h, got %d", collected[Iron])
	}

	// Verify XP level-up & gear bonus
	if !AddRPGXP(reloaded, 150) || reloaded.Level != 2 {
		t.Fatalf("expected level up to 2, got lvl=%d", reloaded.Level)
	}
	baseAtk, _ := HeroAttackDefense(reloaded)
	reloaded.EquippedWeapon = "diamond_excalibur"
	buffedAtk, _ := HeroAttackDefense(reloaded)
	if buffedAtk <= baseAtk {
		t.Fatalf("expected weapon to increase hero attack: base=%d buffed=%d", baseAtk, buffedAtk)
	}
	_ = SavePlayer(reloaded)

	top, err := ListTopPlayers("networth", 5)
	if err != nil || len(top) != 1 || top[0].ID != "628111222" {
		t.Fatalf("unexpected RPG top list: %+v err=%v", top, err)
	}

	// Guild persistence
	g := &Guild{ID: "g-1", Name: "Knights", LeaderID: "628111222", Level: 1, Treasury: 500, Members: []string{"628111222"}}
	if err := SaveGuild(g); err != nil {
		t.Fatalf("SaveGuild: %v", err)
	}
	gotG, err := LoadGuildByName("knights")
	if err != nil || gotG.ID != "g-1" {
		t.Fatalf("LoadGuildByName failed: %+v err=%v", gotG, err)
	}
}
