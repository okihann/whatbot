package tool

import (
	"bot/handlers"
	"bot/types"
	"bot/utils"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func init() {
	handlers.RegisterCommand(types.Command{Name: "manga", Category: "anime", Execute: executeManga})
}

func executeManga(client *whatsmeow.Client, msg *events.Message, args []string) {
	if len(args) == 0 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `/manga <title>`")
		return
	}

	query := url.QueryEscape(strings.Join(args, " "))
	apiURL := fmt.Sprintf("https://api.jikan.moe/v4/manga?q=%s&limit=1", query)

	utils.SendReply(client, msg.Info.Chat, "📚 Searching MyAnimeList...")

	resp, err := http.Get(apiURL)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to reach MyAnimeList API.")
		return
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Title      string `json:"title"`
			Status     string `json:"status"`
			Chapters   int    `json:"chapters"`
			Volumes    int    `json:"volumes"`
			Synopsis   string `json:"synopsis"`
			Score      float64 `json:"score"`
			Images     struct {
				Jpg struct {
					ImageURL string `json:"image_url"`
				} `json:"jpg"`
			} `json:"images"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Data) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ No manga found with that title.")
		return
	}

	m := result.Data[0]
	caption := fmt.Sprintf("📖 *%s*\n\n", m.Title)
	caption += fmt.Sprintf("📊 *Score:* %.2f\n", m.Score)
	caption += fmt.Sprintf("📌 *Status:* %s\n", m.Status)
	caption += fmt.Sprintf("📑 *Chapters:* %d | *Volumes:* %d\n\n", m.Chapters, m.Volumes)
	
	synopsis := m.Synopsis
	if len(synopsis) > 500 { synopsis = synopsis[:500] + "..." }
	caption += fmt.Sprintf("📝 *Synopsis:*\n%s", synopsis)

	// In utils, you can add a generic SendImageFromURL if you want to send the cover, 
    // otherwise sending as text works perfectly:
	utils.SendReply(client, msg.Info.Chat, caption)
}
