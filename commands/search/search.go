package searchcmd

import (
	"context"
	"fmt"
	"html"
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
		{Name: "google", Category: "search", Description: "Search the web (DuckDuckGo / Wikipedia fallback). Usage: .google <query>", Execute: cmdGoogle},
		{Name: "jadwalsholat", Category: "search", Description: "Get daily Islamic prayer times for a city. Usage: .jadwalsholat Jakarta", Execute: cmdJadwalSholat},
		{Name: "lyric", Aliases: []string{"lyrics", "lirik"}, Category: "search", Description: "Search song lyrics via LRCLIB. Usage: .lyric <song name>", Execute: cmdLyric},
		{Name: "wallhaven", Category: "search", Description: "Search HD wallpapers on Wallhaven. Usage: .wallhaven <query>", Execute: cmdWallhaven},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdGoogle(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.google <search query>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	raw, _, err := utils.DownloadHTTPBytes(ctx, "https://html.duckduckgo.com/html/?q="+url.QueryEscape(q), nil)
	if err == nil && len(raw) > 0 {
		re := regexp.MustCompile(`<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>([^<]+)</a>`)
		matches := re.FindAllStringSubmatch(string(raw), 6)
		if len(matches) > 0 {
			var b strings.Builder
			fmt.Fprintf(&b, "🔎 *Web Search: %s*\n\n", q)
			for i, m := range matches {
				link := m[1]
				if u, e := url.Parse(link); e == nil && u.Query().Get("uddg") != "" {
					link = u.Query().Get("uddg")
				}
				title := html.UnescapeString(strings.TrimSpace(m[2]))
				fmt.Fprintf(&b, "%d. *%s*\n   %s\n", i+1, title, link)
			}
			utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
			return
		}
	}

	// Wikipedia fallback
	var wiki struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://en.wikipedia.org/w/api.php?action=query&list=search&format=json&srlimit=5&srsearch="+url.QueryEscape(q), nil, &wiki); err == nil && len(wiki.Query.Search) > 0 {
		stripTag := regexp.MustCompile(`<[^>]+>`)
		var b strings.Builder
		fmt.Fprintf(&b, "🔎 *Search Results: %s*\n\n", q)
		for i, r := range wiki.Query.Search {
			snip := html.UnescapeString(stripTag.ReplaceAllString(r.Snippet, ""))
			fmt.Fprintf(&b, "%d. *%s*\n   %s\n   https://en.wikipedia.org/wiki/%s\n", i+1, r.Title, snip, url.PathEscape(strings.ReplaceAll(r.Title, " ", "_")))
		}
		utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
		return
	}
	utils.SendReply(client, msg.Info.Chat, "❌ No results found for `"+q+"`.")
}

func cmdJadwalSholat(client *whatsmeow.Client, msg *events.Message, args []string) {
	city := strings.TrimSpace(strings.Join(args, " "))
	if city == "" {
		city = "Jakarta"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp struct {
		Data struct {
			Timings struct {
				Imsak   string `json:"Imsak"`
				Fajr    string `json:"Fajr"`
				Sunrise string `json:"Sunrise"`
				Dhuhr   string `json:"Dhuhr"`
				Asr     string `json:"Asr"`
				Maghrib string `json:"Maghrib"`
				Isha    string `json:"Isha"`
			} `json:"timings"`
			Date struct {
				Readable string `json:"readable"`
			} `json:"date"`
		} `json:"data"`
	}
	apiURL := fmt.Sprintf("https://api.aladhan.com/v1/timingsByCity?city=%s&country=Indonesia&method=20", url.QueryEscape(city))
	if err := utils.HTTPGetJSON(ctx, apiURL, nil, &resp); err != nil || resp.Data.Timings.Fajr == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch prayer schedule for `"+city+"`.")
		return
	}
	t := resp.Data.Timings
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🕌 *Jadwal Sholat — %s* (%s)\n• Imsak: `%s`\n• Subuh: `%s`\n• Terbit: `%s`\n• Dzuhur: `%s`\n• Ashar: `%s`\n• Maghrib: `%s`\n• Isya: `%s`",
		city, resp.Data.Date.Readable, t.Imsak, t.Fajr, t.Sunrise, t.Dhuhr, t.Asr, t.Maghrib, t.Isha,
	))
}

func cmdLyric(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.lyric <song name>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var results []struct {
		TrackName   string `json:"trackName"`
		ArtistName  string `json:"artistName"`
		PlainLyrics string `json:"plainLyrics"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://lrclib.net/api/search?q="+url.QueryEscape(q), nil, &results); err != nil || len(results) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Lyrics not found for `"+q+"`.")
		return
	}
	r := results[0]
	txt := strings.TrimSpace(r.PlainLyrics)
	if len(txt) > 3400 {
		txt = txt[:3400] + "\n...(truncated)"
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🎵 *%s* — _%s_\n\n%s", r.TrackName, r.ArtistName, txt))
}

func cmdWallhaven(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		q = "anime landscape"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var resp struct {
		Data []struct {
			Path       string `json:"path"`
			Resolution string `json:"resolution"`
			URL        string `json:"url"`
		} `json:"data"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://wallhaven.cc/api/v1/search?purity=100&q="+url.QueryEscape(q), nil, &resp); err != nil || len(resp.Data) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ No wallpapers found for `"+q+"`.")
		return
	}
	pick := resp.Data[int(time.Now().UnixNano())%len(resp.Data)]
	img, ct, err := utils.DownloadHTTPBytes(ctx, pick.Path, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "🖼️ Wallhaven: "+pick.URL)
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, img, ct, fmt.Sprintf("🖼️ *Wallhaven* (`%s`)\n%s", pick.Resolution, pick.URL))
}
