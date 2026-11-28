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
		{Name: "anilist", Category: "anime", Description: "Search anime/manga on AniList. Usage: .anilist <title>", Execute: cmdAniList},
		{Name: "myanimelist", Aliases: []string{"mal", "anime"}, Category: "anime", Description: "Search anime on MyAnimeList (Jikan). Usage: .mal <title>", Execute: cmdMAL},
		{Name: "character", Category: "anime", Description: "Search an anime character on MyAnimeList. Usage: .character <name>", Execute: cmdCharacter},
		{Name: "jadwalanime", Category: "anime", Description: "Show anime airing schedule. Usage: .jadwalanime [monday..sunday]", Execute: cmdJadwalAnime},
		{Name: "whatanime", Category: "anime", Description: "Identify anime from a replied screenshot (alias for /traceanime).", Execute: cmdWhatAnimeAlias},

		// Indonesian anime & manga site lookups
		{Name: "komiku", Aliases: []string{"kiryuu", "westmanga", "bacakomik"}, Category: "anime", Description: "Search Indonesian manga sources (MangaDex / Komiku).", Usage: ".komiku <title>", Execute: cmdMangaSearch},
		{Name: "samehadaku", Aliases: []string{"otakudesu", "kuronime", "animasu", "sokuja", "alqanime", "kuramanime"}, Category: "anime", Description: "Search Indonesian anime info & airing details.", Usage: ".samehadaku <title>", Execute: cmdMAL},

		// Waifu & anime art gallery (multi-provider fallback: nekos.best -> nekosia.cat -> safebooru -> konachan -> danbooru)
		{Name: "waifu", Category: "anime", Description: "Fetch a random SFW waifu image.", Execute: cmdAnimeGallery("Waifu", "waifu", "cute", "1girl")},
		{Name: "neko", Category: "anime", Description: "Fetch a random SFW neko image.", Execute: cmdAnimeGallery("Neko", "neko", "catgirl", "cat_ears")},
		{Name: "kitsune", Category: "anime", Description: "Fetch a random kitsune image.", Execute: cmdAnimeGallery("Kitsune", "kitsune", "foxgirl", "fox_ears")},
		{Name: "husbu", Category: "anime", Description: "Fetch a random husbando image.", Execute: cmdAnimeGallery("Husbando", "husbando", "", "1boy")},
		{Name: "maid", Category: "anime", Description: "Fetch a random anime maid image.", Execute: cmdAnimeGallery("Maid", "", "maid", "maid")},
		{Name: "bunnygirl", Category: "anime", Description: "Fetch a random SFW bunnygirl anime art.", Execute: cmdAnimeGallery("Bunny Girl", "", "", "bunny_girl")},
		{Name: "schoolgirl", Category: "anime", Description: "Fetch a random SFW schoolgirl anime art.", Execute: cmdAnimeGallery("School Girl", "", "uniform", "school_uniform")},
		{Name: "swimsuit", Category: "anime", Description: "Fetch a random SFW swimsuit anime art.", Execute: cmdAnimeGallery("Swimsuit", "", "", "swimsuit")},
		{Name: "animewallpaper", Category: "anime", Description: "Fetch an HD anime wallpaper.", Execute: cmdAnimeGallery("Anime Wallpaper", "", "", "scenery")},
		{Name: "danbooru", Category: "anime", Description: "Search SFW anime art on Danbooru/Safebooru by tag. Usage: .danbooru <tag>", Execute: cmdDanbooruSearch},
		{Name: "zerochan", Category: "anime", Description: "Search anime art by character/tag.", Usage: ".zerochan <tag>", Execute: cmdDanbooruSearch},
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

var animangaHeaders = map[string]string{
	"User-Agent": "NopeBot/1.0 (+https://github.com/okihann/whatbot)",
}

type animeImageResult struct {
	URL    string
	Artist string
	Source string
}

func fetchNekosBest(ctx context.Context, category string) (*animeImageResult, error) {
	if category == "" {
		return nil, fmt.Errorf("empty category")
	}
	var resp struct {
		Results []struct {
			URL        string `json:"url"`
			ArtistName string `json:"artist_name"`
		} `json:"results"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://nekos.best/api/v2/"+url.PathEscape(category), animangaHeaders, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 || resp.Results[0].URL == "" {
		return nil, fmt.Errorf("empty nekos.best result")
	}
	return &animeImageResult{
		URL:    resp.Results[0].URL,
		Artist: resp.Results[0].ArtistName,
		Source: "nekos.best",
	}, nil
}

func fetchNekosia(ctx context.Context, category string) (*animeImageResult, error) {
	if category == "" {
		return nil, fmt.Errorf("empty category")
	}
	var resp struct {
		Success bool `json:"success"`
		Image   struct {
			Original struct {
				URL string `json:"url"`
			} `json:"original"`
			Compressed struct {
				URL string `json:"url"`
			} `json:"compressed"`
		} `json:"image"`
		Attribution struct {
			Artist struct {
				Username string `json:"username"`
			} `json:"artist"`
		} `json:"attribution"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://api.nekosia.cat/api/v1/images/"+url.PathEscape(category), animangaHeaders, &resp); err != nil {
		return nil, err
	}
	u := resp.Image.Compressed.URL
	if u == "" {
		u = resp.Image.Original.URL
	}
	if u == "" {
		return nil, fmt.Errorf("empty nekosia image")
	}
	return &animeImageResult{
		URL:    u,
		Artist: resp.Attribution.Artist.Username,
		Source: "nekosia.cat",
	}, nil
}

func fetchSafebooru(ctx context.Context, tag string) (*animeImageResult, error) {
	if tag == "" {
		return nil, fmt.Errorf("empty tag")
	}
	var posts []struct {
		Directory string `json:"directory"`
		Image     string `json:"image"`
		FileURL   string `json:"file_url"`
	}
	// Randomize page slightly so repeated calls return fresh art.
	pid := rand.Intn(4)
	apiURL := fmt.Sprintf("https://safebooru.org/index.php?page=dapi&s=post&q=index&json=1&limit=20&pid=%d&tags=%s", pid, url.QueryEscape(tag))
	if err := utils.HTTPGetJSON(ctx, apiURL, animangaHeaders, &posts); err != nil || len(posts) == 0 {
		if pid > 0 {
			apiURL = fmt.Sprintf("https://safebooru.org/index.php?page=dapi&s=post&q=index&json=1&limit=20&tags=%s", url.QueryEscape(tag))
			if err2 := utils.HTTPGetJSON(ctx, apiURL, animangaHeaders, &posts); err2 != nil || len(posts) == 0 {
				return nil, fmt.Errorf("safebooru empty")
			}
		} else {
			return nil, fmt.Errorf("safebooru empty")
		}
	}
	var valid []string
	for _, p := range posts {
		u := p.FileURL
		if u == "" && p.Directory != "" && p.Image != "" {
			u = fmt.Sprintf("https://safebooru.org/images/%s/%s", p.Directory, p.Image)
		}
		lower := strings.ToLower(u)
		if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".png") {
			valid = append(valid, u)
		}
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("no valid safebooru images")
	}
	return &animeImageResult{
		URL:    valid[rand.Intn(len(valid))],
		Source: "safebooru",
	}, nil
}

func fetchKonachan(ctx context.Context, tag string) (*animeImageResult, error) {
	if tag == "" {
		return nil, fmt.Errorf("empty tag")
	}
	var posts []struct {
		SampleURL string `json:"sample_url"`
		FileURL   string `json:"file_url"`
		Author    string `json:"author"`
	}
	q := "rating:safe " + tag
	apiURL := "https://konachan.net/post.json?limit=20&tags=" + url.QueryEscape(q)
	if err := utils.HTTPGetJSON(ctx, apiURL, animangaHeaders, &posts); err != nil || len(posts) == 0 {
		return nil, fmt.Errorf("konachan empty")
	}
	var valid []animeImageResult
	for _, p := range posts {
		u := p.SampleURL
		if u == "" {
			u = p.FileURL
		}
		lower := strings.ToLower(u)
		if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".png") {
			valid = append(valid, animeImageResult{URL: u, Artist: p.Author, Source: "konachan"})
		}
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("no valid konachan images")
	}
	pick := valid[rand.Intn(len(valid))]
	return &pick, nil
}

func fetchDanbooruResult(ctx context.Context, tag string) (*animeImageResult, error) {
	if tag == "" {
		return nil, fmt.Errorf("empty tag")
	}
	// Anonymous Danbooru limits queries to 2 tags max, so take only the primary tag + rating:general.
	firstTag := strings.Fields(tag)[0]
	tags := firstTag + " rating:general"
	var posts []struct {
		FileURL      string `json:"file_url"`
		LargeFileURL string `json:"large_file_url"`
	}
	apiURL := "https://danbooru.donmai.us/posts.json?limit=15&tags=" + url.QueryEscape(tags)
	if err := utils.HTTPGetJSON(ctx, apiURL, animangaHeaders, &posts); err != nil || len(posts) == 0 {
		return nil, fmt.Errorf("danbooru empty")
	}
	var valid []string
	for _, p := range posts {
		u := p.LargeFileURL
		if u == "" {
			u = p.FileURL
		}
		lower := strings.ToLower(u)
		if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".png") {
			valid = append(valid, u)
		}
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("no valid danbooru images")
	}
	return &animeImageResult{
		URL:    valid[rand.Intn(len(valid))],
		Source: "danbooru",
	}, nil
}

func sendAnimeGalleryResult(ctx context.Context, client *whatsmeow.Client, msg *events.Message, title string, res *animeImageResult) {
	caption := "🌸 *" + title + "*"
	if res.Artist != "" {
		caption += fmt.Sprintf("\n🎨 Artist: %s", res.Artist)
	}
	if res.Source != "" {
		caption += fmt.Sprintf(" • _%s_", res.Source)
	}
	img, ct, err := utils.DownloadHTTPBytes(ctx, res.URL, animangaHeaders)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, caption+"\n"+res.URL)
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, caption)
}

func cmdAnimeGallery(title, nekosCat, nekosiaCat, booruTag string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		var res *animeImageResult
		var err error
		if nekosCat != "" {
			if res, err = fetchNekosBest(ctx, nekosCat); err == nil && res != nil {
				sendAnimeGalleryResult(ctx, client, msg, title, res)
				return
			}
		}
		if nekosiaCat != "" {
			if res, err = fetchNekosia(ctx, nekosiaCat); err == nil && res != nil {
				sendAnimeGalleryResult(ctx, client, msg, title, res)
				return
			}
		}
		if booruTag != "" {
			if res, err = fetchSafebooru(ctx, booruTag); err == nil && res != nil {
				sendAnimeGalleryResult(ctx, client, msg, title, res)
				return
			}
			if res, err = fetchKonachan(ctx, booruTag); err == nil && res != nil {
				sendAnimeGalleryResult(ctx, client, msg, title, res)
				return
			}
			if res, err = fetchDanbooruResult(ctx, booruTag); err == nil && res != nil {
				sendAnimeGalleryResult(ctx, client, msg, title, res)
				return
			}
		}
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch "+strings.ToLower(title)+" image right now. Please try again.")
	}
}

func cmdDanbooruSearch(client *whatsmeow.Client, msg *events.Message, args []string) {
	tag := strings.ToLower(strings.TrimSpace(strings.Join(args, "_")))
	if tag == "" {
		tag = "1girl"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if res, err := fetchDanbooruResult(ctx, tag); err == nil && res != nil {
		sendAnimeGalleryResult(ctx, client, msg, tag, res)
		return
	}
	if res, err := fetchSafebooru(ctx, tag); err == nil && res != nil {
		sendAnimeGalleryResult(ctx, client, msg, tag, res)
		return
	}
	if res, err := fetchKonachan(ctx, tag); err == nil && res != nil {
		sendAnimeGalleryResult(ctx, client, msg, tag, res)
		return
	}
	utils.SendReply(client, msg.Info.Chat, "❌ No artwork found for `"+tag+"`.")
}
