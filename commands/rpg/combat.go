package rpgcmd

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/handlers"
	"bot/i18n"
	"bot/rpg"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type dungeonState struct {
	PlayerID  string
	Floor     int
	BossName  string
	BossHP    int
	BossMaxHP int
	BossATK   int
	Defending bool
	ExpiresAt time.Time
}

type duelState struct {
	P1ID      string
	P2ID      string
	P1Name    string
	P2Name    string
	P1HP      int
	P2HP      int
	P1MP      int
	P2MP      int
	Turn      string // P1ID or P2ID
	DefendID  string
	Bet       int64
	Accepted  bool
	ExpiresAt time.Time
}

var (
	combatMu    sync.Mutex
	activeDung  = map[string]*dungeonState{} // chatJID -> dungeon
	activeDuels = map[string]*duelState{}    // chatJID -> duel
)

func init() {
	handlers.RegisterInteractiveSessionHandler(handleRPGCombatReply)

	cmds := []types.Command{
		{Name: "hunt", Aliases: []string{"berburu"}, Category: "rpg", Description: "Hunt wild monsters for Meat, Yen, XP & rare gems (-10 Energy).", Execute: cmdHunt},
		{Name: "mine", Aliases: []string{"nambang"}, Category: "rpg", Description: "Mine deep ore veins for Iron, Gold, Emerald & Diamond (-10 Energy).", Execute: cmdMine},
		{Name: "dungeon", Category: "rpg", Description: "Enter an interactive multi-floor PvE Dungeon with skills & boss loot (-15 Energy).", Execute: cmdDungeon},
		{Name: "train", Aliases: []string{"latih"}, Category: "rpg", Description: "Recruit military units for empire raids. Usage: .train <militia|archer|knight|catapult> [qty]", Execute: cmdTrain},
		{Name: "army", Aliases: []string{"pasukan"}, Category: "rpg", Description: "Inspect your empire's army strength & fortress defense.", Execute: cmdArmy},
		{Name: "raid", Aliases: []string{"serang"}, Category: "rpg", Description: "Raid another ruler's empire to loot unsecured Yen & Iron! Usage: .raid @user", Execute: cmdRaid},
		{Name: "duel", Category: "rpg", Description: "Challenge another player to a turn-based Hero Duel with skills! Usage: .duel @user [bet]", Execute: cmdDuel},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdHunt(client *whatsmeow.Client, msg *events.Message, args []string) {
	const (
		cd   = 2 * time.Minute
		cost = 10
	)
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	last := time.UnixMilli(p.Cooldowns.LastHunt)
	if time.Since(last) < cd {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.cooldown", (cd-time.Since(last)).Round(time.Second)))
		return
	}
	if p.Energy.Current < cost {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_energy", cost, p.Energy.Current, p.Energy.Max))
		return
	}
	if p.Character.Stats.Health < 20 {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg, "❤️ HP terlalu rendah (<20)! Makan daging dengan `.eat meat` dulu.", "❤️ HP too low (<20)! Eat meat or use a Health Potion with `.eat`."))
		return
	}

	var pool []rpg.MonsterTemplate
	for _, m := range rpg.HuntMonsters {
		if m.MinLevel <= p.Level+1 {
			pool = append(pool, m)
		}
	}
	mon := pool[rand.Intn(len(pool))]
	heroAtk, heroDef := rpg.HeroAttackDefense(p)
	dmgTaken := max(4, mon.Attack-heroDef/2+rand.Intn(6))

	p.Energy.Current -= cost
	p.Cooldowns.LastHunt = time.Now().UnixMilli()
	p.Character.Stats.Health = max(1, p.Character.Stats.Health-dmgTaken)

	if heroAtk+rand.Intn(20) < mon.Attack && rand.Intn(4) == 0 {
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			fmt.Sprintf("🐾 Kamu bertemu *%s* namun harus mundur! (`-%d HP`, sisa `%d/%d HP`). Tempa senjata di `.craft`!", mon.Name, dmgTaken, p.Character.Stats.Health, p.Character.Stats.MaxHealth),
			fmt.Sprintf("🐾 You encountered *%s* and had to retreat! (`-%d HP`, `%d/%d HP` left). Craft better gear with `.craft`!", mon.Name, dmgTaken, p.Character.Stats.Health, p.Character.Stats.MaxHealth),
		))
		return
	}

	p.Currency[rpg.Yen] += mon.RewardYen
	p.Resources[rpg.Meat] += mon.DropMeat
	rareMsg := ""
	if mon.RareDrop != "" && rand.Intn(100) < 35 {
		p.Resources[mon.RareDrop]++
		rareMsg = fmt.Sprintf("\n✨ Rare Drop: `+1 %s`!", mon.RareDrop)
	}
	p.Quest.Hunts++
	leveled := rpg.AddRPGXP(p, mon.RewardXP)
	_ = rpg.SavePlayer(p)

	lvlMsg := ""
	if leveled {
		lvlMsg = fmt.Sprintf("\n🎉 *LEVEL UP → Lv.%d!*", p.Level)
	}
	utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
		fmt.Sprintf("🏹 *Berburu Berhasil!* Kamu mengalahkan *%s*!\n• Hadiah: `+¥%d`, `+%d Daging`, `+%d XP`\n• Luka: `-%d HP` (Sisa: `%d/%d`)%s%s", mon.Name, mon.RewardYen, mon.DropMeat, mon.RewardXP, dmgTaken, p.Character.Stats.Health, p.Character.Stats.MaxHealth, rareMsg, lvlMsg),
		fmt.Sprintf("🏹 *Hunt Victory!* You defeated *%s*!\n• Loot: `+¥%d`, `+%d Meat`, `+%d XP`\n• Damage taken: `-%d HP` (`%d/%d` left)%s%s", mon.Name, mon.RewardYen, mon.DropMeat, mon.RewardXP, dmgTaken, p.Character.Stats.Health, p.Character.Stats.MaxHealth, rareMsg, lvlMsg),
	))
}

func cmdMine(client *whatsmeow.Client, msg *events.Message, args []string) {
	const (
		cd   = 2 * time.Minute
		cost = 10
	)
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	last := time.UnixMilli(p.Cooldowns.LastMine)
	if time.Since(last) < cd {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.cooldown", (cd-time.Since(last)).Round(time.Second)))
		return
	}
	if p.Energy.Current < cost {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_energy", cost, p.Energy.Current, p.Energy.Max))
		return
	}
	p.Energy.Current -= cost
	p.Cooldowns.LastMine = time.Now().UnixMilli()

	ironGain := int64(15 + rand.Intn(20) + p.Level*3)
	p.Resources[rpg.Iron] += ironGain
	var drops []string
	drops = append(drops, fmt.Sprintf("🔩 `+%d Iron`", ironGain))

	roll := rand.Intn(100)
	if roll < 40 {
		g := int64(1 + rand.Intn(3))
		p.Resources[rpg.Gold] += g
		drops = append(drops, fmt.Sprintf("🪙 `+%d Gold`", g))
	}
	if roll < 18 {
		e := int64(1 + rand.Intn(2))
		p.Resources[rpg.Emerald] += e
		drops = append(drops, fmt.Sprintf("💚 `+%d Emerald`", e))
	}
	if roll < 7 {
		p.Resources[rpg.Diamond]++
		drops = append(drops, "💎 `+1 Diamond`!")
	}
	p.Quest.Mines++
	rpg.AddRPGXP(p, 25)
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⛏️ *Mining Expedition Complete!*\n%s\n⚡ Energy: `%d/%d`", strings.Join(drops, " | "), p.Energy.Current, p.Energy.Max))
}

func cmdDungeon(client *whatsmeow.Client, msg *events.Message, args []string) {
	const (
		cd   = 5 * time.Minute
		cost = 15
	)
	pid := playerIDFor(client, msg)
	p, err := rpg.LoadPlayer(pid)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	last := time.UnixMilli(p.Cooldowns.LastDungeon)
	if time.Since(last) < cd {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.cooldown", (cd-time.Since(last)).Round(time.Second)))
		return
	}
	if p.Energy.Current < cost {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_energy", cost, p.Energy.Current, p.Energy.Max))
		return
	}
	p.Energy.Current -= cost
	p.Cooldowns.LastDungeon = time.Now().UnixMilli()
	_ = rpg.SavePlayer(p)

	bossHP := 90 + p.Level*35
	bossATK := 12 + p.Level*4
	d := &dungeonState{
		PlayerID:  pid,
		Floor:     1,
		BossName:  "Crypt Warden 💀",
		BossHP:    bossHP,
		BossMaxHP: bossHP,
		BossATK:   bossATK,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	combatMu.Lock()
	activeDung[msg.Info.Chat.ToNonAD().String()] = d
	combatMu.Unlock()

	utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
		fmt.Sprintf("🏰 *DUNGEON LANTAI 1 — %s*\n❤️ Boss HP: `%d/%d` | ❤️ HP Kamu: `%d/%d` | 🔮 MP: `%d/%d`\n\nKetik aksi: `attack`, `fireball` (20 MP), `heal` (18 MP), `defend`, atau `flee`!", d.BossName, d.BossHP, d.BossMaxHP, p.Character.Stats.Health, p.Character.Stats.MaxHealth, p.Character.Stats.Mana, p.Character.Stats.MaxMana),
		fmt.Sprintf("🏰 *DUNGEON FLOOR 1 — %s*\n❤️ Boss HP: `%d/%d` | ❤️ Your HP: `%d/%d` | 🔮 MP: `%d/%d`\n\nReply with: `attack`, `fireball` (20 MP), `heal` (18 MP), `defend`, or `flee`!", d.BossName, d.BossHP, d.BossMaxHP, p.Character.Stats.Health, p.Character.Stats.MaxHealth, p.Character.Stats.Mana, p.Character.Stats.MaxMana),
	))
}

func cmdTrain(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 0 {
		ids := []string{"militia", "archer", "knight", "catapult"}
		var b strings.Builder
		b.WriteString("🪖 *Barracks — Train Units (`.train <unit> [qty]`):*\n\n")
		for _, id := range ids {
			u := rpg.UnitBlueprints[id]
			fmt.Fprintf(&b, "• *%s* (`%s`) — ATK `%d` / DEF `%d` | Cost: `¥%d, %d Iron`\n", u.Name, id, u.Attack, u.Defense, u.CostYen, u.CostIrn)
		}
		utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
		return
	}
	uid := strings.ToLower(fields[0])
	bp, ok := rpg.UnitBlueprints[uid]
	if !ok {
		utils.SendReply(client, msg.Info.Chat, "❌ Unknown unit. Run `.train` to view units.")
		return
	}
	qty := 1
	if len(fields) >= 2 {
		if q, e := strconv.Atoi(fields[1]); e == nil && q >= 1 && q <= 100 {
			qty = q
		}
	}
	needYen := bp.CostYen * int64(qty)
	needIrn := bp.CostIrn * int64(qty)
	if p.Currency[rpg.Yen] < needYen || p.Resources[rpg.Iron] < needIrn {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Need `¥%d` & `%d Iron` to train `%dx %s`.", needYen, needIrn, qty, bp.Name))
		return
	}
	p.Currency[rpg.Yen] -= needYen
	p.Resources[rpg.Iron] -= needIrn
	found := false
	for i := range p.Units {
		if p.Units[i].ID == uid {
			p.Units[i].Quantity += qty
			found = true
			break
		}
	}
	if !found {
		p.Units = append(p.Units, rpg.Unit{ID: bp.ID, Name: bp.Name, Quantity: qty, Attack: bp.Attack, Defense: bp.Defense})
	}
	_ = rpg.SavePlayer(p)
	atk, def := rpg.ArmyAttackDefense(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🪖 Trained `%dx %s`! Total Empire Army: ATK `%d` / DEF `%d`.", qty, bp.Name, atk, def))
}

func cmdArmy(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	atk, def := rpg.ArmyAttackDefense(p)
	var b strings.Builder
	fmt.Fprintf(&b, "🪖 *%s's Military Forces*\n• Total Attack: `%d` | Total Defense: `%d`\n\n", p.Name, atk, def)
	if len(p.Units) == 0 {
		b.WriteString("_No units trained yet. Use `.train`!_")
	} else {
		sort.Slice(p.Units, func(i, j int) bool { return p.Units[i].Attack > p.Units[j].Attack })
		for _, u := range p.Units {
			fmt.Fprintf(&b, "• *%s*: `%d` (ATK %d / DEF %d)\n", u.Name, u.Quantity, u.Attack*u.Quantity, u.Defense*u.Quantity)
		}
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

// ResolveRaid computes raid outcome between attacker and defender.
func ResolveRaid(attacker, defender *rpg.Player) (won bool, lootYen, lootIron int64) {
	aAtk, _ := rpg.ArmyAttackDefense(attacker)
	_, dDef := rpg.ArmyAttackDefense(defender)
	if aAtk+rand.Intn(15) > dDef+rand.Intn(15) {
		lootYen = defender.Currency[rpg.Yen] / 4
		if lootYen > 2500 {
			lootYen = 2500
		}
		lootIron = defender.Resources[rpg.Iron] / 5
		if lootIron > 150 {
			lootIron = 150
		}
		return true, lootYen, lootIron
	}
	return false, 0, 0
}

func cmdRaid(client *whatsmeow.Client, msg *events.Message, args []string) {
	const cd = 15 * time.Minute
	pid := playerIDFor(client, msg)
	attacker, err := rpg.LoadPlayer(pid)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	last := time.UnixMilli(attacker.Cooldowns.LastRaid)
	if time.Since(last) < cd {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.cooldown", (cd-time.Since(last)).Round(time.Second)))
		return
	}
	targets := utils.ExtractTargetJIDs(msg, args)
	if len(targets) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.raid @user`")
		return
	}
	targetID := utils.ResolvePhoneNumber(client, targets[0], waTypes.EmptyJID)
	if targetID == pid {
		utils.SendReply(client, msg.Info.Chat, "😅 You cannot raid your own empire!")
		return
	}
	defender, err := rpg.LoadPlayer(targetID)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Target player hasn't founded an empire yet.")
		return
	}
	if defender.RaidProtectionUntil > time.Now().UnixMilli() {
		rem := time.Until(time.UnixMilli(defender.RaidProtectionUntil)).Round(time.Minute)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🛡️ *%s* is protected by a Raid Shield (`%s` left)!", defender.Name, rem))
		return
	}

	attacker.Cooldowns.LastRaid = time.Now().UnixMilli()
	won, lootYen, lootIron := ResolveRaid(attacker, defender)
	if won {
		defender.Currency[rpg.Yen] -= lootYen
		defender.Resources[rpg.Iron] -= lootIron
		defender.RaidProtectionUntil = time.Now().Add(6 * time.Hour).UnixMilli()
		attacker.Currency[rpg.Yen] += lootYen
		attacker.Resources[rpg.Iron] += lootIron
		rpg.AddRPGXP(attacker, 60)
		_ = rpg.SavePlayer(attacker)
		_ = rpg.SavePlayer(defender)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚔️ *RAID VICTORY!* *%s* breached *%s*'s defenses!\n💰 Looted: `+¥%d` & `+%d Iron`!\n_(Defender received a 6h recovery shield)_", attacker.Name, defender.Name, lootYen, lootIron))
		return
	}
	_ = rpg.SavePlayer(attacker)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🛡️ *RAID REPELLED!* *%s*'s Fortress & Army held strong against *%s*!", defender.Name, attacker.Name))
}

func cmdDuel(client *whatsmeow.Client, msg *events.Message, args []string) {
	pid := playerIDFor(client, msg)
	p1, err := rpg.LoadPlayer(pid)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	targets := utils.ExtractTargetJIDs(msg, args)
	if len(targets) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.duel @user [bet_yen]`")
		return
	}
	p2ID := utils.ResolvePhoneNumber(client, targets[0], waTypes.EmptyJID)
	if p2ID == pid {
		utils.SendReply(client, msg.Info.Chat, "😅 You cannot duel yourself!")
		return
	}
	p2, err := rpg.LoadPlayer(p2ID)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Opponent hasn't started Coinsteel RPG yet.")
		return
	}
	var bet int64
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) >= 2 {
		if b, e := strconv.ParseInt(fields[len(fields)-1], 10, 64); e == nil && b > 0 {
			bet = b
		}
	}
	if bet > 0 && (p1.Currency[rpg.Yen] < bet || p2.Currency[rpg.Yen] < bet) {
		utils.SendReply(client, msg.Info.Chat, "❌ Both duelists must have enough Yen in wallet to cover the bet.")
		return
	}

	d := &duelState{
		P1ID:      pid,
		P2ID:      p2ID,
		P1Name:    p1.Name,
		P2Name:    p2.Name,
		P1HP:      p1.Character.Stats.MaxHealth,
		P2HP:      p2.Character.Stats.MaxHealth,
		P1MP:      p1.Character.Stats.MaxMana,
		P2MP:      p2.Character.Stats.MaxMana,
		Turn:      pid,
		Bet:       bet,
		Accepted:  false,
		ExpiresAt: time.Now().Add(3 * time.Minute),
	}
	combatMu.Lock()
	activeDuels[msg.Info.Chat.ToNonAD().String()] = d
	combatMu.Unlock()

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚔️ *HERO DUEL CHALLENGE!*\n*%s* challenges *%s* (Bet: `¥%d`)!\n*%s*, reply `accept` to begin or `decline` to refuse!", p1.Name, p2.Name, bet, p2.Name))
}

func handleRPGCombatReply(client *whatsmeow.Client, msg *events.Message) bool {
	if msg == nil || msg.Info.IsFromMe {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(utils.GetTextFromMessage(msg.Message)))
	if text == "" || strings.HasPrefix(text, ".") || strings.HasPrefix(text, "/") {
		return false
	}
	chat := msg.Info.Chat.ToNonAD().String()
	pid := playerIDFor(client, msg)

	// 1. Check active Dungeon
	combatMu.Lock()
	dung, hasDung := activeDung[chat]
	if hasDung && time.Now().After(dung.ExpiresAt) {
		delete(activeDung, chat)
		hasDung = false
	}
	combatMu.Unlock()
	if hasDung && dung.PlayerID == pid {
		if text != "attack" && text != "fireball" && text != "heal" && text != "defend" && text != "flee" {
			return false
		}
		p, err := rpg.LoadPlayer(pid)
		if err != nil {
			return true
		}
		if text == "flee" {
			combatMu.Lock()
			delete(activeDung, chat)
			combatMu.Unlock()
			utils.SendReply(client, msg.Info.Chat, "🏃 You fled the dungeon safely.")
			return true
		}
		atk, def := rpg.HeroAttackDefense(p)
		dung.Defending = false
		var log strings.Builder
		switch text {
		case "attack":
			dmg := max(8, atk+rand.Intn(10))
			dung.BossHP -= dmg
			fmt.Fprintf(&log, "⚔️ You struck *%s* for `%d` DMG!\n", dung.BossName, dmg)
		case "fireball":
			if p.Character.Stats.Mana < 20 {
				utils.SendReply(client, msg.Info.Chat, "🔮 Not enough Mana (need 20 MP)! Try `attack` or `defend`.")
				return true
			}
			p.Character.Stats.Mana -= 20
			dmg := 26 + p.Character.Stats.Intelligence*2 + rand.Intn(10)
			dung.BossHP -= dmg
			fmt.Fprintf(&log, "🔥 *Fireball* blasted *%s* for `%d` DMG!\n", dung.BossName, dmg)
		case "heal":
			if p.Character.Stats.Mana < 18 {
				utils.SendReply(client, msg.Info.Chat, "🔮 Not enough Mana (need 18 MP)!")
				return true
			}
			p.Character.Stats.Mana -= 18
			heal := 35 + p.Character.Stats.Intelligence
			p.Character.Stats.Health = min(p.Character.Stats.MaxHealth, p.Character.Stats.Health+heal)
			fmt.Fprintf(&log, "✨ *Holy Heal* restored `+%d HP`!\n", heal)
		case "defend":
			dung.Defending = true
			p.Character.Stats.Mana = min(p.Character.Stats.MaxMana, p.Character.Stats.Mana+10)
			log.WriteString("🛡️ You raised your shield (`+10 MP`, 50% damage reduction)!\n")
		}

		if dung.BossHP <= 0 {
			if dung.Floor < 2 {
				dung.Floor = 2
				dung.BossName = "Obsidian Overlord 👹"
				dung.BossMaxHP = 150 + p.Level*45
				dung.BossHP = dung.BossMaxHP
				dung.BossATK = 18 + p.Level*5
				_ = rpg.SavePlayer(p)
				fmt.Fprintf(&log, "\n🎉 *Floor 1 Cleared!* Descending to *Floor 2 — %s* (`%d HP`)!\nChoose: `attack`, `fireball`, `heal`, `defend`, `flee`.", dung.BossName, dung.BossHP)
				utils.SendReply(client, msg.Info.Chat, log.String())
				return true
			}
			combatMu.Lock()
			delete(activeDung, chat)
			combatMu.Unlock()
			yenReward := int64(650 + p.Level*180)
			p.Currency[rpg.Yen] += yenReward
			p.Resources[rpg.Emerald] += 2
			p.Resources[rpg.Diamond] += 1
			leveled := rpg.AddRPGXP(p, 160)
			_ = rpg.SavePlayer(p)
			fmt.Fprintf(&log, "\n🏆 *DUNGEON CONQUERED!*\nLoot: `+¥%d`, `+2 Emerald`, `+1 Diamond`, `+160 XP`!", yenReward)
			if leveled {
				fmt.Fprintf(&log, "\n🎉 *LEVEL UP → Lv.%d!*", p.Level)
			}
			utils.SendReply(client, msg.Info.Chat, log.String())
			return true
		}

		// Boss counterattack
		bDmg := max(5, dung.BossATK-def/2+rand.Intn(8))
		if dung.Defending {
			bDmg /= 2
		}
		p.Character.Stats.Health -= bDmg
		fmt.Fprintf(&log, "💥 *%s* hit you for `%d` DMG!\n", dung.BossName, bDmg)
		if p.Character.Stats.Health <= 0 {
			p.Character.Stats.Health = 1
			combatMu.Lock()
			delete(activeDung, chat)
			combatMu.Unlock()
			_ = rpg.SavePlayer(p)
			log.WriteString("\n💀 You were defeated in the dungeon! Recover with `.eat`.")
			utils.SendReply(client, msg.Info.Chat, log.String())
			return true
		}
		_ = rpg.SavePlayer(p)
		fmt.Fprintf(&log, "\nBoss HP: `%d/%d` | Your HP: `%d/%d` | MP: `%d/%d`", dung.BossHP, dung.BossMaxHP, p.Character.Stats.Health, p.Character.Stats.MaxHealth, p.Character.Stats.Mana, p.Character.Stats.MaxMana)
		utils.SendReply(client, msg.Info.Chat, log.String())
		return true
	}

	// 2. Check active PvP Duel
	combatMu.Lock()
	duel, hasDuel := activeDuels[chat]
	if hasDuel && time.Now().After(duel.ExpiresAt) {
		delete(activeDuels, chat)
		hasDuel = false
	}
	combatMu.Unlock()
	if !hasDuel {
		return false
	}
	if !duel.Accepted {
		if pid != duel.P2ID {
			return false
		}
		if text == "decline" || text == "tolak" {
			combatMu.Lock()
			delete(activeDuels, chat)
			combatMu.Unlock()
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏳️ *%s* declined the duel.", duel.P2Name))
			return true
		}
		if text == "accept" || text == "terima" {
			duel.Accepted = true
			duel.ExpiresAt = time.Now().Add(5 * time.Minute)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚔️ *DUEL STARTED!* *%s* (`%d HP`) vs *%s* (`%d HP`)\n*%s*'s turn! Reply: `attack`, `fireball`, `heal`, `defend`, or `flee`.", duel.P1Name, duel.P1HP, duel.P2Name, duel.P2HP, duel.P1Name))
			return true
		}
		return false
	}

	if pid != duel.Turn {
		return false
	}
	if text != "attack" && text != "fireball" && text != "heal" && text != "defend" && text != "flee" {
		return false
	}

	actor, _ := rpg.LoadPlayer(pid)
	isP1 := pid == duel.P1ID
	actorName, oppName := duel.P1Name, duel.P2Name
	oppID := duel.P2ID
	if !isP1 {
		actorName, oppName = duel.P2Name, duel.P1Name
		oppID = duel.P1ID
	}

	finishDuel := func(winnerID, winnerName, loserID string, reason string) {
		combatMu.Lock()
		delete(activeDuels, chat)
		combatMu.Unlock()
		w, _ := rpg.LoadPlayer(winnerID)
		l, _ := rpg.LoadPlayer(loserID)
		if w != nil && l != nil {
			if duel.Bet > 0 && l.Currency[rpg.Yen] >= duel.Bet {
				l.Currency[rpg.Yen] -= duel.Bet
				w.Currency[rpg.Yen] += duel.Bet
			}
			rpg.AddRPGXP(w, 80)
			_ = rpg.SavePlayer(w)
			_ = rpg.SavePlayer(l)
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("%s\n🏆 *%s WINS THE DUEL!* (+80 XP, Bet: `¥%d`)", reason, winnerName, duel.Bet))
	}

	if text == "flee" {
		finishDuel(oppID, oppName, pid, fmt.Sprintf("🏃 *%s* surrendered!", actorName))
		return true
	}

	atk, _ := rpg.HeroAttackDefense(actor)
	var actionLog string
	dmg := 0
	curMP := &duel.P1MP
	curHP := &duel.P1HP
	oppHP := &duel.P2HP
	if !isP1 {
		curMP = &duel.P2MP
		curHP = &duel.P2HP
		oppHP = &duel.P1HP
	}

	switch text {
	case "attack":
		dmg = max(10, atk+rand.Intn(10))
		if duel.DefendID == oppID {
			dmg /= 2
		}
		*oppHP -= dmg
		actionLog = fmt.Sprintf("⚔️ *%s* slashed *%s* for `%d` DMG!", actorName, oppName, dmg)
	case "fireball":
		if *curMP < 20 {
			utils.SendReply(client, msg.Info.Chat, "🔮 Not enough MP (need 20)! Use `attack` or `defend`.")
			return true
		}
		*curMP -= 20
		dmg = 24 + actor.Character.Stats.Intelligence*2
		if duel.DefendID == oppID {
			dmg /= 2
		}
		*oppHP -= dmg
		actionLog = fmt.Sprintf("🔥 *%s* cast Fireball on *%s* for `%d` DMG!", actorName, oppName, dmg)
	case "heal":
		if *curMP < 18 {
			utils.SendReply(client, msg.Info.Chat, "🔮 Not enough MP (need 18)!")
			return true
		}
		*curMP -= 18
		*curHP += 30
		actionLog = fmt.Sprintf("✨ *%s* healed `+30 HP`!", actorName)
	case "defend":
		duel.DefendID = pid
		*curMP += 10
		actionLog = fmt.Sprintf("🛡️ *%s* defended (`+10 MP`)!", actorName)
	}
	if text != "defend" {
		duel.DefendID = ""
	}

	if *oppHP <= 0 {
		finishDuel(pid, actorName, oppID, actionLog)
		return true
	}

	duel.Turn = oppID
	duel.ExpiresAt = time.Now().Add(5 * time.Minute)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("%s\n• %s: `%d HP` / `%d MP`\n• %s: `%d HP` / `%d MP`\n\nNext turn: *%s* (`attack`, `fireball`, `heal`, `defend`, `flee`)", actionLog, duel.P1Name, duel.P1HP, duel.P1MP, duel.P2Name, duel.P2HP, duel.P2MP, oppName))
	return true
}
