package rpgcmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"bot/handlers"
	"bot/i18n"
	"bot/rpg"
	"bot/types"
	"bot/utils"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	cmds := []types.Command{
		{Name: "quest", Aliases: []string{"misi"}, Category: "rpg", Description: "View and claim your daily Coinsteel RPG quest rewards.", Execute: cmdQuest},
		{Name: "guild", Aliases: []string{"klan"}, Category: "rpg", Description: "Manage RPG Guilds. Usage: .guild [create <name>|join <name>|leave|info|donate <yen>]", Execute: cmdGuild},
		{Name: "rpgtop", Aliases: []string{"topempire"}, Category: "rpg", Description: "Coinsteel Empire leaderboard. Usage: .rpgtop [networth|level|power]", Execute: cmdRPGTop},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdQuest(client *whatsmeow.Client, msg *events.Message, args []string) {
	p, err := rpg.LoadPlayer(playerIDFor(client, msg))
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	q := &p.Quest
	doneWork := q.Works >= 2
	doneHunt := q.Hunts >= 2
	doneMine := q.Mines >= 1
	allDone := doneWork && doneHunt && doneMine

	if allDone && !q.Claimed {
		q.Claimed = true
		p.Currency[rpg.Yen] += 1000
		p.Resources[rpg.Diamond] += 1
		rpg.AddRPGXP(p, 120)
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			"🎉 *Misi Harian Selesai!*\nKamu menerima: `+¥1.000`, `+1 Diamond`, dan `+120 XP`!",
			"🎉 *Daily Quest Completed!*\nRewards claimed: `+¥1,000`, `+1 Diamond`, and `+120 XP`!",
		))
		return
	}

	mark := func(ok bool) string {
		if ok {
			return "✅"
		}
		return "⬜"
	}
	status := pickLang(client, msg, "Selesaikan semua lalu ketik `.quest` untuk klaim `+¥1.000 & +1 Diamond`!", "Complete all objectives then run `.quest` to claim `+¥1,000 & +1 Diamond`!")
	if q.Claimed {
		status = pickLang(client, msg, "✅ Hadiah hari ini sudah diklaim! Kembali lagi besok.", "✅ Today's quest reward already claimed! Come back tomorrow.")
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"📜 *Daily RPG Quest (%s)*\n• %s Work: `%d/2` (`.work`)\n• %s Hunt: `%d/2` (`.hunt`)\n• %s Mine: `%d/1` (`.mine`)\n\n%s",
		q.DateKey, mark(doneWork), q.Works, mark(doneHunt), q.Hunts, mark(doneMine), q.Mines, status,
	))
}

func cmdGuild(client *whatsmeow.Client, msg *events.Message, args []string) {
	pid := playerIDFor(client, msg)
	p, err := rpg.LoadPlayer(pid)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, i18n.TMsg(client, msg, "rpg.no_account"))
		return
	}
	raw := strings.TrimSpace(strings.Join(args, " "))
	parts := strings.SplitN(raw, " ", 2)
	sub := strings.ToLower(parts[0])

	switch sub {
	case "create", "buat":
		if p.GuildID != "" {
			utils.SendReply(client, msg.Info.Chat, "❌ Leave your current guild first (`.guild leave`).")
			return
		}
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.guild create <Guild Name>` (Costs `¥1,000`)")
			return
		}
		if p.Currency[rpg.Yen] < 1000 {
			utils.SendReply(client, msg.Info.Chat, "❌ Creating a guild costs `¥1,000`.")
			return
		}
		gName := strings.TrimSpace(parts[1])
		if _, err := rpg.LoadGuildByName(gName); err == nil {
			utils.SendReply(client, msg.Info.Chat, "❌ A guild with that name already exists.")
			return
		}
		p.Currency[rpg.Yen] -= 1000
		g := &rpg.Guild{
			ID:        uuid.New().String(),
			Name:      gName,
			LeaderID:  pid,
			Level:     1,
			Treasury:  0,
			Members:   []string{pid},
			CreatedAt: time.Now().Unix(),
		}
		_ = rpg.SaveGuild(g)
		p.GuildID = g.ID
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏰 Guild *%s* founded! Friends can join with `.guild join %s`.", g.Name, g.Name))

	case "join", "gabung":
		if p.GuildID != "" {
			utils.SendReply(client, msg.Info.Chat, "❌ You are already in a guild. Leave first with `.guild leave`.")
			return
		}
		if len(parts) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.guild join <Guild Name>`")
			return
		}
		g, err := rpg.LoadGuildByName(strings.TrimSpace(parts[1]))
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Guild not found.")
			return
		}
		g.Members = append(g.Members, pid)
		_ = rpg.SaveGuild(g)
		p.GuildID = g.ID
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎉 Joined Guild *%s* (`%d` members)!", g.Name, len(g.Members)))

	case "leave", "keluar":
		if p.GuildID == "" {
			utils.SendReply(client, msg.Info.Chat, "ℹ️ You are not in a guild.")
			return
		}
		if g, err := rpg.LoadGuildByID(p.GuildID); err == nil {
			kept := g.Members[:0]
			for _, m := range g.Members {
				if m != pid {
					kept = append(kept, m)
				}
			}
			g.Members = kept
			_ = rpg.SaveGuild(g)
		}
		p.GuildID = ""
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, "👋 Left your guild.")

	case "donate", "donasi":
		if p.GuildID == "" || len(parts) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.guild donate <amount_yen>`")
			return
		}
		amt, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if amt <= 0 || amt > p.Currency[rpg.Yen] {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid donation amount.")
			return
		}
		g, err := rpg.LoadGuildByID(p.GuildID)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Guild not found.")
			return
		}
		p.Currency[rpg.Yen] -= amt
		g.Treasury += amt
		if g.Treasury >= int64(g.Level*5000) {
			g.Level++
		}
		rpg.AddRPGXP(p, amt/20)
		_ = rpg.SaveGuild(g)
		_ = rpg.SavePlayer(p)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💎 Donated `¥%d` to *%s*! Treasury: `¥%d` (Lv.%d).", amt, g.Name, g.Treasury, g.Level))

	default:
		if p.GuildID == "" {
			utils.SendReply(client, msg.Info.Chat, "🏰 You are not in a guild.\n• `.guild create <name>` (¥1,000)\n• `.guild join <name>`")
			return
		}
		g, err := rpg.LoadGuildByID(p.GuildID)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Guild not found.")
			return
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏰 *Guild: %s* (Lv.%d)\n• Leader: `%s`\n• Members: `%d`\n• Treasury: `¥%d`\n\nCommands: `.guild donate <yen>`, `.guild leave`", g.Name, g.Level, g.LeaderID, len(g.Members), g.Treasury))
	}
}

func cmdRPGTop(client *whatsmeow.Client, msg *events.Message, args []string) {
	mode := "networth"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		mode = strings.ToLower(strings.TrimSpace(args[0]))
	}
	players, _ := rpg.ListTopPlayers(mode, 10)
	if len(players) == 0 {
		utils.SendReply(client, msg.Info.Chat, "👑 No rulers registered yet! Be the first with `.start`.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "👑 *Coinsteel Empire Top Rulers (%s)*\n\n", strings.ToUpper(mode))
	for i, p := range players {
		pwr := rpg.TotalArmyPower(&p) + rpg.HeroCombatPower(&p)
		fmt.Fprintf(&b, "%d. *%s* — Lv.%d | ¥%d | ⚔️ %d\n", i+1, p.Name, p.Level, p.NetWorth, pwr)
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}
