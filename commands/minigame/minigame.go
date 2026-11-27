package minigame

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/ai"
	db "bot/databases"
	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

type miniSession struct {
	Kind      string
	Board2048 [4][4]int
	Score     int
	// Minesweeper 5x5
	Mines    [25]bool
	Revealed [25]bool
	Expires  time.Time
}

var (
	miniMu       sync.Mutex
	miniSessions = map[string]*miniSession{}
)

func init() {
	handlers.RegisterInteractiveSessionHandler(handleMiniReply)

	cmds := []types.Command{
		{Name: "minigame", Category: "minigame", Description: "Open the Whatbot Web Arcade hub (12 playable HTML5 games + in-chat boards).", Execute: cmdArcade("snake")},
		{Name: "2048", Category: "minigame", Description: "Play 2048 right in chat (reply w/a/s/d) or in the Web Arcade!", Execute: cmd2048},
		{Name: "minesweeper", Category: "minigame", Description: "Play 5x5 Minesweeper in chat (reply row,col like 2,3) or Web Arcade!", Execute: cmdMinesweeper},
		{Name: "sudoku", Category: "minigame", Description: "Play Sudoku in the Web Arcade.", Execute: cmdArcade("sudoku")},
		{Name: "snake", Category: "minigame", Description: "Play Snake in the Web Arcade.", Execute: cmdArcade("snake")},
		{Name: "tetris", Category: "minigame", Description: "Play Tetris in the Web Arcade.", Execute: cmdArcade("tetris")},
		{Name: "flappybird", Category: "minigame", Description: "Play Flappy Bird in the Web Arcade.", Execute: cmdArcade("flappybird")},
		{Name: "breakout", Category: "minigame", Description: "Play Breakout in the Web Arcade.", Execute: cmdArcade("breakout")},
		{Name: "dinorun", Category: "minigame", Description: "Play Dino Run in the Web Arcade.", Execute: cmdArcade("dinorun")},
		{Name: "frogger", Category: "minigame", Description: "Play Frogger in the Web Arcade.", Execute: cmdArcade("frogger")},
		{Name: "galaga", Category: "minigame", Description: "Play Galaga in the Web Arcade.", Execute: cmdArcade("galaga")},
		{Name: "pacman", Category: "minigame", Description: "Play Pacman in the Web Arcade.", Execute: cmdArcade("pacman")},
		{Name: "racing", Category: "minigame", Description: "Play Racing in the Web Arcade.", Execute: cmdArcade("racing")},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func arcadeURL(slug, user, group string) string {
	base := "http://localhost" + ai.AppConfig.ServerPort
	if dom := strings.TrimSpace(os.Getenv("NGROK_DOMAIN")); dom != "" {
		if !strings.HasPrefix(dom, "http") {
			base = "https://" + dom
		} else {
			base = strings.TrimRight(dom, "/")
		}
	}
	return fmt.Sprintf("%s/games/%s?user=%s&group=%s", base, slug, user, group)
}

func cmdArcade(slug string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
		gid := ""
		if msg.Info.IsGroup {
			gid = msg.Info.Chat.ToNonAD().String()
		}
		link := arcadeURL(slug, uid, gid)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
			"🕹️ *Whatbot Arcade — %s*\nPlay directly in your browser (scores auto-save to `.leaderboard`):\n%s\n\n_Available games: 2048, Breakout, DinoRun, FlappyBird, Frogger, Galaga, Minesweeper, Pacman, Racing, Snake, Sudoku, Tetris._",
			handlers.Title(slug), link,
		))
	}
}

// Slide2048Row merges a single 4-cell 2048 row to the left and returns (newRow, gainedScore).
func Slide2048Row(row [4]int) ([4]int, int) {
	var vals []int
	for _, v := range row {
		if v != 0 {
			vals = append(vals, v)
		}
	}
	gained := 0
	var out [4]int
	idx := 0
	for i := 0; i < len(vals); i++ {
		if i+1 < len(vals) && vals[i] == vals[i+1] {
			merged := vals[i] * 2
			out[idx] = merged
			gained += merged
			idx++
			i++
		} else {
			out[idx] = vals[i]
			idx++
		}
	}
	return out, gained
}

func addTile2048(b *[4][4]int) {
	var empty [][2]int
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			if b[r][c] == 0 {
				empty = append(empty, [2]int{r, c})
			}
		}
	}
	if len(empty) == 0 {
		return
	}
	p := empty[rand.Intn(len(empty))]
	v := 2
	if rand.Intn(10) == 0 {
		v = 4
	}
	b[p[0]][p[1]] = v
}

func render2048(b [4][4]int, score int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🔢 *2048* (Score: `%d`)\n```\n", score)
	for r := 0; r < 4; r++ {
		for c := 0; c < 4; c++ {
			if b[r][c] == 0 {
				sb.WriteString("   . ")
			} else {
				fmt.Fprintf(&sb, "%4d ", b[r][c])
			}
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("```\nReply `w` (up), `a` (left), `s` (down), `d` (right), or `stop`.")
	return sb.String()
}

func cmd2048(client *whatsmeow.Client, msg *events.Message, args []string) {
	s := &miniSession{Kind: "2048", Expires: time.Now().Add(10 * time.Minute)}
	addTile2048(&s.Board2048)
	addTile2048(&s.Board2048)
	miniMu.Lock()
	miniSessions[msg.Info.Chat.ToNonAD().String()] = s
	miniMu.Unlock()

	uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	gid := ""
	if msg.Info.IsGroup {
		gid = msg.Info.Chat.ToNonAD().String()
	}
	utils.SendReply(client, msg.Info.Chat, render2048(s.Board2048, 0)+"\n🌐 Web version: "+arcadeURL("2048", uid, gid))
}

func cmdMinesweeper(client *whatsmeow.Client, msg *events.Message, args []string) {
	s := &miniSession{Kind: "minesweeper", Expires: time.Now().Add(10 * time.Minute)}
	placed := 0
	for placed < 5 {
		p := rand.Intn(25)
		if !s.Mines[p] {
			s.Mines[p] = true
			placed++
		}
	}
	miniMu.Lock()
	miniSessions[msg.Info.Chat.ToNonAD().String()] = s
	miniMu.Unlock()
	utils.SendReply(client, msg.Info.Chat, renderMinesweeper(s, false)+"\nReply `row,col` (1-5, e.g. `2,3`) or `stop`.")
}

func countMines(s *miniSession, r, c int) int {
	n := 0
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			nr, nc := r+dr, c+dc
			if nr >= 0 && nr < 5 && nc >= 0 && nc < 5 && s.Mines[nr*5+nc] {
				n++
			}
		}
	}
	return n
}

func renderMinesweeper(s *miniSession, showMines bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "💣 *Minesweeper 5x5* (Score: `%d`)\n   1  2  3  4  5\n", s.Score)
	for r := 0; r < 5; r++ {
		fmt.Fprintf(&sb, "%d ", r+1)
		for c := 0; c < 5; c++ {
			i := r*5 + c
			if s.Revealed[i] {
				if s.Mines[i] {
					sb.WriteString("💥 ")
				} else {
					sb.WriteString(strconv.Itoa(countMines(s, r, c)) + "️⃣ ")
				}
			} else if showMines && s.Mines[i] {
				sb.WriteString("💣 ")
			} else {
				sb.WriteString("⬜ ")
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func handleMiniReply(client *whatsmeow.Client, msg *events.Message) bool {
	if msg == nil || msg.Info.IsFromMe {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(utils.GetTextFromMessage(msg.Message)))
	if text == "" || strings.HasPrefix(text, ".") || strings.HasPrefix(text, "/") {
		return false
	}
	key := msg.Info.Chat.ToNonAD().String()
	miniMu.Lock()
	s, ok := miniSessions[key]
	if ok && time.Now().After(s.Expires) {
		delete(miniSessions, key)
		ok = false
	}
	miniMu.Unlock()
	if !ok || s == nil {
		return false
	}

	if text == "stop" || text == "nyerah" {
		miniMu.Lock()
		delete(miniSessions, key)
		miniMu.Unlock()
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🛑 MiniGame stopped. Final score: `%d`.", s.Score))
		return true
	}

	switch s.Kind {
	case "2048":
		if text != "w" && text != "a" && text != "s" && text != "d" && text != "up" && text != "left" && text != "down" && text != "right" {
			return false
		}
		before := s.Board2048
		gained := 0
		switch text {
		case "a", "left":
			for r := 0; r < 4; r++ {
				nr, g := Slide2048Row(s.Board2048[r])
				s.Board2048[r] = nr
				gained += g
			}
		case "d", "right":
			for r := 0; r < 4; r++ {
				rev := [4]int{s.Board2048[r][3], s.Board2048[r][2], s.Board2048[r][1], s.Board2048[r][0]}
				nr, g := Slide2048Row(rev)
				s.Board2048[r] = [4]int{nr[3], nr[2], nr[1], nr[0]}
				gained += g
			}
		case "w", "up":
			for c := 0; c < 4; c++ {
				col := [4]int{s.Board2048[0][c], s.Board2048[1][c], s.Board2048[2][c], s.Board2048[3][c]}
				nr, g := Slide2048Row(col)
				for r := 0; r < 4; r++ {
					s.Board2048[r][c] = nr[r]
				}
				gained += g
			}
		case "s", "down":
			for c := 0; c < 4; c++ {
				col := [4]int{s.Board2048[3][c], s.Board2048[2][c], s.Board2048[1][c], s.Board2048[0][c]}
				nr, g := Slide2048Row(col)
				for r := 0; r < 4; r++ {
					s.Board2048[3-r][c] = nr[r]
				}
				gained += g
			}
		}
		if s.Board2048 != before {
			s.Score += gained
			addTile2048(&s.Board2048)
		}
		utils.SendReply(client, msg.Info.Chat, render2048(s.Board2048, s.Score))
		return true

	case "minesweeper":
		parts := strings.Split(strings.ReplaceAll(text, " ", ","), ",")
		if len(parts) != 2 {
			return false
		}
		r, err1 := strconv.Atoi(parts[0])
		c, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || r < 1 || r > 5 || c < 1 || c > 5 {
			return false
		}
		idx := (r-1)*5 + (c - 1)
		if s.Revealed[idx] {
			return true
		}
		s.Revealed[idx] = true
		if s.Mines[idx] {
			miniMu.Lock()
			delete(miniSessions, key)
			miniMu.Unlock()
			utils.SendReply(client, msg.Info.Chat, "💥 *BOOM! You hit a mine!*\n"+renderMinesweeper(s, true))
			return true
		}
		s.Score += 10
		safeLeft := 0
		for i := 0; i < 25; i++ {
			if !s.Mines[i] && !s.Revealed[i] {
				safeLeft++
			}
		}
		if safeLeft == 0 {
			miniMu.Lock()
			delete(miniSessions, key)
			miniMu.Unlock()
			if store := db.GetBotStore(); store != nil {
				uid := utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
				gid := ""
				if msg.Info.IsGroup {
					gid = msg.Info.Chat.ToNonAD().String()
				}
				_ = store.RecordGameResult(uid, gid, "minesweeper", true, false, int64(s.Score))
			}
			utils.SendReply(client, msg.Info.Chat, "🎉 *MINESWEEPER CLEARED!*\n"+renderMinesweeper(s, true))
			return true
		}
		utils.SendReply(client, msg.Info.Chat, renderMinesweeper(s, false))
		return true
	}
	return false
}
