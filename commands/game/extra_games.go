package gamecmd

import (
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

var (
	tebakEmojiBank = []riddle{
		{Question: "🦁 👑", Answer: "lion king", Hint: "Film Disney tentang singa"},
		{Question: "🕷️ 🧑", Answer: "spiderman", Hint: "Superhero jaring laba-laba"},
		{Question: "❄️ 👸", Answer: "frozen", Hint: "Elsa & Anna"},
		{Question: "⚡ 👓 🧙", Answer: "harry potter", Hint: "Penyihir Hogwarts"},
		{Question: "🍍 🏠 🧽", Answer: "spongebob", Hint: "Tinggal di nanas bawah laut"},
		{Question: "🦇 🧑", Answer: "batman", Hint: "Pahlawan kota Gotham"},
		{Question: "🐉 ⚽", Answer: "dragon ball", Hint: "Anime Goku"},
		{Question: "🏴‍☠️ 👒", Answer: "one piece", Hint: "Luffy topi jerami"},
		{Question: "🍜 🦊 🍥", Answer: "naruto", Hint: "Ninja Konoha"},
		{Question: "👻 🚫", Answer: "ghostbusters", Hint: "Pemburu hantu"},
	}

	tebakLogoBank = []riddle{
		{Question: "🍎 Buah apel tergigit berwarna perak/putih", Answer: "apple", Hint: "Pembuat iPhone"},
		{Question: "✔️ Tanda centang sederhana (Swoosh)", Answer: "nike", Hint: "Just Do It"},
		{Question: "🟢 Lingkaran hijau dengan 3 garis gelombang suara hitam", Answer: "spotify", Hint: "Aplikasi streaming musik"},
		{Question: "▶️ Persegi panjang merah dengan segitiga putih di tengah", Answer: "youtube", Hint: "Platform video terbesar"},
		{Question: "💬 Gelembung chat hijau dengan ikon gagang telepon putih", Answer: "whatsapp", Hint: "Aplikasi chat ini"},
		{Question: "🎮 Empat kotak warna-warni membentuk jendela", Answer: "microsoft", Hint: "Pembuat Windows"},
		{Question: "🟡 Huruf M lengkung kuning di latar merah", Answer: "mcdonalds", Hint: "Restoran burger cepat saji"},
		{Question: "☕ Putri duyung berekor dua dalam lingkaran hijau", Answer: "starbucks", Hint: "Kedai kopi global"},
		{Question: "🚗 Empat cincin perak yang saling bertautan", Answer: "audi", Hint: "Merek mobil Jerman"},
		{Question: "🎵 Not balok neon biru-merah di latar hitam", Answer: "tiktok", Hint: "Platform video pendek"},
	}

	tekaTekiBank = []riddle{
		{Question: "Aku selalu datang tapi tidak pernah tiba hari ini. Siapakah aku?", Answer: "besok", Hint: "Waktu setelah hari ini"},
		{Question: "Semakin banyak kamu mengambilnya, semakin banyak yang kamu tinggalkan. Apa itu?", Answer: "langkah", Hint: "Saat berjalan kaki"},
		{Question: "Punya kota tapi tak punya rumah, punya sungai tapi tak punya air. Apa itu?", Answer: "peta", Hint: "Penunjuk arah geografis"},
		{Question: "Selalu basah saat mengeringkan sesuatu. Benda apakah aku?", Answer: "handuk", Hint: "Dipakai setelah mandi"},
		{Question: "Punya leher tapi tidak punya kepala. Benda apakah aku?", Answer: "botol", Hint: "Wadah minuman"},
		{Question: "Bisa pecah hanya dengan diucapkan namanya. Apa itu?", Answer: "keheningan", Hint: "Kebalikan dari kebisingan"},
		{Question: "Punya banyak kunci tapi tak bisa membuka satu pintu pun. Apa itu?", Answer: "piano", Hint: "Alat musik tuts"},
		{Question: "Makin dipotong makin tinggi. Benda apakah itu?", Answer: "celana", Hint: "Pakaian bawahan"},
	}

	susunKalimatBank = []riddle{
		{Question: "pergi / pagi / ke / sekolah / saya / setiap", Answer: "saya pergi ke sekolah setiap pagi", Hint: "Dimulai dengan 'saya pergi...'"},
		{Question: "kopi / hujan / nikmat / saat / minum / sangat", Answer: "minum kopi sangat nikmat saat hujan", Hint: "Dimulai dengan 'minum kopi...'"},
		{Question: "coding / malam / programmer / suka / hari / di", Answer: "programmer suka coding di malam hari", Hint: "Dimulai dengan 'programmer...'"},
		{Question: "tidur / kucing / di / sofa / atas / sedang", Answer: "kucing sedang tidur di atas sofa", Hint: "Dimulai dengan 'kucing sedang...'"},
		{Question: "teman / bermain / seru / game / bersama / sangat", Answer: "bermain game bersama teman sangat seru", Hint: "Dimulai dengan 'bermain game...'"},
	}

	family100Bank = []struct {
		Question string
		Answers  []string
	}{
		{
			Question: "Benda yang wajib dibawa saat bepergian keluar rumah?",
			Answers:  []string{"hp", "dompet", "kunci", "uang", "powerbank"},
		},
		{
			Question: "Makanan khas Indonesia yang populer di mana-mana?",
			Answers:  []string{"nasi goreng", "bakso", "sate", "rendang", "mie ayam"},
		},
		{
			Question: "Hal yang sering dilakukan orang saat bangun tidur?",
			Answers:  []string{"cek hp", "minum air", "mandi", "cuci muka", "sikat gigi"},
		},
		{
			Question: "Sesuatu yang sering bikin orang begadang?",
			Answers:  []string{"main game", "nonton film", "tugas", "kerja", "scroll sosmed"},
		},
		{
			Question: "Minuman favorit saat cuaca panas?",
			Answers:  []string{"es teh", "es jeruk", "air dingin", "es kelapa", "kopi es"},
		},
	}
)

func init() {
	cmds := []types.Command{
		{Name: "family100", Category: "games", Description: "Play Family 100 survey guessing game!", Execute: cmdFamily100},
		{Name: "tebakemoji", Category: "games", Description: "Guess the movie/character from emoji clues.", Execute: cmdRiddleGame("tebakemoji", tebakEmojiBank, 25)},
		{Name: "tebaklogo", Category: "games", Description: "Guess the famous brand from its logo description.", Execute: cmdRiddleGame("tebaklogo", tebakLogoBank, 25)},
		{Name: "tekateki", Category: "games", Description: "Solve a classic logic riddle (Teka-Teki).", Execute: cmdRiddleGame("tekateki", tekaTekiBank, 30)},
		{Name: "susunkalimat", Category: "games", Description: "Rearrange shuffled words into a proper sentence.", Execute: cmdRiddleGame("susunkalimat", susunKalimatBank, 30)},
		{Name: "hint", Category: "games", Description: "Get a hint for the active game in this chat.", Execute: cmdGameHint},
		{Name: "nyerah", Category: "games", Description: "Give up on the active game and reveal the answer.", Execute: cmdGameGiveUp},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdFamily100(client *whatsmeow.Client, msg *events.Message, args []string) {
	item := family100Bank[rand.Intn(len(family100Bank))]
	startSession(msg, &activeGame{
		Kind:      "family100",
		Answer:    strings.ToLower(item.Answers[0]),
		Hint:      fmt.Sprintf("Jawaban teratas diawali '%c' (%d huruf)", item.Answers[0][0], len(item.Answers[0])),
		Points:    35,
		ExpiresAt: time.Now().Add(90 * time.Second),
	})
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🎯 *FAMILY 100* (+35 pts)\n\n*Pertanyaan:* %s\n*Jawaban #1 Survey:* %d huruf (diawali `%c`)\n\n_Balas langsung jawaban teratas survey, atau ketik `.hint` / `.nyerah` (90 detik)._",
		item.Question, len(item.Answers[0]), item.Answers[0][0],
	))
}

func cmdGameHint(client *whatsmeow.Client, msg *events.Message, args []string) {
	gamesMu.Lock()
	g, ok := activeGames[chatKey(msg)]
	gamesMu.Unlock()
	if !ok || g == nil || time.Now().After(g.ExpiresAt) {
		utils.SendReply(client, msg.Info.Chat, "ℹ️ Tidak ada game aktif di chat ini.")
		return
	}
	if g.Hint == "" {
		utils.SendReply(client, msg.Info.Chat, "💡 Hint tidak tersedia untuk game ini.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, "💡 *Hint:* "+g.Hint)
}

func cmdGameGiveUp(client *whatsmeow.Client, msg *events.Message, args []string) {
	gamesMu.Lock()
	g, ok := activeGames[chatKey(msg)]
	if ok {
		delete(activeGames, chatKey(msg))
	}
	gamesMu.Unlock()
	if !ok || g == nil {
		utils.SendReply(client, msg.Info.Chat, "ℹ️ Tidak ada game aktif di chat ini.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏳️ Menyerah! Jawaban yang benar: *%s*", g.Answer))
}
