package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
		// Social / Video Downloaders (powered by yt-dlp + WhatsApp H.264 MP4 pipeline)
		{Name: "downloader", Category: "media", Description: "Universal media downloader for TikTok, IG, FB, Twitter/X, YouTube, CapCut, etc.", Usage: ".downloader <url>", Execute: cmdGenericYtDlp("Media")},
		{Name: "instagram", Aliases: []string{"ig"}, Category: "media", Description: "Download Instagram Reels/Posts.", Usage: ".instagram <url>", Execute: cmdGenericYtDlp("Instagram")},
		{Name: "facebook", Aliases: []string{"fb"}, Category: "media", Description: "Download Facebook videos/reels.", Usage: ".facebook <url>", Execute: cmdGenericYtDlp("Facebook")},
		{Name: "twitter", Aliases: []string{"x"}, Category: "media", Description: "Download Twitter/X videos.", Usage: ".twitter <url>", Execute: cmdGenericYtDlp("Twitter/X")},
		{Name: "threads", Category: "media", Description: "Download Threads videos.", Usage: ".threads <url>", Execute: cmdGenericYtDlp("Threads")},
		{Name: "douyin", Category: "media", Description: "Download Douyin videos.", Usage: ".douyin <url>", Execute: cmdGenericYtDlp("Douyin")},
		{Name: "capcut", Category: "media", Description: "Download CapCut template videos.", Usage: ".capcut <url>", Execute: cmdGenericYtDlp("CapCut")},
		{Name: "xiaohongshu", Aliases: []string{"xhs"}, Category: "media", Description: "Download Xiaohongshu (RedNote) videos.", Usage: ".xiaohongshu <url>", Execute: cmdGenericYtDlp("Xiaohongshu")},
		{Name: "pixiv", Category: "media", Description: "Fetch Pixiv artwork info/preview by ID or URL.", Usage: ".pixiv <id|url>", Execute: cmdPixiv},
		{Name: "pinterestdownloader", Aliases: []string{"pinterestvideo"}, Category: "media", Description: "Download Pinterest video/pin via URL.", Usage: ".pinterestvideo <url>", Execute: cmdGenericYtDlp("Pinterest")},

		// File host / paste / Minecraft tools
		{Name: "mediafire", Category: "media", Description: "Download file from a public MediaFire link.", Usage: ".mediafire <url>", Execute: cmdMediaFire},
		{Name: "gdrive", Category: "media", Description: "Download file from a public Google Drive link.", Usage: ".gdrive <url>", Execute: cmdGDrive},
		{Name: "pixeldrain", Category: "media", Description: "Download file from a PixelDrain link or ID.", Usage: ".pixeldrain <url|id>", Execute: cmdPixelDrain},
		{Name: "gofile", Category: "media", Description: "Inspect or fetch public Gofile link.", Usage: ".gofile <url>", Execute: cmdGofile},
		{Name: "pastebin", Category: "media", Description: "Fetch raw text from a Pastebin URL or ID.", Usage: ".pastebin <url|id>", Execute: cmdPastebin},
		{Name: "gist", Category: "media", Description: "Fetch files from a GitHub Gist URL or ID.", Usage: ".gist <url|id>", Execute: cmdGist},
		{Name: "minecraft", Category: "media", Description: "Check Minecraft server status or player skin. Usage: .minecraft <server_ip|skin username>", Execute: cmdMinecraft},

		// YouTube & Music extensions
		{Name: "youtube", Category: "media", Description: "Download YouTube video. Usage: .youtube <url|query>", Execute: cmdYouTubeVideo},
		{Name: "ytsearch", Aliases: []string{"ytmusicsearch"}, Category: "media", Description: "Search YouTube / YouTube Music tracks.", Usage: ".ytsearch <query>", Execute: cmdYtSearch},
		{Name: "ytthumb", Category: "media", Description: "Download HD thumbnail of a YouTube video.", Usage: ".ytthumb <url>", Execute: cmdYtThumb},
		{Name: "ytclip", Category: "media", Description: "Clip a YouTube video segment. Usage: .ytclip <url> <start_sec> <end_sec>", Execute: cmdYtClip},
		{Name: "ytsub", Category: "media", Description: "Fetch YouTube subtitles/transcript.", Usage: ".ytsub <url>", Execute: cmdYtSub},
		{Name: "ytmusic", Aliases: []string{"play2", "playdownloader", "album"}, Category: "media", Description: "Search and download music track as MP3.", Usage: ".ytmusic <song name or URL>", Execute: cmdMusicPlay},
		{Name: "spotify", Category: "media", Description: "Resolve a Spotify track URL/query and download matching audio.", Usage: ".spotify <spotify_url or song>", Execute: cmdSpotify},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdGenericYtDlp(label string) func(*whatsmeow.Client, *events.Message, []string) {
	return func(client *whatsmeow.Client, msg *events.Message, args []string) {
		rawURL := utils.ExtractURL(strings.Join(args, " "))
		if rawURL == "" {
			if q := utils.ExtractQuotedOrOwnMessage(msg); q != nil {
				rawURL = utils.ExtractURL(utils.GetTextFromMessage(q))
			}
		}
		if rawURL == "" {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("Usage: reply to or pass a %s URL.", label))
			return
		}
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⏳ Downloading %s media...", label))

		tmpDir, err := os.MkdirTemp("", "nopebot-dl-*")
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Temp dir error: "+err.Error())
			return
		}
		defer os.RemoveAll(tmpDir)

		outRaw := filepath.Join(tmpDir, "video.mp4")
		if err := utils.DownloadVideo(rawURL, outRaw, "720p"); err != nil {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ %s download failed: %v", label, err))
			return
		}
		outReady := filepath.Join(tmpDir, "ready.mp4")
		finalPath := outRaw
		if err := utils.EnsureWhatsAppVideo(outRaw, outReady); err == nil {
			finalPath = outReady
		}
		data, err := os.ReadFile(finalPath)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Failed to read downloaded video.")
			return
		}
		thumb, secs := utils.ExtractVideoMeta(finalPath)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, err := utils.SendVideoData(ctx, client, msg.Info.Chat, data, fmt.Sprintf("✅ %s Download", label), secs, thumb); err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Upload to WhatsApp failed: "+err.Error())
		}
	}
}

func cmdPixiv(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	re := regexp.MustCompile(`\d{5,}`)
	id := re.FindString(raw)
	if id == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.pixiv <illust_id or URL>`")
		return
	}
	imgURL := fmt.Sprintf("https://pixiv.cat/%s.jpg", id)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	data, ct, err := utils.DownloadHTTPBytes(ctx, imgURL, nil)
	if err != nil || !strings.HasPrefix(ct, "image/") {
		imgURL = fmt.Sprintf("https://pixiv.cat/%s-1.jpg", id)
		data, ct, err = utils.DownloadHTTPBytes(ctx, imgURL, nil)
	}
	if err != nil || !strings.HasPrefix(ct, "image/") {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch Pixiv artwork `"+id+"`.")
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, data, ct, fmt.Sprintf("🎨 Pixiv #%s", id))
}

func cmdMediaFire(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawURL := utils.ExtractURL(strings.Join(args, " "))
	if rawURL == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.mediafire <url>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	htmlBytes, _, err := utils.DownloadHTTPBytes(ctx, rawURL, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to fetch MediaFire page: "+err.Error())
		return
	}
	re := regexp.MustCompile(`https://download\d+\.mediafire\.com/[^"'\s<>]+`)
	dlURL := re.FindString(string(htmlBytes))
	if dlURL == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not extract direct download link from MediaFire.")
		return
	}
	data, ct, err := utils.DownloadHTTPBytes(ctx, dlURL, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ File download failed: "+err.Error())
		return
	}
	name := utils.SanitizeFilename(filepath.Base(strings.Split(dlURL, "?")[0]))
	_, _ = utils.SendDocumentData(ctx, client, msg.Info.Chat, data, ct, name, "📦 MediaFire: "+name)
}

func cmdGDrive(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	re := regexp.MustCompile(`(?:/d/|id=)([a-zA-Z0-9_-]{20,})`)
	m := re.FindStringSubmatch(raw)
	if len(m) < 2 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.gdrive <public_google_drive_link>`")
		return
	}
	dlURL := "https://drive.google.com/uc?export=download&id=" + m[1]
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, ct, err := utils.DownloadHTTPBytes(ctx, dlURL, nil)
	if err != nil || strings.Contains(ct, "text/html") {
		utils.SendReply(client, msg.Info.Chat, "⚠️ File is either private or too large for direct GDrive download.\nDirect link: "+dlURL)
		return
	}
	_, _ = utils.SendDocumentData(ctx, client, msg.Info.Chat, data, ct, "gdrive_"+m[1], "☁️ Google Drive File")
}

func cmdPixelDrain(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	id := raw
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		id = raw[idx+1:]
	}
	id = strings.Split(id, "?")[0]
	if id == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.pixeldrain <url|id>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var info struct {
		Name string `json:"name"`
		Mime string `json:"mime_type"`
	}
	_ = utils.HTTPGetJSON(ctx, "https://pixeldrain.com/api/file/"+id+"/info", nil, &info)
	data, ct, err := utils.DownloadHTTPBytes(ctx, "https://pixeldrain.com/api/file/"+id, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ PixelDrain download failed: "+err.Error())
		return
	}
	name := utils.SanitizeFilename(info.Name)
	if name == "media" {
		name = "pixeldrain_" + id
	}
	_, _ = utils.SendDocumentData(ctx, client, msg.Info.Chat, data, ct, name, "📦 PixelDrain: "+name)
}

func cmdGofile(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.gofile <url>`")
		return
	}
	id := raw
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		id = raw[idx+1:]
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📂 *Gofile Folder:* `%s`\nOpen: https://gofile.io/d/%s", id, id))
}

func cmdPastebin(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	id := raw
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		id = raw[idx+1:]
	}
	if id == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.pastebin <url|id>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data, _, err := utils.DownloadHTTPBytes(ctx, "https://pastebin.com/raw/"+id, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Pastebin fetch failed: "+err.Error())
		return
	}
	txt := string(data)
	if len(txt) > 3500 {
		txt = txt[:3500] + "\n...(truncated)"
	}
	utils.SendReply(client, msg.Info.Chat, "📋 *Pastebin (`"+id+"`):*\n```\n"+txt+"\n```")
}

func cmdGist(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	id := raw
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		id = raw[idx+1:]
	}
	if id == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.gist <url|id>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var resp struct {
		Description string `json:"description"`
		Files       map[string]struct {
			Filename string `json:"filename"`
			Content  string `json:"content"`
		} `json:"files"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://api.github.com/gists/"+id, nil, &resp); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Gist fetch failed: "+err.Error())
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🐙 *GitHub Gist:* %s\n\n", resp.Description)
	for _, f := range resp.Files {
		content := f.Content
		if len(content) > 1500 {
			content = content[:1500] + "\n...(truncated)"
		}
		fmt.Fprintf(&b, "*%s*\n```\n%s\n```\n", f.Filename, content)
		if b.Len() > 3500 {
			break
		}
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdMinecraft(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.minecraft <server_ip>` or `.minecraft skin <username>`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fields := strings.Fields(raw)
	if len(fields) >= 2 && strings.EqualFold(fields[0], "skin") {
		user := fields[1]
		data, ct, err := utils.DownloadHTTPBytes(ctx, "https://mc-heads.net/body/"+url.PathEscape(user)+"/256", nil)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ Could not load skin for `"+user+"`.")
			return
		}
		_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, data, ct, "⛏️ Minecraft Skin: *"+user+"*")
		return
	}
	var status struct {
		Online  bool   `json:"online"`
		Host    string `json:"host"`
		Port    int    `json:"port"`
		Version struct {
			NameClean string `json:"name_clean"`
		} `json:"version"`
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
		} `json:"players"`
		MOTD struct {
			Clean string `json:"clean"`
		} `json:"motd"`
	}
	if err := utils.HTTPGetJSON(ctx, "https://api.mcstatus.io/v2/status/java/"+url.PathEscape(fields[0]), nil, &status); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not query server: "+err.Error())
		return
	}
	if !status.Online {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("⛏️ Server `%s` is *OFFLINE*.", fields[0]))
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"⛏️ *Minecraft Server Online*\n• Host: `%s:%d`\n• Version: `%s`\n• Players: `%d/%d`\n• MOTD:\n%s",
		status.Host, status.Port, status.Version.NameClean, status.Players.Online, status.Players.Max, status.MOTD.Clean,
	))
}

func cmdYouTubeVideo(client *whatsmeow.Client, msg *events.Message, args []string) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.youtube <url or search query>`")
		return
	}
	targetURL := utils.ExtractURL(query)
	if targetURL == "" {
		results, err := utils.SearchYouTube(query, 1)
		if err != nil || len(results) == 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ No YouTube video found for `"+query+"`.")
			return
		}
		targetURL = results[0].URL
	}
	cmdGenericYtDlp("YouTube")(client, msg, []string{targetURL})
}

func cmdYtSearch(client *whatsmeow.Client, msg *events.Message, args []string) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.ytsearch <query>`")
		return
	}
	results, err := utils.SearchYouTube(query, 8)
	if err != nil || len(results) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ No results found.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🔎 *YouTube Search: %s*\n\n", query)
	for i, r := range results {
		dur := fmt.Sprintf("%d:%02d", int(r.Duration)/60, int(r.Duration)%60)
		fmt.Fprintf(&b, "%d. *%s* (`%s`)\n   %s\n", i+1, r.Title, dur, r.URL)
	}
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func cmdYtThumb(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawURL := utils.ExtractURL(strings.Join(args, " "))
	if rawURL == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.ytthumb <youtube_url>`")
		return
	}
	info, err := utils.GetVideoInfo(rawURL, "best")
	if err != nil || info == nil || info.Thumbnail == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch thumbnail.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data, ct, err := utils.DownloadHTTPBytes(ctx, info.Thumbnail, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Thumbnail download failed.")
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, data, ct, "🖼️ "+info.Title)
}

func cmdYtClip(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) < 3 {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.ytclip <youtube_url> <start_sec> <end_sec>` (e.g. `.ytclip https://youtu.be/... 10 25`)")
		return
	}
	rawURL, startSec, endSec := fields[0], fields[1], fields[2]
	utils.SendReply(client, msg.Info.Chat, "✂️ Clipping YouTube segment...")
	tmpDir, err := os.MkdirTemp("", "nopebot-ytclip-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpDir)

	fullPath := filepath.Join(tmpDir, "full.mp4")
	if err := utils.DownloadVideo(rawURL, fullPath, "480p"); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Download failed: "+err.Error())
		return
	}
	ffPath, cleanup, err := utils.GetFFmpegPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ FFmpeg unavailable.")
		return
	}
	defer cleanup()

	clipPath := filepath.Join(tmpDir, "clip.mp4")
	cmd := exec.Command(ffPath, "-y", "-ss", startSec, "-to", endSec, "-i", fullPath, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart", clipPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Clip failed: %v\n%s", err, string(out)))
		return
	}
	data, _ := os.ReadFile(clipPath)
	thumb, secs := utils.ExtractVideoMeta(clipPath)
	_, _ = utils.SendVideoData(context.Background(), client, msg.Info.Chat, data, fmt.Sprintf("✂️ Clip %ss-%ss", startSec, endSec), secs, thumb)
}

func cmdYtSub(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawURL := utils.ExtractURL(strings.Join(args, " "))
	if rawURL == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.ytsub <youtube_url>`")
		return
	}
	ytPath, cleanup, err := utils.GetYtDlpPath()
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ yt-dlp unavailable.")
		return
	}
	defer cleanup()
	tmpDir, err := os.MkdirTemp("", "nopebot-ytsub-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ytPath, "--skip-download", "--write-auto-sub", "--write-sub", "--sub-lang", "en,id", "--convert-subs", "srt", "-o", filepath.Join(tmpDir, "sub.%(ext)s"), rawURL)
	_ = cmd.Run()

	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".srt") || strings.HasSuffix(e.Name(), ".vtt") {
			data, _ := os.ReadFile(filepath.Join(tmpDir, e.Name()))
			_, _ = utils.SendDocumentData(context.Background(), client, msg.Info.Chat, data, "text/plain", e.Name(), "📝 Subtitles: "+e.Name())
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, "❌ No subtitles available for this video.")
}

func cmdMusicPlay(client *whatsmeow.Client, msg *events.Message, args []string) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.ytmusic <song name or URL>`")
		return
	}
	targetURL := utils.ExtractURL(query)
	title := query
	if targetURL == "" {
		results, err := utils.SearchYouTube(query+" audio", 1)
		if err != nil || len(results) == 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ No track found for `"+query+"`.")
			return
		}
		targetURL = results[0].URL
		title = results[0].Title
	}
	utils.SendReply(client, msg.Info.Chat, "🎧 Downloading audio: *"+title+"*...")

	tmpDir, err := os.MkdirTemp("", "nopebot-music-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpDir)

	outMP3 := filepath.Join(tmpDir, "track.mp3")
	if err := utils.DownloadAudioMP3(targetURL, outMP3); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Audio download failed: "+err.Error())
		return
	}
	data, err := os.ReadFile(outMP3)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to read MP3.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, _ = utils.SendAudioData(ctx, client, msg.Info.Chat, data, "audio/mpeg", 0, false)
}

func cmdSpotify(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.spotify <spotify_url or song name>`")
		return
	}
	searchQuery := raw
	if spURL := utils.ExtractURL(raw); spURL != "" && strings.Contains(spURL, "spotify.com") {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var oembed struct {
			Title string `json:"title"`
		}
		if err := utils.HTTPGetJSON(ctx, "https://open.spotify.com/oembed?url="+url.QueryEscape(spURL), nil, &oembed); err == nil && oembed.Title != "" {
			searchQuery = oembed.Title
		} else {
			// Fallback parse JSON-LD/title
			if page, _, err := utils.DownloadHTTPBytes(ctx, spURL, nil); err == nil {
				var m map[string]any
				_ = json.Unmarshal(page, &m)
				re := regexp.MustCompile(`<title>([^<]+)</title>`)
				if sm := re.FindSubmatch(page); len(sm) >= 2 {
					searchQuery = strings.TrimSuffix(string(sm[1]), " | Spotify")
				}
			}
		}
	}
	cmdMusicPlay(client, msg, []string{searchQuery})
}
