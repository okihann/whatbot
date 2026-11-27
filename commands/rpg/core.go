package rpgcmd

import (
	"context"
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

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type worksiteSession struct {
	PlayerID   string
	BuildingID string
}

var (
	regMu          sync.Mutex
	regPending     = map[string]bool{}
	worksiteMu     sync.Mutex
	worksiteActive = map[string]worksiteSession{}
)

func init() {
	handlers.RegisterInteractiveSessionHandler(handleRegistrationReply)
	handlers.RegisterReactionHandler(handleWorksiteReaction)

	cmds := []types.Command{
		{Name: "start", Aliases: []string{"rpgstart"}, Category: "rpg", Description: "Begin your Coinsteel Empire RPG journey. Usage: .start [age]", Execute: cmdStart},
		{Name: "rpgprofile", Aliases: []string{"empire", "rprofile", "inv", "inventory"}, Category: "rpg", Description: "View your Coinsteel Empire & Hero dashboard.", Execute: cmdRPGProfile},
		{Name: "work", Aliases: []string{"kerja"}, Category: "rpg", Description: "Work a job to earn Yen & XP (-10 Energy).", Execute: cmdWork},
		{Name: "build", Aliases: []string{"bangun"}, Category: "rpg", Description: "Construct a new empire building. Usage: .build [building_id]", Execute: cmdBuild},
		{Name: "upgrade", Category: "rpg", Description: "Upgrade an active building for higher production. Usage: .upgrade <building_id>", Execute: cmdUpgradeBuilding},
		{Name: "buildings", Aliases: []string{"bangunan"}, Category: "rpg", Description: "View all buildings in your empire.", Execute: cmdBuildings},
		{Name: "worksite", Aliases: []string{"proyek"}, Category: "rpg", Description: "View & contribute to your active construction worksite (react ⛏️ or use .contribute).", Execute: cmdWorksite},
		{Name: "contribute", Aliases: []string{"garap"}, Category: "rpg", Description: "Spend energy directly on your active construction worksite. Usage: .contribute [10]", Execute: cmdContribute},
		{Name: "collect", Aliases: []string{"claim", "panen"}, Category: "rpg", Description: "Collect passive Yen & resource production from all active buildings.", Execute: cmdCollect},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func playerIDFor(client *whatsmeow.Client, msg *events.Message) string {
	return utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
}

func isID(client *whatsmeow.Client, msg *events.Message) bool {
	return i18n.ResolveLang(client, msg) == i18n.LangID
}

func pickLang(client *whatsmeow.Client, msg *events.Message, idText, enText string) string {
	if isID(client, msg) {
		return idText
	}
	return enText
}

func cmdStart(client *whatsmeow.Client, msg *events.Message, args []string) {
	pid := playerIDFor(client, msg)
	if _, err := rpg.LoadPlayer(pid); err == nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.already_started"))
		return
	}

	// Quick registration: .start 20
	if len(args) > 0 {
		if age, err := strconv.Atoi(strings.TrimSpace(args[0])); err == nil && age >= 10 && age <= 100 {
			name := strings.TrimSpace(msg.Info.PushName)
			if name == "" {
				name = pid
			}
			p, err := rpg.CreateNewPlayer(pid, name, age)
			if err != nil {
				utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
				return
			}
			utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.created", p.Name, p.Age, p.Currency[rpg.Yen]))
			return
		}
	}

	regMu.Lock()
	regPending[pid] = true
	regMu.Unlock()
	utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.ask_age"))
}

func handleRegistrationReply(client *whatsmeow.Client, msg *events.Message) bool {
	if msg == nil || msg.Info.IsFromMe {
		return false
	}
	pid := playerIDFor(client, msg)
	regMu.Lock()
	waiting := regPending[pid]
	regMu.Unlock()
	if !waiting {
		return false
	}
	text := strings.TrimSpace(utils.GetTextFromMessage(msg.Message))
	if text == "" || strings.HasPrefix(text, ".") || strings.HasPrefix(text, "/") {
		return false
	}
	age, err := strconv.Atoi(text)
	if err != nil || age < 10 || age > 100 {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.invalid_age"))
		return true
	}
	regMu.Lock()
	delete(regPending, pid)
	regMu.Unlock()

	name := strings.TrimSpace(msg.Info.PushName)
	if name == "" {
		name = pid
	}
	p, err := rpg.CreateNewPlayer(pid, name, age)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
		return true
	}
	utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.created", p.Name, p.Age, p.Currency[rpg.Yen]))
	return true
}

func cmdRPGProfile(client *whatsmeow.Client, msg *events.Message, args []string) {
	pid := playerIDFor(client, msg)
	p, err := rpg.LoadPlayer(pid)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	_ = rpg.SavePlayer(p) // persist regen

	armyAtk, armyDef := rpg.ArmyAttackDefense(p)
	heroAtk, heroDef := rpg.HeroAttackDefense(p)
	weapon := "-"
	if w, ok := rpg.GearBlueprints[p.EquippedWeapon]; ok {
		weapon = w.Name
	}
	armor := "-"
	if a, ok := rpg.GearBlueprints[p.EquippedArmor]; ok {
		armor = a.Name
	}
	shieldTxt := pickLang(client, msg, "Tidak Aktif", "Inactive")
	if p.RaidProtectionUntil > time.Now().UnixMilli() {
		rem := time.Until(time.UnixMilli(p.RaidProtectionUntil)).Round(time.Minute)
		shieldTxt = fmt.Sprintf("🛡️ %s", rem)
	}
	zone, _ := rpg.GetZone(p.Location)
	zoneName := zone.Name
	if isID(client, msg) {
		zoneName = zone.NameID
	}
	petTxt := "-"
	if pet := rpg.GetEquippedPet(p); pet != nil {
		petTxt = fmt.Sprintf("%s (Lv.%d)", pet.Name, pet.Level)
	}
	titleTag := ""
	if p.Title != "" {
		titleTag = fmt.Sprintf(" [%s]", p.Title)
	}

	var b strings.Builder
	if isID(client, msg) {
		fmt.Fprintf(&b, "👑 *KERAJAAN %s*%s\n", strings.ToUpper(p.Name), titleTag)
		fmt.Fprintf(&b, "_Penguasa Lv.%d (%d/%d XP) | Kekayaan: ¥%d_\n", p.Level, p.XP, p.Level*120, p.NetWorth)
		fmt.Fprintf(&b, "🗺️ Lokasi: *%s* (`%s`)\n\n", zoneName, zone.ID)
		fmt.Fprintf(&b, "⚡ Energi: `%d/%d` | ❤️ HP: `%d/%d` | 🔮 MP: `%d/%d`\n", p.Energy.Current, p.Energy.Max, p.Character.Stats.Health, p.Character.Stats.MaxHealth, p.Character.Stats.Mana, p.Character.Stats.MaxMana)
		fmt.Fprintf(&b, "💴 Yen: `¥%d` | 🏦 Bank: `¥%d / ¥%d`\n\n", p.Currency[rpg.Yen], p.Bank.Balance, rpg.EffectiveBankCapacity(p))
		b.WriteString("📦 *SUMBER DAYA*\n")
		fmt.Fprintf(&b, "🔩 Besi: `%d` | 🥬 Sayur: `%d` | 🍎 Buah: `%d` | 🥩 Daging: `%d`\n", p.Resources[rpg.Iron], p.Resources[rpg.Vegetables], p.Resources[rpg.Fruit], p.Resources[rpg.Meat])
		fmt.Fprintf(&b, "🪙 Emas: `%d` | 💚 Zamrud: `%d` | 💎 Berlian: `%d`\n\n", p.Resources[rpg.Gold], p.Resources[rpg.Emerald], p.Resources[rpg.Diamond])
		b.WriteString("⚔️ *HERO & PASUKAN*\n")
		fmt.Fprintf(&b, "• Hero ATK/DEF: `%d / %d` | Senjata: *%s* | Armor: *%s*\n", heroAtk, heroDef, weapon, armor)
		fmt.Fprintf(&b, "• Pet: *%s* | Poin Stat: `%d` (`.alloc`)\n", petTxt, p.StatPoints)
		fmt.Fprintf(&b, "• Pasukan ATK/DEF: `%d / %d` | Bangunan: `%d` | Perisai: %s\n", armyAtk, armyDef, len(p.Buildings), shieldTxt)
	} else {
		fmt.Fprintf(&b, "👑 *EMPIRE OF %s*%s\n", strings.ToUpper(p.Name), titleTag)
		fmt.Fprintf(&b, "_Level %d Ruler (%d/%d XP) | Net Worth: ¥%d_\n", p.Level, p.XP, p.Level*120, p.NetWorth)
		fmt.Fprintf(&b, "🗺️ Location: *%s* (`%s`)\n\n", zoneName, zone.ID)
		fmt.Fprintf(&b, "⚡ Energy: `%d/%d` | ❤️ HP: `%d/%d` | 🔮 MP: `%d/%d`\n", p.Energy.Current, p.Energy.Max, p.Character.Stats.Health, p.Character.Stats.MaxHealth, p.Character.Stats.Mana, p.Character.Stats.MaxMana)
		fmt.Fprintf(&b, "💴 Yen: `¥%d` | 🏦 Bank: `¥%d / ¥%d`\n\n", p.Currency[rpg.Yen], p.Bank.Balance, rpg.EffectiveBankCapacity(p))
		b.WriteString("📦 *RESOURCES*\n")
		fmt.Fprintf(&b, "🔩 Iron: `%d` | 🥬 Veg: `%d` | 🍎 Fruit: `%d` | 🥩 Meat: `%d`\n", p.Resources[rpg.Iron], p.Resources[rpg.Vegetables], p.Resources[rpg.Fruit], p.Resources[rpg.Meat])
		fmt.Fprintf(&b, "🪙 Gold: `%d` | 💚 Emerald: `%d` | 💎 Diamond: `%d`\n\n", p.Resources[rpg.Gold], p.Resources[rpg.Emerald], p.Resources[rpg.Diamond])
		b.WriteString("⚔️ *HERO & ARMY*\n")
		fmt.Fprintf(&b, "• Hero ATK/DEF: `%d / %d` | Weapon: *%s* | Armor: *%s*\n", heroAtk, heroDef, weapon, armor)
		fmt.Fprintf(&b, "• Pet: *%s* | Stat Points: `%d` (`.alloc`)\n", petTxt, p.StatPoints)
		fmt.Fprintf(&b, "• Army ATK/DEF: `%d / %d` | Buildings: `%d` | Shield: %s\n", armyAtk, armyDef, len(p.Buildings), shieldTxt)
	}

	if len(p.Items) > 0 {
		var inv []string
		for k, v := range p.Items {
			if v > 0 {
				inv = append(inv, fmt.Sprintf("%s x%d", k, v))
			}
		}
		if len(inv) > 0 {
			sort.Strings(inv)
			fmt.Fprintf(&b, "• 🎒 Bag: `%s`\n", strings.Join(inv, "`, `"))
		}
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdWork(client *whatsmeow.Client, msg *events.Message, args []string) {
	const (
		workCooldown = 3 * time.Minute
		energyCost   = 10
	)
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	last := time.UnixMilli(p.Cooldowns.LastJob)
	if time.Since(last) < workCooldown {
		rem := (workCooldown - time.Since(last)).Round(time.Second)
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.cooldown", rem))
		return
	}
	if p.Energy.Current < energyCost {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_energy", energyCost, p.Energy.Current, p.Energy.Max))
		return
	}

	jobsID := []string{"Petani Kerajaan", "Pandai Besi", "Pedagang Rempah", "Pengawal Istana", "Arsitek Baja"}
	jobsEN := []string{"Royal Farmer", "Master Blacksmith", "Spice Merchant", "Palace Guard", "Steel Architect"}
	idx := rand.Intn(len(jobsEN))
	jobName := jobsEN[idx]
	if isID(client, msg) {
		jobName = jobsID[idx]
	}

	p.Energy.Current -= energyCost
	p.Cooldowns.LastJob = time.Now().UnixMilli()
	yenGained := rand.Int63n(160) + 80 + int64(p.Level*25)
	xpGained := int64(25 + rand.Intn(20))
	p.Currency[rpg.Yen] += yenGained
	p.Quest.Works++
	leveled := rpg.AddRPGXP(p, xpGained)
	_ = rpg.SavePlayer(p)

	extra := ""
	if leveled {
		extra = pickLang(client, msg, fmt.Sprintf("\n🎉 *NAIK LEVEL!* Sekarang kamu Penguasa Lv.%d!", p.Level), fmt.Sprintf("\n🎉 *LEVEL UP!* You are now Level %d!", p.Level))
	}
	utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.work_ok", jobName, yenGained, xpGained, energyCost, p.Energy.Current, p.Energy.Max, extra))
}

func formatCosts(costs map[rpg.Resource]int64) string {
	order := []rpg.Resource{rpg.Yen, rpg.Iron, rpg.Gold, rpg.Emerald, rpg.Diamond, rpg.Meat}
	var parts []string
	for _, r := range order {
		if c := costs[r]; c > 0 {
			if r == rpg.Yen {
				parts = append(parts, fmt.Sprintf("¥%d", c))
			} else {
				parts = append(parts, fmt.Sprintf("%d %s", c, r))
			}
		}
	}
	return strings.Join(parts, ", ")
}

func cmdBuild(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	if len(args) == 0 {
		ids := make([]string, 0, len(rpg.BuildingBlueprints))
		for id := range rpg.BuildingBlueprints {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var b strings.Builder
		b.WriteString(pickLang(client, msg, "🔨 *Daftar Bangunan Kerajaan*\nKetik `.build <id>` untuk mulai membangun:\n\n", "🔨 *Available Empire Buildings*\nRun `.build <id>` to start construction:\n\n"))
		for _, id := range ids {
			bp := rpg.BuildingBlueprints[id]
			name := bp.Name
			if isID(client, msg) {
				name = bp.NameID
			}
			prod := ""
			if bp.Production.Rate > 0 {
				prod = fmt.Sprintf(" | 📈 %.0f %s/h", bp.Production.Rate, bp.Production.Resource)
			}
			if bp.BankBonus > 0 {
				prod += fmt.Sprintf(" | 🏦 +¥%d cap", bp.BankBonus)
			}
			if bp.DefenseBonus >= 15 {
				prod += fmt.Sprintf(" | 🛡️ +%d DEF", bp.DefenseBonus)
			}
			fmt.Fprintf(&b, "• *%s* (`%s`)%s\n  Biaya/Cost: `%s` | Labor: `%d`\n", name, id, prod, formatCosts(bp.Cost), bp.RequiredLabor)
		}
		utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
		return
	}
	if p.ActiveWorksiteID != "" {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg, "🚧 Kamu masih punya proyek pembangunan aktif! Ketik `.worksite` atau `.contribute`.", "🚧 You already have an active construction project! Use `.worksite` or `.contribute`."))
		return
	}
	bid := strings.ToLower(strings.TrimSpace(args[0]))
	bp, ok := rpg.GetBuildingData(bid)
	if !ok {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg, "❌ ID bangunan tidak valid. Ketik `.build` untuk melihat daftar.", "❌ Invalid building ID. Use `.build` to see blueprints."))
		return
	}
	for res, cost := range bp.Cost {
		have := p.Resources[res]
		if res == rpg.Yen {
			have = p.Currency[res]
		}
		if have < cost {
			utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
				fmt.Sprintf("❌ Sumber daya tidak cukup! Butuh `%d %s`, kamu punya `%d`.", cost, res, have),
				fmt.Sprintf("❌ Not enough resources! Need `%d %s`, you have `%d`.", cost, res, have),
			))
			return
		}
	}
	for res, cost := range bp.Cost {
		if res == rpg.Yen {
			p.Currency[res] -= cost
		} else {
			p.Resources[res] -= cost
		}
	}
	nb := rpg.Building{
		InstanceID:       uuid.New().String(),
		ID:               bp.ID,
		Name:             bp.Name,
		Level:            1,
		Status:           "under_construction",
		RequiredLabor:    bp.RequiredLabor,
		CurrentLabor:     0,
		Production:       bp.Production,
		MaxStorage:       bp.MaxStorage,
		LastIncomeUpdate: time.Now().Unix(),
		DefenseBonus:     bp.DefenseBonus,
	}
	p.Buildings = append(p.Buildings, nb)
	p.ActiveWorksiteID = nb.InstanceID
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
		fmt.Sprintf("✅ Pembangunan *%s* dimulai! Ketik `.worksite` (reaksi ⛏️) atau `.contribute` untuk menyelesaikannya.", bp.NameID),
		fmt.Sprintf("✅ Construction begun on *%s*! Use `.worksite` (react ⛏️) or `.contribute` to complete it.", bp.Name),
	))
}

func cmdUpgradeBuilding(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	if len(args) == 0 {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg, "Penggunaan: `.upgrade <building_id>` (contoh: `.upgrade iron_mine`)", "Usage: `.upgrade <building_id>` (e.g. `.upgrade iron_mine`)"))
		return
	}
	if p.ActiveWorksiteID != "" {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg, "🚧 Selesaikan proyek aktifmu dulu dengan `.worksite`.", "🚧 Finish your active `.worksite` first."))
		return
	}
	bid := strings.ToLower(strings.TrimSpace(args[0]))
	bp, ok := rpg.GetBuildingData(bid)
	if !ok {
		utils.SendReply(client, msg.Info.Chat, "❌ Unknown building ID.")
		return
	}
	idx := -1
	for i, b := range p.Buildings {
		if b.ID == bid && b.Status == "active" {
			idx = i
			break
		}
	}
	if idx == -1 {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg, "❌ Kamu belum memiliki bangunan aktif tersebut.", "❌ You don't own an active building of that type."))
		return
	}
	b := &p.Buildings[idx]
	if b.Level >= bp.MaxLevel {
		utils.SendReply(client, msg.Info.Chat, "⭐ Building is already at max level!")
		return
	}
	mult := int64(b.Level + 1)
	for res, baseCost := range bp.Cost {
		need := baseCost * mult
		have := p.Resources[res]
		if res == rpg.Yen {
			have = p.Currency[res]
		}
		if have < need {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Need `%d %s` to upgrade to Lv.%d.", need, res, b.Level+1))
			return
		}
	}
	for res, baseCost := range bp.Cost {
		need := baseCost * mult
		if res == rpg.Yen {
			p.Currency[res] -= need
		} else {
			p.Resources[res] -= need
		}
	}
	b.Level++
	b.Status = "under_construction"
	b.CurrentLabor = 0
	b.RequiredLabor = bp.RequiredLabor + b.Level*20
	p.ActiveWorksiteID = b.InstanceID
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
		fmt.Sprintf("⬆️ Upgrade *%s → Lv.%d* dimulai! Gunakan `.worksite` atau `.contribute`.", bp.NameID, b.Level),
		fmt.Sprintf("⬆️ Upgrading *%s → Lv.%d*! Use `.worksite` or `.contribute`.", bp.Name, b.Level),
	))
}

func cmdBuildings(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	if len(p.Buildings) == 0 {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg, "🏗️ Belum ada bangunan. Ketik `.build` untuk mulai!", "🏗️ No buildings yet. Use `.build` to get started!"))
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🏰 *%s — Buildings (%d)*\n\n", p.Name, len(p.Buildings))
	for _, bld := range p.Buildings {
		st := "✅ Active"
		if bld.Status == "under_construction" {
			st = fmt.Sprintf("🚧 %d/%d labor", bld.CurrentLabor, bld.RequiredLabor)
		}
		stored := ""
		if bld.StoredIncome >= 1 {
			stored = fmt.Sprintf(" | Ready: `%.0f %s`", bld.StoredIncome, bld.Production.Resource)
		}
		fmt.Fprintf(&b, "• *%s* (`%s`) Lv.%d — %s%s\n", bld.Name, bld.ID, bld.Level, st, stored)
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func findActiveBuilding(p *rpg.Player) (*rpg.Building, int) {
	if p.ActiveWorksiteID == "" {
		return nil, -1
	}
	for i := range p.Buildings {
		if p.Buildings[i].InstanceID == p.ActiveWorksiteID {
			return &p.Buildings[i], i
		}
	}
	return nil, -1
}

func progressBar(cur, maxVal int) string {
	const width = 10
	if maxVal <= 0 {
		return "[██████████]"
	}
	filled := (cur * width) / maxVal
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func cmdWorksite(client *whatsmeow.Client, msg *events.Message, args []string) {
	pid := playerIDFor(client, msg)
	p, err := rpg.LoadPlayer(pid)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	b, _ := findActiveBuilding(p)
	if b == nil {
		p.ActiveWorksiteID = ""
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.worksite_none"))
		return
	}
	pct := float64(b.CurrentLabor) * 100 / float64(max(1, b.RequiredLabor))
	text := i18n.TMsg(client, msg, "rpg.worksite_card", b.Name, b.Level, progressBar(b.CurrentLabor, b.RequiredLabor), pct, b.CurrentLabor, b.RequiredLabor, p.Energy.Current, p.Energy.Max)
	sent, err := utils.SendReply(client, msg.Info.Chat, text)
	if err != nil {
		return
	}
	worksiteMu.Lock()
	worksiteActive[sent.ID] = worksiteSession{PlayerID: pid, BuildingID: b.InstanceID}
	worksiteMu.Unlock()

	// Seed ⛏️ reaction just like NopebotGO
	_, _ = client.SendMessage(context.Background(), msg.Info.Chat, &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{
				FromMe:    proto.Bool(true),
				ID:        proto.String(sent.ID),
				RemoteJID: proto.String(msg.Info.Chat.String()),
			},
			Text:              proto.String("⛏️"),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
	})
}

func applyWorksiteContribution(client *whatsmeow.Client, msg *events.Message, pid string, amount int, editMsgID string) {
	p, err := rpg.LoadPlayer(pid)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	b, idx := findActiveBuilding(p)
	if b == nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.worksite_none"))
		return
	}
	if amount <= 0 {
		amount = 10
	}
	need := b.RequiredLabor - b.CurrentLabor
	if amount > need {
		amount = need
	}
	if p.Energy.Current < amount {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_energy", amount, p.Energy.Current, p.Energy.Max))
		return
	}
	p.Energy.Current -= amount
	b.CurrentLabor += amount
	rpg.AddRPGXP(p, int64(amount))

	if b.CurrentLabor >= b.RequiredLabor {
		b.Status = "active"
		b.LastIncomeUpdate = time.Now().Unix()
		p.ActiveWorksiteID = ""
		p.Buildings[idx] = *b
		_ = rpg.SavePlayer(p)
		if editMsgID != "" {
			worksiteMu.Lock()
			delete(worksiteActive, editMsgID)
			worksiteMu.Unlock()
		}
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.worksite_done", b.Name, b.Level))
		return
	}

	p.Buildings[idx] = *b
	_ = rpg.SavePlayer(p)
	pct := float64(b.CurrentLabor) * 100 / float64(max(1, b.RequiredLabor))
	updated := i18n.TMsg(client, msg, "rpg.worksite_card", b.Name, b.Level, progressBar(b.CurrentLabor, b.RequiredLabor), pct, b.CurrentLabor, b.RequiredLabor, p.Energy.Current, p.Energy.Max)
	if editMsgID != "" {
		edit := client.BuildEdit(msg.Info.Chat, editMsgID, &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(updated)},
		})
		if _, err := client.SendMessage(context.Background(), msg.Info.Chat, edit); err == nil {
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, updated)
}

func handleWorksiteReaction(client *whatsmeow.Client, msg *events.Message) bool {
	if msg == nil || msg.Message.GetReactionMessage() == nil {
		return false
	}
	react := msg.Message.GetReactionMessage()
	targetID := react.GetKey().GetID()
	worksiteMu.Lock()
	sess, ok := worksiteActive[targetID]
	worksiteMu.Unlock()
	if !ok {
		return false
	}
	pid := playerIDFor(client, msg)
	if sess.PlayerID != pid {
		return false
	}
	txt := react.GetText()
	if txt != "⛏" && txt != "⛏️" {
		return false
	}
	applyWorksiteContribution(client, msg, pid, 10, targetID)
	return true
}

func cmdContribute(client *whatsmeow.Client, msg *events.Message, args []string) {
	amt := 10
	if len(args) > 0 {
		if v, err := strconv.Atoi(strings.TrimSpace(args[0])); err == nil && v > 0 {
			amt = v
		}
	}
	applyWorksiteContribution(client, msg, playerIDFor(client, msg), amt, "")
}

func cmdCollect(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	collected := rpg.CollectAllIncome(p)
	if len(collected) == 0 {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.collect_none"))
		return
	}
	p.Quest.Collects++
	_ = rpg.SavePlayer(p)
	var lines []string
	for res, qty := range collected {
		if res == rpg.Yen {
			lines = append(lines, fmt.Sprintf("• 💴 `+¥%d`", qty))
		} else {
			lines = append(lines, fmt.Sprintf("• 📦 `+%d %s`", qty, res))
		}
	}
	sort.Strings(lines)
	utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.collect_ok", strings.Join(lines, "\n")))
}
