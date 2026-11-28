package funcmd

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	cmds := []types.Command{
		{Name: "cek", Category: "games", Description: "Fun daily meter (khodam, ganteng, pintar, luck, etc.). Usage: .cek ganteng [name]", Execute: cmdCek},
		{Name: "dadu", Aliases: []string{"dice"}, Category: "games", Description: "Roll a 6-sided (or N-sided) die. Usage: .dadu [sides]", Execute: cmdDadu},
		{Name: "koin", Aliases: []string{"coin", "flipcoin"}, Category: "games", Description: "Flip a coin (Heads / Tails).", Execute: cmdKoin},
		{Name: "suit", Aliases: []string{"rps"}, Category: "games", Description: "Play Rock-Paper-Scissors vs bot. Usage: .suit <batu|gunting|kertas>", Execute: cmdSuit},
		{Name: "ship", Category: "games", Description: "Calculate love compatibility between two names. Usage: .ship Alice | Bob", Execute: cmdShip},
		{Name: "roast", Category: "games", Description: "Playful roast for a name or mentioned user.", Execute: cmdRoast},
		{Name: "fact", Category: "games", Description: "Fetch a random fun fact.", Execute: cmdFact},
		{Name: "debate", Category: "games", Description: "Generate a fun debate topic for the group.", Execute: cmdDebate},
		{Name: "mostlikely", Category: "games", Description: "Generate a 'Who is most likely to...' prompt.", Execute: cmdMostLikely},
		{Name: "reaction", Category: "games", Description: "Send an anime reaction GIF/image. Usage: .reaction <hug|pat|slap|bonk|poke|kiss|cry|happy|wave>", Execute: cmdReaction},
		{Name: "truthordare", Aliases: []string{"tod"}, Category: "games", Description: "Get a Truth or Dare prompt. Usage: .tod [truth|dare]", Execute: cmdTruthOrDare},
		{Name: "wouldyourather", Aliases: []string{"wyr"}, Category: "games", Description: "Get a 'Would You Rather' question.", Execute: cmdWYR},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func dailySeed(key string) uint64 {
	sum := sha256.Sum256([]byte(time.Now().Format("2006-01-02") + ":" + strings.ToLower(key)))
	return binary.BigEndian.Uint64(sum[:8])
}

func cmdCek(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.cek <khodam|ganteng|cantik|pintar|luck|wibu> [name]`")
		return
	}
	attr := strings.ToLower(fields[0])
	target := msg.Info.PushName
	if len(fields) > 1 {
		target = strings.Join(fields[1:], " ")
	}
	if target == "" {
		target = "You"
	}
	seed := dailySeed(attr + ":" + target)
	if attr == "khodam" {
		khodams := []string{"Naga Emas 🐉", "Macan Putih 🐯", "Kucing Oren 🐱", "Kulkas 2 Pintu 🧊", "Tutup Panci 🍳", "Elang Hitam 🦅", "Paus Biru 🐋", "Sandal Jepit 🩴", "Rubah Ekor 9 🦊", "Batu Bata 🧱", "Kopi Susu ☕", "Kura-kura Ninja 🐢"}
		pick := khodams[seed%uint64(len(khodams))]
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔮 *Cek Khodam*\n• Nama: *%s*\n• Khodam hari ini: *%s*", target, pick))
		return
	}
	pct := int(seed % 101)
	bars := pct / 10
	bar := strings.Repeat("█", bars) + strings.Repeat("░", 10-bars)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎯 *Cek %s*\n• Nama: *%s*\n• Skor: `%d%%` [%s]", handlers.Title(attr), target, pct, bar))
}

func cmdDadu(client *whatsmeow.Client, msg *events.Message, args []string) {
	sides := 6
	if len(args) > 0 {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(args[0]), "%d", &n); err == nil && n >= 2 && n <= 1000 {
			sides = n
		}
	}
	roll := rand.Intn(sides) + 1
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎲 Rolled a *d%d*: **%d**", sides, roll))
}

func cmdKoin(client *whatsmeow.Client, msg *events.Message, args []string) {
	if rand.Intn(2) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🪙 Coin Flip: *HEADS (Angka)*")
	} else {
		utils.SendReply(client, msg.Info.Chat, "🪙 Coin Flip: *TAILS (Gambar)*")
	}
}

func cmdSuit(client *whatsmeow.Client, msg *events.Message, args []string) {
	choice := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	norm := map[string]string{
		"batu": "rock", "rock": "rock", "✊": "rock",
		"gunting": "scissors", "scissors": "scissors", "✌️": "scissors",
		"kertas": "paper", "paper": "paper", "✋": "paper",
	}
	user, ok := norm[choice]
	if !ok {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.suit <batu|gunting|kertas>` (or `rock|paper|scissors`)")
		return
	}
	opts := []string{"rock", "paper", "scissors"}
	bot := opts[rand.Intn(3)]
	emoji := map[string]string{"rock": "✊ Batu", "paper": "✋ Kertas", "scissors": "✌️ Gunting"}
	result := "🤝 *Draw!*"
	if (user == "rock" && bot == "scissors") || (user == "paper" && bot == "rock") || (user == "scissors" && bot == "paper") {
		result = "🎉 *You Win!*"
	} else if user != bot {
		result = "🤖 *Bot Wins!*"
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("You: %s vs Bot: %s\n%s", emoji[user], emoji[bot], result))
}

func cmdShip(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	parts := strings.Split(raw, "|")
	if len(parts) < 2 {
		parts = strings.Fields(raw)
	}
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.ship Alice | Bob`")
		return
	}
	a, b := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	pct := int(dailySeed("ship:"+a+":"+b) % 101)
	hearts := pct / 20
	bar := strings.Repeat("❤️", hearts) + strings.Repeat("🤍", 5-hearts)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💘 *Love Match*\n*%s* × *%s*\nScore: `%d%%` %s", a, b, pct, bar))
}

func cmdRoast(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := strings.TrimSpace(strings.Join(args, " "))
	if target == "" {
		target = msg.Info.PushName
	}
	roasts := []string{
		"%s has the energy of a phone at 1%% battery that somehow lasts 4 hours out of pure stubbornness.",
		"%s brings everyone joy... mostly when they finally mute their mic.",
		"%s is proof that autocorrect can't fix everything.",
		"%s's Wi-Fi signal has more commitment than their sleep schedule.",
		"If procrastination were an Olympic sport, %s would register tomorrow.",
	}
	utils.SendReply(client, msg.Info.Chat, "🔥 "+fmt.Sprintf(roasts[rand.Intn(len(roasts))], target))
}

func cmdFact(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var resp struct {
		Text string `json:"text"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://uselessfacts.jsph.pl/api/v2/facts/random?language=en", nil, &resp); err == nil && resp.Text != "" {
		utils.SendReply(client, msg.Info.Chat, "💡 *Fun Fact:*\n"+resp.Text)
		return
	}
	fallback := []string{
		"Honey never spoils — edible 3,000-year-old honey has been found in Egyptian tombs.",
		"Octopuses have three hearts and blue blood.",
		"Bananas are botanically berries, while strawberries are not.",
	}
	utils.SendReply(client, msg.Info.Chat, "💡 *Fun Fact:*\n"+fallback[rand.Intn(len(fallback))])
}

func cmdDebate(client *whatsmeow.Client, msg *events.Message, args []string) {
	topics := []string{
		"Bubur diaduk vs Bubur tidak diaduk?",
		"Indomie Goreng vs Indomie Kuah?",
		"Pineapple on pizza: masterpiece or crime?",
		"Coffee in the morning vs Tea in the morning?",
		"Tabs vs Spaces?",
		"Cats vs Dogs — which makes the better roommate?",
		"Dark mode forever vs Light mode during daytime?",
	}
	utils.SendReply(client, msg.Info.Chat, "⚖️ *Group Debate Topic:*\n*"+topics[rand.Intn(len(topics))]+"*")
}

func cmdMostLikely(client *whatsmeow.Client, msg *events.Message, args []string) {
	prompts := []string{
		"Who is most likely to sleep through 10 alarms?",
		"Who is most likely to reply 3 days late with 'sorry just saw this'?",
		"Who is most likely to become accidentally famous on TikTok?",
		"Who is most likely to spend their whole paycheck on food?",
		"Who is most likely to survive a zombie apocalypse?",
		"Who is most likely to forget their own birthday?",
	}
	utils.SendReply(client, msg.Info.Chat, "👀 *Most Likely To:*\n"+prompts[rand.Intn(len(prompts))])
}

func cmdReaction(client *whatsmeow.Client, msg *events.Message, args []string) {
	valid := map[string]bool{"hug": true, "pat": true, "slap": true, "bonk": true, "poke": true, "kiss": true, "cry": true, "happy": true, "wave": true, "wink": true, "smile": true, "blush": true}
	act := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if !valid[act] {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.reaction <hug|pat|slap|bonk|poke|kiss|cry|happy|wave|wink|smile|blush>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp struct {
		URL string `json:"url"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://api.waifu.pics/sfw/"+act, nil, &resp); err != nil || resp.URL == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch reaction.")
		return
	}
	data, ct, err := utils.DownloadHTTPBytes(ctx, resp.URL, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "🎭 "+resp.URL)
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, data, ct, "🎭 *"+handlers.Title(act)+"*")
}

func cmdTruthOrDare(client *whatsmeow.Client, msg *events.Message, args []string) {
	truths := []string{
		"What is the most embarrassing thing you've done in public?",
		"What's a secret talent nobody in this group knows about?",
		"Who was your very first crush?",
		"What's the last lie you told?",
		"What is your weirdest late-night snack?",
	}
	dares := []string{
		"Send a voice note singing the chorus of your favorite song!",
		"Change your bio to 'Powered by NopeBot' for 1 hour.",
		"Type your next 3 messages using only emojis.",
		"Send the 5th photo in your gallery (SFW only!).",
		"Compliment the person who texted right before you.",
	}
	mode := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if mode != "truth" && mode != "dare" {
		if rand.Intn(2) == 0 {
			mode = "truth"
		} else {
			mode = "dare"
		}
	}
	if mode == "truth" {
		utils.SendReply(client, msg.Info.Chat, "🗣️ *TRUTH:*\n"+truths[rand.Intn(len(truths))])
	} else {
		utils.SendReply(client, msg.Info.Chat, "🔥 *DARE:*\n"+dares[rand.Intn(len(dares))])
	}
}

func cmdWYR(client *whatsmeow.Client, msg *events.Message, args []string) {
	qs := []string{
		"Would you rather be able to fly OR be invisible?",
		"Would you rather have unlimited free food OR unlimited free travel?",
		"Would you rather never need sleep OR never need to eat?",
		"Would you rather know every language OR play every instrument?",
		"Would you rather travel 100 years into the past OR 100 years into the future?",
	}
	utils.SendReply(client, msg.Info.Chat, "🤔 *Would You Rather:*\n"+qs[rand.Intn(len(qs))])
}
