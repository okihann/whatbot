package gamecmd

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/assets"
	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

type riddle struct {
	Question string
	Answer   string
	Hint     string
}

type activeGame struct {
	Kind      string
	Answer    string
	Hint      string
	Points    int64
	ExpiresAt time.Time
	// Specialized states
	WordleTarget  string
	WordleGuesses []string
	TTTBoard      [9]string
	TTTP1         string
	TTTP2         string
	TTTTurn       string
	BombPos       int
	BombOpened    [9]bool
	SnakesPos     map[string]int
	ChessBoard    [8][8]rune
	ChessTurn     string // "w" or "b"
	SambungLast   string
	SambungUsed   map[string]bool
}

var (
	gamesMu     sync.Mutex
	activeGames = map[string]*activeGame{}
)

var (
	bankAsahOtak = []riddle{
		{"I speak without a mouth and hear without ears. What am I?", "echo", "You hear it in a canyon"},
		{"The more you take, the more you leave behind. What are they?", "footsteps", "Walking"},
		{"What has keys that open no locks?", "piano", "Musical instrument"},
		{"What gets wetter as it dries?", "towel", "Bathroom item"},
		{"Apa yang selalu naik tapi tidak pernah turun?", "umur", "Bertambah setiap ulang tahun"},
	}
	bankCakLontong = []riddle{
		{"Kucing apa yang romantis?", "kucingta", "Kucing + cinta"},
		{"Hewan apa yang paling kaya?", "beruang", "Ber-uang"},
		{"Orang sakit biasanya minum...", "obat", "Dari apotek"},
		{"Matahari tenggelam di sebelah...", "gawat", "Kalau tenggelam beneran kan..."},
		{"Kalau ada kebakaran, yang pertama datang adalah...", "api", "Sebelum pemadam"},
	}
	bankSiapakahAku = []riddle{
		{"Aku punya jarum tapi tidak bisa menjahit. Siapakah aku?", "jam", "Penunjuk waktu"},
		{"Aku berwarna putih, dingin, dan sering ada di kulkas. Siapakah aku?", "es", "Air beku"},
		{"Aku punya layar dan keyboard, sering dipakai ngoding. Siapakah aku?", "laptop", "Komputer portabel"},
		{"Aku punya sayap tapi bukan burung, membawa penumpang di langit. Siapakah aku?", "pesawat", "Terbang di bandara"},
	}
	bankTebakBendera = []riddle{
		{"🇮🇩 Bendera negara apakah ini?", "indonesia", "Ibu kota Jakarta"},
		{"🇯🇵 Bendera negara apakah ini?", "jepang", "Negeri Sakura"},
		{"🇰🇷 Bendera negara apakah ini?", "korea selatan", "Ibu kota Seoul"},
		{"🇧🇷 Bendera negara apakah ini?", "brazil", "Amerika Selatan"},
		{"🇫🇷 Bendera negara apakah ini?", "prancis", "Menara Eiffel"},
		{"🇩🇪 Bendera negara apakah ini?", "jerman", "Ibu kota Berlin"},
		{"🇬🇧 Bendera negara apakah ini?", "inggris", "Big Ben"},
	}
	bankTebakIbukota = []riddle{
		{"Apa ibu kota Jepang?", "tokyo", "Kota terbesar di Jepang"},
		{"Apa ibu kota Thailand?", "bangkok", "Krung Thep"},
		{"Apa ibu kota Australia?", "canberra", "Bukan Sydney"},
		{"Apa ibu kota Turki?", "ankara", "Bukan Istanbul"},
		{"Apa ibu kota Kanada?", "ottawa", "Bukan Toronto"},
		{"Apa ibu kota Korea Selatan?", "seoul", "K-Pop"},
	}
	bankTebakLirik = []riddle{
		{"Lanjutkan lirik: 'Twinkle twinkle little ___'", "star", "Bintang"},
		{"Lanjutkan lirik: 'Balonku ada lima, rupa-rupa ___'", "warnanya", "Warna"},
		{"Lanjutkan lirik: 'Never gonna give you ___'", "up", "Rick Astley"},
		{"Lanjutkan lirik: 'Bintang kecil di langit yang ___'", "biru", "Warna langit"},
	}
	bankTebakGambar = []riddle{
		{"🔥 + 🦊 = ?", "firefox", "Browser"},
		{"⭐ + ☕ = ?", "starbucks", "Coffee shop"},
		{"🌧️ + 🎀 = ?", "rainbow", "Pelangi"},
		{"☀️ + 🌻 = ?", "sunflower", "Bunga matahari"},
		{"🧈 + 🪰 = ?", "butterfly", "Kupu-kupu"},
	}
	wordleWords = []string{"APPLE", "BRAIN", "CRANE", "FLAME", "GRAPE", "HOUSE", "LIGHT", "MUSIC", "OCEAN", "PLANT", "RIVER", "SMILE", "STONE", "TIGER", "WATER", "WORLD"}
	kataWords   = []string{"KOMPUTER", "INDONESIA", "PROGRAM", "WHATSAPP", "ALGORITMA", "MATAHARI", "PELANGI", "KERAJAAN", "SEKOLAH", "TELEPON"}
)

func init() {
	handlers.RegisterInteractiveSessionHandler(handleActiveGameReply)

	cmds := []types.Command{
		// Trivia & word games
		{Name: "asahotak", Category: "games", Description: "Play Asah Otak riddle game.", Execute: cmdRiddleGame("asahotak", bankAsahOtak, 25)},
		{Name: "caklontong", Category: "games", Description: "Play Cak Lontong pun trivia.", Execute: cmdRiddleGame("caklontong", bankCakLontong, 25)},
		{Name: "siapakahaku", Category: "games", Description: "Play Siapakah Aku guessing game.", Execute: cmdRiddleGame("siapakahaku", bankSiapakahAku, 25)},
		{Name: "tebakbendera", Category: "games", Description: "Guess the country from its flag.", Execute: cmdRiddleGame("tebakbendera", bankTebakBendera, 20)},
		{Name: "tebakibukota", Category: "games", Description: "Guess the capital city.", Execute: cmdRiddleGame("tebakibukota", bankTebakIbukota, 20)},
		{Name: "tebaklirik", Category: "games", Description: "Complete the song lyrics.", Execute: cmdRiddleGame("tebaklirik", bankTebakLirik, 20)},
		{Name: "tebakgambar", Category: "games", Description: "Guess the word from emoji rebus clues.", Execute: cmdRiddleGame("tebakgambar", bankTebakGambar, 25)},
		{Name: "tebakkata", Aliases: []string{"susunkata"}, Category: "games", Description: "Unscramble the shuffled word.", Execute: cmdSusunKata},
		{Name: "sambungkata", Category: "games", Description: "Play word chain (Sambung Kata) in chat.", Execute: cmdSambungKata},
		{Name: "math", Category: "games", Description: "Timed mental math challenge.", Execute: cmdMathGame},
		{Name: "wordle", Category: "games", Description: "Play 5-letter Wordle in WhatsApp chat.", Execute: cmdWordle},

		// Board & party games
		{Name: "tebakbom", Category: "games", Description: "Avoid the hidden bomb on a 3x3 grid!", Execute: cmdTebakBom},
		{Name: "tictactoe", Aliases: []string{"ttt"}, Category: "games", Description: "Play Tic-Tac-Toe vs Bot (or PvP). Reply 1-9 to move.", Execute: cmdTicTacToe},
		{Name: "catur", Aliases: []string{"chess"}, Category: "games", Description: "Play Chess with rendered PNG board! Move using e2e4 format.", Execute: cmdChess},
		{Name: "ulartangga", Category: "games", Description: "Play Snakes & Ladders! Type `roll` to move.", Execute: cmdUlarTangga},
		{Name: "russianroulette", Category: "games", Description: "1-in-6 chance Russian Roulette!", Execute: cmdRussianRoulette},

		// Stats, Badges & Leaderboards
		{Name: "gamestats", Aliases: []string{"profilegame"}, Category: "games", Description: "View your game wins, points, and win streaks.", Execute: cmdGameStats},
		{Name: "leaderboard", Category: "games", Description: "View this group's (or global) game leaderboard.", Execute: cmdLeaderboard(false)},
		{Name: "globalleaderboard", Category: "games", Description: "View global game leaderboard across all chats.", Execute: cmdLeaderboard(true)},
		{Name: "badges", Aliases: []string{"groupachievements"}, Category: "games", Description: "View unlocked game badges and achievements.", Execute: cmdBadges},
		{Name: "groupstats", Aliases: []string{"groupmvp", "groupcarry", "groupcontrib", "groupcompare"}, Category: "games", Description: "View group MVP, top contributor, and group game comparison.", Execute: cmdGroupGameStats},
		{Name: "rival", Category: "games", Description: "Compare your game score against a mentioned rival.", Execute: cmdRival},
		{Name: "gamelimit", Category: "games", Description: "Show game session limits (unlimited on self-hosted NopeBot).", Execute: cmdGameLimit},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func chatKey(msg *events.Message) string {
	return msg.Info.Chat.ToNonAD().String()
}

func startSession(msg *events.Message, g *activeGame) {
	gamesMu.Lock()
	activeGames[chatKey(msg)] = g
	gamesMu.Unlock()
}

func endSession(msg *events.Message) {
	gamesMu.Lock()
	delete(activeGames, chatKey(msg))
	gamesMu.Unlock()
}

func recordWin(client *whatsmeow.Client, msg *events.Message, game string, pts int64) {
	store := db.GetBotStore()
	if store == nil {
		return
	}
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	gid := ""
	if msg.Info.IsGroup {
		gid = msg.Info.Chat.ToNonAD().String()
	}
	_ = store.RecordGameResult(uid, gid, game, true, false, pts)
	_, _ = store.AddBalance(uid, pts, "Game win: "+game)
}

func cmdRiddleGame(kind string, bank []riddle, pts int64) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		q := bank[rand.Intn(len(bank))]
		startSession(msg, &activeGame{
			Kind:      kind,
			Answer:    strings.ToLower(q.Answer),
			Hint:      q.Hint,
			Points:    pts,
			ExpiresAt: time.Now().Add(90 * time.Second),
		})
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎮 *%s* (+%d pts)\n\n%s\n\n_Reply directly with your answer, `hint`, or `nyerah` (90s)._", handlers.Title(kind), pts, q.Question))
	}
}

func cmdSusunKata(client *whatsmeow.Client, msg *events.Message, args []string) {
	word := kataWords[rand.Intn(len(kataWords))]
	runes := []rune(word)
	rand.Shuffle(len(runes), func(i, j int) { runes[i], runes[j] = runes[j], runes[i] })
	scrambled := string(runes)
	startSession(msg, &activeGame{
		Kind:      "susunkata",
		Answer:    strings.ToLower(word),
		Hint:      fmt.Sprintf("Starts with '%c' (%d letters)", word[0], len(word)),
		Points:    25,
		ExpiresAt: time.Now().Add(90 * time.Second),
	})
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔤 *Susun Kata* (+25 pts)\n\nUnscramble: *%s*\n\n_Reply with the word, `hint`, or `nyerah`._", scrambled))
}

func cmdSambungKata(client *whatsmeow.Client, msg *events.Message, args []string) {
	starters := []string{"apple", "tiger", "rumah", "hujan", "naga", "awan"}
	startWord := starters[rand.Intn(len(starters))]
	startSession(msg, &activeGame{
		Kind:        "sambungkata",
		SambungLast: startWord,
		SambungUsed: map[string]bool{startWord: true},
		Points:      10,
		ExpiresAt:   time.Now().Add(2 * time.Minute),
	})
	lastChar := strings.ToUpper(string(startWord[len(startWord)-1]))
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔗 *Sambung Kata Started!*\nWord: *%s*\nSend any new word starting with *%s* (type `nyerah` to stop).", strings.ToUpper(startWord), lastChar))
}

func cmdMathGame(client *whatsmeow.Client, msg *events.Message, args []string) {
	a, b := rand.Intn(40)+10, rand.Intn(30)+5
	ops := []string{"+", "-", "*"}
	op := ops[rand.Intn(len(ops))]
	ans := 0
	switch op {
	case "+":
		ans = a + b
	case "-":
		ans = a - b
	case "*":
		a = rand.Intn(15) + 3
		b = rand.Intn(12) + 3
		ans = a * b
	}
	startSession(msg, &activeGame{
		Kind:      "math",
		Answer:    strconv.Itoa(ans),
		Hint:      fmt.Sprintf("Between %d and %d", ans-10, ans+10),
		Points:    20,
		ExpiresAt: time.Now().Add(45 * time.Second),
	})
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🧮 *Quick Math* (+20 pts)\nWhat is *%d %s %d*?\n_Reply with the number (45s)._", a, op, b))
}

// ScoreWordle compares guess against target and returns emoji squares.
func ScoreWordle(target, guess string) string {
	target = strings.ToUpper(target)
	guess = strings.ToUpper(guess)
	var out strings.Builder
	for i := 0; i < 5 && i < len(guess); i++ {
		if guess[i] == target[i] {
			out.WriteString("🟩")
		} else if strings.ContainsRune(target, rune(guess[i])) {
			out.WriteString("🟨")
		} else {
			out.WriteString("⬛")
		}
	}
	return out.String()
}

func cmdWordle(client *whatsmeow.Client, msg *events.Message, args []string) {
	target := wordleWords[rand.Intn(len(wordleWords))]
	startSession(msg, &activeGame{
		Kind:         "wordle",
		WordleTarget: target,
		Points:       40,
		ExpiresAt:    time.Now().Add(5 * time.Minute),
	})
	utils.SendReply(client, msg.Info.Chat, "🟩 *Wordle Started!*\nGuess the 5-letter English word (6 attempts).\nSend any 5-letter word directly, or `nyerah` to reveal.")
}

func cmdTebakBom(client *whatsmeow.Client, msg *events.Message, args []string) {
	startSession(msg, &activeGame{
		Kind:      "tebakbom",
		BombPos:   rand.Intn(9),
		Points:    15,
		ExpiresAt: time.Now().Add(3 * time.Minute),
	})
	utils.SendReply(client, msg.Info.Chat, "💣 *Tebak Bom!*\nPick a number *1-9* to open safe tiles without hitting the hidden bomb!\n1️⃣2️⃣3️⃣\n4️⃣5️⃣6️⃣\n7️⃣8️⃣9️⃣")
}

func renderTTTBoard(b [9]string) string {
	cell := func(i int) string {
		if b[i] == "" {
			return strconv.Itoa(i + 1)
		}
		return b[i]
	}
	return fmt.Sprintf("```\n %s | %s | %s \n---+---+---\n %s | %s | %s \n---+---+---\n %s | %s | %s \n```",
		cell(0), cell(1), cell(2), cell(3), cell(4), cell(5), cell(6), cell(7), cell(8))
}

// CheckTTTWinner returns "X", "O", "DRAW", or "".
func CheckTTTWinner(b [9]string) string {
	lines := [][3]int{{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, {0, 3, 6}, {1, 4, 7}, {2, 5, 8}, {0, 4, 8}, {2, 4, 6}}
	for _, l := range lines {
		if b[l[0]] != "" && b[l[0]] == b[l[1]] && b[l[1]] == b[l[2]] {
			return b[l[0]]
		}
	}
	full := true
	for _, c := range b {
		if c == "" {
			full = false
			break
		}
	}
	if full {
		return "DRAW"
	}
	return ""
}

func cmdTicTacToe(client *whatsmeow.Client, msg *events.Message, args []string) {
	g := &activeGame{
		Kind:      "tictactoe",
		Points:    30,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	startSession(msg, g)
	utils.SendReply(client, msg.Info.Chat, "❌⭕ *Tic-Tac-Toe vs Bot!*\nYou are *X*. Send a number *1-9* to place your mark:\n"+renderTTTBoard(g.TTTBoard))
}

func initChessBoard() [8][8]rune {
	return [8][8]rune{
		{'r', 'n', 'b', 'q', 'k', 'b', 'n', 'r'},
		{'p', 'p', 'p', 'p', 'p', 'p', 'p', 'p'},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{'P', 'P', 'P', 'P', 'P', 'P', 'P', 'P'},
		{'R', 'N', 'B', 'Q', 'K', 'B', 'N', 'R'},
	}
}

// RenderChessBoardPNG renders the 8x8 chess board as a crisp PNG image.
func RenderChessBoardPNG(board [8][8]rune) []byte {
	const sq, margin = 56, 24
	const size = sq*8 + margin*2
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{0x11, 0x1b, 0x21, 0xff}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, bg)
		}
	}
	light := color.RGBA{0xee, 0xee, 0xd2, 0xff}
	dark := color.RGBA{0x76, 0x96, 0x56, 0xff}
	face, _ := utils.NewFace(assets.FontLiberationSansBold, 32)
	lblFace, _ := utils.NewFace(assets.FontLiberationSansBold, 14)

	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			col := light
			if (r+c)%2 == 1 {
				col = dark
			}
			x0, y0 := margin+c*sq, margin+r*sq
			for y := y0; y < y0+sq; y++ {
				for x := x0; x < x0+sq; x++ {
					img.SetRGBA(x, y, col)
				}
			}
			if p := board[r][c]; p != 0 && face != nil {
				ink := color.RGBA{0x11, 0x11, 0x11, 0xff}
				if p >= 'A' && p <= 'Z' {
					ink = color.RGBA{0x0b, 0x4f, 0x6c, 0xff}
				}
				utils.DrawLinesCentered(img, face, []string{string(p)}, image.Rect(x0, y0, x0+sq, y0+sq), 34, ink)
			}
		}
		if lblFace != nil {
			utils.DrawLinesCentered(img, lblFace, []string{strconv.Itoa(8 - r)}, image.Rect(0, margin+r*sq, margin, margin+(r+1)*sq), 16, color.White)
			utils.DrawLinesCentered(img, lblFace, []string{string(rune('a' + r))}, image.Rect(margin+r*sq, size-margin, margin+(r+1)*sq, size), 16, color.White)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func cmdChess(client *whatsmeow.Client, msg *events.Message, args []string) {
	g := &activeGame{
		Kind:       "catur",
		ChessBoard: initChessBoard(),
		ChessTurn:  "w",
		Points:     50,
		ExpiresAt:  time.Now().Add(15 * time.Minute),
	}
	startSession(msg, g)
	pngBytes := RenderChessBoardPNG(g.ChessBoard)
	_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, pngBytes, "image/png", "♟️ *Chess Started!* (UPPERCASE = White, lowercase = Black)\nWhite to move. Send moves like `e2e4`, or `nyerah` to resign.")
}

func cmdUlarTangga(client *whatsmeow.Client, msg *events.Message, args []string) {
	g := &activeGame{
		Kind:      "ulartangga",
		SnakesPos: map[string]int{},
		Points:    35,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	startSession(msg, g)
	utils.SendReply(client, msg.Info.Chat, "🐍🪜 *Ular Tangga (Snakes & Ladders)!*\nFirst to reach tile *30* wins!\nType `roll` to roll the die (or `nyerah` to end).")
}

func cmdRussianRoulette(client *whatsmeow.Client, msg *events.Message, args []string) {
	if rand.Intn(6) == 0 {
		utils.SendReply(client, msg.Info.Chat, "💥 *BANG!* You hit the chamber! Better luck next time 💀")
		return
	}
	recordWin(client, msg, "russianroulette", 15)
	utils.SendReply(client, msg.Info.Chat, "🔫 *Click!* You survived! `+15` points & coins.")
}

func handleActiveGameReply(client *whatsmeow.Client, msg *events.Message) bool {
	if msg == nil || msg.Info.IsFromMe {
		return false
	}
	text := strings.TrimSpace(utils.GetTextFromMessage(msg.Message))
	if text == "" || strings.HasPrefix(text, ".") || strings.HasPrefix(text, "/") {
		return false
	}

	key := chatKey(msg)
	gamesMu.Lock()
	g, ok := activeGames[key]
	if ok && time.Now().After(g.ExpiresAt) {
		delete(activeGames, key)
		ok = false
	}
	gamesMu.Unlock()
	if !ok || g == nil {
		return false
	}

	lower := strings.ToLower(text)
	if lower == "nyerah" || lower == "skip" || lower == "giveup" {
		endSession(msg)
		ans := g.Answer
		if g.WordleTarget != "" {
			ans = g.WordleTarget
		}
		if ans != "" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🏳️ Game ended! The answer was: *%s*", ans))
		} else {
			utils.SendReply(client, msg.Info.Chat, "🏳️ Game ended.")
		}
		return true
	}
	if lower == "hint" && g.Hint != "" {
		utils.SendReply(client, msg.Info.Chat, "💡 Hint: _"+g.Hint+"_")
		return true
	}

	switch g.Kind {
	case "wordle":
		guess := strings.ToUpper(strings.TrimSpace(text))
		if len(guess) != 5 {
			return false
		}
		for _, r := range guess {
			if r < 'A' || r > 'Z' {
				return false
			}
		}
		score := ScoreWordle(g.WordleTarget, guess)
		g.WordleGuesses = append(g.WordleGuesses, fmt.Sprintf("%s  `%s`", score, guess))
		if guess == g.WordleTarget {
			endSession(msg)
			recordWin(client, msg, "wordle", g.Points)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎉 *Wordle Solved in %d/6!* (+%d pts)\n\n%s", len(g.WordleGuesses), g.Points, strings.Join(g.WordleGuesses, "\n")))
			return true
		}
		if len(g.WordleGuesses) >= 6 {
			endSession(msg)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💀 *Out of attempts!* Word was *%s*\n\n%s", g.WordleTarget, strings.Join(g.WordleGuesses, "\n")))
			return true
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🟩 *Wordle (%d/6)*\n%s", len(g.WordleGuesses), strings.Join(g.WordleGuesses, "\n")))
		return true

	case "tebakbom":
		n, err := strconv.Atoi(lower)
		if err != nil || n < 1 || n > 9 {
			return false
		}
		idx := n - 1
		if g.BombOpened[idx] {
			utils.SendReply(client, msg.Info.Chat, "Tile already opened! Pick another (1-9).")
			return true
		}
		if idx == g.BombPos {
			endSession(msg)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("💥 *BOOM!* Tile %d had the bomb! Game over.", n))
			return true
		}
		g.BombOpened[idx] = true
		opened := 0
		var b strings.Builder
		for i := 0; i < 9; i++ {
			if g.BombOpened[i] {
				opened++
				b.WriteString("✅")
			} else {
				b.WriteString(strconv.Itoa(i+1) + "️⃣")
			}
			if i%3 == 2 {
				b.WriteByte('\n')
			}
		}
		if opened >= 8 {
			endSession(msg)
			recordWin(client, msg, "tebakbom", 40)
			utils.SendReply(client, msg.Info.Chat, "🎉 *ALL SAFE TILES CLEARED!* (+40 pts)\n"+b.String())
			return true
		}
		recordWin(client, msg, "tebakbom", 5)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Safe! (+5 pts) Pick next (1-9):\n%s", b.String()))
		return true

	case "tictactoe":
		n, err := strconv.Atoi(lower)
		if err != nil || n < 1 || n > 9 {
			return false
		}
		if g.TTTBoard[n-1] != "" {
			utils.SendReply(client, msg.Info.Chat, "Cell taken! Choose an empty number 1-9.")
			return true
		}
		g.TTTBoard[n-1] = "X"
		if w := CheckTTTWinner(g.TTTBoard); w != "" {
			endSession(msg)
			if w == "X" {
				recordWin(client, msg, "tictactoe", g.Points)
				utils.SendReply(client, msg.Info.Chat, "🎉 *You Win!* (+30 pts)\n"+renderTTTBoard(g.TTTBoard))
			} else {
				utils.SendReply(client, msg.Info.Chat, "🤝 *Draw!*\n"+renderTTTBoard(g.TTTBoard))
			}
			return true
		}
		// Bot move
		var empties []int
		for i, c := range g.TTTBoard {
			if c == "" {
				empties = append(empties, i)
			}
		}
		if len(empties) > 0 {
			g.TTTBoard[empties[rand.Intn(len(empties))]] = "O"
		}
		if w := CheckTTTWinner(g.TTTBoard); w != "" {
			endSession(msg)
			if w == "O" {
				utils.SendReply(client, msg.Info.Chat, "🤖 *Bot Wins!*\n"+renderTTTBoard(g.TTTBoard))
			} else {
				utils.SendReply(client, msg.Info.Chat, "🤝 *Draw!*\n"+renderTTTBoard(g.TTTBoard))
			}
			return true
		}
		utils.SendReply(client, msg.Info.Chat, renderTTTBoard(g.TTTBoard))
		return true

	case "catur":
		if len(lower) != 4 || lower[0] < 'a' || lower[0] > 'h' || lower[2] < 'a' || lower[2] > 'h' || lower[1] < '1' || lower[1] > '8' || lower[3] < '1' || lower[3] > '8' {
			return false
		}
		fc, fr := int(lower[0]-'a'), 8-int(lower[1]-'0')
		tc, tr := int(lower[2]-'a'), 8-int(lower[3]-'0')
		piece := g.ChessBoard[fr][fc]
		if piece == 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ No piece on `"+lower[:2]+"`.")
			return true
		}
		isWhite := piece >= 'A' && piece <= 'Z'
		if (g.ChessTurn == "w" && !isWhite) || (g.ChessTurn == "b" && isWhite) {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚠️ It is *%s*'s turn.", map[string]string{"w": "White (UPPERCASE)", "b": "Black (lowercase)"}[g.ChessTurn]))
			return true
		}
		captured := g.ChessBoard[tr][tc]
		g.ChessBoard[tr][tc] = piece
		g.ChessBoard[fr][fc] = 0
		if captured == 'k' || captured == 'K' {
			endSession(msg)
			recordWin(client, msg, "catur", g.Points)
			_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, RenderChessBoardPNG(g.ChessBoard), "image/png", "👑 *Checkmate / King Captured!* (+50 pts)")
			return true
		}
		if g.ChessTurn == "w" {
			g.ChessTurn = "b"
		} else {
			g.ChessTurn = "w"
		}
		nextName := map[string]string{"w": "White", "b": "Black"}[g.ChessTurn]
		_, _ = utils.SendImageData(context.Background(), client, msg.Info.Chat, RenderChessBoardPNG(g.ChessBoard), "image/png", fmt.Sprintf("♟️ Moved `%s`. Next turn: *%s*.", lower, nextName))
		return true

	case "ulartangga":
		if lower != "roll" && lower != "lempar" {
			return false
		}
		name := msg.Info.PushName
		if name == "" {
			name = utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
		}
		roll := rand.Intn(6) + 1
		pos := g.SnakesPos[name] + roll
		note := ""
		ladders := map[int]int{4: 14, 9: 21, 17: 26}
		snakes := map[int]int{16: 6, 24: 11, 28: 19}
		if up, ok := ladders[pos]; ok {
			pos = up
			note = fmt.Sprintf(" 🪜 Ladder up to *%d*!", pos)
		} else if down, ok := snakes[pos]; ok {
			pos = down
			note = fmt.Sprintf(" 🐍 Snake down to *%d*!", pos)
		}
		g.SnakesPos[name] = pos
		if pos >= 30 {
			endSession(msg)
			recordWin(client, msg, "ulartangga", g.Points)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎲 *%s* rolled `%d` and reached tile *30*! 🎉 Winner! (+35 pts)", name, roll))
			return true
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎲 *%s* rolled `%d` → tile *%d/30*%s", name, roll, pos, note))
		return true

	case "sambungkata":
		word := strings.Fields(lower)[0]
		if len(word) < 3 {
			return false
		}
		need := g.SambungLast[len(g.SambungLast)-1]
		if word[0] != need {
			return false
		}
		if g.SambungUsed[word] {
			utils.SendReply(client, msg.Info.Chat, "⚠️ Word `"+word+"` was already used!")
			return true
		}
		g.SambungUsed[word] = true
		g.SambungLast = word
		g.ExpiresAt = time.Now().Add(2 * time.Minute)
		recordWin(client, msg, "sambungkata", 5)
		nextChar := strings.ToUpper(string(word[len(word)-1]))
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ *%s* accepted (+5 pts)! Next word must start with *%s*.", strings.ToUpper(word), nextChar))
		return true

	default:
		if g.Answer != "" && lower == g.Answer {
			endSession(msg)
			recordWin(client, msg, g.Kind, g.Points)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎉 *Correct!* Answer: *%s*\n🏆 *%s* earned `+%d` points & coins!", g.Answer, msg.Info.PushName, g.Points))
			return true
		}
	}
	return false
}

func cmdGameStats(client *whatsmeow.Client, msg *events.Message, args []string) {
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	rows, _ := db.GetBotStore().GetUserGameStats(uid)
	if len(rows) == 0 {
		utils.SendReply(client, msg.Info.Chat, "🎮 You haven't played any games yet! Try `.wordle`, `.math`, `.asahotak`, or `.catur`.")
		return
	}
	var totalPts int64
	totalWins, bestStreak := 0, 0
	var b strings.Builder
	b.WriteString("🎮 *Your Game Profile*\n\n")
	for _, r := range rows {
		totalPts += r.Points
		totalWins += r.Wins
		if r.BestStreak > bestStreak {
			bestStreak = r.BestStreak
		}
		fmt.Fprintf(&b, "• *%s*: `%dW` | `%d` pts | Best Streak `%d`\n", handlers.Title(r.Game), r.Wins, r.Points, r.BestStreak)
	}
	fmt.Fprintf(&b, "\n*Total:* `%d` wins | `%d` pts | Best Streak `%d`", totalWins, totalPts, bestStreak)
	utils.SendReply(client, msg.Info.Chat, b.String())
}

func cmdLeaderboard(global bool) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		gid := ""
		title := "Global Game Leaderboard"
		if !global && msg.Info.IsGroup {
			gid = msg.Info.Chat.ToNonAD().String()
			title = "Group Game Leaderboard"
		}
		entries, _ := db.GetBotStore().GameLeaderboard(gid, 10)
		if len(entries) == 0 {
			utils.SendReply(client, msg.Info.Chat, "🏆 No scores recorded yet.")
			return
		}
		var b strings.Builder
		fmt.Fprintf(&b, "🏆 *%s*\n\n", title)
		for i, e := range entries {
			fmt.Fprintf(&b, "%d. `%s` — *%d* pts (`%dW`, streak `%d`)\n", i+1, e.ID, e.Points, e.Wins, e.Best)
		}
		utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
	}
}

func cmdBadges(client *whatsmeow.Client, msg *events.Message, args []string) {
	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	rows, _ := db.GetBotStore().GetUserGameStats(uid)
	var pts int64
	wins := 0
	for _, r := range rows {
		pts += r.Points
		wins += r.Wins
	}
	var badges []string
	if wins >= 1 {
		badges = append(badges, "🥉 First Blood (1+ win)")
	}
	if wins >= 10 {
		badges = append(badges, "🥈 Veteran Player (10+ wins)")
	}
	if wins >= 50 {
		badges = append(badges, "🥇 Grandmaster (50+ wins)")
	}
	if pts >= 500 {
		badges = append(badges, "💎 High Roller (500+ pts)")
	}
	if len(rows) >= 5 {
		badges = append(badges, "🎯 Jack of All Games (5+ game modes)")
	}
	if len(badges) == 0 {
		badges = append(badges, "🌱 Newcomer — win your first game to unlock badges!")
	}
	utils.SendReply(client, msg.Info.Chat, "🏅 *Your Badges*\n• "+strings.Join(badges, "\n• "))
}

func cmdGroupGameStats(client *whatsmeow.Client, msg *events.Message, args []string) {
	gid := msg.Info.Chat.ToNonAD().String()
	entries, _ := db.GetBotStore().GameLeaderboard(gid, 5)
	groups, _ := db.GetBotStore().GroupGameSummary(5)

	var b strings.Builder
	b.WriteString("🏆 *Group Game Analytics*\n\n")
	if len(entries) > 0 {
		fmt.Fprintf(&b, "👑 *Group MVP:* `%s` (*%d* pts, `%dW`)\n", entries[0].ID, entries[0].Points, entries[0].Wins)
		var sum int64
		for _, e := range entries {
			sum += e.Points
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Points > entries[j].Points })
		b.WriteString("\n*Top Contributors:*\n")
		for i, e := range entries {
			pct := 0.0
			if sum > 0 {
				pct = float64(e.Points) * 100 / float64(sum)
			}
			fmt.Fprintf(&b, "%d. `%s` — `%d` pts (%.1f%%)\n", i+1, e.ID, e.Points, pct)
		}
	} else {
		b.WriteString("No group game results yet.\n")
	}
	if len(groups) > 0 {
		b.WriteString("\n*Top Groups Overall:*\n")
		for i, g := range groups {
			fmt.Fprintf(&b, "%d. `%s` — `%d` pts\n", i+1, g.ID, g.Points)
		}
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdRival(client *whatsmeow.Client, msg *events.Message, args []string) {
	targets := utils.ExtractTargetJIDs(msg, args)
	if len(targets) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.rival @user`")
		return
	}
	me := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	them := utils.ResolvePhoneNumber(client, targets[0], targets[0])
	sumStats := func(uid string) (int, int64) {
		rows, _ := db.GetBotStore().GetUserGameStats(uid)
		w := 0
		var p int64
		for _, r := range rows {
			w += r.Wins
			p += r.Points
		}
		return w, p
	}
	mw, mp := sumStats(me)
	tw, tp := sumStats(them)
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⚔️ *Rival Comparison*\n• You (`%s`): `%dW` | *%d* pts\n• Rival (`%s`): `%dW` | *%d* pts", me, mw, mp, them, tw, tp))
}

func cmdGameLimit(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat, "🎮 Game Limit: *Unlimited* on self-hosted NopeBot!")
}
