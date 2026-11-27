package rpg

import (
	"math"
	"time"
)

type BuildingBlueprint struct {
	ID            string
	Name          string
	NameID        string
	Description   string
	MaxLevel      int
	Cost          map[Resource]int64
	Production    Production // Rate = units per hour at level 1
	MaxStorage    int
	RequiredLabor int
	DefenseBonus  int
	BankBonus     int64
}

type UnitBlueprint struct {
	ID      string
	Name    string
	CostYen int64
	CostIrn int64
	Attack  int
	Defense int
}

type GearBlueprint struct {
	ID    string
	Name  string
	Slot  string // "weapon" or "armor"
	Bonus int    // ATK for weapon, DEF/MaxHP for armor
	Cost  map[Resource]int64
}

type MonsterTemplate struct {
	Name      string
	MinLevel  int
	HP        int
	Attack    int
	RewardYen int64
	RewardXP  int64
	DropMeat  int64
	RareDrop  Resource
}

type ShopItem struct {
	ID       string
	Name     string
	PriceYen int64
	DescID   string
	DescEN   string
}

var (
	BuildingBlueprints = map[string]BuildingBlueprint{
		"iron_mine": {
			ID: "iron_mine", Name: "Iron Mine", NameID: "Tambang Besi",
			Description: "Extracts raw iron for construction, gear, and armies.",
			MaxLevel:    15, Cost: map[Resource]int64{Yen: 400, Iron: 40},
			Production: Production{Resource: Iron, Rate: 36}, MaxStorage: 500, RequiredLabor: 60, DefenseBonus: 5,
		},
		"farm": {
			ID: "farm", Name: "Vegetable Farm", NameID: "Kebun Sayur",
			Description: "Cultivates fresh vegetables to restore energy.",
			MaxLevel:    15, Cost: map[Resource]int64{Yen: 250, Iron: 25},
			Production: Production{Resource: Vegetables, Rate: 45}, MaxStorage: 600, RequiredLabor: 50, DefenseBonus: 2,
		},
		"orchard": {
			ID: "orchard", Name: "Fruit Orchard", NameID: "Kebun Buah",
			Description: "Produces sweet fruit for fast energy recovery.",
			MaxLevel:    15, Cost: map[Resource]int64{Yen: 300, Iron: 30},
			Production: Production{Resource: Fruit, Rate: 40}, MaxStorage: 600, RequiredLabor: 50, DefenseBonus: 2,
		},
		"ranch": {
			ID: "ranch", Name: "Livestock Ranch", NameID: "Peternakan",
			Description: "Raises livestock for hearty meat (+HP & +Energy).",
			MaxLevel:    12, Cost: map[Resource]int64{Yen: 550, Iron: 50},
			Production: Production{Resource: Meat, Rate: 25}, MaxStorage: 400, RequiredLabor: 70, DefenseBonus: 3,
		},
		"market_stall": {
			ID: "market_stall", Name: "Market Stall", NameID: "Pasar Kerajaan",
			Description: "Generates steady passive Yen income every hour.",
			MaxLevel:    20, Cost: map[Resource]int64{Yen: 600, Iron: 60},
			Production: Production{Resource: Yen, Rate: 180}, MaxStorage: 3000, RequiredLabor: 70, DefenseBonus: 2,
		},
		"gold_mine": {
			ID: "gold_mine", Name: "Gold Mine", NameID: "Tambang Emas",
			Description: "Extracts precious gold ore for high-value market trading.",
			MaxLevel:    12, Cost: map[Resource]int64{Yen: 1500, Iron: 150},
			Production: Production{Resource: Gold, Rate: 12}, MaxStorage: 200, RequiredLabor: 100, DefenseBonus: 8,
		},
		"emerald_quarry": {
			ID: "emerald_quarry", Name: "Emerald Quarry", NameID: "Tambang Zamrud",
			Description: "Mines rare emeralds used in elite weapons and armor.",
			MaxLevel:    10, Cost: map[Resource]int64{Yen: 3500, Iron: 300, Gold: 15},
			Production: Production{Resource: Emerald, Rate: 6}, MaxStorage: 120, RequiredLabor: 120, DefenseBonus: 12,
		},
		"diamond_drill": {
			ID: "diamond_drill", Name: "Diamond Drill", NameID: "Bor Berlian",
			Description: "Deep-core drill extracting ultra-rare diamonds.",
			MaxLevel:    10, Cost: map[Resource]int64{Yen: 7500, Iron: 500, Emerald: 10},
			Production: Production{Resource: Diamond, Rate: 3}, MaxStorage: 60, RequiredLabor: 150, DefenseBonus: 15,
		},
		"fortress": {
			ID: "fortress", Name: "Steel Fortress", NameID: "Benteng Baja",
			Description: "Massive empire defense bonus protecting against enemy raids.",
			MaxLevel:    15, Cost: map[Resource]int64{Yen: 1200, Iron: 200},
			Production: Production{Resource: Yen, Rate: 0}, MaxStorage: 0, RequiredLabor: 90, DefenseBonus: 45,
		},
		"bank_vault": {
			ID: "bank_vault", Name: "Royal Vault", NameID: "Brankas Kerajaan",
			Description: "Expands your Bank storage capacity by +¥15,000 per level.",
			MaxLevel:    15, Cost: map[Resource]int64{Yen: 1000, Iron: 120},
			Production: Production{Resource: Yen, Rate: 0}, MaxStorage: 0, RequiredLabor: 80, DefenseBonus: 15, BankBonus: 15000,
		},
	}

	UnitBlueprints = map[string]UnitBlueprint{
		"militia":  {ID: "militia", Name: "Militia", CostYen: 120, CostIrn: 10, Attack: 6, Defense: 5},
		"archer":   {ID: "archer", Name: "Archer", CostYen: 220, CostIrn: 20, Attack: 12, Defense: 6},
		"knight":   {ID: "knight", Name: "Knight", CostYen: 450, CostIrn: 45, Attack: 22, Defense: 20},
		"catapult": {ID: "catapult", Name: "Catapult", CostYen: 1100, CostIrn: 120, Attack: 55, Defense: 15},
	}

	GearBlueprints = map[string]GearBlueprint{
		"iron_sword":        {ID: "iron_sword", Name: "Iron Sword", Slot: "weapon", Bonus: 10, Cost: map[Resource]int64{Yen: 300, Iron: 40}},
		"steel_greatsword":  {ID: "steel_greatsword", Name: "Steel Greatsword", Slot: "weapon", Bonus: 22, Cost: map[Resource]int64{Yen: 900, Iron: 120, Gold: 5}},
		"emerald_blade":     {ID: "emerald_blade", Name: "Emerald Blade", Slot: "weapon", Bonus: 40, Cost: map[Resource]int64{Yen: 2500, Iron: 200, Emerald: 12}},
		"diamond_excalibur": {ID: "diamond_excalibur", Name: "Diamond Excalibur", Slot: "weapon", Bonus: 75, Cost: map[Resource]int64{Yen: 6000, Gold: 25, Diamond: 8}},
		"leather_vest":      {ID: "leather_vest", Name: "Leather Vest", Slot: "armor", Bonus: 10, Cost: map[Resource]int64{Yen: 250, Meat: 15}},
		"iron_armor":        {ID: "iron_armor", Name: "Iron Armor", Slot: "armor", Bonus: 25, Cost: map[Resource]int64{Yen: 850, Iron: 110}},
		"emerald_plate":     {ID: "emerald_plate", Name: "Emerald Plate", Slot: "armor", Bonus: 45, Cost: map[Resource]int64{Yen: 2400, Iron: 180, Emerald: 10}},
		"diamond_aegis":     {ID: "diamond_aegis", Name: "Diamond Aegis", Slot: "armor", Bonus: 80, Cost: map[Resource]int64{Yen: 6500, Gold: 20, Diamond: 10}},
	}

	DefaultHeroSkills = []Skill{
		{ID: "slash", Name: "Power Slash", Power: 18, ManaCost: 10},
		{ID: "fireball", Name: "Fireball", Power: 28, ManaCost: 20},
		{ID: "shield", Name: "Shield Wall", Power: 0, ManaCost: 12},
		{ID: "heal", Name: "Holy Heal", Power: 35, ManaCost: 18},
	}

	HuntMonsters = []MonsterTemplate{
		{Name: "Forest Slime 🟢", MinLevel: 1, HP: 40, Attack: 6, RewardYen: 90, RewardXP: 25, DropMeat: 2},
		{Name: "Shadow Wolf 🐺", MinLevel: 1, HP: 65, Attack: 11, RewardYen: 150, RewardXP: 40, DropMeat: 4},
		{Name: "Goblin Raider 👺", MinLevel: 2, HP: 95, Attack: 16, RewardYen: 240, RewardXP: 65, DropMeat: 5, RareDrop: Iron},
		{Name: "Iron Golem 🤖", MinLevel: 4, HP: 160, Attack: 24, RewardYen: 420, RewardXP: 110, DropMeat: 6, RareDrop: Gold},
		{Name: "Emerald Wyvern 🐉", MinLevel: 6, HP: 260, Attack: 35, RewardYen: 750, RewardXP: 180, DropMeat: 10, RareDrop: Emerald},
		{Name: "Ancient Diamond Dragon 💎🐉", MinLevel: 9, HP: 420, Attack: 52, RewardYen: 1500, RewardXP: 320, DropMeat: 18, RareDrop: Diamond},
	}

	ShopItems = []ShopItem{
		{ID: "energy_potion", Name: "Energy Potion ⚡", PriceYen: 180, DescID: "Pulihkan +20 Energi secara instan.", DescEN: "Restores +20 Energy instantly."},
		{ID: "hp_potion", Name: "Health Potion ❤️", PriceYen: 150, DescID: "Pulihkan +60 HP & +30 Mana.", DescEN: "Restores +60 HP & +30 Mana."},
		{ID: "raid_shield", Name: "12h Raid Shield 🛡️", PriceYen: 800, DescID: "Lindungi kerajaan dari serangan raid selama 12 jam.", DescEN: "Protects your empire from raids for 12 hours."},
		{ID: "iron_pack", Name: "Iron Pack (x50) 🔩", PriceYen: 350, DescID: "Tambahkan +50 Besi ke gudang.", DescEN: "Adds +50 Iron to storage."},
	}

	BaseMarketPrices = map[Resource]int64{
		Iron:       6,
		Vegetables: 4,
		Fruit:      5,
		Meat:       12,
		Gold:       85,
		Emerald:    210,
		Diamond:    520,
	}
)

// MarketPrice returns a smooth, deterministic hourly fluctuating sell price for r.
func MarketPrice(r Resource) int64 {
	base, ok := BaseMarketPrices[r]
	if !ok {
		return 0
	}
	hourSlot := float64(time.Now().Unix() / 3600)
	phase := float64(len(string(r))*7) * 0.618
	mult := 1.0 + 0.20*math.Sin(hourSlot*0.7+phase)
	p := int64(math.Round(float64(base) * mult))
	if p < 1 {
		p = 1
	}
	return p
}

// GetBuildingData looks up a blueprint by ID.
func GetBuildingData(id string) (BuildingBlueprint, bool) {
	b, ok := BuildingBlueprints[id]
	return b, ok
}
