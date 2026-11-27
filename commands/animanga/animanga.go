package animanga

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
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
		// Animanga database & schedule
		{Name: "anilist", Category: "animanga", Description: "Search anime/manga on AniList. Usage: .anilist <title>", Execute: cmdAniList},
		{Name: "myanimelist", Aliases: []string{"mal", "anime"}, Category: "animanga", Description: "Search anime on MyAnimeList (Jikan). Usage: .mal <title>", Execute: cmdMAL},
		{Name: "character", Category: "animanga", Description: "Search an anime character on MyAnimeList. Usage: .character <name>", Execute: cmdCharacter},
		{Name: "jadwalanime", Category: "animanga", Description: "Show anime airing schedule. Usage: .jadwalanime [monday..sunday]", Execute: cmdJadwalAnime},
		{Name: "whatanime", Category: "animanga", Description: "Identify anime from a replied screenshot (alias for /traceanime).", Execute: cmdWhatAnimeAlias},

		// Indonesian anime & manga site lookups
		{Name: "komiku", Aliases: []string{"kiryuu", "westmanga", "bacakomik"}, Category: "animanga", Description: "Search Indonesian manga sources (MangaDex / Komiku).", Usage: ".komiku <title>", Execute: cmdMangaSearch},
		{Name: "samehadaku", Aliases: []string{"otakudesu", "kuronime", "animasu", "sokuja", "alqanime", "kuramanime"}, Category: "animanga", Description: "Search Indonesian anime info & airing details.", Usage: ".samehadaku <title>", Execute: cmdMAL},

		// Waifu & anime art gallery
		{Name: "waifu", Category: "waifu", Description: "Fetch a random SFW waifu image.", Execute: cmdWaifuPics("waifu")},
		{Name: "neko", Category: "waifu", Description: "Fetch a random SFW neko image.", Execute: cmdWaifuPics("neko")},
		{Name: "kitsune", Category: "waifu", Description: "Fetch a random kitsune image.", Execute: cmdNekosBest("kitsune")},
		{Name: "husbu", Category: "waifu", Description: "Fetch a random husbando image.", Execute: cmdNekosBest("husbando")},
		{Name: "maid", Category: "waifu", Description: "Fetch a random anime maid image.", Execute: cmdWaifuIM("maid")},
		{Name: "bunnygirl", Category: "waifu", Description: "Fetch a random SFW bunnygirl anime art.", Execute: cmdDanbooruTag("bunny_girl rating:general")},
		{Name: "schoolgirl", Category: "waifu", Description: "Fetch a random SFW schoolgirl anime art.", Execute: cmdDanbooruTag("school_uniform rating:general")},
		{Name: "swimsuit", Category: "waifu", Description: "Fetch a random SFW swimsuit anime art.", Execute: cmdDanbooruTag("swimsuit rating:general")},
		{Name: "animewallpaper", Category: "waifu", Description: "Fetch an HD anime wallpaper.", Execute: cmdDanbooruTag("scenery highres rating:general")},
		{Name: "danbooru", Category: "waifu", Description: "Search SFW anime art on Danbooru by tag. Usage: .danbooru <tag>", Execute: cmdDanbooruSearch},
		{Name: "zerochan", Category: "waifu", Description: "Search anime art by character/tag.", Usage: ".zerochan <tag>", Execute: cmdDanbooruSearch},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdAniList(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.anilist <anime or manga title>`")
		return
	}
	gql := map[string]any{
		"query": `query ($search: String) { Media(search: $search, type: ANIME) { title { romaji english native } episodes status averageScore seasonYear genres description(asHtml: false) siteUrl coverImage { large } } }`,
		"variables": map[string]any{
			"search": q,
		},
	}
	body, _ := json.Marshal(gql)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://graphql.anilist.co", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ AniList error: "+err.Error())
		return
	}
	defer resp.Body.Close()
	var res struct {
		Data struct {
			Media struct {
				Title struct {
					Romaji  string `json:"romaji"`
					English string `json:"english"`
				} `json:"title"`
				Episodes     int      `json:"episodes"`
				Status       string   `json:"status"`
				AverageScore int      `json:"averageScore"`
				SeasonYear   int      `json:"seasonYear"`
				Genres       []string `json:"genres"`
				Description  string   `json:"description"`
				SiteURL      string   `json:"siteUrl"`
				CoverImage   struct {
					Large string `json:"large"`
				} `json:"coverImage"`
			} `json:"Media"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil || res.Data.Media.Title.Romaji == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ No AniList entry found for `"+q+"`.")
		return
	}
	m := res.Data.Media
	desc := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(html.UnescapeString(m.Description), "")
	if len(desc) > 500 {
		desc = desc[:500] + "..."
	}
	caption := fmt.Sprintf(
		"🌸 *%s* (%d)\n• Score: `%d/100` | Episodes: `%d` | Status: `%s`\n• Genres: %s\n• Link: %s\n\n%s",
		m.Title.Romaji, m.SeasonYear, m.AverageScore, m.Episodes, m.Status, strings.Join(m.Genres, ", "), m.SiteURL, desc,
	)
	if m.CoverImage.Large != "" {
		if img, ct, err := utils.DownloadHTTPBytes(ctx, m.CoverImage.Large, nil); err == nil {
			_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, caption)
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, caption)
}

func cmdMAL(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.mal <anime title>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp struct {
		Data []struct {
			Title    string  `json:"title"`
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
	if err := utils.HTTPGetJSON(ctx, "https://api.jikan.moe/v4/anime?limit=1&q="+url.QueryEscape(q), nil, &resp); err != nil || len(resp.Data) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Anime not found for `"+q+"`.")
		return
	}
	a := resp.Data[0]
	syn := a.Synopsis
	if len(syn) > 500 {
		syn = syn[:500] + "..."
	}
	caption := fmt.Sprintf("🌸 *%s*\n• Score: `%.2f` | Episodes: `%d` | Status: `%s`\n• MAL: %s\n\n%s", a.Title, a.Score, a.Episodes, a.Status, a.URL, syn)
	if a.Images.JPG.ImageURL != "" {
		if img, ct, err := utils.DownloadHTTPBytes(ctx, a.Images.JPG.ImageURL, nil); err == nil {
			_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, caption)
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, caption)
}

func cmdCharacter(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.character <name>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp struct {
		Data []struct {
			Name      string `json:"name"`
			NameKanji string `json:"name_kanji"`
			About     string `json:"about"`
			Favorites int    `json:"favorites"`
			URL       string `json:"url"`
			Images    struct {
				JPG struct {
					ImageURL string `json:"image_url"`
				} `json:"jpg"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://api.jikan.moe/v4/characters?limit=1&q="+url.QueryEscape(q), nil, &resp); err != nil || len(resp.Data) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Character not found.")
		return
	}
	c := resp.Data[0]
	about := c.About
	if len(about) > 500 {
		about = about[:500] + "..."
	}
	caption := fmt.Sprintf("✨ *%s* (%s)\n• Favorites: `%d`\n• MAL: %s\n\n%s", c.Name, c.NameKanji, c.Favorites, c.URL, about)
	if c.Images.JPG.ImageURL != "" {
		if img, ct, err := utils.DownloadHTTPBytes(ctx, c.Images.JPG.ImageURL, nil); err == nil {
			_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, caption)
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, caption)
}

func cmdJadwalAnime(client *whatsmeow.Client, msg *events.Message, args []string) {
	day := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if day == "" {
		day = strings.ToLower(time.Now().Weekday().String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp struct {
		Data []struct {
			Title string  `json:"title"`
			Score float64 `json:"score"`
			URL   string  `json:"url"`
		} `json:"data"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://api.jikan.moe/v4/schedules?limit=12&filter="+url.QueryEscape(day), nil, &resp); err != nil || len(resp.Data) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not load schedule for `"+day+"`.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📅 *Anime Schedule (%s)*\n\n", handlers.Title(day))
	for i, a := range resp.Data {
		fmt.Fprintf(&b, "%d. *%s* (⭐ %.1f)\n", i+1, a.Title, a.Score)
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdWhatAnimeAlias(client *whatsmeow.Client, msg *events.Message, args []string) {
	if cmd, ok := handlers.Commands["traceanime"]; ok {
		cmd.Execute(client, msg, args)
		return
	}
	utils.SendReply(client, msg.Info.Chat, "Use `/traceanime` while replying to an anime screenshot.")
}

func cmdMangaSearch(client *whatsmeow.Client, msg *events.Message, args []string) {
	if cmd, ok := handlers.Commands["manga"]; ok {
		cmd.Execute(client, msg, args)
		return
	}
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.komiku <manga title>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp struct {
		Data []struct {
			Title    string  `json:"title"`
			Chapters int     `json:"chapters"`
			Score    float64 `json:"score"`
			URL      string  `json:"url"`
		} `json:"data"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://api.jikan.moe/v4/manga?limit=5&q="+url.QueryEscape(q), nil, &resp); err != nil || len(resp.Data) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Manga not found.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📚 *Manga Search: %s*\n\n", q)
	for i, m := range resp.Data {
		fmt.Fprintf(&b, "%d. *%s* (⭐ %.2f | Ch: %d)\n   %s\n", i+1, m.Title, m.Score, m.Chapters, m.URL)
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdWaifuPics(category string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var resp struct {
			URL string `json:"url"`
		}
		if err := utils.HTTPGetJSON(ctx, "https://api.waifu.pics/sfw/"+category, nil, &resp); err != nil || resp.URL == "" {
			utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch "+category+" image.")
			return
		}
		img, ct, err := utils.DownloadHTTPBytes(ctx, resp.URL, nil)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "🌸 "+resp.URL)
			return
		}
		_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, "🌸 "+handlers.Title(category))
	}
}

func cmdNekosBest(category string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var resp struct {
			Results []struct {
				URL        string `json:"url"`
				ArtistName string `json:"artist_name"`
			} `json:"results"`
		}
		if err := utils.HTTPGetJSON(ctx, "https://nekos.best/api/v2/"+category, nil, &resp); err != nil || len(resp.Results) == 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch "+category+".")
			return
		}
		r := resp.Results[0]
		img, ct, err := utils.DownloadHTTPBytes(ctx, r.URL, nil)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "🌸 "+r.URL)
			return
		}
		_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, fmt.Sprintf("🌸 %s (Artist: %s)", handlers.Title(category), r.ArtistName))
	}
}

func cmdWaifuIM(tag string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var resp struct {
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		}
		if err := utils.HTTPGetJSON(ctx, "https://api.waifu.im/search?is_nsfw=false&included_tags="+url.QueryEscape(tag), nil, &resp); err != nil || len(resp.Images) == 0 {
			cmdDanbooruTag(tag+" rating:general")(client, msg, args)
			return
		}
		img, ct, err := utils.DownloadHTTPBytes(ctx, resp.Images[0].URL, nil)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "🌸 "+resp.Images[0].URL)
			return
		}
		_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, "🌸 "+handlers.Title(tag))
	}
}

func cmdDanbooruTag(tags string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		fetchDanbooru(client, msg, tags)
	}
}

func cmdDanbooruSearch(client *whatsmeow.Client, msg *events.Message, args []string) {
	tag := strings.TrimSpace(strings.Join(args, "_"))
	if tag == "" {
		tag = "1girl"
	}
	fetchDanbooru(client, msg, tag+" rating:general")
}

func fetchDanbooru(client *whatsmeow.Client, msg *events.Message, tags string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var posts []struct {
		FileURL      string `json:"file_url"`
		LargeFileURL string `json:"large_file_url"`
		TagString    string `json:"tag_string_character"`
	}
	apiURL := "https://danbooru.donmai.us/posts.json?limit=15&tags=" + url.QueryEscape(tags)
	if err := utils.HTTPGetJSON(ctx, apiURL, nil, &posts); err != nil || len(posts) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ No artwork found for `"+tags+"`.")
		return
	}
	var valid []string
	for _, p := range posts {
		u := p.LargeFileURL
		if u == "" {
			u = p.FileURL
		}
		if strings.HasSuffix(strings.ToLower(u), ".jpg") || strings.HasSuffix(strings.ToLower(u), ".png") || strings.HasSuffix(strings.ToLower(u), ".jpeg") {
			valid = append(valid, u)
		}
	}
	if len(valid) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ No image results available.")
		return
	}
	pick := valid[rand.Intn(len(valid))]
	img, ct, err := utils.DownloadHTTPBytes(ctx, pick, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "🎨 "+pick)
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, "🎨 "+tags)
}
