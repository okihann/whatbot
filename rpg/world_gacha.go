package rpg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"time"

	"bot/assets"
	db "bot/databases"
	"bot/utils"
)

// Zone represents a region on the Coinsteel Fantasy World Map.
type Zone struct {
	ID         string
	Name       string
	NameID     string
	MinLevel   int
	TravelCost int // Energy to travel here
	X, Y       int // Map coordinates on 720x440 canvas
	Color      color.RGBA
	DescEN     string
	DescID     string
	BaseYen    int64
	RareDrop   Resource
}

// WorldZones defines the 6 explorable regions of Coinsteel.
var WorldZones = []Zone{
	{
		ID: "emerald_plains", Name: "Emerald Plains", NameID: "Dataran Zamrud",
		MinLevel: 1, TravelCost: 0, X: 115, Y: 310, Color: color.RGBA{0x25, 0xd3, 0x66, 0xff},
		DescEN: "Peaceful green meadows & starter farms.", DescID: "Padang hijau damai & pertanian pemula.",
		BaseYen: 120, RareDrop: Iron,
	},
	{
		ID: "whispering_woods", Name: "Whispering Woods", NameID: "Hutan Berbisik",
		MinLevel: 2, TravelCost: 5, X: 255, Y: 185, Color: color.RGBA{0x1e, 0x8f, 0x4e, 0xff},
		DescEN: "Mystic forest hiding ancient chests & goblin camps.", DescID: "Hutan mistis penuh peti kuno & goblin.",
		BaseYen: 210, RareDrop: Fruit,
	},
	{
		ID: "ironfang_mountains", Name: "Ironfang Peaks", NameID: "Puncak Taring Besi",
		MinLevel: 3, TravelCost: 8, X: 405, Y: 115, Color: color.RGBA{0x8c, 0x9b, 0xa8, 0xff},
		DescEN: "Rich iron & gold veins guarded by golems.", DescID: "Tambang besi & emas dijaga golem.",
		BaseYen: 340, RareDrop: Gold,
	},
	{
		ID: "sunken_ruins", Name: "Sunken Ruins", NameID: "Reruntuhan Tenggelam",
		MinLevel: 5, TravelCost: 10, X: 365, Y: 325, Color: color.RGBA{0x38, 0xa3, 0xd1, 0xff},
		DescEN: "Flooded temple holding ancient emeralds & relics.", DescID: "Kuil tenggelam menyimpan zamrud & relik.",
		BaseYen: 520, RareDrop: Emerald,
	},
	{
		ID: "volcanic_caldera", Name: "Volcanic Caldera", NameID: "Kaldera Vulkanik",
		MinLevel: 7, TravelCost: 12, X: 565, Y: 285, Color: color.RGBA{0xe8, 0x5d, 0x3f, 0xff},
		DescEN: "Molten dragon lair brimming with diamonds.", DescID: "Sarang naga lava penuh berlian.",
		BaseYen: 820, RareDrop: Diamond,
	},
	{
		ID: "astral_citadel", Name: "Astral Citadel", NameID: "Benteng Astral",
		MinLevel: 10, TravelCost: 15, X: 600, Y: 105, Color: color.RGBA{0xb5, 0x7b, 0xff, 0xff},
		DescEN: "Floating sky fortress of mythic treasures.", DescID: "Benteng langit harta mitologi.",
		BaseYen: 1350, RareDrop: Diamond,
	},
}

// GetZone looks up a zone by ID.
func GetZone(id string) (Zone, bool) {
	for _, z := range WorldZones {
		if z.ID == id {
			return z, true
		}
	}
	return WorldZones[0], false
}

// PetCatalog defines collectible gacha companion pets.
var PetCatalog = map[string]Pet{
	"ember_fox":         {ID: "ember_fox", Name: "Ember Fox 🦊", Rarity: "Rare", Level: 1, BonusATK: 6, BonusDEF: 3, BonusYenPct: 5, BonusXPPct: 5},
	"iron_pup":          {ID: "iron_pup", Name: "Iron Golem Pup 🤖", Rarity: "Rare", Level: 1, BonusATK: 4, BonusDEF: 10, BonusYenPct: 5, BonusXPPct: 5},
	"emerald_owl":       {ID: "emerald_owl", Name: "Emerald Owl 🦉", Rarity: "Epic", Level: 1, BonusATK: 10, BonusDEF: 8, BonusYenPct: 12, BonusXPPct: 15},
	"mini_dragon":       {ID: "mini_dragon", Name: "Mini Dragon 🐉", Rarity: "Legendary", Level: 1, BonusATK: 24, BonusDEF: 18, BonusYenPct: 20, BonusXPPct: 20},
	"celestial_phoenix": {ID: "celestial_phoenix", Name: "Celestial Phoenix 🔥🕊️", Rarity: "Mythic", Level: 1, BonusATK: 42, BonusDEF: 35, BonusYenPct: 35, BonusXPPct: 35},
}

func init() {
	// Register Gacha-exclusive Mythic & Legendary weapons/armor in GearBlueprints.
	GearBlueprints["celestial_blade"] = GearBlueprint{ID: "celestial_blade", Name: "Celestial Blade ✨", Slot: "weapon", Bonus: 110, Cost: map[Resource]int64{Yen: 999999}}
	GearBlueprints["void_reaper"] = GearBlueprint{ID: "void_reaper", Name: "Void Reaper 🌌", Slot: "weapon", Bonus: 155, Cost: map[Resource]int64{Yen: 999999}}
	GearBlueprints["dragon_emperor_plate"] = GearBlueprint{ID: "dragon_emperor_plate", Name: "Dragon Emperor Plate 🐉", Slot: "armor", Bonus: 135, Cost: map[Resource]int64{Yen: 999999}}
}

// GachaDrop describes a single item rolled from the Gacha banner.
type GachaDrop struct {
	Rarity      string
	Emoji       string
	Name        string
	Description string
}

// RollGacha executes count pulls (1 or 10), updating pity counters and awarding drops directly to p.
func RollGacha(p *Player, count int) []GachaDrop {
	drops := make([]GachaDrop, 0, count)
	for i := 0; i < count; i++ {
		p.GachaTotalPulls++
		p.GachaPityEpic++
		p.GachaPityLegend++

		rarity := "Common"
		roll := rand.Intn(100)
		switch {
		case p.GachaPityLegend >= 50:
			if rand.Intn(4) == 0 {
				rarity = "Mythic"
			} else {
				rarity = "Legendary"
			}
		case roll < 1:
			rarity = "Mythic"
		case roll < 5:
			rarity = "Legendary"
		case p.GachaPityEpic >= 10 || roll < 20:
			rarity = "Epic"
		case roll < 50:
			rarity = "Rare"
		default:
			rarity = "Common"
		}

		if rarity == "Epic" || rarity == "Legendary" || rarity == "Mythic" {
			p.GachaPityEpic = 0
		}
		if rarity == "Legendary" || rarity == "Mythic" {
			p.GachaPityLegend = 0
		}

		drops = append(drops, grantGachaReward(p, rarity))
	}
	return drops
}

func grantGachaReward(p *Player, rarity string) GachaDrop {
	addPet := func(petID string) GachaDrop {
		tpl := PetCatalog[petID]
		for idx := range p.Pets {
			if p.Pets[idx].ID == petID {
				// Duplicate pet levels it up!
				p.Pets[idx].Level++
				return GachaDrop{
					Rarity:      tpl.Rarity,
					Emoji:       rarityEmoji(tpl.Rarity),
					Name:        tpl.Name + fmt.Sprintf(" (Lv.%d UP!)", p.Pets[idx].Level),
					Description: "Duplicate companion boosted its level!",
				}
			}
		}
		p.Pets = append(p.Pets, tpl)
		if p.EquippedPet == "" {
			p.EquippedPet = tpl.ID
		}
		return GachaDrop{
			Rarity:      tpl.Rarity,
			Emoji:       rarityEmoji(tpl.Rarity),
			Name:        tpl.Name,
			Description: fmt.Sprintf("Companion Pet (+%d ATK, +%d DEF, +%d%% XP)", tpl.BonusATK, tpl.BonusDEF, tpl.BonusXPPct),
		}
	}

	switch rarity {
	case "Mythic":
		switch rand.Intn(3) {
		case 0:
			return addPet("celestial_phoenix")
		case 1:
			p.Items["void_reaper"]++
			return GachaDrop{Rarity: "Mythic", Emoji: "🌈", Name: "Void Reaper 🌌", Description: "Mythic Weapon (+155 ATK) — `.equip void_reaper`"}
		default:
			p.Items["dragon_emperor_plate"]++
			return GachaDrop{Rarity: "Mythic", Emoji: "🌈", Name: "Dragon Emperor Plate 🐉", Description: "Mythic Armor (+135 DEF) — `.equip dragon_emperor_plate`"}
		}

	case "Legendary":
		switch rand.Intn(3) {
		case 0:
			return addPet("mini_dragon")
		case 1:
			p.Items["celestial_blade"]++
			return GachaDrop{Rarity: "Legendary", Emoji: "🟡", Name: "Celestial Blade ✨", Description: "Legendary Weapon (+110 ATK) — `.equip celestial_blade`"}
		default:
			p.Resources[Diamond] += 5
			p.Currency[Yen] += 1500
			return GachaDrop{Rarity: "Legendary", Emoji: "🟡", Name: "Royal Diamond Hoard 💎", Description: "+5 Diamonds & +¥1,500"}
		}

	case "Epic":
		switch rand.Intn(3) {
		case 0:
			return addPet("emerald_owl")
		case 1:
			p.Resources[Emerald] += 5
			return GachaDrop{Rarity: "Epic", Emoji: "🟣", Name: "Emerald Cache 💚", Description: "+5 Emeralds"}
		default:
			p.Items["energy_potion"] += 3
			p.Items["hp_potion"] += 2
			return GachaDrop{Rarity: "Epic", Emoji: "🟣", Name: "Alchemist Bundle 🧪", Description: "+3 Energy Potions & +2 HP Potions"}
		}

	case "Rare":
		switch rand.Intn(3) {
		case 0:
			return addPet("ember_fox")
		case 1:
			return addPet("iron_pup")
		default:
			p.Resources[Gold] += 6
			return GachaDrop{Rarity: "Rare", Emoji: "🔵", Name: "Gold Bullion 🪙", Description: "+6 Gold"}
		}

	default:
		switch rand.Intn(3) {
		case 0:
			p.Resources[Iron] += 35
			return GachaDrop{Rarity: "Common", Emoji: "⚪", Name: "Iron Crate 🔩", Description: "+35 Iron"}
		case 1:
			p.Currency[Yen] += 220
			return GachaDrop{Rarity: "Common", Emoji: "⚪", Name: "Coin Pouch 💴", Description: "+¥220"}
		default:
			p.Items["energy_potion"]++
			return GachaDrop{Rarity: "Common", Emoji: "⚪", Name: "Energy Potion ⚡", Description: "+1 Energy Potion"}
		}
	}
}

func rarityEmoji(r string) string {
	switch r {
	case "Mythic":
		return "🌈"
	case "Legendary":
		return "🟡"
	case "Epic":
		return "🟣"
	case "Rare":
		return "🔵"
	default:
		return "⚪"
	}
}

// RenderWorldMapPNG renders the 6-region Coinsteel Fantasy World Map with roads,
// danger badges, visited indicators, and the player's current location pin.
func RenderWorldMapPNG(p *Player) []byte {
	const W, H = 720, 440
	img := image.NewRGBA(image.Rect(0, 0, W, H))

	// Deep parchment-ocean dark gradient background
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			r := uint8(13 + (y*14)/H)
			g := uint8(24 + (x*16)/W)
			b := uint8(34 + (y*20)/H)
			img.SetRGBA(x, y, color.RGBA{r, g, b, 255})
		}
	}

	// Soft continent landmass silhouette
	for y := 48; y < H-42; y++ {
		for x := 42; x < W-42; x++ {
			dx := float64(x-W/2) / 310.0
			dy := float64(y-H/2) / 170.0
			if dx*dx+dy*dy < 1.05 {
				img.SetRGBA(x, y, color.RGBA{24, 39, 48, 255})
			}
		}
	}

	// Draw connecting roads between zones
	roads := [][2]int{{0, 1}, {1, 2}, {0, 3}, {1, 3}, {2, 5}, {3, 4}, {4, 5}}
	for _, rd := range roads {
		z1, z2 := WorldZones[rd[0]], WorldZones[rd[1]]
		drawDashedLine(img, z1.X, z1.Y, z2.X, z2.Y, color.RGBA{110, 140, 155, 255})
	}

	titleFace, _ := utils.NewFace(assets.FontLiberationSansBold, 22)
	labelFace, _ := utils.NewFace(assets.FontLiberationSansBold, 14)
	if titleFace != nil {
		utils.DrawLinesCentered(img, titleFace, []string{"NOPEBOT RPG — WORLD MAP"}, image.Rect(0, 8, W, 42), 24, color.RGBA{0xe9, 0xed, 0xef, 0xff})
	}

	curLoc := "emerald_plains"
	if p != nil && p.Location != "" {
		curLoc = p.Location
	}

	for _, z := range WorldZones {
		isHere := z.ID == curLoc
		radius := 22
		if isHere {
			// Golden outer halo for player's current location
			drawFilledCircle(img, z.X, z.Y, radius+9, color.RGBA{0xf6, 0xc4, 0x53, 0xff})
		}
		drawFilledCircle(img, z.X, z.Y, radius, z.Color)
		drawFilledCircle(img, z.X, z.Y, radius-5, color.RGBA{16, 26, 33, 255})
		drawFilledCircle(img, z.X, z.Y, radius-9, z.Color)

		if labelFace != nil {
			tag := fmt.Sprintf("%s (Lv.%d)", z.Name, z.MinLevel)
			if isHere {
				tag = "★ " + tag
			}
			ink := color.RGBA{0xe9, 0xed, 0xef, 0xff}
			if isHere {
				ink = color.RGBA{0xf6, 0xc4, 0x53, 0xff}
			}
			utils.DrawLinesCentered(img, labelFace, []string{tag}, image.Rect(z.X-105, z.Y+28, z.X+105, z.Y+48), 16, ink)
		}
	}

	if labelFace != nil {
		utils.DrawLinesCentered(img, labelFace, []string{"Use .travel <zone_id> and .explore to discover loot & encounters!"}, image.Rect(0, H-32, W, H-8), 16, color.RGBA{0x86, 0x96, 0xa0, 0xff})
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func drawFilledCircle(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	b := img.Bounds()
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := cx - r; x <= cx+r; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				img.SetRGBA(x, y, col)
			}
		}
	}
}

func drawDashedLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	dx, dy := float64(x1-x0), float64(y1-y0)
	steps := int(math.Hypot(dx, dy))
	for i := 0; i <= steps; i++ {
		if (i/6)%2 == 1 {
			continue
		}
		x := x0 + int(dx*float64(i)/float64(max(1, steps)))
		y := y0 + int(dy*float64(i)/float64(max(1, steps)))
		for oy := -1; oy <= 1; oy++ {
			for ox := -1; ox <= 1; ox++ {
				img.SetRGBA(x+ox, y+oy, col)
			}
		}
	}
}

// --- Shared World Boss Persistence ---

const worldBossKVKey = "rpg_world_boss"

func spawnFreshWorldBoss() *WorldBoss {
	bosses := []struct {
		name string
		zone string
		hp   int64
	}{
		{"Chrono Leviathan 🌊🐉", "sunken_ruins", 2500},
		{"Crimson Magma Titan 🔥👹", "volcanic_caldera", 3500},
		{"Astral Void Sovereign 🌌👑", "astral_citadel", 5000},
	}
	pick := bosses[rand.Intn(len(bosses))]
	return &WorldBoss{
		ID:           fmt.Sprintf("WB-%d", time.Now().Unix()%10000),
		Name:         pick.name,
		Zone:         pick.zone,
		HP:           pick.hp,
		MaxHP:        pick.hp,
		RewardPool:   pick.hp * 2,
		Contributors: map[string]int64{},
		Defeated:     false,
		SpawnedAt:    time.Now().Unix(),
	}
}

func LoadWorldBoss() *WorldBoss {
	store := db.GetBotStore()
	if store != nil {
		if raw, ok, err := store.GetKV(worldBossKVKey); err == nil && ok && raw != "" {
			var wb WorldBoss
			if json.Unmarshal([]byte(raw), &wb) == nil && wb.MaxHP > 0 {
				if wb.Contributors == nil {
					wb.Contributors = map[string]int64{}
				}
				// Respawn automatically if defeated > 30 min ago
				if wb.Defeated && time.Since(time.Unix(wb.SpawnedAt, 0)) > 30*time.Minute {
					fresh := spawnFreshWorldBoss()
					_ = SaveWorldBoss(fresh)
					return fresh
				}
				return &wb
			}
		}
	}
	fresh := spawnFreshWorldBoss()
	_ = SaveWorldBoss(fresh)
	return fresh
}

func SaveWorldBoss(wb *WorldBoss) error {
	store := db.GetBotStore()
	if store == nil || wb == nil {
		return nil
	}
	data, err := json.Marshal(wb)
	if err != nil {
		return err
	}
	return store.SetKV(worldBossKVKey, string(data))
}
