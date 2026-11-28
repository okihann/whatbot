package funcmd

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
	"unicode"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	cmds := []types.Command{
		// Classic Indonesian Text Fun
		{Name: "alay", Category: "games", Description: "Convert text into classic Indonesian alay style. Usage: .alay <text>", Execute: cmdAlay},
		{Name: "halah", Category: "games", Description: "Replace all vowels with 'a'. Usage: .halah <text>", Execute: cmdVowelSwap('a')},
		{Name: "heleh", Category: "games", Description: "Replace all vowels with 'e'. Usage: .heleh <text>", Execute: cmdVowelSwap('e')},
		{Name: "hilih", Category: "games", Description: "Replace all vowels with 'i'. Usage: .hilih <text>", Execute: cmdVowelSwap('i')},
		{Name: "holoh", Category: "games", Description: "Replace all vowels with 'o'. Usage: .holoh <text>", Execute: cmdVowelSwap('o')},
		{Name: "huluh", Category: "games", Description: "Replace all vowels with 'u'. Usage: .huluh <text>", Execute: cmdVowelSwap('u')},

		// Group & Chat Fun
		{Name: "apakah", Category: "games", Description: "Ask a yes/no magic 8-ball question. Usage: .apakah <question>", Execute: cmdApakah},
		{Name: "kapankah", Category: "games", Description: "Predict when something will happen. Usage: .kapankah <question>", Execute: cmdKapankah},
		{Name: "siapakah", Category: "games", Description: "Pick a random group member for a question. Usage: .siapakah <question>", Execute: cmdSiapakah},
		{Name: "rate", Category: "games", Description: "Rate anything from 0 to 100%. Usage: .rate <thing>", Execute: cmdRate},
		{Name: "cekkhodam", Category: "games", Description: "Check someone's funny guardian spirit (khodam). Usage: .cekkhodam [name]", Execute: cmdCekKhodam},
		{Name: "jadian", Category: "games", GroupOnly: true, Description: "Randomly pair two members in this group as a couple!", Execute: cmdJadian},
		{Name: "randomnumber", Aliases: []string{"randnum"}, Category: "games", Description: "Pick a random number. Usage: .randomnumber [min] [max]", Execute: cmdRandomNumber},
		{Name: "randomtag", Category: "games", GroupOnly: true, Description: "Tag a random member in this group.", Execute: cmdRandomTag},
		{Name: "truth", Category: "games", Description: "Get a random Truth prompt.", Execute: cmdDirectTruth},
		{Name: "dare", Category: "games", Description: "Get a random Dare challenge.", Execute: cmdDirectDare},
		{Name: "pantun", Category: "games", Description: "Get a random Indonesian pantun.", Execute: cmdPantun},
		{Name: "katabijak", Category: "games", Description: "Get a random wise quote.", Execute: cmdKataBijak},
		{Name: "puisi", Category: "games", Description: "Get a short poem.", Execute: cmdPuisi},
		{Name: "meme", Category: "games", Description: "Fetch a random meme image.", Execute: cmdMeme},
		{Name: "ppcp", Aliases: []string{"couplepp"}, Category: "games", Description: "Get a matching anime couple profile picture pair.", Execute: cmdPPCP},

		// Ramalan / Divination
		{Name: "zodiak", Category: "games", Description: "Check zodiac sign & fortune. Usage: .zodiak <sign or DD-MM>", Execute: cmdZodiak},
		{Name: "shio", Category: "games", Description: "Check Chinese Zodiac (Shio) by birth year. Usage: .shio <year>", Execute: cmdShio},
		{Name: "weton", Category: "games", Description: "Calculate Javanese Weton (Pasaran & Neptu). Usage: .weton <YYYY-MM-DD>", Execute: cmdWeton},
		{Name: "artinama", Category: "games", Description: "Generate a fun meaning & personality reading for a name. Usage: .artinama <name>", Execute: cmdArtiNama},
		{Name: "kecocokannama", Category: "games", Description: "Check name compatibility score. Usage: .kecocokannama <name1> | <name2>", Execute: cmdKecocokanNama},
		{Name: "ramalanjodoh", Category: "games", Description: "Fun soulmate compatibility reading. Usage: .ramalanjodoh <name1> | <name2>", Execute: cmdRamalanJodoh},
		{Name: "tafsirmimpi", Category: "games", Description: "Interpret a dream keyword. Usage: .tafsirmimpi <dream>", Execute: cmdTafsirMimpi},
		{Name: "nohoki", Category: "games", Description: "Check lucky score of a number/phone. Usage: .nohoki <number>", Execute: cmdNoHoki},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func stableSeed(s string) uint64 {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(s))))
	return binary.BigEndian.Uint64(sum[:8])
}

func cmdAlay(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.alay <text>`")
		return
	}
	leet := map[rune]rune{'a': '4', 'e': '3', 'i': '1', 'o': '0', 's': '5', 'g': '9', 't': '7'}
	var b strings.Builder
	for i, r := range text {
		lr := unicode.ToLower(r)
		if rep, ok := leet[lr]; ok && i%2 == 0 {
			b.WriteRune(rep)
		} else if i%2 == 1 {
			b.WriteRune(unicode.ToUpper(r))
		} else {
			b.WriteRune(lr)
		}
	}
	utils.SendReply(client, msg.Info.Chat, b.String())
}

func cmdVowelSwap(target rune) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		text := strings.TrimSpace(strings.Join(args, " "))
		if text == "" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Usage: `.h%cl%ch <text>`", target, target))
			return
		}
		var b strings.Builder
		for _, r := range text {
			switch r {
			case 'a', 'e', 'i', 'o', 'u':
				b.WriteRune(target)
			case 'A', 'E', 'I', 'O', 'U':
				b.WriteRune(unicode.ToUpper(target))
			default:
				b.WriteRune(r)
			}
		}
		utils.SendReply(client, msg.Info.Chat, b.String())
	}
}

func cmdApakah(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.apakah <pertanyaan>`")
		return
	}
	answers := []string{
		"Ya, pasti! ✅", "Kemungkinan besar iya 👍", "Bisa jadi 🤔", "Tidak mungkin ❌",
		"Kayaknya nggak deh 😅", "100% IYA! 🔥", "Coba tanya lagi nanti 🔮", "Sepertinya tidak 🙅",
		"Tentu saja! ✨", "Jangan terlalu berharap 😬",
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔮 *Apakah %s?*\n💬 *Jawaban:* %s", strings.TrimSuffix(q, "?"), answers[rand.Intn(len(answers))]))
}

func cmdKapankah(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.kapankah <pertanyaan>`")
		return
	}
	units := []string{"detik lagi", "menit lagi", "jam lagi", "hari lagi", "minggu lagi", "bulan lagi", "tahun lagi"}
	n := rand.Intn(12) + 1
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⏳ *Kapankah %s?*\n💬 *Jawaban:* %d %s!", strings.TrimSuffix(q, "?"), n, units[rand.Intn(len(units))]))
}

func cmdSiapakah(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.siapakah <pertanyaan>`")
		return
	}
	if !msg.Info.IsGroup {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎯 *Siapakah %s?*\nJelas kamu lah! 😄", strings.TrimSuffix(q, "?")))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil || gi == nil || len(gi.Participants) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not load group members.")
		return
	}
	chosen := gi.Participants[rand.Intn(len(gi.Participants))]
	tag := chosen.JID.ToNonAD().User
	utils.SendMentionReply(client, msg.Info.Chat, fmt.Sprintf("🎯 *Siapakah %s?*\n👉 @%s!", strings.TrimSuffix(q, "?"), tag), []string{chosen.JID.ToNonAD().String()})
}

func cmdRate(client *whatsmeow.Client, msg *events.Message, args []string) {
	thing := strings.TrimSpace(strings.Join(args, " "))
	if thing == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.rate <something>`")
		return
	}
	score := int(stableSeed("rate:"+thing) % 101)
	bar := strings.Repeat("█", score/10) + strings.Repeat("░", 10-score/10)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⭐ *Rating:* %s\n`[%s]` *%d/100*", thing, bar, score))
}

func cmdCekKhodam(client *whatsmeow.Client, msg *events.Message, args []string) {
	name := strings.TrimSpace(strings.Join(args, " "))
	if name == "" {
		name = msg.Info.PushName
	}
	if name == "" {
		name = "Kamu"
	}
	khodams := []string{
		"Harimau Putih 🐯", "Naga Emas 🐉", "Kucing Oren Barbar 🐱", "Kulkas 2 Pintu 🧊",
		"Sapu Lidi Terbang 🧹", "Tutup Panci Sakti 🍳", "Kura-Kura Ninja 🐢", "Burung Garuda 🦅",
		"Pecel Lele Mas Eko 🍽️", "Kipas Angin Miyako 🌀", "Sigit Rendang 🍛", "Tuyul Pensiunan 👻",
		"Sandal Swallow Putus 🩴", "Biawak Jawa 🦎", "Macan Cisewu 🦁", "Elang Hitam 🦅",
		"Ayam Geprek Level 10 🌶️", "Serigala Bulan Purnama 🐺", "Gajah Terbang 🐘", "Laba-Laba Sunda 🕷️",
	}
	idx := int(stableSeed("khodam:"+name+time.Now().Format("2006-01-02")) % uint64(len(khodams)))
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔮 *Cek Khodam*\n• Nama: *%s*\n• Khodam: *%s*", name, khodams[idx]))
}

func cmdJadian(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil || gi == nil || len(gi.Participants) < 2 {
		utils.SendReply(client, msg.Info.Chat, "❌ Need at least 2 members in the group.")
		return
	}
	i := rand.Intn(len(gi.Participants))
	j := rand.Intn(len(gi.Participants) - 1)
	if j >= i {
		j++
	}
	p1 := gi.Participants[i].JID.ToNonAD()
	p2 := gi.Participants[j].JID.ToNonAD()
	pct := 50 + rand.Intn(51)
	utils.SendMentionReply(client, msg.Info.Chat,
		fmt.Sprintf("💘 *Ciee Jadian!*\n@%s ❤️ @%s\nKecocokan: *%d%%* — langgeng terus ya! 🎉", p1.User, p2.User, pct),
		[]string{p1.String(), p2.String()},
	)
}

func cmdRandomNumber(client *whatsmeow.Client, msg *events.Message, args []string) {
	minVal, maxVal := 1, 100
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 1 {
		if v, err := strconv.Atoi(fields[0]); err == nil && v > 1 {
			maxVal = v
		}
	} else if len(fields) >= 2 {
		a, e1 := strconv.Atoi(fields[0])
		b, e2 := strconv.Atoi(fields[1])
		if e1 == nil && e2 == nil {
			if a > b {
				a, b = b, a
			}
			minVal, maxVal = a, b
		}
	}
	if maxVal <= minVal {
		maxVal = minVal + 1
	}
	n := minVal + rand.Intn(maxVal-minVal+1)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎲 Random number (%d–%d): *%d*", minVal, maxVal, n))
}

func cmdRandomTag(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	gi, err := client.GetGroupInfo(ctx, msg.Info.Chat)
	if err != nil || gi == nil || len(gi.Participants) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not load group members.")
		return
	}
	p := gi.Participants[rand.Intn(len(gi.Participants))].JID.ToNonAD()
	note := strings.TrimSpace(strings.Join(args, " "))
	if note == "" {
		note = "Kamu terpilih secara acak! 🎯"
	}
	utils.SendMentionReply(client, msg.Info.Chat, fmt.Sprintf("👉 @%s — %s", p.User, note), []string{p.String()})
}

func cmdDirectTruth(client *whatsmeow.Client, msg *events.Message, args []string) {
	cmdTruthOrDare(client, msg, []string{"truth"})
}

func cmdDirectDare(client *whatsmeow.Client, msg *events.Message, args []string) {
	cmdTruthOrDare(client, msg, []string{"dare"})
}

var pantunBank = []string{
	"Jalan-jalan ke kota Blitar,\nJangan lupa membeli sukun.\nKalau ingin menjadi pintar,\nBelajarlah dengan tekun.",
	"Beli mangga di pasar baru,\nPulangnya naik perahu.\nJangan sedih wahai kawanku,\nHari esok selalu baru.",
	"Pergi memancing di pinggir kali,\nDapat ikan besar sekali.\nWalau tugas menumpuk tinggi,\nTetap semangat jalani hari!",
	"Ada kucing mengejar tikus,\nTikus lari masuk ke gua.\nSenyum manis hati pun tulus,\nRezeki lancar sepanjang masa.",
	"Makan siang pakai sambal terasi,\nMinumnya es teh manis dingin.\nKalau sudah punya ambisi,\nKejar terus jangan pernah ingin mundur lagi!",
}

func cmdPantun(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "📜 *Pantun:*\n\n"+pantunBank[rand.Intn(len(pantunBank))])
}

var kataBijakBank = []string{
	"Kesuksesan dimulai dari keberanian untuk mencoba, meski langkah pertama terasa kecil.",
	"Jangan bandingkan prosesmu dengan hasil orang lain. Setiap orang punya waktunya masing-masing.",
	"Kegagalan bukan akhir dari segalanya, melainkan guru terbaik menuju versi dirimu yang lebih kuat.",
	"Waktu terbaik untuk memulai adalah kemarin. Waktu terbaik kedua adalah hari ini.",
	"Disiplin kecil yang dilakukan setiap hari akan mengalahkan bakat besar yang jarang dilatih.",
	"Tenang bukan berarti tidak peduli, melainkan tahu mana hal yang layak diperjuangkan.",
}

func cmdKataBijak(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "✨ *Kata Bijak:*\n\n_\""+kataBijakBank[rand.Intn(len(kataBijakBank))]+"\"_")
}

var puisiBank = []string{
	"*Senja di Ujung Hari*\n\nLangit jingga perlahan meredup,\nMenyimpan lelah di balik awan.\nNamun esok fajar pasti hidup,\nMembawa harapan yang tak pernah padam.",
	"*Langkah Kecil*\n\nBukan seberapa cepat kau berlari,\nMelainkan seberapa kuat kau bertahan.\nDi setiap hujan yang turun sunyi,\nSelalu tumbuh bunga di tepi jalan.",
	"*Malam dan Bintang*\n\nKetika gelap menyelimuti bumi,\nBintang kecil tetap setia bersinar.\nBegitu pula mimpimu di dalam hati,\nTeruslah menyala, jangan biarkan pudar.",
}

func cmdPuisi(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "🖋️ "+puisiBank[rand.Intn(len(puisiBank))])
}

func cmdMeme(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	body, _, err := utils.HTTPGetBytes(ctx, "https://meme-api.com/gimme", nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch meme right now: "+err.Error())
		return
	}
	var res struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		Subreddit string `json:"subreddit"`
		NSFW      bool   `json:"nsfw"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.URL == "" || res.NSFW {
		utils.SendReply(client, msg.Info.Chat, "⚠️ Skipped NSFW or empty meme — try `.meme` again!")
		return
	}
	imgBytes, _, err := utils.HTTPGetBytes(ctx, res.URL, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed downloading meme image.")
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, imgBytes, "image/jpeg", fmt.Sprintf("😂 *%s*\n_r/%s_", res.Title, res.Subreddit))
}

func cmdPPCP(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	endpoints := []string{
		"https://api.waifu.pics/sfw/waifu",
		"https://api.waifu.pics/sfw/neko",
	}
	labels := []string{"💑 *Couple PFP (1/2)*", "💑 *Couple PFP (2/2)*"}
	for i, ep := range endpoints {
		body, _, err := utils.HTTPGetBytes(ctx, ep, nil)
		if err != nil {
			continue
		}
		var res struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(body, &res) == nil && res.URL != "" {
			if img, _, err := utils.HTTPGetBytes(ctx, res.URL, nil); err == nil {
				_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, "image/jpeg", labels[i])
			}
		}
	}
}

// ==================== RAMALAN / DIVINATION ====================

func cmdZodiak(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "♈ Usage: `.zodiak <aries|taurus|gemini|...|DD-MM>`")
		return
	}
	signs := map[string]struct{ name, dates, element, trait string }{
		"aries":       {"♈ Aries", "21 Mar – 19 Apr", "Api / Fire", "Berani, penuh energi, dan suka tantangan baru."},
		"taurus":      {"♉ Taurus", "20 Apr – 20 Mei", "Tanah / Earth", "Setia, sabar, dan menghargai kenyamanan."},
		"gemini":      {"♊ Gemini", "21 Mei – 20 Jun", "Udara / Air", "Cerdas, komunikatif, dan mudah beradaptasi."},
		"cancer":      {"♋ Cancer", "21 Jun – 22 Jul", "Air / Water", "Peka, penyayang, dan sangat peduli keluarga."},
		"leo":         {"♌ Leo", "23 Jul – 22 Agu", "Api / Fire", "Karismatik, percaya diri, dan berjiwa pemimpin."},
		"virgo":       {"♍ Virgo", "23 Agu – 22 Sep", "Tanah / Earth", "Teliti, perfeksionis, dan analitis."},
		"libra":       {"♎ Libra", "23 Sep – 22 Okt", "Udara / Air", "Adil, diplomatis, dan menyukai keharmonisan."},
		"scorpio":     {"♏ Scorpio", "23 Okt – 21 Nov", "Air / Water", "Misterius, fokus, dan penuh tekad."},
		"sagittarius": {"♐ Sagittarius", "22 Nov – 21 Des", "Api / Fire", "Optimis, jujur, dan berjiwa petualang."},
		"sagitarius":  {"♐ Sagittarius", "22 Nov – 21 Des", "Api / Fire", "Optimis, jujur, dan berjiwa petualang."},
		"capricorn":   {"♑ Capricorn", "22 Des – 19 Jan", "Tanah / Earth", "Disiplin, ambisius, dan pekerja keras."},
		"aquarius":    {"♒ Aquarius", "20 Jan – 18 Feb", "Udara / Air", "Kreatif, independen, dan berpikiran maju."},
		"pisces":      {"♓ Pisces", "19 Feb – 20 Mar", "Air / Water", "Imajinatif, empatik, dan berhati lembut."},
	}
	if parts := strings.Split(strings.ReplaceAll(q, "/", "-"), "-"); len(parts) == 2 {
		d, e1 := strconv.Atoi(parts[0])
		m, e2 := strconv.Atoi(parts[1])
		if e1 == nil && e2 == nil {
			switch {
			case (m == 3 && d >= 21) || (m == 4 && d <= 19):
				q = "aries"
			case (m == 4 && d >= 20) || (m == 5 && d <= 20):
				q = "taurus"
			case (m == 5 && d >= 21) || (m == 6 && d <= 20):
				q = "gemini"
			case (m == 6 && d >= 21) || (m == 7 && d <= 22):
				q = "cancer"
			case (m == 7 && d >= 23) || (m == 8 && d <= 22):
				q = "leo"
			case (m == 8 && d >= 23) || (m == 9 && d <= 22):
				q = "virgo"
			case (m == 9 && d >= 23) || (m == 10 && d <= 22):
				q = "libra"
			case (m == 10 && d >= 23) || (m == 11 && d <= 21):
				q = "scorpio"
			case (m == 11 && d >= 22) || (m == 12 && d <= 21):
				q = "sagittarius"
			case (m == 12 && d >= 22) || (m == 1 && d <= 19):
				q = "capricorn"
			case (m == 1 && d >= 20) || (m == 2 && d <= 18):
				q = "aquarius"
			default:
				q = "pisces"
			}
		}
	}
	info, ok := signs[q]
	if !ok {
		utils.SendReply(client, msg.Info.Chat, "❌ Zodiak tidak dikenali. Contoh: `.zodiak leo` atau `.zodiak 17-08`")
		return
	}
	luck := 60 + int(stableSeed("zodiak:"+q+time.Now().Format("2006-01-02"))%41)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"*%s* (%s)\n• *Elemen:* %s\n• *Karakter:* %s\n• *Hoki Hari Ini:* `%d%%` ✨",
		info.name, info.dates, info.element, info.trait, luck,
	))
}

func cmdShio(client *whatsmeow.Client, msg *events.Message, args []string) {
	year, err := strconv.Atoi(strings.TrimSpace(strings.Join(args, "")))
	if err != nil || year < 1900 || year > 2100 {
		utils.SendReply(client, msg.Info.Chat, "🐉 Usage: `.shio <tahun_lahir>` (contoh: `.shio 2002`)")
		return
	}
	shios := []string{
		"🐀 Tikus (Rat)", "🐂 Kerbau (Ox)", "🐅 Macan (Tiger)", "🐇 Kelinci (Rabbit)",
		"🐉 Naga (Dragon)", "🐍 Ular (Snake)", "🐎 Kuda (Horse)", "🐐 Kambing (Goat)",
		"🐒 Monyet (Monkey)", "🐓 Ayam (Rooster)", "🐕 Anjing (Dog)", "🐖 Babi (Pig)",
	}
	elements := []string{"Logam (Metal)", "Logam (Metal)", "Air (Water)", "Air (Water)", "Kayu (Wood)", "Kayu (Wood)", "Api (Fire)", "Api (Fire)", "Tanah (Earth)", "Tanah (Earth)"}
	idx := (year - 1900) % 12
	if idx < 0 {
		idx += 12
	}
	el := elements[year%10]
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏮 *Shio Tahun %d*\n• *Shio:* %s\n• *Elemen:* %s", year, shios[idx], el))
}

func cmdWeton(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, ""))
	dt, err := time.Parse("2006-01-02", raw)
	if err != nil {
		dt, err = time.Parse("02-01-2006", raw)
	}
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "📅 Usage: `.weton YYYY-MM-DD` atau `.weton DD-MM-YYYY` (contoh: `.weton 2002-08-17`)")
		return
	}
	hariNames := []string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	hariNeptu := []int{5, 4, 3, 7, 8, 6, 9}
	pasaranNames := []string{"Legi", "Pahing", "Pon", "Wage", "Kliwon"}
	pasaranNeptu := []int{5, 9, 7, 4, 8}

	// Reference date: 1970-01-01 was Kamis Wage (pasaran index 3)
	days := int(dt.UTC().Unix() / 86400)
	pIdx := ((days+3)%5 + 5) % 5
	wDay := int(dt.Weekday())
	neptu := hariNeptu[wDay] + pasaranNeptu[pIdx]
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🗓️ *Perhitungan Weton Jawa*\n• *Tanggal:* %s\n• *Weton:* *%s %s*\n• *Neptu:* `%d` (%s %d + %s %d)",
		dt.Format("02 Jan 2006"), hariNames[wDay], pasaranNames[pIdx], neptu,
		hariNames[wDay], hariNeptu[wDay], pasaranNames[pIdx], pasaranNeptu[pIdx],
	))
}

func cmdArtiNama(client *whatsmeow.Client, msg *events.Message, args []string) {
	name := strings.TrimSpace(strings.Join(args, " "))
	if name == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.artinama <nama>`")
		return
	}
	traits := []string{
		"memiliki jiwa kepemimpinan yang kuat, setia kawan, dan penuh rasa ingin tahu.",
		"kreatif, hangat terhadap orang sekitar, dan selalu membawa suasana positif.",
		"cerdas dalam mengambil keputusan, sabar menghadapi tekanan, dan berhati lembut.",
		"mandiri, pekerja keras, dan memiliki pesona alami yang disukai banyak orang.",
		"penuh imajinasi, jujur, serta selalu berusaha memberi yang terbaik bagi keluarga.",
	}
	t := traits[int(stableSeed("artinama:"+name)%uint64(len(traits)))]
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✨ *Arti Nama: %s*\nNama ini menggambarkan sosok yang %s", name, t))
}

func cmdKecocokanNama(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.Split(strings.Join(args, " "), "|")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.kecocokannama Nama1 | Nama2`")
		return
	}
	a, b := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	pct := 30 + int(stableSeed("cocok:"+a+":"+b)%71)
	bar := strings.Repeat("❤️", pct/20) + strings.Repeat("🤍", 5-pct/20)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💞 *Kecocokan Nama*\n*%s* + *%s*\n%s *%d%%*", a, b, bar, pct))
}

func cmdRamalanJodoh(client *whatsmeow.Client, msg *events.Message, args []string) {
	parts := strings.Split(strings.Join(args, " "), "|")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.ramalanjodoh Nama1 | Nama2`")
		return
	}
	a, b := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	pct := 40 + int(stableSeed("jodoh:"+a+":"+b)%61)
	note := "Komunikasi yang baik akan membuat hubungan makin erat! 💫"
	if pct >= 85 {
		note = "Sangat serasi! Jodoh idaman banget ini! 💍"
	} else if pct < 60 {
		note = "Butuh saling pengertian lebih, tapi tetap bisa langgeng! 🌱"
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔮 *Ramalan Jodoh*\n*%s* ❤️ *%s*\n• *Kecocokan:* `%d%%`\n• *Ramalan:* %s", a, b, pct, note))
}

func cmdTafsirMimpi(client *whatsmeow.Client, msg *events.Message, args []string) {
	dream := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if dream == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.tafsirmimpi <kata kunci>` (contoh: `.tafsirmimpi ular`)")
		return
	}
	known := map[string]string{
		"ular":    "Sering dikaitkan dengan datangnya rezeki tak terduga atau sinyal agar lebih waspada terhadap lingkungan sekitar.",
		"uang":    "Melambangkan keinginan akan kestabilan finansial atau peluang usaha baru yang akan segera datang.",
		"air":     "Air jernih menandakan ketenangan batin dan keberkahan; air keruh mengajakmu untuk lebih menenangkan pikiran.",
		"terbang": "Simbol kebebasan, ambisi tinggi, dan keberhasilan melewati rintangan besar.",
		"jatuh":   "Menggambarkan rasa cemas akan kehilangan kendali — tandanya perlu istirahat dan menata ulang prioritas.",
		"gigi":    "Sering dimaknai sebagai tanda perubahan fase hidup atau kekhawatiran terhadap orang terdekat.",
		"hujan":   "Pertanda berkah, pembaruan energi, dan datangnya kabar baik.",
	}
	for k, v := range known {
		if strings.Contains(dream, k) {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🌙 *Tafsir Mimpi: %s*\n%s", dream, v))
			return
		}
	}
	generic := []string{
		"Menandakan akan datangnya perubahan positif dan peluang baru dalam waktu dekat.",
		"Mencerminkan pikiran bawah sadar yang sedang mencari ketenangan dan arah baru.",
		"Pertanda baik: usaha yang sedang kamu jalani perlahan mulai menunjukkan hasil.",
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🌙 *Tafsir Mimpi: %s*\n%s", dream, generic[int(stableSeed("mimpi:"+dream)%uint64(len(generic)))]))
}

func cmdNoHoki(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := utils.NormalizeTarget(strings.Join(args, ""))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.nohoki <nomor>`")
		return
	}
	sum := 0
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			sum += int(r - '0')
		}
	}
	score := 50 + ((sum * 7) % 51)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🍀 *Cek Nomor Hoki*\n• *Nomor:* `%s`\n• *Skor Hoki:* `%d%%`\n• *Angka Keberuntungan:* `%d`", raw, score, (sum%9)+1))
}
