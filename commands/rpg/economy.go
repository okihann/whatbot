package rpgcmd

import (
	"fmt"
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
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	cmds := []types.Command{
		{Name: "bank", Category: "rpg", Description: "Manage RPG bank. Usage: .bank [deposit|withdraw|upgrade|transfer] [amount|all]", Execute: cmdBank},
		{Name: "shop", Aliases: []string{"toko", "rpgmarket"}, Category: "rpg", Description: "View Coinsteel Shop items & live hourly resource market prices.", Execute: cmdShop},
		{Name: "buy", Aliases: []string{"beli"}, Category: "rpg", Description: "Buy an item from the RPG shop. Usage: .buy <item_id> [qty]", Execute: cmdBuy},
		{Name: "sell", Aliases: []string{"jual"}, Category: "rpg", Description: "Sell empire resources at live hourly market prices. Usage: .sell <resource> <qty|all>", Execute: cmdSell},
		{Name: "eat", Aliases: []string{"makan", "rest"}, Category: "rpg", Description: "Eat food or use potions to restore Energy & HP. Usage: .eat [vegetables|fruit|meat|energy_potion|hp_potion]", Execute: cmdEat},
		{Name: "craft", Aliases: []string{"tempa"}, Category: "rpg", Description: "Craft weapons & armor from mined resources. Usage: .craft [gear_id]", Execute: cmdCraft},
		{Name: "equip", Aliases: []string{"pakai"}, Category: "rpg", Description: "Equip crafted weapon or armor. Usage: .equip <gear_id>", Execute: cmdEquip},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdBank(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	capLimit := rpg.EffectiveBankCapacity(p)
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 0 {
		upgradeCost := int64(p.Bank.Level * 1200)
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			fmt.Sprintf("🏦 *Bank Coinsteel (Lv.%d)*\n• Saldo Bank: `¥%d / ¥%d`\n• Dompet (Yen): `¥%d`\n• Biaya Upgrade (+¥10.000 cap): `¥%d`\n\nPerintah:\n• `.bank deposit <jumlah|all>`\n• `.bank withdraw <jumlah|all>`\n• `.bank upgrade`\n• `.bank transfer @user <jumlah>`", p.Bank.Level, p.Bank.Balance, capLimit, p.Currency[rpg.Yen], upgradeCost),
			fmt.Sprintf("🏦 *Bank of Coinsteel (Lv.%d)*\n• Bank Balance: `¥%d / ¥%d`\n• Wallet (Yen): `¥%d`\n• Upgrade Cost (+¥10,000 cap): `¥%d`\n\nCommands:\n• `.bank deposit <amount|all>`\n• `.bank withdraw <amount|all>`\n• `.bank upgrade`\n• `.bank transfer @user <amount>`", p.Bank.Level, p.Bank.Balance, capLimit, p.Currency[rpg.Yen], upgradeCost),
		))
		return
	}

	action := strings.ToLower(fields[0])
	switch action {
	case "deposit", "dep", "setor":
		if len(fields) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.bank deposit <amount|all>`")
			return
		}
		var amt int64
		if strings.EqualFold(fields[1], "all") {
			amt = p.Currency[rpg.Yen]
			if p.Bank.Balance+amt > capLimit {
				amt = capLimit - p.Bank.Balance
			}
		} else {
			amt, _ = strconv.ParseInt(fields[1], 10, 64)
		}
		if amt <= 0 || amt > p.Currency[rpg.Yen] {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid deposit amount.")
			return
		}
		if p.Bank.Balance+amt > capLimit {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Exceeds bank capacity (`¥%d`). Use `.bank upgrade`!", capLimit))
			return
		}
		p.Currency[rpg.Yen] -= amt
		p.Bank.Balance += amt
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Deposited `¥%d`! Bank: `¥%d / ¥%d`.", amt, p.Bank.Balance, capLimit))

	case "withdraw", "wd", "tarik":
		if len(fields) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.bank withdraw <amount|all>`")
			return
		}
		var amt int64
		if strings.EqualFold(fields[1], "all") {
			amt = p.Bank.Balance
		} else {
			amt, _ = strconv.ParseInt(fields[1], 10, 64)
		}
		if amt <= 0 || amt > p.Bank.Balance {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid withdraw amount.")
			return
		}
		p.Bank.Balance -= amt
		p.Currency[rpg.Yen] += amt
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Withdrew `¥%d`! Wallet: `¥%d`.", amt, p.Currency[rpg.Yen]))

	case "upgrade":
		cost := int64(p.Bank.Level * 1200)
		if p.Currency[rpg.Yen] < cost {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Need `¥%d` in wallet to upgrade Bank.", cost))
			return
		}
		p.Currency[rpg.Yen] -= cost
		p.Bank.Level++
		p.Bank.Capacity += 10000
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏦 Bank upgraded to *Lv.%d*! New capacity: `¥%d`.", p.Bank.Level, rpg.EffectiveBankCapacity(p)))

	case "transfer", "tf":
		targets := utils.ExtractTargetJIDs(msg, fields[1:])
		if len(targets) == 0 || len(fields) < 3 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.bank transfer @user <amount>`")
			return
		}
		amt, _ := strconv.ParseInt(fields[len(fields)-1], 10, 64)
		if amt <= 0 || amt > p.Currency[rpg.Yen] {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid transfer amount.")
			return
		}
		targetNum := utils.ResolvePhoneNumber(client, targets[0], waTypes.EmptyJID)
		other, err := rpg.LoadPlayer(targetNum)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Target player hasn't started Coinsteel RPG yet.")
			return
		}
		p.Currency[rpg.Yen] -= amt
		other.Currency[rpg.Yen] += amt
		_ = rpg.SavePlayer(p)
		_ = rpg.SavePlayer(other)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💸 Transferred `¥%d` to *%s*!", amt, other.Name))

	default:
		utils.SendReply(client, msg.Info.Chat, "Usage: `.bank [deposit|withdraw|upgrade|transfer]`")
	}
}

func cmdShop(client *whatsmeow.Client, msg *events.Message, args []string) {
	var b strings.Builder
	b.WriteString(pickLang(client, msg, "🛒 *TOKO & PASAR COINSTEEL*\n\n*Item Toko (`.buy <id> [qty]`):*\n", "🛒 *COINSTEEL SHOP & MARKET*\n\n*Shop Items (`.buy <id> [qty]`):*\n"))
	for _, it := range rpg.ShopItems {
		desc := it.DescEN
		if isID(client, msg) {
			desc = it.DescID
		}
		fmt.Fprintf(&b, "• *%s* (`%s`) — `¥%d`\n  _%s_\n", it.Name, it.ID, it.PriceYen, desc)
	}
	b.WriteString(pickLang(client, msg, "\n📈 *Harga Jual Pasar Saat Ini (`.sell <res> <qty|all>`):*\n", "\n📈 *Live Hourly Sell Prices (`.sell <res> <qty|all>`):*\n"))
	order := []rpg.Resource{rpg.Iron, rpg.Vegetables, rpg.Fruit, rpg.Meat, rpg.Gold, rpg.Emerald, rpg.Diamond}
	for _, r := range order {
		fmt.Fprintf(&b, "• `%s`: `¥%d` / unit\n", r, rpg.MarketPrice(r))
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdBuy(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.buy <item_id> [qty]` (see `.shop`)")
		return
	}
	itemID := strings.ToLower(fields[0])
	qty := 1
	if len(fields) >= 2 {
		if q, e := strconv.Atoi(fields[1]); e == nil && q >= 1 && q <= 50 {
			qty = q
		}
	}
	var found *rpg.ShopItem
	for i := range rpg.ShopItems {
		if rpg.ShopItems[i].ID == itemID {
			found = &rpg.ShopItems[i]
			break
		}
	}
	if found == nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Unknown shop item. Use `.shop`.")
		return
	}
	totalCost := found.PriceYen * int64(qty)
	if p.Currency[rpg.Yen] < totalCost {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Need `¥%d`, you have `¥%d`.", totalCost, p.Currency[rpg.Yen]))
		return
	}
	p.Currency[rpg.Yen] -= totalCost
	switch itemID {
	case "iron_pack":
		p.Resources[rpg.Iron] += int64(50 * qty)
	case "raid_shield":
		base := time.Now()
		if p.RaidProtectionUntil > base.UnixMilli() {
			base = time.UnixMilli(p.RaidProtectionUntil)
		}
		p.RaidProtectionUntil = base.Add(time.Duration(12*qty) * time.Hour).UnixMilli()
	default:
		p.Items[itemID] += qty
	}
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Bought `%dx %s` for `¥%d`!", qty, found.Name, totalCost))
}

func cmdSell(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.sell <iron|vegetables|fruit|meat|gold|emerald|diamond> <qty|all>`")
		return
	}
	res := rpg.Resource(strings.ToLower(fields[0]))
	price := rpg.MarketPrice(res)
	if price == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Resource cannot be sold. Check `.shop`.")
		return
	}
	have := p.Resources[res]
	var qty int64
	if strings.EqualFold(fields[1], "all") {
		qty = have
	} else {
		qty, _ = strconv.ParseInt(fields[1], 10, 64)
	}
	if qty <= 0 || qty > have {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ You only have `%d %s`.", have, res))
		return
	}
	earned := qty * price
	p.Resources[res] -= qty
	p.Currency[rpg.Yen] += earned
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💰 Sold `%d %s` at `¥%d/unit` → earned *+¥%d*!", qty, res, price, earned))
}

func cmdEat(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	choice := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if choice == "" {
		// Auto-pick best available food/potion
		for _, cand := range []string{"meat", "fruit", "vegetables", "energy_potion", "hp_potion"} {
			if cand == "energy_potion" || cand == "hp_potion" {
				if p.Items[cand] > 0 {
					choice = cand
					break
				}
			} else if p.Resources[rpg.Resource(cand)] > 0 {
				choice = cand
				break
			}
		}
	}
	switch choice {
	case "vegetables", "sayur":
		if p.Resources[rpg.Vegetables] < 5 {
			utils.SendReply(client, msg.Info.Chat, "❌ Need at least 5 vegetables.")
			return
		}
		p.Resources[rpg.Vegetables] -= 5
		p.Energy.Current = min(p.Energy.Max, p.Energy.Current+10)
	case "fruit", "buah":
		if p.Resources[rpg.Fruit] < 4 {
			utils.SendReply(client, msg.Info.Chat, "❌ Need at least 4 fruit.")
			return
		}
		p.Resources[rpg.Fruit] -= 4
		p.Energy.Current = min(p.Energy.Max, p.Energy.Current+12)
	case "meat", "daging":
		if p.Resources[rpg.Meat] < 2 {
			utils.SendReply(client, msg.Info.Chat, "❌ Need at least 2 meat.")
			return
		}
		p.Resources[rpg.Meat] -= 2
		p.Energy.Current = min(p.Energy.Max, p.Energy.Current+15)
		p.Character.Stats.Health = min(p.Character.Stats.MaxHealth, p.Character.Stats.Health+35)
	case "energy_potion":
		if p.Items["energy_potion"] < 1 {
			utils.SendReply(client, msg.Info.Chat, "❌ No Energy Potion. Buy in `.shop`.")
			return
		}
		p.Items["energy_potion"]--
		p.Energy.Current = min(p.Energy.Max, p.Energy.Current+20)
	case "hp_potion":
		if p.Items["hp_potion"] < 1 {
			utils.SendReply(client, msg.Info.Chat, "❌ No Health Potion. Buy in `.shop`.")
			return
		}
		p.Items["hp_potion"]--
		p.Character.Stats.Health = min(p.Character.Stats.MaxHealth, p.Character.Stats.Health+60)
		p.Character.Stats.Mana = min(p.Character.Stats.MaxMana, p.Character.Stats.Mana+30)
	default:
		utils.SendReply(client, msg.Info.Chat, "🍽️ Usage: `.eat <vegetables|fruit|meat|energy_potion|hp_potion>`")
		return
	}
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🍽️ Consumed *%s*!\n⚡ Energy: `%d/%d` | ❤️ HP: `%d/%d` | 🔮 MP: `%d/%d`", choice, p.Energy.Current, p.Energy.Max, p.Character.Stats.Health, p.Character.Stats.MaxHealth, p.Character.Stats.Mana, p.Character.Stats.MaxMana))
}

func cmdCraft(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	if len(args) == 0 {
		ids := make([]string, 0, len(rpg.GearBlueprints))
		for id := range rpg.GearBlueprints {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var b strings.Builder
		b.WriteString(pickLang(client, msg, "⚒️ *Pandai Besi (`.craft <id>`):*\n\n", "⚒️ *Blacksmith Forge (`.craft <id>`):*\n\n"))
		for _, id := range ids {
			g := rpg.GearBlueprints[id]
			fmt.Fprintf(&b, "• *%s* (`%s`) [%s +%d]\n  Cost: `%s`\n", g.Name, id, g.Slot, g.Bonus, formatCosts(g.Cost))
		}
		utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
		return
	}
	gid := strings.ToLower(strings.TrimSpace(args[0]))
	bp, ok := rpg.GearBlueprints[gid]
	if !ok {
		utils.SendReply(client, msg.Info.Chat, "❌ Unknown gear ID. Run `.craft` to list recipes.")
		return
	}
	for res, cost := range bp.Cost {
		have := p.Resources[res]
		if res == rpg.Yen {
			have = p.Currency[res]
		}
		if have < cost {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Need `%d %s` (have `%d`).", cost, res, have))
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
	p.Items[gid]++
	// Auto-equip if slot is empty
	if bp.Slot == "weapon" && p.EquippedWeapon == "" {
		p.EquippedWeapon = gid
	}
	if bp.Slot == "armor" && p.EquippedArmor == "" {
		p.EquippedArmor = gid
	}
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚒️ Forged *%s* (`+%d %s`)! Equip anytime with `.equip %s`.", bp.Name, bp.Bonus, bp.Slot, gid))
}

func cmdEquip(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	gid := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	bp, ok := rpg.GearBlueprints[gid]
	if !ok || p.Items[gid] < 1 {
		utils.SendReply(client, msg.Info.Chat, "❌ You don't own that gear. Craft it with `.craft`.")
		return
	}
	if bp.Slot == "weapon" {
		p.EquippedWeapon = gid
	} else {
		p.EquippedArmor = gid
	}
	_ = rpg.SavePlayer(p)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🛡️ Equipped *%s* (`+%d %s`)!", bp.Name, bp.Bonus, bp.Slot))
}
