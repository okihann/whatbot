package animanga

import (
	"context"
	"encoding/json"
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

var fallbackAnimeQuotes = []struct {
	Quote, Character, Anime string
}{
	{"People's lives don't end when they die. It ends when they lose faith.", "Itachi Uchiha", "Naruto Shippuden"},
	{"If you don't take risks, you can't create a future.", "Monkey D. Luffy", "One Piece"},
	{"Hard work is worthless for those that don't believe in themselves.", "Naruto Uzumaki", "Naruto"},
	{"Whatever you lose, you'll find it again. But what you throw away you'll never get back.", "Kenshin Himura", "Rurouni Kenshin"},
	{"Power comes in response to a need, not a desire.", "Goku", "Dragon Ball Z"},
	{"The world isn't perfect. But it's there for us, doing the best it can.", "Roy Mustang", "Fullmetal Alchemist"},
	{"A lesson without pain is meaningless. That's because no one can gain without sacrificing something.", "Edward Elric", "Fullmetal Alchemist: Brotherhood"},
	{"Giving up kills people. When people reject giving up, they finally win the right to transcend humanity.", "Alucard", "Hellsing"},
	{"If you win, you live. If you lose, you die. If you don't fight, you can't win!", "Eren Yeager", "Attack on Titan"},
	{"Knowing what it feels to be in pain is exactly why we try to be kind to others.", "Jiraiya", "Naruto Shippuden"},
}

func init() {
	cmds := []types.Command{
		{Name: "quotesanime", Category: "anime", Description: "Get a random iconic anime quote.", Execute: cmdQuotesAnime},
		{Name: "randomanime", Category: "anime", Description: "Get a random anime recommendation with synopsis and score.", Execute: cmdRandomAnime},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdQuotesAnime(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if body, _, err := utils.HTTPGetBytes(ctx, "https://api.animechan.io/v1/quotes/random", nil); err == nil {
		var res struct {
			Data struct {
				Content   string `json:"content"`
				Anime     struct{ Name string }
				Character struct{ Name string }
			} `json:"data"`
		}
		if json.Unmarshal(body, &res) == nil && res.Data.Content != "" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
				"🌸 *Anime Quote*\n\n_\"%s\"_\n\n— *%s* (%s)",
				res.Data.Content, res.Data.Character.Name, res.Data.Anime.Name,
			))
			return
		}
	}
	q := fallbackAnimeQuotes[rand.Intn(len(fallbackAnimeQuotes))]
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🌸 *Anime Quote*\n\n_\"%s\"_\n\n— *%s* (%s)",
		q.Quote, q.Character, q.Anime,
	))
}

func cmdRandomAnime(client *whatsmeow.Client, msg *events.Message, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	body, _, err := utils.HTTPGetBytes(ctx, "https://api.jikan.moe/v4/random/anime?sfw=true", nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch random anime: "+err.Error())
		return
	}
	var res struct {
		Data struct {
			Title    string  `json:"title"`
			Type     string  `json:"type"`
			Episodes int     `json:"episodes"`
			Score    float64 `json:"score"`
			Status   string  `json:"status"`
			Synopsis string  `json:"synopsis"`
			URL      string  `json:"url"`
			Images   struct {
				JPG struct {
					ImageURL string `json:"image_url"`
				} `json:"jpg"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Data.Title == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not parse Jikan random anime response.")
		return
	}
	d := res.Data
	syn := strings.TrimSpace(d.Synopsis)
	if len(syn) > 400 {
		syn = syn[:400] + "..."
	}
	caption := fmt.Sprintf(
		"🎲 *Random Anime*\n\n• *Title:* %s\n• *Type:* %s (%d eps)\n• *Score:* %.2f\n• *Status:* %s\n• *Link:* %s\n\n%s",
		d.Title, d.Type, d.Episodes, d.Score, d.Status, d.URL, syn,
	)
	if d.Images.JPG.ImageURL != "" {
		if img, _, err := utils.HTTPGetBytes(ctx, d.Images.JPG.ImageURL, nil); err == nil {
			_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, "image/jpeg", caption)
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, caption)
}
