package rpg

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	db "bot/databases"
)

// LoadPlayer reads a player from sequential WAL SQLite, applies passive
// Energy/HP/MP regeneration and building income accrual, and returns it.
func LoadPlayer(playerID string) (*Player, error) {
	store := db.GetBotStore()
	if store == nil {
		return nil, fmt.Errorf("bot store not initialized")
	}
	raw, ok, err := store.GetRPGPlayerJSON(playerID)
	if err != nil {
		return nil, err
	}
	if !ok || raw == "" {
		return nil, fmt.Errorf("player not found")
	}
	var p Player
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	ensurePlayerDefaults(&p)
	ApplyRegen(&p, time.Now())
	return &p, nil
}

// SavePlayer recalculates NetWorth & Power and persists the player into SQLite.
func SavePlayer(p *Player) error {
	store := db.GetBotStore()
	if store == nil || p == nil || p.ID == "" {
		return nil
	}
	ensurePlayerDefaults(p)
	p.NetWorth = CalculateNetWorth(p)
	power := TotalArmyPower(p) + HeroCombatPower(p)
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return store.SaveRPGPlayerJSON(p.ID, p.Name, p.Level, p.NetWorth, int64(power), string(raw))
}

// CreateNewPlayer initializes a new Coinsteel Empire ruler and saves to SQLite.
func CreateNewPlayer(playerID, name string, age int) (*Player, error) {
	initialEnergy := 30
	if age >= 18 && age <= 35 {
		initialEnergy = 35
	} else if age > 50 {
		initialEnergy = 25
	}
	startingYen := rand.Int63n(151) + 250 // 250 - 400 Yen
	now := time.Now()
	p := &Player{
		ID:       playerID,
		Name:     name,
		Age:      age,
		Level:    1,
		XP:       0,
		NetWorth: startingYen,
		Currency: map[Resource]int64{
			Yen: startingYen,
		},
		Resources: map[Resource]int64{
			Iron:       50,
			Meat:       5,
			Vegetables: 20,
			Fruit:      20,
			Gold:       0,
			Emerald:    0,
			Diamond:    0,
		},
		Items: map[string]int{
			"energy_potion": 1,
		},
		Energy: Energy{Current: initialEnergy, Max: initialEnergy},
		Bank:   Bank{Balance: 0, Capacity: 10000, Level: 1},
		CombatStats: CombatStats{
			Attack:  10,
			Defense: 10,
		},
		Character: Character{
			Level: 1,
			Stats: CharStats{
				Strength:     12,
				Dexterity:    10,
				Intelligence: 10,
				Health:       100,
				MaxHealth:    100,
				Mana:         50,
				MaxMana:      50,
			},
			Skills: DefaultHeroSkills,
		},
		RaidProtectionUntil: now.Add(24 * time.Hour).UnixMilli(),
		LastRegenUnix:       now.Unix(),
		Quest:               DailyQuestState{DateKey: now.Format("2006-01-02")},
	}
	if err := SavePlayer(p); err != nil {
		return nil, err
	}
	return p, nil
}

func ensurePlayerDefaults(p *Player) {
	if p.Currency == nil {
		p.Currency = map[Resource]int64{}
	}
	if p.Resources == nil {
		p.Resources = map[Resource]int64{}
	}
	if p.Items == nil {
		p.Items = map[string]int{}
	}
	if p.Level < 1 {
		p.Level = 1
	}
	if p.Energy.Max < 20 {
		p.Energy.Max = 25
	}
	if p.Bank.Capacity < 10000 {
		p.Bank.Capacity = 10000
	}
	if p.Bank.Level < 1 {
		p.Bank.Level = 1
	}
	if p.Character.Stats.MaxHealth < 100 {
		p.Character.Stats.MaxHealth = 100
		p.Character.Stats.Health = 100
	}
	if p.Character.Stats.MaxMana < 50 {
		p.Character.Stats.MaxMana = 50
		p.Character.Stats.Mana = 50
	}
	if len(p.Character.Skills) == 0 {
		p.Character.Skills = DefaultHeroSkills
	}
	if p.Location == "" {
		p.Location = "emerald_plains"
	}
	if p.VisitedZones == nil {
		p.VisitedZones = map[string]bool{"emerald_plains": true}
	}
	today := time.Now().Format("2006-01-02")
	if p.Quest.DateKey != today {
		p.Quest = DailyQuestState{DateKey: today}
	}
}

// ApplyRegen grants passive Energy (+1 / 2min), HP (+5 / min), Mana (+5 / min),
// and accrues StoredIncome on all active buildings based on elapsed wall time.
func ApplyRegen(p *Player, now time.Time) {
	if p.LastRegenUnix <= 0 {
		p.LastRegenUnix = now.Unix()
	}
	elapsedSec := now.Unix() - p.LastRegenUnix
	if elapsedSec >= 60 {
		mins := int(elapsedSec / 60)
		energyGain := mins / 2
		if energyGain > 0 {
			p.Energy.Current = min(p.Energy.Max, p.Energy.Current+energyGain)
		}
		p.Character.Stats.Health = min(p.Character.Stats.MaxHealth, p.Character.Stats.Health+mins*5)
		p.Character.Stats.Mana = min(p.Character.Stats.MaxMana, p.Character.Stats.Mana+mins*5)
		if energyGain > 0 {
			p.LastRegenUnix += int64(energyGain*2) * 60
		} else {
			p.LastRegenUnix = now.Unix()
		}
	}

	// Accrue passive production on active buildings
	nowSec := now.Unix()
	for i := range p.Buildings {
		b := &p.Buildings[i]
		if b.Status != "active" || b.Production.Rate <= 0 {
			continue
		}
		if b.LastIncomeUpdate <= 0 {
			b.LastIncomeUpdate = nowSec
			continue
		}
		hours := float64(nowSec-b.LastIncomeUpdate) / 3600.0
		if hours <= 0 {
			continue
		}
		// Each level multiplies production by 1 + 0.5*(level-1)
		levelMult := 1.0 + 0.5*float64(max(1, b.Level)-1)
		gained := hours * b.Production.Rate * levelMult
		b.StoredIncome += gained
		maxCap := float64(b.MaxStorage * max(1, b.Level))
		if maxCap > 0 && b.StoredIncome > maxCap {
			b.StoredIncome = maxCap
		}
		b.LastIncomeUpdate = nowSec
	}
}

// CollectAllIncome drains StoredIncome from all active buildings into player's storage.
func CollectAllIncome(p *Player) map[Resource]int64 {
	ApplyRegen(p, time.Now())
	collected := map[Resource]int64{}
	for i := range p.Buildings {
		b := &p.Buildings[i]
		if b.Status != "active" || b.StoredIncome < 1 {
			continue
		}
		whole := int64(b.StoredIncome)
		b.StoredIncome -= float64(whole)
		if b.Production.Resource == Yen {
			p.Currency[Yen] += whole
		} else {
			p.Resources[b.Production.Resource] += whole
		}
		collected[b.Production.Resource] += whole
	}
	return collected
}

// AddRPGXP awards XP and levels up ruler & hero stats cleanly (granting +3 StatPoints per level).
func AddRPGXP(p *Player, xp int64) bool {
	if pet := GetEquippedPet(p); pet != nil && pet.BonusXPPct > 0 {
		xp += (xp * int64(pet.BonusXPPct)) / 100
	}
	p.XP += xp
	leveled := false
	for {
		need := int64(p.Level * 120)
		if p.XP < need {
			break
		}
		p.XP -= need
		p.Level++
		p.Character.Level = p.Level
		p.StatPoints += 3
		p.Energy.Max += 5
		p.Energy.Current = p.Energy.Max
		p.Character.Stats.MaxHealth += 15
		p.Character.Stats.Health = p.Character.Stats.MaxHealth
		p.Character.Stats.MaxMana += 8
		p.Character.Stats.Mana = p.Character.Stats.MaxMana
		p.Character.Stats.Strength += 3
		p.Character.Stats.Dexterity += 2
		p.Character.Stats.Intelligence += 2
		leveled = true
	}
	return leveled
}

// GetEquippedPet returns the player's currently active companion pet, if any.
func GetEquippedPet(p *Player) *Pet {
	if p == nil || p.EquippedPet == "" {
		return nil
	}
	for i := range p.Pets {
		if p.Pets[i].ID == p.EquippedPet {
			return &p.Pets[i]
		}
	}
	return nil
}

// CalculateNetWorth computes total empire net worth across Yen, Bank, Resources, Buildings, and Army.
func CalculateNetWorth(p *Player) int64 {
	total := p.Currency[Yen] + p.Bank.Balance
	for r, qty := range p.Resources {
		total += qty * BaseMarketPrices[r]
	}
	for _, b := range p.Buildings {
		total += int64(b.Level) * 400
	}
	for _, u := range p.Units {
		if bp, ok := UnitBlueprints[u.ID]; ok {
			total += int64(u.Quantity) * bp.CostYen
		}
	}
	total += int64(len(p.Pets)) * 1200
	return total
}

// TotalArmyPower returns combined attack + defense of military units + fortress buildings.
func TotalArmyPower(p *Player) int {
	atk, def := ArmyAttackDefense(p)
	return atk + def
}

// ArmyAttackDefense returns (totalAttack, totalDefense) for empire raids.
func ArmyAttackDefense(p *Player) (int, int) {
	atk := p.CombatStats.Attack
	def := p.CombatStats.Defense
	for _, u := range p.Units {
		atk += u.Quantity * u.Attack
		def += u.Quantity * u.Defense
	}
	for _, b := range p.Buildings {
		if b.Status == "active" {
			def += b.DefenseBonus * max(1, b.Level)
		}
	}
	return atk, def
}

// HeroCombatPower computes the hero's effective duel/hunt attack & defense score.
func HeroCombatPower(p *Player) int {
	atk, def := HeroAttackDefense(p)
	return atk + def + p.Character.Stats.MaxHealth/10
}

// HeroAttackDefense returns (attack, defense) for PvE hunting, World Boss, & PvP duels.
func HeroAttackDefense(p *Player) (int, int) {
	atk := p.Character.Stats.Strength + p.Character.Stats.Dexterity/2
	def := p.Character.Stats.Dexterity / 2
	if w, ok := GearBlueprints[p.EquippedWeapon]; ok {
		atk += w.Bonus
	}
	if a, ok := GearBlueprints[p.EquippedArmor]; ok {
		def += a.Bonus
	}
	if pet := GetEquippedPet(p); pet != nil {
		atk += pet.BonusATK + pet.Level*2
		def += pet.BonusDEF + pet.Level
	}
	return atk, def
}

// BankCapacity returns base bank capacity plus all active Royal Vault bonuses.
func EffectiveBankCapacity(p *Player) int64 {
	cap := p.Bank.Capacity
	for _, b := range p.Buildings {
		if b.Status == "active" && b.ID == "bank_vault" {
			cap += int64(b.Level) * 15000
		}
	}
	return cap
}

// ListTopPlayers fetches top RPG players from SQLite ordered by networth, level, or power.
func ListTopPlayers(orderBy string, limit int) ([]Player, error) {
	store := db.GetBotStore()
	if store == nil {
		return nil, nil
	}
	raws, err := store.ListRPGPlayersJSON(orderBy, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Player, 0, len(raws))
	for _, raw := range raws {
		var p Player
		if json.Unmarshal([]byte(raw), &p) == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// Guild persistence helpers
func SaveGuild(g *Guild) error {
	store := db.GetBotStore()
	if store == nil || g == nil {
		return nil
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return store.SaveRPGGuildJSON(g.ID, g.Name, g.LeaderID, g.Level, g.Treasury, string(raw))
}

func LoadGuildByName(name string) (*Guild, error) {
	store := db.GetBotStore()
	if store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	raw, ok, err := store.GetRPGGuildByNameJSON(name)
	if err != nil || !ok {
		return nil, fmt.Errorf("guild not found")
	}
	var g Guild
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func LoadGuildByID(id string) (*Guild, error) {
	store := db.GetBotStore()
	if store == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	raw, ok, err := store.GetRPGGuildByIDJSON(id)
	if err != nil || !ok {
		return nil, fmt.Errorf("guild not found")
	}
	var g Guild
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, err
	}
	return &g, nil
}
