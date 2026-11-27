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

func TestRPGWorldMapGachaPetsAndWorldBoss(t *testing.T) {
	dir := t.TempDir()
	store, err := db.OpenBotStore(filepath.Join(dir, "botdata.db"))
	if err != nil {
		t.Fatalf("OpenBotStore: %v", err)
	}
	defer store.Close()
	db.SetBotStore(store)
	defer db.SetBotStore(nil)

	p, err := CreateNewPlayer("628999000", "Lancelot", 24)
	if err != nil {
		t.Fatalf("CreateNewPlayer: %v", err)
	}
	if p.Location != "emerald_plains" {
		t.Fatalf("expected starting location emerald_plains, got %s", p.Location)
	}

	// 1. Visual World Map PNG
	pngBytes := RenderWorldMapPNG(p)
	if len(pngBytes) < 100 || string(pngBytes[:4]) != "\x89PNG" {
		t.Fatalf("expected valid PNG bytes from RenderWorldMapPNG, got len=%d", len(pngBytes))
	}

	// 2. Gacha 10-pull Epic+ pity and 50-pull Legendary/Mythic hard pity
	p.GachaPityEpic = 9
	drops := RollGacha(p, 1)
	if len(drops) != 1 || (drops[0].Rarity != "Epic" && drops[0].Rarity != "Legendary" && drops[0].Rarity != "Mythic") {
		t.Fatalf("expected Epic+ on 10th pity pull, got %+v", drops)
	}
	p.GachaPityLegend = 49
	dropsLegend := RollGacha(p, 1)
	if len(dropsLegend) != 1 || (dropsLegend[0].Rarity != "Legendary" && dropsLegend[0].Rarity != "Mythic") {
		t.Fatalf("expected Legendary or Mythic on 50th hard pity pull, got %+v", dropsLegend)
	}
	if p.GachaPityLegend != 0 {
		t.Fatalf("expected legendary pity counter reset to 0, got %d", p.GachaPityLegend)
	}

	// 3. Companion Pet combat & stat points bonus
	baseAtk, baseDef := HeroAttackDefense(p)
	p.Pets = append(p.Pets, PetCatalog["mini_dragon"])
	p.EquippedPet = "mini_dragon"
	petAtk, petDef := HeroAttackDefense(p)
	if petAtk <= baseAtk || petDef <= baseDef {
		t.Fatalf("expected pet to boost hero ATK/DEF: before=(%d,%d) after=(%d,%d)", baseAtk, baseDef, petAtk, petDef)
	}
	if !AddRPGXP(p, 200) || p.StatPoints < 3 {
		t.Fatalf("expected level up to grant >=3 StatPoints, got lvl=%d pts=%d", p.Level, p.StatPoints)
	}

	// 4. Shared World Boss SQLite KV persistence
	wb := LoadWorldBoss()
	if wb == nil || wb.MaxHP <= 0 {
		t.Fatalf("expected active WorldBoss, got %+v", wb)
	}
	wb.HP -= 500
	wb.Contributors[p.ID] = 500
	if err := SaveWorldBoss(wb); err != nil {
		t.Fatalf("SaveWorldBoss: %v", err)
	}
	reloadedWB := LoadWorldBoss()
	if reloadedWB.Contributors[p.ID] != 500 || reloadedWB.HP != wb.HP {
		t.Fatalf("WorldBoss did not persist cleanly: got %+v", reloadedWB)
	}
}
