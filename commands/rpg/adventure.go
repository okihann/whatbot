package rpgcmd

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"bot/handlers"
	"bot/i18n"
	"bot/rpg"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	cmds := []types.Command{
		// World Map & Exploration
		{Name: "map", Aliases: []string{"worldmap", "peta"}, Category: "rpg", Description: "Render the visual Coinsteel World Map PNG showing your location and all 6 regions.", Execute: cmdMap},
		{Name: "travel", Aliases: []string{"go", "pergi"}, Category: "rpg", Description: "Travel to another region on the World Map. Usage: .travel <zone_id>", Execute: cmdTravel},
		{Name: "explore", Aliases: []string{"jelajah"}, Category: "rpg", Description: "Explore your current World Map region for treasure chests, shrines, and encounters (-8 Energy).", Execute: cmdExplore},

		// Gacha Banner & Pity
		{Name: "gacha", Aliases: []string{"pull", "summon"}, Category: "rpg", Description: "Pull from the NopeBot Mythic Gacha Banner! Usage: .gacha [1|10] (¥500 / ¥4,500)", Execute: cmdGacha},
		{Name: "pity", Aliases: []string{"banner"}, Category: "rpg", Description: "Check Gacha drop rates, featured Mythic gear/pets, and your current pity counters.", Execute: cmdPity},

		// Companion Pets
		{Name: "pets", Aliases: []string{"pet"}, Category: "rpg", Description: "View your collected Gacha companion pets & passive bonuses.", Execute: cmdPets},
		{Name: "petequip", Category: "rpg", Description: "Equip a companion pet for combat & XP bonuses. Usage: .petequip <pet_id>", Execute: cmdPetEquip},
		{Name: "petfeed", Category: "rpg", Description: "Feed meat/fruit to your equipped pet to level it up!", Execute: cmdPetFeed},

		// Shared World Boss
		{Name: "worldboss", Aliases: []string{"wb"}, Category: "rpg", Description: "View the live server-wide World Boss HP bar & top damage contributors.", Execute: cmdWorldBoss},
		{Name: "wbattack", Aliases: []string{"wba"}, Category: "rpg", Description: "Attack the shared World Boss for massive ranked Yen & Diamond rewards (-10 Energy).", Execute: cmdWBAttack},

		// Hero Stats & Achievements / Titles
		{Name: "herostats", Aliases: []string{"alloc"}, Category: "rpg", Description: "View or allocate Hero Stat Points. Usage: .alloc <str|dex|int> <points>", Execute: cmdAllocStats},
		{Name: "rpgachievements", Aliases: []string{"titles", "settitle"}, Category: "rpg", Description: "View unlocked RPG achievements & equip titles. Usage: .titles [title_name]", Execute: cmdRPGAchievements},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdMap(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	pngBytes := rpg.RenderWorldMapPNG(p)
	var b strings.Builder
	b.WriteString(pickLang(client, msg, "🗺️ *PETA DUNIA NOPEBOT RPG*\n\n", "🗺️ *NOPEBOT RPG WORLD MAP*\n\n"))
	for _, z := range rpg.WorldZones {
		pin := "📍"
		if z.ID == p.Location {
			pin = "⭐ *HERE*"
		}
		name := z.Name
		desc := z.DescEN
		if isID(client, msg) {
			name = z.NameID
			desc = z.DescID
		}
		fmt.Fprintf(&b, "%s *%s* (`%s`) — Min Lv.%d | Travel: `%d⚡`\n   _%s_\n", pin, name, z.ID, z.MinLevel, z.TravelCost, desc)
	}
	b.WriteString(pickLang(client, msg, "\nKetik `.travel <zone_id>` untuk pindah & `.explore` untuk menjelajah!", "\nUse `.travel <zone_id>` to move & `.explore` to venture out!"))
	_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, pngBytes, "image/png", strings.TrimSpace(b.String()))
}

func cmdTravel(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	dest := strings.ToLower(strings.TrimSpace(strings.Join(args, "_")))
	z, ok := rpg.GetZone(dest)
	if !ok {
		utils.SendReply(client, msg.Info.Chat, "🗺️ Unknown zone. Run `.map` to see all zone IDs (e.g. `.travel whispering_woods`).")
		return
	}
	if p.Location == z.ID {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📍 You are already in *%s*!", z.Name))
		return
	}
	if p.Level < z.MinLevel {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔒 You must be *Level %d* to travel to *%s* (current: Lv.%d).", z.MinLevel, z.Name, p.Level))
		return
	}
	if p.Energy.Current < z.TravelCost {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_energy", z.TravelCost, p.Energy.Current, p.Energy.Max))
		return
	}
	p.Energy.Current -= z.TravelCost
	p.Location = z.ID
	firstVisit := !p.VisitedZones[z.ID]
	p.VisitedZones[z.ID] = true
	bonus := ""
	if firstVisit {
		rpg.AddRPGXP(p, 50)
		bonus = pickLang(client, msg, "\n✨ *Bonus Penemuan Baru:* `+50 XP`!", "\n✨ *First Discovery Bonus:* `+50 XP`!")
	}
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
		fmt.Sprintf("🧭 Kamu tiba di *%s* (`-%d⚡`). Ketik `.explore` untuk menjelajah!%s", z.NameID, z.TravelCost, bonus),
		fmt.Sprintf("🧭 Arrived at *%s* (`-%d⚡`). Run `.explore` to search for loot!%s", z.Name, z.TravelCost, bonus),
	))
}

func cmdExplore(client *whatsmeow.Client, msg *events.Message, args []string) {
	const (
		cd   = 90 * time.Second
		cost = 8
	)
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	last := time.UnixMilli(p.Cooldowns.LastExplore)
	if time.Since(last) < cd {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.cooldown", (cd-time.Since(last)).Round(time.Second)))
		return
	}
	if p.Energy.Current < cost {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_energy", cost, p.Energy.Current, p.Energy.Max))
		return
	}
	z, _ := rpg.GetZone(p.Location)
	p.Energy.Current -= cost
	p.Cooldowns.LastExplore = time.Now().UnixMilli()

	roll := rand.Intn(100)
	var outcome string
	switch {
	case roll < 30: // Treasure chest
		yen := z.BaseYen + rand.Int63n(z.BaseYen/2+50)
		p.Currency[rpg.Yen] += yen
		p.Resources[z.RareDrop] += 2
		rpg.AddRPGXP(p, 40)
		outcome = fmt.Sprintf("🎁 *Ancient Treasure Chest!*\nFound `+¥%d`, `+2 %s`, and `+40 XP` in *%s*!", yen, z.RareDrop, z.Name)
	case roll < 55: // Ambush battle
		yen := z.BaseYen
		p.Currency[rpg.Yen] += yen
		p.Resources[rpg.Meat] += 3
		p.MonstersSlain++
		rpg.AddRPGXP(p, 55)
		outcome = fmt.Sprintf("⚔️ *Wild Ambush Survived!*\nYou defeated local beasts in *%s* for `+¥%d`, `+3 Meat`, and `+55 XP`!", z.Name, yen)
	case roll < 75: // Healing shrine
		p.Character.Stats.Health = p.Character.Stats.MaxHealth
		p.Character.Stats.Mana = p.Character.Stats.MaxMana
		p.Energy.Current = min(p.Energy.Max, p.Energy.Current+12)
		rpg.AddRPGXP(p, 30)
		outcome = fmt.Sprintf("⛩️ *Mystic Shrine Found in %s!*\nYour HP, Mana & `+12⚡` Energy were blessed and restored!", z.Name)
	case roll < 92: // Hidden mineral cache
		qty := int64(2 + rand.Intn(3))
		p.Resources[z.RareDrop] += qty
		rpg.AddRPGXP(p, 35)
		outcome = fmt.Sprintf("💎 *Hidden Vein Discovered in %s!*\nHarvested `+%d %s` & `+35 XP`!", z.Name, qty, z.RareDrop)
	default: // Free gacha ticket / big jackpot!
		drop := rpg.RollGacha(p, 1)[0]
		outcome = fmt.Sprintf("🌟 *MYSTIC RELIC SHRINE in %s!*\nYou received a FREE Gacha Relic: %s *[%s] %s* — %s", z.Name, drop.Emoji, drop.Rarity, drop.Name, drop.Description)
	}
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, outcome)
}

func cmdGacha(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	count := 1
	cost := int64(500)
	if len(args) > 0 && strings.TrimSpace(args[0]) == "10" {
		count = 10
		cost = 4500 // 10% discount on 10x multi-pull!
	}
	if p.Currency[rpg.Yen] < cost {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			fmt.Sprintf("🎰 Yen tidak cukup! Pull %dx butuh `¥%d` (kamu punya `¥%d`).", count, cost, p.Currency[rpg.Yen]),
			fmt.Sprintf("🎰 Not enough Yen! %dx pull costs `¥%d` (you have `¥%d`).", count, cost, p.Currency[rpg.Yen]),
		))
		return
	}
	p.Currency[rpg.Yen] -= cost
	drops := rpg.RollGacha(p, count)
	_ = rpg.SavePlayer(p)

	var b strings.Builder
	fmt.Fprintf(&b, "🎰 *NOPEBOT MYTHIC GACHA (%dx Pull)*\n\n", count)
	for i, d := range drops {
		fmt.Fprintf(&b, "%d. %s *[%s]* *%s*\n   _%s_\n", i+1, d.Emoji, d.Rarity, d.Name, d.Description)
	}
	fmt.Fprintf(&b, "\n📊 Pity: Epic `%d/10` | Legendary/Mythic `%d/50` | Wallet: `¥%d`", p.GachaPityEpic, p.GachaPityLegend, p.Currency[rpg.Yen])
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdPity(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🎰 *NopeBot Mythic Gacha Banner*\n• 1x Pull: `¥500` (`.gacha`) | 10x Pull: `¥4,500` (`.gacha 10`)\n\n*Drop Rates:*\n• 🌈 Mythic (`1%%`): Celestial Phoenix Pet, Void Reaper (+155 ATK), Dragon Emperor Plate (+135 DEF)\n• 🟡 Legendary (`4%%`): Mini Dragon Pet, Celestial Blade (+110 ATK), Royal Diamond Hoard\n• 🟣 Epic (`15%%`): Emerald Owl Pet, Emerald Cache, Alchemist Bundle\n• 🔵 Rare (`30%%`): Ember Fox Pet, Iron Golem Pup, Gold Bullion\n• ⚪ Common (`50%%`): Iron Crate, Coin Pouch, Energy Potion\n\n*Your Pity Progress:*\n• Total Pulls: `%d`\n• Guaranteed Epic+ Pity: `%d / 10`\n• Guaranteed Legendary+ Pity: `%d / 50`",
		p.GachaTotalPulls, p.GachaPityEpic, p.GachaPityLegend,
	))
}

func cmdPets(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	if len(p.Pets) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🐾 You don't have any companion pets yet! Pull on `.gacha` to summon pets.")
		return
	}
	var b strings.Builder
	b.WriteString("🐾 *Your Companion Pets*\n\n")
	for _, pet := range p.Pets {
		eq := ""
		if pet.ID == p.EquippedPet {
			eq = " ✅ *EQUIPPED*"
		}
		fmt.Fprintf(&b, "• *%s* (`%s`) [%s] Lv.%d%s\n  ATK `+%d` | DEF `+%d` | Yen `+%d%%` | XP `+%d%%`\n",
			pet.Name, pet.ID, pet.Rarity, pet.Level, eq, pet.BonusATK+pet.Level*2, pet.BonusDEF+pet.Level, pet.BonusYenPct, pet.BonusXPPct)
	}
	b.WriteString("\nUse `.petequip <pet_id>` to switch or `.petfeed` (costs 5 Meat) to level up!")
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdPetEquip(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	id := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	for _, pet := range p.Pets {
		if pet.ID == id {
			p.EquippedPet = pet.ID
			_ = rpg.SavePlayer(p)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🐾 Equipped *%s* (Lv.%d)!", pet.Name, pet.Level))
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, "❌ Pet not found in your collection. Run `.pets`.")
}

func cmdPetFeed(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	pet := rpg.GetEquippedPet(p)
	if pet == nil {
		utils.SendReply(client, msg.Info.Chat, "🐾 Equip a pet first with `.petequip <id>`.")
		return
	}
	if p.Resources[rpg.Meat] < 5 {
		utils.SendReply(client, msg.Info.Chat, "❌ Feeding requires `5 Meat` (hunt with `.hunt`).")
		return
	}
	p.Resources[rpg.Meat] -= 5
	pet.XP += 50
	leveled := false
	if pet.XP >= pet.Level*100 {
		pet.XP -= pet.Level * 100
		pet.Level++
		leveled = true
	}
	_ = rpg.SavePlayer(p)
	if leveled {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🥩 Fed *%s*! 🎉 Pet leveled up to *Lv.%d*!", pet.Name, pet.Level))
	} else {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🥩 Fed *%s* (`%d/%d XP` to Lv.%d)!", pet.Name, pet.XP, pet.Level*100, pet.Level+1))
	}
}

func cmdWorldBoss(client *whatsmeow.Client, msg *events.Message, args []string) {
	wb := rpg.LoadWorldBoss()
	pct := float64(wb.HP) * 100 / float64(max(1, wb.MaxHP))
	bar := progressBar(int(wb.HP), int(wb.MaxHP))

	type contrib struct {
		id  string
		dmg int64
	}
	var list []contrib
	for k, v := range wb.Contributors {
		list = append(list, contrib{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].dmg > list[j].dmg })

	var b strings.Builder
	fmt.Fprintf(&b, "🌋 *WORLD BOSS: %s* (`%s`)\n", wb.Name, wb.ID)
	fmt.Fprintf(&b, "❤️ HP: `%s` *%.1f%%* (`%d / %d`)\n", bar, pct, wb.HP, wb.MaxHP)
	fmt.Fprintf(&b, "💰 Reward Pool: `¥%d` + Diamonds\n", wb.RewardPool)
	if wb.Defeated {
		b.WriteString("\n✅ *DEFEATED!* A new World Boss will spawn soon.\n")
	} else {
		b.WriteString("\n⚔️ Run `.wbattack` to deal damage & claim rewards!\n")
	}
	if len(list) > 0 {
		b.WriteString("\n*Top Damage Dealers:*\n")
		for i, c := range list {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&b, "%d. `%s` — `%d` DMG\n", i+1, c.id, c.dmg)
		}
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdWBAttack(client *whatsmeow.Client, msg *events.Message, args []string) {
	const (
		cd   = 3 * time.Minute
		cost = 10
	)
	pid := playerIDFor(client, msg)
	p, err := rpg.LoadPlayer(pid)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	wb := rpg.LoadWorldBoss()
	if wb.Defeated {
		utils.SendReply(client, msg.Info.Chat, "🌋 The current World Boss is already defeated! Wait for the next spawn.")
		return
	}
	last := time.UnixMilli(p.Cooldowns.LastWorldBoss)
	if time.Since(last) < cd {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.cooldown", (cd-time.Since(last)).Round(time.Second)))
		return
	}
	if p.Energy.Current < cost {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_energy", cost, p.Energy.Current, p.Energy.Max))
		return
	}

	p.Energy.Current -= cost
	p.Cooldowns.LastWorldBoss = time.Now().UnixMilli()
	heroAtk, _ := rpg.HeroAttackDefense(p)
	armyAtk, _ := rpg.ArmyAttackDefense(p)
	dmg := int64(heroAtk*3 + armyAtk/2 + rand.Intn(40) + 25)
	if dmg > wb.HP {
		dmg = wb.HP
	}
	wb.HP -= dmg
	wb.Contributors[pid] += dmg
	p.WorldBossDamage += dmg
	hitYen := dmg * 2
	p.Currency[rpg.Yen] += hitYen
	rpg.AddRPGXP(p, 50)

	killBonus := ""
	if wb.HP <= 0 {
		wb.HP = 0
		wb.Defeated = true
		wb.SpawnedAt = time.Now().Unix() // start 30m respawn timer
		p.Currency[rpg.Yen] += 2000
		p.Resources[rpg.Diamond] += 3
		killBonus = "\n\n🏆 *KILLING BLOW!* You slayed the World Boss and claimed `+¥2,000` & `+3 Diamonds`!"
	}
	_ = rpg.SaveWorldBoss(wb)
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚔️ *World Boss Strike!*\nDealt *%d DMG* to *%s* (`%d/%d HP` left)\nEarned: `+¥%d` & `+50 XP`!%s", dmg, wb.Name, wb.HP, wb.MaxHP, hitYen, killBonus))
}

func cmdAllocStats(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
			"📊 *Hero Stats (%s)*\n• Unspent Stat Points: *%d*\n• STR (Attack): `%d`\n• DEX (Crit/DEF): `%d`\n• INT (Magic/Heal): `%d`\n\nUsage: `.alloc <str|dex|int> <points>`",
			p.Name, p.StatPoints, p.Character.Stats.Strength, p.Character.Stats.Dexterity, p.Character.Stats.Intelligence,
		))
		return
	}
	stat := strings.ToLower(fields[0])
	pts, err := strconv.Atoi(fields[1])
	if err != nil || pts <= 0 || pts > p.StatPoints {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Invalid points. You have `%d` unspent stat points.", p.StatPoints))
		return
	}
	switch stat {
	case "str", "strength":
		p.Character.Stats.Strength += pts
	case "dex", "dexterity":
		p.Character.Stats.Dexterity += pts
	case "int", "intelligence":
		p.Character.Stats.Intelligence += pts
	default:
		utils.SendReply(client, msg.Info.Chat, "Usage: `.alloc <str|dex|int> <points>`")
		return
	}
	p.StatPoints -= pts
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Allocated `+%d %s`! Remaining stat points: `%d`.", pts, strings.ToUpper(stat), p.StatPoints))
}

func cmdRPGAchievements(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	var unlocked []string
	if len(p.VisitedZones) >= 3 {
		unlocked = append(unlocked, "World Explorer 🧭")
	}
	if len(p.VisitedZones) >= 6 {
		unlocked = append(unlocked, "Grand Cartographer 🗺️")
	}
	if p.GachaTotalPulls >= 10 {
		unlocked = append(unlocked, "Lucky Gambler 🎰")
	}
	if len(p.Pets) >= 1 {
		unlocked = append(unlocked, "Beast Tamer 🐾")
	}
	if p.WorldBossDamage >= 500 {
		unlocked = append(unlocked, "Titan Slayer 🌋")
	}
	if p.NetWorth >= 10000 {
		unlocked = append(unlocked, "Coinsteel Tycoon 💰")
	}
	if p.Level >= 10 {
		unlocked = append(unlocked, "Immortal Sovereign 👑")
	}
	p.UnlockedTitles = unlocked

	if len(args) > 0 {
		want := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
		for _, t := range unlocked {
			if strings.Contains(strings.ToLower(t), want) {
				p.Title = t
				_ = rpg.SavePlayer(p)
				utils.SendReply(client, msg.Info.Chat, "🏷️ Equipped Title: *"+t+"*!")
				return
			}
		}
		utils.SendReply(client, msg.Info.Chat, "❌ Title not unlocked yet.")
		return
	}
	_ = rpg.SavePlayer(p)
	if len(unlocked) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🏅 No RPG titles unlocked yet! Explore zones (`.map`), pull `.gacha`, or strike `.wbattack`!")
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏅 *Unlocked RPG Titles* (Active: *%s*)\n• %s\n\nEquip with `.settitle <keyword>`.", p.Title, strings.Join(unlocked, "\n• ")))
}
