package rpg

// Resource represents currencies and raw empire resources in Coinsteel RPG.
type Resource string

const (
	Yen        Resource = "yen"
	Iron       Resource = "iron"
	Meat       Resource = "meat"
	Vegetables Resource = "vegetables"
	Fruit      Resource = "fruit"
	Gold       Resource = "gold"
	Emerald    Resource = "emerald"
	Diamond    Resource = "diamond"
)

// Player holds the complete Empire + Hero RPG state persisted in SQLite.
type Player struct {
	ID                  string             `json:"id"`
	Name                string             `json:"name"`
	Age                 int                `json:"age"`
	Level               int                `json:"level"`
	XP                  int64              `json:"xp"`
	NetWorth            int64              `json:"netWorth"`
	Currency            map[Resource]int64 `json:"currency"`
	Resources           map[Resource]int64 `json:"resources"`
	Items               map[string]int     `json:"items"` // potions, shields, crafted gear
	EquippedWeapon      string             `json:"equippedWeapon"`
	EquippedArmor       string             `json:"equippedArmor"`
	Energy              Energy             `json:"energy"`
	Buildings           []Building         `json:"buildings"`
	Units               []Unit             `json:"units"`
	CombatStats         CombatStats        `json:"combatStats"`
	Character           Character          `json:"character"`
	Bank                Bank               `json:"bank"`
	Cooldowns           Cooldowns          `json:"cooldowns"`
	RaidProtectionUntil int64              `json:"raidProtectionUntil"`
	ActiveWorksiteID    string             `json:"activeWorksiteId"`
	GuildID             string             `json:"guildId"`
	Quest               DailyQuestState    `json:"quest"`
	LastRegenUnix       int64              `json:"lastRegenUnix"`
	Location            string             `json:"location"`
	VisitedZones        map[string]bool    `json:"visitedZones"`
	StatPoints          int                `json:"statPoints"`
	GachaPityEpic       int                `json:"gachaPityEpic"`
	GachaPityLegend     int                `json:"gachaPityLegend"`
	GachaTotalPulls     int                `json:"gachaTotalPulls"`
	Pets                []Pet              `json:"pets"`
	EquippedPet         string             `json:"equippedPet"`
	Relics              []string           `json:"relics"`
	Title               string             `json:"title"`
	UnlockedTitles      []string           `json:"unlockedTitles"`
	MonstersSlain       int                `json:"monstersSlain"`
	DungeonsCleared     int                `json:"dungeonsCleared"`
	RaidsWon            int                `json:"raidsWon"`
	WorldBossDamage     int64              `json:"worldBossDamage"`
}

type Pet struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Rarity      string `json:"rarity"` // Rare, Epic, Legendary, Mythic
	Level       int    `json:"level"`
	XP          int    `json:"xp"`
	BonusATK    int    `json:"bonusAtk"`
	BonusDEF    int    `json:"bonusDef"`
	BonusYenPct int    `json:"bonusYenPct"`
	BonusXPPct  int    `json:"bonusXpPct"`
}

type WorldBoss struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Zone         string           `json:"zone"`
	HP           int64            `json:"hp"`
	MaxHP        int64            `json:"maxHp"`
	RewardPool   int64            `json:"rewardPool"`
	Contributors map[string]int64 `json:"contributors"`
	Defeated     bool             `json:"defeated"`
	SpawnedAt    int64            `json:"spawnedAt"`
}

type Energy struct {
	Current int `json:"current"`
	Max     int `json:"max"`
}

type CombatStats struct {
	Attack  int `json:"attack"`
	Defense int `json:"defense"`
}

type Bank struct {
	Balance  int64 `json:"balance"`
	Capacity int64 `json:"capacity"`
	Level    int   `json:"level"`
}

type Cooldowns struct {
	LastRaid      int64 `json:"lastRaid"`
	LastJob       int64 `json:"lastJob"`
	LastHunt      int64 `json:"lastHunt"`
	LastMine      int64 `json:"lastMine"`
	LastDungeon   int64 `json:"lastDungeon"`
	LastExplore   int64 `json:"lastExplore"`
	LastWorldBoss int64 `json:"lastWorldBoss"`
}

type Character struct {
	Level       int       `json:"level"`
	XP          int       `json:"xp"`
	Stats       CharStats `json:"stats"`
	Skills      []Skill   `json:"skills"`
	InDuel      bool      `json:"inDuel"`
	IsDefending bool      `json:"isDefending"`
}

type CharStats struct {
	Strength     int `json:"strength"`
	Dexterity    int `json:"dexterity"`
	Intelligence int `json:"intelligence"`
	Health       int `json:"health"`
	MaxHealth    int `json:"maxHealth"`
	Mana         int `json:"mana"`
	MaxMana      int `json:"maxMana"`
}

type Skill struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Power    int    `json:"power"`
	ManaCost int    `json:"manaCost"`
}

type Building struct {
	InstanceID       string     `json:"instanceId"`
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Level            int        `json:"level"`
	Status           string     `json:"status"` // "active" or "under_construction"
	CurrentLabor     int        `json:"currentLabor"`
	RequiredLabor    int        `json:"requiredLabor"`
	Production       Production `json:"production"`
	StoredIncome     float64    `json:"storedIncome"`
	MaxStorage       int        `json:"maxStorage"`
	LastIncomeUpdate int64      `json:"lastIncomeUpdate"`
	DefenseBonus     int        `json:"defenseBonus"`
}

type Production struct {
	Resource Resource `json:"resource"`
	Rate     float64  `json:"rate"` // units per hour at level 1
}

type Unit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Attack   int    `json:"attack"`
	Defense  int    `json:"defense"`
}

type DailyQuestState struct {
	DateKey  string `json:"dateKey"`
	Works    int    `json:"works"`
	Hunts    int    `json:"hunts"`
	Mines    int    `json:"mines"`
	Collects int    `json:"collects"`
	Claimed  bool   `json:"claimed"`
}

type Guild struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	LeaderID  string   `json:"leaderId"`
	Level     int      `json:"level"`
	Treasury  int64    `json:"treasury"`
	Members   []string `json:"members"`
	CreatedAt int64    `json:"createdAt"`
}
