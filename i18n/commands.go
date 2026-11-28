package i18n

import "strings"

// Canonical category identifiers (9 clean groups total: 8 user + 1 owner-only).
const (
	CatGeneral = "general"
	CatMedia   = "media"
	CatTools   = "tools"
	CatGames   = "games"
	CatRPG     = "rpg"
	CatMarket  = "market"
	CatAnime   = "anime"
	CatGroup   = "group"
	CatOwner   = "owner"
)

// CategoryMeta holds bilingual titles, emoji, and accepted query aliases.
type CategoryMeta struct {
	Key     string
	Emoji   string
	TitleEN string
	TitleID string
	Aliases []string
}

// CanonicalCategoryList defines the display order of all 9 categories.
var CanonicalCategoryList = []CategoryMeta{
	{
		Key:     CatGeneral,
		Emoji:   "📋",
		TitleEN: "General & Profile",
		TitleID: "Umum & Profil",
		Aliases: []string{"general", "main", "profile", "confess", "menfess", "info", "umum", "profil"},
	},
	{
		Key:     CatMedia,
		Emoji:   "📥",
		TitleEN: "Downloader & Media",
		TitleID: "Unduhan & Media",
		Aliases: []string{"media", "downloader", "download", "dl", "youtube", "yt", "music", "musik", "sticker", "stiker", "audiofx", "audio", "sound", "unduhan"},
	},
	{
		Key:     CatTools,
		Emoji:   "🛠️",
		TitleEN: "Tools & Search",
		TitleID: "Alat & Pencarian",
		Aliases: []string{"tools", "tool", "search", "util", "utility", "alat", "pencarian", "cari"},
	},
	{
		Key:     CatGames,
		Emoji:   "🎮",
		TitleEN: "Games & Fun",
		TitleID: "Game & Hiburan",
		Aliases: []string{"games", "game", "minigame", "minigames", "fun", "hiburan", "permainan", "arcade"},
	},
	{
		Key:     CatRPG,
		Emoji:   "⚔️",
		TitleEN: "RPG",
		TitleID: "RPG",
		Aliases: []string{"rpg", "empire", "coinsteel", "kerajaan"},
	},
	{
		Key:     CatMarket,
		Emoji:   "📈",
		TitleEN: "Market",
		TitleID: "Pasar",
		Aliases: []string{"market", "pasar", "crypto", "stocks", "saham", "trading"},
	},
	{
		Key:     CatAnime,
		Emoji:   "🌸",
		TitleEN: "Anime",
		TitleID: "Anime",
		Aliases: []string{"anime", "animanga", "manga", "waifu", "wibu"},
	},
	{
		Key:     CatGroup,
		Emoji:   "🛡️",
		TitleEN: "Group",
		TitleID: "Grup",
		Aliases: []string{"group", "group_admin", "group_security", "groupadmin", "groupsec", "grup", "admin", "security", "keamanan"},
	},
	{
		Key:     CatOwner,
		Emoji:   "👑",
		TitleEN: "Owner",
		TitleID: "Pemilik",
		Aliases: []string{"owner", "bot", "pemilik", "system", "sistem"},
	},
}

var categoryAliasMap = func() map[string]string {
	m := make(map[string]string, 64)
	for _, c := range CanonicalCategoryList {
		m[c.Key] = c.Key
		m[strings.ToLower(c.TitleEN)] = c.Key
		m[strings.ToLower(c.TitleID)] = c.Key
		for _, a := range c.Aliases {
			m[strings.ToLower(a)] = c.Key
		}
	}
	return m
}()

var categoryMetaMap = func() map[string]CategoryMeta {
	m := make(map[string]CategoryMeta, len(CanonicalCategoryList))
	for _, c := range CanonicalCategoryList {
		m[c.Key] = c
	}
	return m
}()

// NormalizeCategory maps any legacy, English, or Indonesian category name to
// one of the 9 canonical category keys.
func NormalizeCategory(raw string) string {
	q := strings.ToLower(strings.TrimSpace(raw))
	if q == "" {
		return CatGeneral
	}
	if key, ok := categoryAliasMap[q]; ok {
		return key
	}
	qNorm := strings.ReplaceAll(q, " ", "_")
	if key, ok := categoryAliasMap[qNorm]; ok {
		return key
	}
	return q
}

// CategoryEmoji returns the emoji for a canonical (or legacy) category.
func CategoryEmoji(cat string) string {
	if meta, ok := categoryMetaMap[NormalizeCategory(cat)]; ok {
		return meta.Emoji
	}
	return "📁"
}

// CategoryTitle returns the localized category title in lang ("id" or "en").
func CategoryTitle(lang, cat string) string {
	key := NormalizeCategory(cat)
	if meta, ok := categoryMetaMap[key]; ok {
		if strings.EqualFold(strings.TrimSpace(lang), LangID) {
			return meta.TitleID
		}
		return meta.TitleEN
	}
	return strings.ToUpper(key)
}

// MatchCategory resolves a user's menu query (canonical key, English/Indonesian
// title, legacy alias, or 3+ char prefix) against the allowed categories list.
func MatchCategory(cats []string, query string) (string, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return "", false
	}
	allowed := make(map[string]bool, len(cats))
	for _, c := range cats {
		allowed[c] = true
	}

	// 1. Exact canonical key or alias match (supports EN, ID, and legacy names).
	if mapped, ok := categoryAliasMap[q]; ok && allowed[mapped] {
		return mapped, true
	}
	qNorm := strings.ReplaceAll(q, " ", "_")
	if mapped, ok := categoryAliasMap[qNorm]; ok && allowed[mapped] {
		return mapped, true
	}

	// 2. Prefix match (3+ chars) against canonical key, EN title, ID title, or aliases.
	if len(q) >= 3 {
		for _, c := range cats {
			meta, ok := categoryMetaMap[c]
			if !ok {
				if strings.HasPrefix(strings.ToLower(c), q) {
					return c, true
				}
				continue
			}
			if strings.HasPrefix(meta.Key, q) ||
				strings.HasPrefix(strings.ToLower(meta.TitleEN), q) ||
				strings.HasPrefix(strings.ToLower(meta.TitleID), q) {
				return c, true
			}
			for _, a := range meta.Aliases {
				if strings.HasPrefix(a, q) {
					return c, true
				}
			}
		}
	}
	return "", false
}

// CommandDesc holds refined bilingual descriptions for a command.
type CommandDesc struct {
	EN string
	ID string
}

// CommandDescription returns the refined description for cmdName in lang
// ("id" or "en"), falling back cleanly to fallbackEN when not cataloged.
func CommandDescription(lang, cmdName, fallbackEN string) string {
	key := strings.ToLower(strings.TrimSpace(cmdName))
	isID := strings.EqualFold(strings.TrimSpace(lang), LangID)
	if entry, ok := commandDescriptions[key]; ok {
		if isID && entry.ID != "" {
			return entry.ID
		}
		if entry.EN != "" {
			return entry.EN
		}
	}
	return strings.TrimSpace(fallbackEN)
}

// HasBilingualCommandDescription reports whether cmdName has both non-empty EN
// and ID descriptions registered in the refined command catalog.
func HasBilingualCommandDescription(cmdName string) (hasEN, hasID bool) {
	key := strings.ToLower(strings.TrimSpace(cmdName))
	entry, ok := commandDescriptions[key]
	if !ok {
		return false, false
	}
	return strings.TrimSpace(entry.EN) != "", strings.TrimSpace(entry.ID) != ""
}

var commandDescriptions = map[string]CommandDesc{
	// ==================== GENERAL & PROFILE ====================
	"help":           {EN: "Open the interactive command menu. Usage: .menu [category|command|all]", ID: "Buka menu perintah interaktif. Contoh: .menu [kategori|perintah|all]"},
	"about":          {EN: "Show information about NopeBot.", ID: "Tampilkan informasi tentang NopeBot."},
	"ping":           {EN: "Check bot response latency and uptime.", ID: "Cek kecepatan respons dan waktu aktif bot."},
	"owner":          {EN: "Show bot owner contact information.", ID: "Tampilkan kontak pemilik bot."},
	"sc":             {EN: "Show bot source and build information.", ID: "Tampilkan informasi sumber dan build bot."},
	"limit":          {EN: "Check your account tier, balance, and activity stats.", ID: "Cek status akun, saldo koin, dan statistik aktivitasmu."},
	"cekbenefit":     {EN: "View available NopeBot features and perks.", ID: "Lihat daftar fitur dan keuntungan NopeBot."},
	"balance":        {EN: "Check your coin balance and level.", ID: "Cek saldo koin dan level kamu."},
	"balancehistory": {EN: "View your recent coin transaction history.", ID: "Lihat riwayat transaksi koin terakhirmu."},
	"daily":          {EN: "Claim your daily coin and XP reward.", ID: "Klaim hadiah koin dan XP harianmu."},
	"redeem":         {EN: "Redeem a promo reward code. Usage: .redeem <CODE>", ID: "Tukarkan kode promo hadiah. Contoh: .redeem <KODE>"},
	"setlang":        {EN: "Switch language between Indonesian (id) and English (en). Usage: .setlang <id|en> [group]", ID: "Ubah bahasa ke Indonesia (id) atau Inggris (en). Contoh: .setlang <id|en> [group]"},
	"top":            {EN: "View top users by balance, XP, or messages. Usage: .top [balance|xp|msg]", ID: "Lihat peringkat teratas berdasarkan koin, XP, atau pesan. Contoh: .top [balance|xp|msg]"},
	"profile":        {EN: "View your profile or a mentioned user's profile.", ID: "Lihat profilmu atau profil pengguna yang disebut."},
	"level":          {EN: "Check your current XP and level progress.", ID: "Cek progres XP dan level kamu saat ini."},
	"afk":            {EN: "Set your AFK status with an optional reason. Usage: .afk [reason]", ID: "Aktifkan status AFK dengan alasan opsional. Contoh: .afk [alasan]"},
	"cekpremium":     {EN: "Check your active premium status.", ID: "Cek status premium akunmu."},
	"getpp":          {EN: "Fetch your own or a mentioned user's profile picture.", ID: "Ambil foto profil kamu atau pengguna yang disebut/dibalas."},
	"setname":        {EN: "Update your profile display name. Usage: .setname <name>", ID: "Ubah nama tampilan profilmu. Contoh: .setname <nama>"},
	"setage":         {EN: "Update your profile age. Usage: .setage <number>", ID: "Ubah usia di profilmu. Contoh: .setage <angka>"},
	"setgender":      {EN: "Update your profile gender. Usage: .setgender <male|female|other>", ID: "Ubah gender di profilmu. Contoh: .setgender <male|female|other>"},
	"setbio":         {EN: "Update your profile bio. Usage: .setbio <text>", ID: "Ubah bio profilmu. Contoh: .setbio <teks>"},
	"setprefix":      {EN: "Set a personal command prefix. Usage: .setprefix <symbol|reset>", ID: "Atur prefix perintah pribadimu. Contoh: .setprefix <simbol|reset>"},
	"linkaccount":    {EN: "Get your WhatsApp Web UI & Arcade link (Owner gets OTP login code).", ID: "Dapatkan tautan Web UI & Arcade (Owner mendapat kode OTP login)."},
	"confess":        {EN: "Send an anonymous message via DM. Usage: .confess <phone> | <message> [| alias]", ID: "Kirim pesan rahasia (menfess) anonim lewat DM. Contoh: .confess <nomor> | <pesan> [| samaran]"},
	"balasconfess":   {EN: "Reply anonymously to a confession thread. Usage: .balasconfess <ID> <reply>", ID: "Balas pesan confess secara anonim. Contoh: .balasconfess <ID> <balasan>"},
	"stopconfess":    {EN: "Close an active anonymous confession thread. Usage: .stopconfess <ID>", ID: "Tutup sesi confess yang sedang aktif. Contoh: .stopconfess <ID>"},
	"blockconfess":   {EN: "Anonymously block a confession sender and close the thread. Usage: .blockconfess <ID>", ID: "Blokir pengirim confess secara anonim dan tutup sesi. Contoh: .blockconfess <ID>"},
	"confessmode":    {EN: "Enable or disable receiving anonymous confessions. Usage: .confessmode on|off", ID: "Aktifkan atau matikan penerimaan confess anonim. Contoh: .confessmode on|off"},
	"confessinbox":   {EN: "View your recent anonymous confession threads.", ID: "Lihat riwayat pesan confess anonim kamu."},

	// ==================== DOWNLOADER & MEDIA ====================
	"dl":                  {EN: "Download media from Instagram, TikTok, X, Facebook, Reddit, YouTube, etc.", ID: "Unduh media dari Instagram, TikTok, X, Facebook, Reddit, YouTube, dll."},
	"downloader":          {EN: "Universal social media downloader. Usage: .downloader <url>", ID: "Pengunduh media sosial serbaguna. Contoh: .downloader <tautan>"},
	"tiktok":              {EN: "Download TikTok videos or photo slideshows without watermark.", ID: "Unduh video atau slideshow foto TikTok tanpa watermark."},
	"instagram":           {EN: "Download Instagram Reels, posts, and carousels.", ID: "Unduh Reels, postingan, dan galeri Instagram."},
	"facebook":            {EN: "Download Facebook videos and Reels.", ID: "Unduh video dan Reels Facebook."},
	"twitter":             {EN: "Download Twitter / X videos and media.", ID: "Unduh video dan media dari Twitter / X."},
	"threads":             {EN: "Download Threads videos and media.", ID: "Unduh video dan media dari Threads."},
	"capcut":              {EN: "Download CapCut template videos.", ID: "Unduh video template CapCut."},
	"douyin":              {EN: "Download Douyin videos.", ID: "Unduh video dari Douyin."},
	"xiaohongshu":         {EN: "Download Xiaohongshu (RedNote) videos.", ID: "Unduh video dari Xiaohongshu (RedNote)."},
	"pinterestdownloader": {EN: "Download Pinterest video or image from a link.", ID: "Unduh video atau gambar dari tautan Pinterest."},
	"pixiv":               {EN: "Fetch Pixiv artwork info and preview by ID or URL.", ID: "Ambil info dan pratinjau karya Pixiv lewat ID atau tautan."},
	"gdrive":              {EN: "Download a public file from Google Drive.", ID: "Unduh file publik dari Google Drive."},
	"mediafire":           {EN: "Download a public file from MediaFire.", ID: "Unduh file publik dari MediaFire."},
	"gofile":              {EN: "Inspect or download from a public Gofile link.", ID: "Periksa atau unduh file dari tautan publik Gofile."},
	"pixeldrain":          {EN: "Download a file from a PixelDrain link or ID.", ID: "Unduh file dari tautan atau ID PixelDrain."},
	"gist":                {EN: "Fetch files from a GitHub Gist URL or ID.", ID: "Ambil isi file dari tautan atau ID GitHub Gist."},
	"pastebin":            {EN: "Fetch raw text from a Pastebin URL or ID.", ID: "Ambil teks mentah dari tautan atau ID Pastebin."},
	"minecraft":           {EN: "Check Minecraft server status or player skin. Usage: .minecraft <ip|skin user>", ID: "Cek status server Minecraft atau skin pemain. Contoh: .minecraft <ip|skin nama>"},
	"youtube":             {EN: "Search or download a YouTube video. Usage: .youtube <url|query>", ID: "Cari atau unduh video YouTube. Contoh: .youtube <tautan|judul>"},
	"ytmp4":               {EN: "Search or download a YouTube video as MP4.", ID: "Cari atau unduh video YouTube dalam format MP4."},
	"ytmp3":               {EN: "Search or download YouTube audio as MP3.", ID: "Cari atau unduh audio YouTube dalam format MP3."},
	"ytclip":              {EN: "Clip a specific time range from a YouTube video. Usage: .ytclip <url> <start> <end>", ID: "Potong bagian waktu tertentu dari video YouTube. Contoh: .ytclip <tautan> <mulai> <akhir>"},
	"ytsearch":            {EN: "Search YouTube and YouTube Music tracks.", ID: "Cari video di YouTube atau lagu di YouTube Music."},
	"ytsub":               {EN: "Fetch subtitles or transcript from a YouTube video.", ID: "Ambil subtitle atau transkrip dari video YouTube."},
	"ytthumb":             {EN: "Download the HD thumbnail of a YouTube video.", ID: "Unduh thumbnail HD dari video YouTube."},
	"spotify":             {EN: "Search or resolve a Spotify track and download its audio.", ID: "Cari atau ambil lagu Spotify dan unduh audionya."},
	"ytmusic":             {EN: "Search and download a music track as MP3.", ID: "Cari dan unduh lagu musik dalam format MP3."},
	"sticker":             {EN: "Convert a photo, GIF, or short video into a WebP sticker.", ID: "Ubah foto, GIF, atau video pendek menjadi stiker WebP."},
	"smeme":               {EN: "Make a brat sticker from text, or a meme sticker from a replied photo.", ID: "Buat stiker brat dari teks, atau stiker meme dari foto yang dibalas."},
	"soundsticker":        {EN: "Create a Sound Sticker from video with audio, or image + voice note.", ID: "Buat Stiker Bersuara dari video, atau gabungkan foto/stiker dengan audio."},
	"qc":                  {EN: "Generate a WhatsApp quote-bubble sticker. Usage: .qc <text>", ID: "Buat stiker gelembung kutipan WhatsApp. Contoh: .qc <teks>"},
	"emojimix":            {EN: "Combine two emojis with Emoji Kitchen. Usage: .emojimix 😂+🔥", ID: "Gabungkan dua emoji lewat Emoji Kitchen. Contoh: .emojimix 😂+🔥"},
	"getexif":             {EN: "Inspect pack name and author metadata of a replied sticker.", ID: "Lihat metadata nama paket dan pembuat stiker yang dibalas."},
	"linesticker":         {EN: "Download stickers from a public LINE Store pack URL.", ID: "Unduh stiker dari tautan paket publik LINE Store."},
	"togif":               {EN: "Convert a replied animated sticker into an MP4 video.", ID: "Ubah stiker animasi yang dibalas menjadi video MP4."},
	"toimage":             {EN: "Convert a replied WebP sticker into a PNG image.", ID: "Ubah stiker WebP yang dibalas menjadi gambar PNG."},
	"shazam":              {EN: "Identify a song from a replied audio or video clip.", ID: "Kenali judul lagu dari audio atau video yang dibalas."},
	"audioeffect":         {EN: "Apply FFmpeg audio effects (bass, nightcore, 8d, robot, etc.) to replied media.", ID: "Terapkan efek audio FFmpeg (bass, nightcore, 8d, robot, dll.) pada media yang dibalas."},

	// ==================== TOOLS & SEARCH ====================
	"get":            {EN: "Save replied media (including View-Once) to your private chat.", ID: "Simpan media yang dibalas (termasuk Sekali Lihat) ke chat pribadimu."},
	"readviewonce":   {EN: "Recover replied View-Once media to your private chat.", ID: "Buka dan simpan media Sekali Lihat yang dibalas ke chat pribadimu."},
	"whatmusic":      {EN: "Identify a song from a replied audio or video clip.", ID: "Kenali judul lagu dari audio atau video yang dibalas."},
	"google":         {EN: "Search the web via DuckDuckGo / Wikipedia. Usage: .google <query>", ID: "Cari informasi di web. Contoh: .google <kata kunci>"},
	"pinterest":      {EN: "Search Pinterest images or download from a Pinterest link.", ID: "Cari gambar di Pinterest atau unduh dari tautan Pinterest."},
	"wallhaven":      {EN: "Search HD wallpapers on Wallhaven. Usage: .wallhaven <query>", ID: "Cari wallpaper HD di Wallhaven. Contoh: .wallhaven <kata kunci>"},
	"lyric":          {EN: "Search song lyrics. Usage: .lyric <song name>", ID: "Cari lirik lagu. Contoh: .lyric <judul lagu>"},
	"generatelrc":    {EN: "Fetch synced .lrc lyrics file for a song. Usage: .generatelrc <song>", ID: "Ambil file lirik tersinkronisasi (.lrc) untuk lagu. Contoh: .generatelrc <lagu>"},
	"jadwalsholat":   {EN: "Check daily Islamic prayer times for a city. Usage: .jadwalsholat Jakarta", ID: "Cek jadwal sholat harian untuk suatu kota. Contoh: .jadwalsholat Jakarta"},
	"cuaca":          {EN: "Check live weather forecast for a city. Usage: .cuaca Jakarta", ID: "Cek prakiraan cuaca terkini untuk suatu kota. Contoh: .cuaca Jakarta"},
	"gempa":          {EN: "Get the latest Indonesia earthquake update from BMKG.", ID: "Lihat info gempa bumi terbaru di Indonesia dari BMKG."},
	"translate":      {EN: "Translate text to another language. Usage: .translate <lang> <text>", ID: "Terjemahkan teks ke bahasa lain. Contoh: .translate en halo dunia"},
	"tts":            {EN: "Convert text into a speech voice note. Usage: .tts [lang] <text>", ID: "Ubah teks menjadi pesan suara (TTS). Contoh: .tts [bahasa] <teks>"},
	"upload":         {EN: "Upload replied media to temporary file hosting and get a link.", ID: "Unggah media yang dibalas ke hosting sementara dan dapatkan tautannya."},
	"calculator":     {EN: "Evaluate a math expression. Usage: .calc (12+8)*3/4", ID: "Hitung ekspresi matematika. Contoh: .calc (12+8)*3/4"},
	"color":          {EN: "Preview a hex color swatch with RGB/HSL values. Usage: .color #00a884", ID: "Pratinjau warna hex beserta nilai RGB/HSL. Contoh: .color #00a884"},
	"qr":             {EN: "Generate a QR code PNG from text or URL. Usage: .qr <text>", ID: "Buat gambar kode QR dari teks atau tautan. Contoh: .qr <teks>"},
	"barcode":        {EN: "Generate a barcode PNG from text. Usage: .barcode <text>", ID: "Buat gambar barcode dari teks. Contoh: .barcode <teks>"},
	"coascii":        {EN: "Convert a replied image into ASCII art.", ID: "Ubah gambar yang dibalas menjadi seni teks ASCII."},
	"crop":           {EN: "Center-crop a replied image into a 1:1 square.", ID: "Potong tengah gambar yang dibalas menjadi persegi 1:1."},
	"resize":         {EN: "Resize a replied image. Usage: .resize <width> <height>", ID: "Ubah ukuran gambar yang dibalas. Contoh: .resize <lebar> <tinggi>"},
	"rotate":         {EN: "Rotate a replied image clockwise (90/180/270). Usage: .rotate 90", ID: "Putar gambar yang dibalas searah jarum jam (90/180/270). Contoh: .rotate 90"},
	"flip":           {EN: "Flip a replied image horizontally or vertically. Usage: .flip [h|v]", ID: "Balik gambar yang dibalas secara horizontal atau vertikal. Contoh: .flip [h|v]"},
	"blur":           {EN: "Apply a blur effect to a replied image.", ID: "Berikan efek buram (blur) pada gambar yang dibalas."},
	"pixelate":       {EN: "Apply a pixelate effect to a replied image.", ID: "Berikan efek piksel pada gambar yang dibalas."},
	"jadiburik":      {EN: "Compress a replied image into ultra-low quality JPEG.", ID: "Ubah gambar yang dibalas menjadi kualitas super burik (rendah)."},
	"stitch":         {EN: "Mirror-stitch a replied image side by side.", ID: "Gabungkan gambar yang dibalas secara simetris berdampingan."},
	"testimage":      {EN: "Generate an SMPTE color bar test pattern image.", ID: "Buat gambar pola uji warna SMPTE."},
	"iqc":            {EN: "Generate an iPhone-style quote card. Usage: .iqc <text>", ID: "Buat kartu kutipan bergaya notifikasi iPhone. Contoh: .iqc <teks>"},
	"tomp3":          {EN: "Extract MP3 audio from a replied video or voice note.", ID: "Ekstrak audio MP3 dari video atau pesan suara yang dibalas."},
	"topdf":          {EN: "Convert a replied image into a PDF document.", ID: "Ubah gambar yang dibalas menjadi dokumen PDF."},
	"trim":           {EN: "Trim a replied video or audio clip. Usage: .trim <start_sec> <end_sec>", ID: "Potong durasi video atau audio yang dibalas. Contoh: .trim <detik_awal> <detik_akhir>"},
	"compress":       {EN: "Compress a replied video or image for smaller file size.", ID: "Kompres ukuran file video atau gambar yang dibalas."},
	"mediaquality":   {EN: "Inspect resolution, codec, bitrate, and duration of replied media.", ID: "Periksa resolusi, codec, bitrate, dan durasi media yang dibalas."},
	"todocmsg":       {EN: "Resend a replied media file as a WhatsApp document.", ID: "Kirim ulang media yang dibalas sebagai dokumen WhatsApp."},
	"getmsgid":       {EN: "Show the message ID of a replied message.", ID: "Tampilkan ID pesan dari pesan yang dibalas."},
	"getidch":        {EN: "Extract channel JID from a WhatsApp Channel invite link.", ID: "Ambil ID saluran (Channel) dari tautan undangan WhatsApp Channel."},
	"whatsappstatus": {EN: "Show current WhatsApp connection and companion status.", ID: "Tampilkan status koneksi WhatsApp dan mode Companion saat ini."},
	"encode":         {EN: "Encode text (b64, b32, b85, hex, bin, oct, rot13, atbash, morse, url, html).", ID: "Encode teks (b64, b32, b85, hex, bin, oct, rot13, atbash, morse, url, html)."},
	"decode":         {EN: "Decode text (b64, b32, b85, hex, bin, oct, rot13, atbash, morse, url, html, jwt).", ID: "Decode teks (b64, b32, b85, hex, bin, oct, rot13, atbash, morse, url, html, jwt)."},
	"b64":            {EN: "Base64 encode or decode text. Usage: .b64 [enc|dec] <text>", ID: "Encode atau decode Base64. Contoh: .b64 [enc|dec] <teks>"},
	"b32":            {EN: "Base32 encode or decode text. Usage: .b32 [enc|dec] <text>", ID: "Encode atau decode Base32. Contoh: .b32 [enc|dec] <teks>"},
	"ascii85":        {EN: "ASCII85 / Base85 encode or decode text.", ID: "Encode atau decode teks ASCII85 / Base85."},
	"hex":            {EN: "Hexadecimal encode or decode text.", ID: "Encode atau decode teks heksadesimal."},
	"bin":            {EN: "Binary encode or decode text.", ID: "Encode atau decode teks biner."},
	"oct":            {EN: "Octal encode or decode text.", ID: "Encode atau decode teks oktal."},
	"rot13":          {EN: "Apply ROT13 cipher to text.", ID: "Terapkan sandi ROT13 pada teks."},
	"atbash":         {EN: "Apply Atbash cipher to text.", ID: "Terapkan sandi Atbash pada teks."},
	"morse":          {EN: "Encode or decode Morse code.", ID: "Encode atau decode sandi Morse."},
	"url":            {EN: "URL percent-encode or decode text.", ID: "Encode atau decode format URL."},
	"html":           {EN: "Escape or unescape HTML entities.", ID: "Escape atau unescape entitas HTML."},
	"jwt":            {EN: "Decode a JWT header and payload.", ID: "Dekode header dan isi payload token JWT."},
	"upper":          {EN: "Convert text to UPPERCASE.", ID: "Ubah teks menjadi HURUF KAPITAL."},
	"lower":          {EN: "Convert text to lowercase.", ID: "Ubah teks menjadi huruf kecil."},
	"reverse":        {EN: "Reverse the characters of a text.", ID: "Balik urutan karakter pada teks."},
	"lorem":          {EN: "Generate Lorem Ipsum placeholder text. Usage: .lorem [paragraphs]", ID: "Buat teks contoh Lorem Ipsum. Contoh: .lorem [jumlah_paragraf]"},
	"readmore":       {EN: "Create a WhatsApp Read More spoiler message. Usage: .readmore front|hidden", ID: "Buat pesan spoiler Baca Selengkapnya. Contoh: .readmore depan|tersembunyi"},

	// ==================== GAMES & FUN ====================
	"cek":               {EN: "Fun daily meter (luck, smart, khodam, etc.). Usage: .cek <type> [name]", ID: "Cek meteran lucu harian (khodam, ganteng, hoki, dll.). Contoh: .cek <jenis> [nama]"},
	"dadu":              {EN: "Roll a 6-sided (or N-sided) die. Usage: .dadu [sides]", ID: "Lempar dadu 6 sisi (atau N sisi). Contoh: .dadu [jumlah_sisi]"},
	"koin":              {EN: "Flip a coin (Heads or Tails).", ID: "Lempar koin (Angka atau Gambar)."},
	"suit":              {EN: "Play Rock-Paper-Scissors against the bot. Usage: .suit <batu|gunting|kertas>", ID: "Main Suit (Batu-Gunting-Kertas) lawan bot. Contoh: .suit <batu|gunting|kertas>"},
	"ship":              {EN: "Calculate love compatibility between two names. Usage: .ship Alice | Bob", ID: "Hitung kecocokan dua nama. Contoh: .ship Budi | Ani"},
	"roast":             {EN: "Generate a playful roast for a name or mentioned user.", ID: "Buat candaan roasting ringan untuk nama atau pengguna yang disebut."},
	"fact":              {EN: "Fetch a random fun fact.", ID: "Tampilkan fakta menarik acak."},
	"debate":            {EN: "Generate a fun debate topic for the chat.", ID: "Buat topik debat seru untuk grup."},
	"mostlikely":        {EN: "Generate a 'Who is most likely to...' prompt.", ID: "Buat pertanyaan 'Siapa yang paling mungkin...' untuk grup."},
	"truthordare":       {EN: "Get a Truth or Dare challenge. Usage: .tod [truth|dare]", ID: "Dapatkan tantangan Truth or Dare. Contoh: .tod [truth|dare]"},
	"wouldyourather":    {EN: "Get a 'Would You Rather' question.", ID: "Dapatkan pertanyaan pilihan 'Lebih Pilih Mana'."},
	"reaction":          {EN: "Send an anime reaction GIF/image. Usage: .reaction <hug|pat|slap|bonk|poke|kiss|cry|happy|wave>", ID: "Kirim GIF/gambar reaksi anime. Contoh: .reaction <hug|pat|slap|bonk|poke|kiss|cry|happy|wave>"},
	"asahotak":          {EN: "Play the Asah Otak riddle game.", ID: "Main tebak-tebakan Asah Otak."},
	"caklontong":        {EN: "Play Cak Lontong pun trivia.", ID: "Main kuis plesetan Cak Lontong."},
	"siapakahaku":       {EN: "Play the 'Who Am I' guessing game.", ID: "Main permainan tebak Siapakah Aku."},
	"tebakkata":         {EN: "Unscramble the shuffled word.", ID: "Tebak dan susun kata yang diacak."},
	"tebakgambar":       {EN: "Guess the word from emoji clues.", ID: "Tebak kata dari petunjuk emoji."},
	"tebakbendera":      {EN: "Guess the country from its flag.", ID: "Tebak nama negara dari benderanya."},
	"tebakibukota":      {EN: "Guess the capital city of a country.", ID: "Tebak ibu kota dari suatu negara."},
	"tebaklirik":        {EN: "Complete the missing song lyrics.", ID: "Lanjutkan potongan lirik lagu yang hilang."},
	"tebakbom":          {EN: "Avoid the hidden bomb on a 3x3 grid.", ID: "Hindari bom tersembunyi di papan 3x3."},
	"sambungkata":       {EN: "Play word chain (Sambung Kata) in chat.", ID: "Main permainan Sambung Kata di chat."},
	"math":              {EN: "Solve a timed mental math challenge.", ID: "Selesaikan tantangan hitung cepat berbatas waktu."},
	"wordle":            {EN: "Play 5-letter Wordle right in WhatsApp chat.", ID: "Main tebak kata Wordle 5 huruf di chat."},
	"tictactoe":         {EN: "Play Tic-Tac-Toe vs Bot or PvP (reply 1-9).", ID: "Main Tic-Tac-Toe lawan bot atau pemain lain (balas 1-9)."},
	"catur":             {EN: "Play Chess with a rendered PNG board (move like e2e4).", ID: "Main Catur dengan papan gambar PNG (langkah seperti e2e4)."},
	"ulartangga":        {EN: "Play Snakes & Ladders in chat (type roll).", ID: "Main Ular Tangga di chat (ketik roll)."},
	"russianroulette":   {EN: "Try your luck in 1-in-6 Russian Roulette.", ID: "Uji keberuntungan di Russian Roulette (peluang 1 banding 6)."},
	"gamestats":         {EN: "View your game wins, points, and win streaks.", ID: "Lihat jumlah menang, poin, dan win streak game kamu."},
	"badges":            {EN: "View your unlocked game badges and achievements.", ID: "Lihat lencana dan pencapaian game yang sudah kamu buka."},
	"leaderboard":       {EN: "View this group's or global game leaderboard.", ID: "Lihat papan peringkat game di grup ini atau global."},
	"globalleaderboard": {EN: "View the global game leaderboard across all chats.", ID: "Lihat papan peringkat game global di semua chat."},
	"groupstats":        {EN: "View group game MVP and activity breakdown.", ID: "Lihat MVP dan statistik game grup."},
	"rival":             {EN: "Compare your game stats against a mentioned rival.", ID: "Bandingkan skor game kamu dengan rival yang disebut."},
	"gamelimit":         {EN: "Check your game session limit.", ID: "Cek batas sesi permainanmu."},
	"minigame":          {EN: "Open the NopeBot Web Arcade hub (12 HTML5 games).", ID: "Buka pusat NopeBot Web Arcade (12 game HTML5)."},
	"2048":              {EN: "Play 2048 in chat (reply w/a/s/d) or Web Arcade.", ID: "Main 2048 di chat (balas w/a/s/d) atau di Web Arcade."},
	"minesweeper":       {EN: "Play 5x5 Minesweeper in chat (reply row,col) or Web Arcade.", ID: "Main Minesweeper 5x5 di chat (balas baris,kolom) atau di Web Arcade."},
	"snake":             {EN: "Play Snake in the Web Arcade.", ID: "Main Snake di Web Arcade."},
	"tetris":            {EN: "Play Tetris in the Web Arcade.", ID: "Main Tetris di Web Arcade."},
	"pacman":            {EN: "Play Pac-Man in the Web Arcade.", ID: "Main Pac-Man di Web Arcade."},
	"flappybird":        {EN: "Play Flappy Bird in the Web Arcade.", ID: "Main Flappy Bird di Web Arcade."},
	"dinorun":           {EN: "Play Dino Run in the Web Arcade.", ID: "Main Dino Run di Web Arcade."},
	"breakout":          {EN: "Play Breakout in the Web Arcade.", ID: "Main Breakout di Web Arcade."},
	"frogger":           {EN: "Play Frogger in the Web Arcade.", ID: "Main Frogger di Web Arcade."},
	"galaga":            {EN: "Play Galaga in the Web Arcade.", ID: "Main Galaga di Web Arcade."},
	"racing":            {EN: "Play Racing in the Web Arcade.", ID: "Main Racing di Web Arcade."},
	"sudoku":            {EN: "Play Sudoku in the Web Arcade.", ID: "Main Sudoku di Web Arcade."},

	// ==================== RPG ====================
	"start":           {EN: "Start your Coinsteel Empire RPG journey. Usage: .start [age]", ID: "Mulai petualangan RPG Kerajaan Coinsteel. Contoh: .start [usia]"},
	"rpgprofile":      {EN: "View your Empire and Hero dashboard.", ID: "Lihat dasbor Kerajaan dan Hero RPG kamu."},
	"rpgtop":          {EN: "View the RPG leaderboard. Usage: .rpgtop [networth|level|power]", ID: "Lihat papan peringkat RPG. Contoh: .rpgtop [networth|level|power]"},
	"rpgachievements": {EN: "View unlocked RPG achievements and equip titles. Usage: .titles [name]", ID: "Lihat pencapaian RPG dan pasang gelar. Contoh: .titles [nama_gelar]"},
	"herostats":       {EN: "View or allocate Hero stat points. Usage: .alloc <str|dex|int> <pts>", ID: "Lihat atau alokasikan poin status Hero. Contoh: .alloc <str|dex|int> <poin>"},
	"work":            {EN: "Work a job to earn Yen and XP (-10 Energy).", ID: "Bekerja untuk mendapatkan Yen dan XP (-10 Energi)."},
	"hunt":            {EN: "Hunt wild monsters for Meat, Yen, XP, and gems (-10 Energy).", ID: "Berburu monster untuk Daging, Yen, XP, dan permata (-10 Energi)."},
	"mine":            {EN: "Mine deep ore veins for Iron, Gold, Emerald, and Diamond (-10 Energy).", ID: "Menambang Besi, Emas, Zamrud, dan Berlian (-10 Energi)."},
	"explore":         {EN: "Explore your current World Map zone for chests and events (-8 Energy).", ID: "Jelajahi wilayah Peta Dunia saat ini untuk peti harta & event (-8 Energi)."},
	"map":             {EN: "Render the visual Coinsteel World Map PNG showing all 6 zones.", ID: "Tampilkan gambar Peta Dunia Coinsteel beserta semua 6 wilayah."},
	"travel":          {EN: "Travel to another World Map region. Usage: .travel <zone_id>", ID: "Bepergian ke wilayah lain di Peta Dunia. Contoh: .travel <id_zona>"},
	"build":           {EN: "Construct a new empire building. Usage: .build [building_id]", ID: "Bangun gedung baru di kerajaanmu. Contoh: .build [id_gedung]"},
	"buildings":       {EN: "View all buildings in your empire.", ID: "Lihat semua bangunan yang ada di kerajaanmu."},
	"worksite":        {EN: "View or contribute energy to your active construction worksite.", ID: "Lihat atau sumbang energi ke proyek pembangunan aktifmu."},
	"contribute":      {EN: "Spend energy directly on your active construction. Usage: .contribute [10]", ID: "Sumbang energi langsung ke pembangunan aktifmu. Contoh: .contribute [10]"},
	"upgrade":         {EN: "Upgrade an active building for higher output. Usage: .upgrade <building_id>", ID: "Tingkatkan level bangunan aktif untuk hasil lebih besar. Contoh: .upgrade <id_gedung>"},
	"collect":         {EN: "Collect passive Yen and resource production from buildings.", ID: "Ambil hasil produksi Yen dan sumber daya dari bangunanmu."},
	"shop":            {EN: "View the RPG Shop and live hourly resource prices.", ID: "Lihat Toko RPG dan harga pasar sumber daya per jam."},
	"buy":             {EN: "Buy an item from the RPG Shop. Usage: .buy <item_id> [qty]", ID: "Beli barang dari Toko RPG. Contoh: .buy <id_barang> [jumlah]"},
	"sell":            {EN: "Sell empire resources at live market prices. Usage: .sell <res> <qty|all>", ID: "Jual sumber daya kerajaan di harga pasar. Contoh: .sell <sumber> <jumlah|all>"},
	"eat":             {EN: "Eat food or use potions to restore Energy and HP. Usage: .eat [item]", ID: "Makan makanan atau minum ramuan untuk memulihkan Energi & HP. Contoh: .eat [item]"},
	"bank":            {EN: "Manage your RPG bank. Usage: .bank [deposit|withdraw|upgrade|transfer] [amt]", ID: "Kelola bank RPG. Contoh: .bank [deposit|withdraw|upgrade|transfer] [jumlah]"},
	"craft":           {EN: "Craft weapons and armor from mined resources. Usage: .craft [gear_id]", ID: "Tempa senjata dan zirah dari hasil tambang. Contoh: .craft [id_gear]"},
	"equip":           {EN: "Equip a crafted weapon or armor. Usage: .equip <gear_id>", ID: "Pasang senjata atau zirah yang dimiliki. Contoh: .equip <id_gear>"},
	"gacha":           {EN: "Summon Mythic gear and pets from the Gacha Banner. Usage: .gacha [1|10]", ID: "Tarik Gacha untuk mendapatkan gear dan pet Mythic. Contoh: .gacha [1|10]"},
	"pity":            {EN: "Check Gacha rates, featured Mythic items, and your pity count.", ID: "Cek peluang Gacha, hadiah utama Mythic, dan hitungan pity kamu."},
	"pets":            {EN: "View your collected companion pets and passive bonuses.", ID: "Lihat koleksi pet pendamping dan bonus pasifnya."},
	"petequip":        {EN: "Equip a companion pet for combat and XP bonuses. Usage: .petequip <pet_id>", ID: "Pasang pet pendamping untuk bonus tempur & XP. Contoh: .petequip <id_pet>"},
	"petfeed":         {EN: "Feed meat or fruit to level up your equipped pet.", ID: "Beri makan daging/buah untuk menaikkan level pet aktifmu."},
	"quest":           {EN: "View and claim your daily RPG quest rewards.", ID: "Lihat dan klaim hadiah misi harian RPG kamu."},
	"dungeon":         {EN: "Enter an interactive multi-floor PvE Dungeon (-15 Energy).", ID: "Masuk ke Dungeon PvE bertingkat untuk hadiah bos (-15 Energi)."},
	"duel":            {EN: "Challenge a player to a turn-based Hero Duel. Usage: .duel @user [bet]", ID: "Tantang pemain lain dalam Duel Hero bergiliran. Contoh: .duel @user [taruhan]"},
	"army":            {EN: "Inspect your empire's army strength and fortress defense.", ID: "Periksa kekuatan pasukan dan pertahanan benteng kerajaanmu."},
	"train":           {EN: "Recruit military units for raids. Usage: .train <militia|archer|knight|catapult> [qty]", ID: "Latih pasukan militer untuk penyerangan. Contoh: .train <militia|archer|knight|catapult> [jumlah]"},
	"raid":            {EN: "Raid another player's empire for Yen and Iron. Usage: .raid @user", ID: "Serang kerajaan pemain lain untuk merampas Yen & Besi. Contoh: .raid @user"},
	"guild":           {EN: "Create, join, or manage an RPG Guild. Usage: .guild [create|join|leave|info|donate]", ID: "Buat, gabung, atau kelola Guild RPG. Contoh: .guild [create|join|leave|info|donate]"},
	"worldboss":       {EN: "View the live server-wide World Boss HP and top attackers.", ID: "Lihat HP World Boss global dan daftar penyumbang damage teratas."},
	"wbattack":        {EN: "Attack the shared World Boss for Yen and Diamond rewards (-10 Energy).", ID: "Serang World Boss bersama untuk hadiah Yen & Berlian (-10 Energi)."},

	// ==================== MARKET ====================
	"market":       {EN: "Check live market prices (Crypto, Stocks, IDX, Gold, Forex). Usage: .market <symbol>", ID: "Cek harga pasar live (Kripto, Saham AS/IDX, Emas, Forex). Contoh: .market <simbol>"},
	"markettop":    {EN: "See top gaining and losing assets right now across Crypto, Stocks, Gold & IDX.", ID: "Lihat aset yang sedang NAIK 📈 dan TURUN 📉 di Kripto, Saham, Emas & IDX."},
	"marketai":     {EN: "Run technical & AI analysis (RSI-14, SMA-7/20, S/R). Usage: .marketai <symbol>", ID: "Analisa teknikal & AI (RSI-14, SMA-7/20, Support/Resistance). Contoh: .marketai <simbol>"},
	"marketalert":  {EN: "Set price or percent alerts. Usage: .marketalert add <SYM> <above|below|pct_up|pct_down> <val>", ID: "Atur peringatan harga/persentase. Contoh: .marketalert add <SIMBOL> <above|below|pct_up|pct_down> <nilai>"},
	"marketremind": {EN: "Schedule automatic UP/DOWN market updates. Usage: .marketremind on [min] [SYMS] | off | status | now", ID: "Jadwalkan pengingat pasar NAIK/TURUN otomatis. Contoh: .marketremind on [menit] [SIMBOL] | off | status | now"},

	// ==================== ANIME ====================
	"myanimelist":    {EN: "Search anime details on MyAnimeList. Usage: .mal <title>", ID: "Cari detail anime di MyAnimeList. Contoh: .mal <judul>"},
	"anilist":        {EN: "Search anime or manga on AniList. Usage: .anilist <title>", ID: "Cari anime atau manga di AniList. Contoh: .anilist <judul>"},
	"manga":          {EN: "Search manga details and synopsis on MyAnimeList.", ID: "Cari detail dan sinopsis manga di MyAnimeList."},
	"character":      {EN: "Search an anime character on MyAnimeList. Usage: .character <name>", ID: "Cari info karakter anime di MyAnimeList. Contoh: .character <nama>"},
	"jadwalanime":    {EN: "Show weekly anime airing schedule. Usage: .jadwalanime [monday..sunday]", ID: "Lihat jadwal tayang anime mingguan. Contoh: .jadwalanime [hari]"},
	"komiku":         {EN: "Search Indonesian manga titles (MangaDex / Komiku).", ID: "Cari judul manga berbahasa Indonesia (MangaDex / Komiku)."},
	"samehadaku":     {EN: "Search Indonesian anime info and airing details.", ID: "Cari info dan jadwal tayang anime subtitle Indonesia."},
	"traceanime":     {EN: "Identify anime title, episode, and timestamp from a replied screenshot.", ID: "Cari judul anime, episode, dan menit dari tangkapan layar yang dibalas."},
	"whatanime":      {EN: "Identify anime from a replied screenshot.", ID: "Identifikasi judul anime dari tangkapan layar yang dibalas."},
	"waifu":          {EN: "Fetch a random SFW waifu anime image.", ID: "Kirim gambar waifu anime SFW acak."},
	"neko":           {EN: "Fetch a random SFW neko anime image.", ID: "Kirim gambar neko anime SFW acak."},
	"husbu":          {EN: "Fetch a random SFW husbando anime image.", ID: "Kirim gambar husbando anime SFW acak."},
	"kitsune":        {EN: "Fetch a random SFW kitsune anime image.", ID: "Kirim gambar kitsune anime SFW acak."},
	"maid":           {EN: "Fetch a random SFW anime maid image.", ID: "Kirim gambar maid anime SFW acak."},
	"bunnygirl":      {EN: "Fetch a random SFW bunnygirl anime image.", ID: "Kirim gambar bunnygirl anime SFW acak."},
	"schoolgirl":     {EN: "Fetch a random SFW schoolgirl anime image.", ID: "Kirim gambar schoolgirl anime SFW acak."},
	"swimsuit":       {EN: "Fetch a random SFW swimsuit anime image.", ID: "Kirim gambar swimsuit anime SFW acak."},
	"animewallpaper": {EN: "Fetch an HD anime wallpaper.", ID: "Ambil wallpaper anime HD."},
	"danbooru":       {EN: "Search SFW anime art by tag on Danbooru/Safebooru. Usage: .danbooru <tag>", ID: "Cari karya anime SFW berdasarkan tag. Contoh: .danbooru <tag>"},
	"zerochan":       {EN: "Search anime artwork by character or tag on Zerochan.", ID: "Cari ilustrasi anime berdasarkan karakter atau tag di Zerochan."},

	// ==================== GROUP ====================
	"add":             {EN: "Add member(s) to the group. Usage: .add 628xxx", ID: "Tambahkan anggota ke grup. Contoh: .add 628xxx"},
	"remove":          {EN: "Remove member(s) from the group. Usage: .remove @user", ID: "Keluarkan anggota dari grup. Contoh: .remove @user"},
	"promote":         {EN: "Promote member(s) to group admin.", ID: "Jadikan anggota sebagai admin grup."},
	"demote":          {EN: "Demote admin(s) back to regular member.", ID: "Turunkan jabatan admin menjadi anggota biasa."},
	"open":            {EN: "Open group chat so all members can send messages.", ID: "Buka chat grup agar semua anggota bisa mengirim pesan."},
	"close":           {EN: "Close group chat so only admins can send messages.", ID: "Tutup chat grup agar hanya admin yang bisa mengirim pesan."},
	"schedulegroup":   {EN: "Schedule daily auto open/close. Usage: .schedulegroup <HH:MM> <HH:MM> | off", ID: "Jadwalkan buka/tutup grup otomatis harian. Contoh: .schedulegroup <JJ:MM> <JJ:MM> | off"},
	"setsubject":      {EN: "Change the group name.", ID: "Ubah nama grup."},
	"setdesc":         {EN: "Change the group description.", ID: "Ubah deskripsi grup."},
	"setppgc":         {EN: "Change the group photo (reply to an image).", ID: "Ubah foto profil grup (balas gambar)."},
	"linkgc":          {EN: "Get the group invite link.", ID: "Ambil tautan undangan grup."},
	"revoke":          {EN: "Reset and generate a new group invite link.", ID: "Reset dan buat tautan undangan grup baru."},
	"joinreq":         {EN: "List or approve/reject pending join requests. Usage: .joinreq [approve|reject all|@phone]", ID: "Lihat atau setujui/tolak permintaan bergabung. Contoh: .joinreq [approve|reject all|@nomor]"},
	"leavegroup":      {EN: "Make the bot leave this group.", ID: "Keluarkan bot dari grup ini."},
	"delete":          {EN: "Delete a replied message in the group.", ID: "Hapus pesan yang dibalas di grup."},
	"pinmsg":          {EN: "Pin a replied message in the group.", ID: "Sematkan (pin) pesan yang dibalas di grup."},
	"unpinmsg":        {EN: "Unpin a replied message in the group.", ID: "Lepas sematan pesan yang dibalas di grup."},
	"ephemeral":       {EN: "Set disappearing message timer. Usage: .ephemeral <off|24h|7d|90d>", ID: "Atur durasi pesan sementara. Contoh: .ephemeral <off|24h|7d|90d>"},
	"tagall":          {EN: "Mention all group members visibly.", ID: "Sebut (tag) semua anggota grup secara terlihat."},
	"hidetag":         {EN: "Send an announcement mentioning all members silently.", ID: "Kirim pengumuman dengan menyebut semua anggota secara tersembunyi."},
	"groupinfo":       {EN: "Display group details, admins, and member count.", ID: "Tampilkan detail grup, daftar admin, dan jumlah anggota."},
	"groupid":         {EN: "Show the current group JID.", ID: "Tampilkan JID grup saat ini."},
	"groupsettings":   {EN: "Show all active settings and security toggles for this group.", ID: "Tampilkan semua pengaturan dan fitur keamanan aktif di grup ini."},
	"gstats":          {EN: "Show group message and member activity statistics.", ID: "Tampilkan statistik pesan dan aktivitas anggota grup."},
	"listaktif":       {EN: "Show the most active members in this group.", ID: "Tampilkan anggota paling aktif di grup ini."},
	"sider":           {EN: "List inactive members (no messages in 7 days).", ID: "Tampilkan anggota pasif (tidak mengirim pesan dalam 7 hari)."},
	"cekbotadmin":     {EN: "Check whether the bot is an admin in this group.", ID: "Cek apakah bot sudah menjadi admin di grup ini."},
	"syncgroup":       {EN: "Refresh group metadata from WhatsApp.", ID: "Segarkan metadata grup dari WhatsApp."},
	"welcome":         {EN: "Toggle welcome & goodbye messages. Usage: .welcome on|off", ID: "Aktifkan atau matikan pesan sambutan & perpisahan. Contoh: .welcome on|off"},
	"setwelcome":      {EN: "Set custom welcome message (@user, {group}, {count}).", ID: "Atur pesan sambutan khusus (@user, {group}, {count})."},
	"setgoodbye":      {EN: "Set custom goodbye message (@user, {group}, {count}).", ID: "Atur pesan perpisahan khusus (@user, {group}, {count})."},
	"simulatewelcome": {EN: "Preview the group welcome message.", ID: "Pratinjau pesan sambutan grup."},
	"simulategoodbye": {EN: "Preview the group goodbye message.", ID: "Pratinjau pesan perpisahan grup."},
	"attendance":      {EN: "Start, view, or clear group attendance. Usage: .attendance [start <title>|list|clear]", ID: "Mulai, lihat, atau hapus daftar hadir grup. Contoh: .attendance [start <judul>|list|clear]"},
	"present":         {EN: "Mark yourself present in the active group attendance.", ID: "Catat kehadiranmu di absensi grup yang aktif."},
	"autorespon":      {EN: "Manage keyword auto-replies. Usage: .autorespon add <key>|<reply> | del <key> | list", ID: "Kelola balasan otomatis kata kunci. Contoh: .autorespon add <kunci>|<balasan> | del <kunci> | list"},
	"adzan":           {EN: "Set prayer time reminder city for this group. Usage: .adzan <city|off>", ID: "Atur pengingat waktu adzan untuk grup. Contoh: .adzan <kota|off>"},
	"settimezone":     {EN: "Set the group timezone (e.g. Asia/Jakarta).", ID: "Atur zona waktu grup (contoh: Asia/Jakarta)."},
	"listtimezone":    {EN: "Show common supported timezones.", ID: "Tampilkan daftar zona waktu yang didukung."},
	"groupprefix":     {EN: "Set a custom command prefix for this group. Usage: .groupprefix <symbol|reset>", ID: "Atur prefix perintah khusus untuk grup ini. Contoh: .groupprefix <simbol|reset>"},
	"adminonly":       {EN: "Restrict bot commands in this group to admins only. Usage: .adminonly on|off", ID: "Batasi perintah bot di grup ini hanya untuk admin. Contoh: .adminonly on|off"},
	"cmd":             {EN: "Enable or disable a command or category in this group. Usage: .cmd <on|off> <name>", ID: "Aktifkan atau matikan perintah/kategori di grup ini. Contoh: .cmd <on|off> <nama>"},
	"debugpriority":   {EN: "Inspect active prefixes and middleware status for this group.", ID: "Periksa prefix aktif dan status middleware di grup ini."},
	"antilink":        {EN: "Block links in group. Usage: .antilink on|off|delete|warn|kick", ID: "Blokir tautan di grup. Contoh: .antilink on|off|delete|warn|kick"},
	"antispam":        {EN: "Toggle flood/spam message protection. Usage: .antispam on|off", ID: "Aktifkan atau matikan perlindungan spam/flood. Contoh: .antispam on|off"},
	"antibot":         {EN: "Toggle AntiBot guard. Usage: .antibot on|off", ID: "Aktifkan atau matikan penjaga AntiBot. Contoh: .antibot on|off"},
	"antibug":         {EN: "Toggle oversized/virtex bug message protection. Usage: .antibug on|off", ID: "Aktifkan atau matikan perlindungan pesan bug/virtex. Contoh: .antibug on|off"},
	"anticall":        {EN: "Toggle group call protection flag. Usage: .anticall on|off", ID: "Aktifkan atau matikan perlindungan panggilan grup. Contoh: .anticall on|off"},
	"antihidetag":     {EN: "Block non-admin silent mass mentions. Usage: .antihidetag on|off", ID: "Blokir hidetag massal dari non-admin. Contoh: .antihidetag on|off"},
	"antimedia":       {EN: "Delete media sent by non-admins. Usage: .antimedia on|off", ID: "Hapus media yang dikirim oleh non-admin. Contoh: .antimedia on|off"},
	"antisw":          {EN: "Block WhatsApp Status forwards in group. Usage: .antisw on|off", ID: "Blokir terusan Status WhatsApp di grup. Contoh: .antisw on|off"},
	"antitagsw":       {EN: "Block group Status mention tags. Usage: .antitagsw on|off", ID: "Blokir mention grup di Status WhatsApp. Contoh: .antitagsw on|off"},
	"antivo":          {EN: "Automatically unlock and repost View-Once media in this group. Usage: .antivo on|off", ID: "Buka dan kirim ulang otomatis media Sekali Lihat di grup ini. Contoh: .antivo on|off"},
	"antikudeta":      {EN: "Auto-demote rogue admins who mass-demote others. Usage: .antikudeta on|off", ID: "Turunkan otomatis admin yang melakukan demote massal. Contoh: .antikudeta on|off"},
	"badword":         {EN: "Manage group badword filter. Usage: .badword on|off|add <word>|del <word>|list", ID: "Kelola filter kata kasar grup. Contoh: .badword on|off|add <kata>|del <kata>|list"},
	"warn":            {EN: "Warn a member (auto-kicks at warning limit). Usage: .warn @user [reason]", ID: "Beri peringatan ke anggota (otomatis keluarkan jika batas tercapai). Contoh: .warn @user [alasan]"},
	"warnings":        {EN: "Check warnings for yourself or a mentioned member.", ID: "Cek jumlah peringatanmu atau anggota yang disebut."},
	"listwarning":     {EN: "List all warned members in this group.", ID: "Lihat daftar semua anggota yang mendapat peringatan di grup ini."},
	"resetwarn":       {EN: "Reset warnings for a member. Usage: .resetwarn @user", ID: "Reset peringatan anggota. Contoh: .resetwarn @user"},
	"mutemember":      {EN: "Mute a member (auto-deletes their messages). Usage: .mutemember @user [min]", ID: "Bisukan anggota (pesan otomatis dihapus). Contoh: .mutemember @user [menit]"},
	"unmutemember":    {EN: "Unmute a muted member. Usage: .unmutemember @user", ID: "Buka bisu anggota. Contoh: .unmutemember @user"},
	"listmute":        {EN: "List currently muted members in this group.", ID: "Lihat daftar anggota yang sedang dibisukan di grup ini."},
	"banmember":       {EN: "Ban and kick a member from this group. Usage: .banmember @user", ID: "Blokir dan keluarkan anggota dari grup. Contoh: .banmember @user"},
	"unbanmember":     {EN: "Unban a member from this group. Usage: .unbanmember <phone>", ID: "Cabut blokir anggota di grup ini. Contoh: .unbanmember <nomor>"},
	"listbanmember":   {EN: "List banned members in this group.", ID: "Lihat daftar anggota yang diblokir di grup ini."},

	// ==================== OWNER (HIDDEN FROM NON-OWNERS) ====================
	"addowner":          {EN: "Add a new bot owner by replying to their message.", ID: "Tambahkan pemilik bot baru dengan membalas pesannya."},
	"removeowner":       {EN: "Remove a bot owner by replying to their message.", ID: "Hapus pemilik bot dengan membalas pesannya."},
	"anti-delete":       {EN: "Forward deleted messages from targets to your private chat. Usage: .anti-delete [on|off|add|remove|list]", ID: "Teruskan pesan yang dihapus dari target ke chat pribadimu. Contoh: .anti-delete [on|off|add|remove|list]"},
	"auto-get":          {EN: "Automatically save View-Once media from targets to your private chat. Usage: .auto-get [on|off|add|remove|list]", ID: "Simpan otomatis media Sekali Lihat dari target ke chat pribadimu. Contoh: .auto-get [on|off|add|remove|list]"},
	"status":            {EN: "Post a reshareable WhatsApp Status (text/image/video/audio) or delete one.", ID: "Unggah Status WhatsApp (teks/foto/video/audio) atau hapus status."},
	"statuspost":        {EN: "Post text or replied image/video to WhatsApp Status (reshareable).", ID: "Unggah teks atau foto/video yang dibalas ke Status WhatsApp."},
	"channelpost":       {EN: "Post text to a WhatsApp Channel (Newsletter). Usage: .channelpost <jid> <text>", ID: "Kirim postingan teks ke WhatsApp Channel. Contoh: .channelpost <jid> <teks>"},
	"bot":               {EN: "Show bot runtime, Android Companion status, and SQLite WAL diagnostics.", ID: "Tampilkan runtime bot, status Android Companion, dan diagnostik SQLite WAL."},
	"botsettings":       {EN: "Show current per-bot tenant settings.", ID: "Tampilkan pengaturan aktif untuk akun bot ini."},
	"gostats":           {EN: "Show Go memory, goroutine, and garbage collector stats.", ID: "Tampilkan statistik memori Go, goroutine, dan GC."},
	"msgstats":          {EN: "Show message/command throughput and SQLite WAL stats.", ID: "Tampilkan statistik jumlah pesan/perintah dan SQLite WAL."},
	"selfmode":          {EN: "Toggle self-mode (only owner can run commands). Usage: .selfmode on|off", ID: "Aktifkan atau matikan mode pribadi (hanya owner). Contoh: .selfmode on|off"},
	"groupmode":         {EN: "Toggle group-only mode (ignore non-owner DMs). Usage: .groupmode on|off", ID: "Aktifkan atau matikan mode khusus grup (abaikan DM non-owner). Contoh: .groupmode on|off"},
	"autoread":          {EN: "Toggle automatic read receipts. Usage: .autoread on|off", ID: "Aktifkan atau matikan centang biru otomatis. Contoh: .autoread on|off"},
	"rejectcall":        {EN: "Toggle automatic incoming call rejection. Usage: .rejectcall on|off", ID: "Aktifkan atau matikan penolakan panggilan masuk otomatis. Contoh: .rejectcall on|off"},
	"answercall":        {EN: "Set auto-reply text sent when a call is rejected. Usage: .answercall <text|off>", ID: "Atur pesan balasan otomatis saat panggilan ditolak. Contoh: .answercall <teks|off>"},
	"pacing":            {EN: "Set per-chat command cooldown in milliseconds. Usage: .pacing <ms>", ID: "Atur jeda perintah per chat dalam milidetik. Contoh: .pacing <ms>"},
	"nsfw":              {EN: "Toggle NSFW command gate for this group or bot. Usage: .nsfw on|off", ID: "Aktifkan atau matikan izin perintah NSFW untuk grup/bot. Contoh: .nsfw on|off"},
	"setbotname":        {EN: "Set custom bot display name for this tenant. Usage: .setbotname <name>", ID: "Atur nama tampilan bot untuk akun ini. Contoh: .setbotname <nama>"},
	"setbotowner":       {EN: "Set buyer/owner phone number for this tenant. Usage: .setbotowner <phone>", ID: "Atur nomor pemilik/penyewa untuk akun bot ini. Contoh: .setbotowner <nomor>"},
	"setpp":             {EN: "Update the bot's profile picture (reply to an image).", ID: "Ubah foto profil bot (balas gambar)."},
	"setprivacy":        {EN: "Update WhatsApp account privacy settings. Usage: .setprivacy <field> <value>", ID: "Ubah pengaturan privasi akun WhatsApp. Contoh: .setprivacy <jenis> <nilai>"},
	"getprivacysetting": {EN: "View current WhatsApp account privacy settings.", ID: "Lihat pengaturan privasi akun WhatsApp saat ini."},
	"clearchat":         {EN: "Clear locally cached chat history for this chat.", ID: "Bersihkan cache riwayat lokal untuk chat ini."},
	"listgroup":         {EN: "List all joined WhatsApp groups.", ID: "Tampilkan daftar semua grup WhatsApp yang diikuti bot."},
	"cektanpagrup":      {EN: "List active non-group chats.", ID: "Tampilkan daftar chat aktif selain grup."},
	"join":              {EN: "Join a WhatsApp group via invite link. Usage: .join <link>", ID: "Gabung ke grup WhatsApp lewat tautan undangan. Contoh: .join <tautan>"},
	"banchat":           {EN: "Mute bot responses in this group.", ID: "Nonaktifkan respons bot di grup ini."},
	"unbanchat":         {EN: "Re-enable bot responses in this group.", ID: "Aktifkan kembali respons bot di grup ini."},
	"banlist":           {EN: "List all muted/banned groups.", ID: "Tampilkan daftar semua grup yang diblokir/dinonaktifkan."},
	"disableallgroup":   {EN: "Disable all non-owner features in every group.", ID: "Matikan semua fitur non-owner di seluruh grup."},
	"enablegroup":       {EN: "Re-enable all features for the current group.", ID: "Aktifkan kembali semua fitur untuk grup ini."},
	"premium":           {EN: "Grant local premium days to a user. Usage: .premium <phone> <days>", ID: "Berikan masa aktif premium ke pengguna. Contoh: .premium <nomor> <hari>"},
	"createredeem":      {EN: "Create a promo redeem code. Usage: .createredeem <CODE> <coins> <xp> <maxUses>", ID: "Buat kode promo hadiah. Contoh: .createredeem <KODE> <koin> <xp> <maks_pakai>"},
	"jadibot":           {EN: "Queue a sequential Android Companion QR pairing slot.", ID: "Antrekan slot pairing QR Android Companion baru."},
	"lockjadibot":       {EN: "Lock or unlock .jadibot pairing. Usage: .lockjadibot on|off", ID: "Kunci atau buka fitur pairing .jadibot. Contoh: .lockjadibot on|off"},
	"trackbot":          {EN: "List all active linked bot tenants and QR queue status.", ID: "Lihat semua akun bot aktif dan status antrean QR."},
	"eval":              {EN: "Evaluate message properties for debugging.", ID: "Evaluasi properti pesan untuk debugging."},
	"ytversion":         {EN: "Check the embedded yt-dlp version.", ID: "Cek versi yt-dlp yang tertanam di bot."},

	// ==================== NEWLY ADDED COMMANDS ====================
	// General
	"ai":        {EN: "Ask the configured AI assistant anything. Usage: .ai <question>", ID: "Tanya apa saja ke asisten AI. Contoh: .ai <pertanyaan>"},
	"transfer":  {EN: "Transfer coins to another user. Usage: .transfer @user <amount>", ID: "Transfer koin ke pengguna lain. Contoh: .transfer @user <jumlah>"},
	"report":    {EN: "Send a bug report or feedback to the bot owner. Usage: .report <message>", ID: "Kirim laporan bug atau saran ke pemilik bot. Contoh: .report <pesan>"},
	"changelog": {EN: "Show recent NopeBot updates and features.", ID: "Tampilkan daftar pembaruan dan fitur terbaru NopeBot."},

	// Anime
	"quotesanime": {EN: "Get a random iconic anime quote.", ID: "Tampilkan kutipan ikonik anime secara acak."},
	"randomanime": {EN: "Get a random anime recommendation with synopsis and score.", ID: "Dapatkan rekomendasi anime acak lengkap dengan sinopsis dan skor."},

	// Media, Stickers, Audio FX & Makers
	"bass":       {EN: "Apply heavy bass boost audio effect to replied media.", ID: "Tambahkan efek bass kuat pada audio/video yang dibalas."},
	"blown":      {EN: "Apply distorted blown speaker audio effect to replied media.", ID: "Tambahkan efek speaker pecah (blown) pada audio/video yang dibalas."},
	"deep":       {EN: "Apply deep pitched voice audio effect to replied media.", ID: "Ubah suara audio/video yang dibalas menjadi lebih berat (deep)."},
	"earrape":    {EN: "Apply ultra-loud earrape audio effect to replied media.", ID: "Tambahkan efek suara super keras (earrape) pada audio/video yang dibalas."},
	"fast":       {EN: "Speed up replied audio or video.", ID: "Percepat tempo audio atau video yang dibalas."},
	"fat":        {EN: "Apply thick fat audio effect to replied media.", ID: "Tambahkan efek suara tebal (fat) pada audio/video yang dibalas."},
	"nightcore":  {EN: "Apply nightcore speed+pitch effect to replied media.", ID: "Terapkan efek nightcore pada audio/video yang dibalas."},
	"robot":      {EN: "Apply robotic voice audio effect to replied media.", ID: "Ubah suara audio/video yang dibalas menjadi suara robot."},
	"slow":       {EN: "Slow down replied audio or video.", ID: "Perlambat tempo audio atau video yang dibalas."},
	"tupai":      {EN: "Apply high-pitched squirrel/chipmunk audio effect.", ID: "Ubah suara audio/video yang dibalas menjadi suara tupai (chipmunk)."},
	"soundcloud": {EN: "Download a SoundCloud track as MP3. Usage: .soundcloud <url>", ID: "Unduh lagu SoundCloud dalam format MP3. Contoh: .soundcloud <tautan>"},
	"tiktokmp3":  {EN: "Extract MP3 audio from a TikTok link. Usage: .tiktokmp3 <url>", ID: "Ekstrak audio MP3 dari tautan TikTok. Contoh: .tiktokmp3 <tautan>"},
	"hd":         {EN: "Upscale and sharpen a replied image to 2x HD.", ID: "Tingkatkan ketajaman dan resolusi gambar yang dibalas ke 2x HD."},
	"tovn":       {EN: "Convert replied audio or video to a WhatsApp voice note.", ID: "Ubah audio atau video yang dibalas menjadi pesan suara (VN)."},
	"toviewonce": {EN: "Resend a replied photo or video as View-Once.", ID: "Kirim ulang foto atau video yang dibalas sebagai Sekali Lihat."},
	"toptv":      {EN: "Convert a replied short video into a circular Video Note.", ID: "Ubah video pendek yang dibalas menjadi Video Note bulat (PTV)."},
	"scircle":    {EN: "Convert a replied image into a circular WebP sticker.", ID: "Ubah gambar yang dibalas menjadi stiker bulat WebP."},
	"ttp":        {EN: "Create a clean text-to-sticker WebP. Usage: .ttp <text>", ID: "Buat stiker teks bersih dari tulisan. Contoh: .ttp <teks>"},
	"attp":       {EN: "Create an animated rainbow text sticker. Usage: .attp <text>", ID: "Buat stiker teks pelangi bergerak. Contoh: .attp <teks>"},
	"brat":       {EN: "Generate a classic lime-green Brat text sticker. Usage: .brat <text>", ID: "Buat stiker teks Brat hijau limau klasik. Contoh: .brat <teks>"},
	"swm":        {EN: "Change pack/author watermark of a replied sticker. Usage: .swm Pack | Author", ID: "Ubah watermark paket/pembuat stiker yang dibalas. Contoh: .swm Paket | Pembuat"},
	"setwm":      {EN: "Save your default sticker watermark. Usage: .setwm Pack | Author", ID: "Simpan watermark stiker bawaanmu. Contoh: .setwm Paket | Pembuat"},
	"resetwm":    {EN: "Reset your default sticker watermark to NopeBot.", ID: "Reset watermark stiker bawaanmu ke NopeBot."},
	"hartatahta": {EN: "Generate a holographic 'HARTA TAHTA <NAME>' image. Usage: .hartatahta <name>", ID: "Buat gambar holografik 'HARTA TAHTA <NAMA>'. Contoh: .hartatahta <nama>"},
	"iqcpink":    {EN: "Generate a pink aesthetic quote card. Usage: .iqcpink <text>", ID: "Buat kartu kutipan estetik warna pink. Contoh: .iqcpink <teks>"},
	"fakereply":  {EN: "Generate a fake WhatsApp chat bubble image. Usage: .fakereply Name | Text", ID: "Buat gambar gelembung chat WhatsApp palsu. Contoh: .fakereply Nama | Pesan"},

	// Tools & Search
	"alquran":       {EN: "Read an Al-Quran verse with Arabic, ID & EN translations. Usage: .alquran <surah> [ayah]", ID: "Baca ayat Al-Quran beserta terjemahan ID & EN. Contoh: .alquran <surah> [ayat]"},
	"alkitab":       {EN: "Look up a Bible verse. Usage: .alkitab <book chapter:verse>", ID: "Cari ayat Alkitab. Contoh: .alkitab John 3:16"},
	"wiki":          {EN: "Search Wikipedia article summaries (ID/EN). Usage: .wiki <topic>", ID: "Cari ringkasan artikel Wikipedia (ID/EN). Contoh: .wiki <topik>"},
	"infounsur":     {EN: "Look up a chemical element by symbol, name, or number. Usage: .infounsur Au", ID: "Cari info unsur kimia lewat simbol, nama, atau nomor atom. Contoh: .infounsur Au"},
	"ipchecker":     {EN: "Check IP address geolocation, ISP, and ASN. Usage: .ipchecker <ip>", ID: "Cek lokasi geografis, ISP, dan ASN alamat IP. Contoh: .ipchecker <ip>"},
	"shortlink":     {EN: "Shorten a long URL via TinyURL. Usage: .shortlink <url>", ID: "Perpendek tautan panjang lewat TinyURL. Contoh: .shortlink <tautan>"},
	"ssweb":         {EN: "Capture a live screenshot of a website URL. Usage: .ssweb <url>", ID: "Ambil tangkapan layar situs web. Contoh: .ssweb <tautan>"},
	"nulis":         {EN: "Write text onto a lined notebook paper image. Usage: .nulis <text>", ID: "Tulis teks di atas gambar kertas buku tulis bergaris. Contoh: .nulis <teks>"},
	"kodebahasa":    {EN: "Show ISO language codes for .translate and .tts.", ID: "Tampilkan daftar kode bahasa ISO untuk .translate dan .tts."},
	"reminder":      {EN: "Set or list timed reminders in this chat. Usage: .reminder 10m <msg> | list", ID: "Atur atau lihat pengingat berwaktu di chat ini. Contoh: .reminder 10m <pesan> | list"},
	"delreminder":   {EN: "Delete an active reminder by ID. Usage: .delreminder <id>", ID: "Hapus pengingat aktif berdasarkan ID. Contoh: .delreminder <id>"},
	"resetreminder": {EN: "Clear all active reminders in this chat.", ID: "Hapus semua pengingat aktif di chat ini."},
	"cekperangkat":  {EN: "Detect the WhatsApp platform (Android/iOS/Web) of a replied message.", ID: "Deteksi perangkat WhatsApp (Android/iOS/Web) dari pesan yang dibalas."},

	// Games, Fun & Ramalan
	"alay":          {EN: "Convert text into Indonesian alay style. Usage: .alay <text>", ID: "Ubah teks ke gaya tulisan alay. Contoh: .alay <teks>"},
	"halah":         {EN: "Replace all vowels with 'a'. Usage: .halah <text>", ID: "Ubah semua huruf vokal teks menjadi 'a'. Contoh: .halah <teks>"},
	"heleh":         {EN: "Replace all vowels with 'e'. Usage: .heleh <text>", ID: "Ubah semua huruf vokal teks menjadi 'e'. Contoh: .heleh <teks>"},
	"hilih":         {EN: "Replace all vowels with 'i'. Usage: .hilih <text>", ID: "Ubah semua huruf vokal teks menjadi 'i'. Contoh: .hilih <teks>"},
	"holoh":         {EN: "Replace all vowels with 'o'. Usage: .holoh <text>", ID: "Ubah semua huruf vokal teks menjadi 'o'. Contoh: .holoh <teks>"},
	"huluh":         {EN: "Replace all vowels with 'u'. Usage: .huluh <text>", ID: "Ubah semua huruf vokal teks menjadi 'u'. Contoh: .huluh <teks>"},
	"apakah":        {EN: "Ask a yes/no magic 8-ball question. Usage: .apakah <question>", ID: "Tanya pertanyaan ya/tidak secara acak. Contoh: .apakah <pertanyaan>"},
	"kapankah":      {EN: "Predict randomly when something will happen. Usage: .kapankah <question>", ID: "Ramal kapan suatu hal akan terjadi. Contoh: .kapankah <pertanyaan>"},
	"siapakah":      {EN: "Pick a random group member to answer 'Who is...'. Usage: .siapakah <question>", ID: "Pilih anggota grup secara acak untuk pertanyaan 'Siapakah...'. Contoh: .siapakah <pertanyaan>"},
	"rate":          {EN: "Rate anything from 0 to 100%. Usage: .rate <thing>", ID: "Beri nilai 0 sampai 100% untuk apa saja. Contoh: .rate <sesuatu>"},
	"cekkhodam":     {EN: "Check someone's funny guardian spirit (khodam). Usage: .cekkhodam [name]", ID: "Cek khodam lucu seseorang hari ini. Contoh: .cekkhodam [nama]"},
	"jadian":        {EN: "Randomly pair two members in this group as a couple.", ID: "Pasangkan dua anggota grup secara acak untuk seru-seruan."},
	"randomnumber":  {EN: "Generate a random number in a range. Usage: .randomnumber [min] [max]", ID: "Hasilkan angka acak dalam rentang tertentu. Contoh: .randomnumber [min] [maks]"},
	"randomtag":     {EN: "Tag a random member in this group.", ID: "Sebut (tag) satu anggota grup secara acak."},
	"truth":         {EN: "Get a random Truth question.", ID: "Dapatkan pertanyaan Truth acak."},
	"dare":          {EN: "Get a random Dare challenge.", ID: "Dapatkan tantangan Dare acak."},
	"pantun":        {EN: "Get a random Indonesian pantun.", ID: "Tampilkan pantun Indonesia acak."},
	"katabijak":     {EN: "Get a random wise quote.", ID: "Tampilkan kata bijak dan motivasi acak."},
	"puisi":         {EN: "Get a short Indonesian poem.", ID: "Tampilkan puisi pendek."},
	"meme":          {EN: "Fetch a random SFW meme image.", ID: "Kirim gambar meme lucu acak (SFW)."},
	"ppcp":          {EN: "Get a pair of anime couple profile pictures.", ID: "Dapatkan sepasang foto profil couple anime."},
	"zodiak":        {EN: "Check zodiac profile and daily luck. Usage: .zodiak <sign|DD-MM>", ID: "Cek karakter zodiak dan hoki harian. Contoh: .zodiak <zodiak|DD-MM>"},
	"shio":          {EN: "Check Chinese Zodiac (Shio) by birth year. Usage: .shio <year>", ID: "Cek Shio dan elemen berdasarkan tahun lahir. Contoh: .shio <tahun>"},
	"weton":         {EN: "Calculate Javanese Weton (Pasaran & Neptu). Usage: .weton <YYYY-MM-DD>", ID: "Hitung Weton Jawa (Pasaran & Neptu). Contoh: .weton <YYYY-MM-DD>"},
	"artinama":      {EN: "Generate a fun personality reading for a name. Usage: .artinama <name>", ID: "Lihat ramalan arti nama seru. Contoh: .artinama <nama>"},
	"kecocokannama": {EN: "Check compatibility score of two names. Usage: .kecocokannama A | B", ID: "Cek persentase kecocokan dua nama. Contoh: .kecocokannama A | B"},
	"ramalanjodoh":  {EN: "Fun soulmate compatibility reading. Usage: .ramalanjodoh A | B", ID: "Ramalan kecocokan jodoh seru. Contoh: .ramalanjodoh A | B"},
	"tafsirmimpi":   {EN: "Look up a dream interpretation. Usage: .tafsirmimpi <keyword>", ID: "Cari tafsir mimpi berdasarkan kata kunci. Contoh: .tafsirmimpi <kata>"},
	"nohoki":        {EN: "Check lucky score of a number. Usage: .nohoki <number>", ID: "Cek skor keberuntungan suatu nomor. Contoh: .nohoki <nomor>"},
	"family100":     {EN: "Play the Family 100 survey guessing game.", ID: "Main permainan tebak survei Family 100."},
	"tebakemoji":    {EN: "Guess the movie or character from emoji clues.", ID: "Tebak film atau karakter dari petunjuk emoji."},
	"tebaklogo":     {EN: "Guess the brand from its logo description.", ID: "Tebak merek terkenal dari deskripsi logonya."},
	"tekateki":      {EN: "Solve a classic Indonesian riddle (Teka-Teki).", ID: "Jawab teka-teki logika seru."},
	"susunkalimat":  {EN: "Rearrange shuffled words into a proper sentence.", ID: "Susun kata acak menjadi kalimat yang benar."},
	"hint":          {EN: "Get a hint for the active game in this chat.", ID: "Dapatkan petunjuk untuk game yang sedang aktif di chat ini."},
	"nyerah":        {EN: "Give up on the active game and reveal the answer.", ID: "Menyerah dari game aktif dan tampilkan jawabannya."},

	// Group
	"rules":      {EN: "View the rules for this group.", ID: "Lihat peraturan grup ini."},
	"setrules":   {EN: "Set custom rules for this group. Usage: .setrules <text>", ID: "Atur peraturan khusus untuk grup ini. Contoh: .setrules <teks>"},
	"delrules":   {EN: "Delete the custom rules for this group.", ID: "Hapus peraturan khusus grup ini."},
	"vote":       {EN: "Start a vote session in this group. Usage: .vote <topic>", ID: "Mulai sesi voting di grup ini. Contoh: .vote <topik>"},
	"upvote":     {EN: "Vote YES in the active group vote.", ID: "Pilih YA (upvote) pada voting grup yang aktif."},
	"devote":     {EN: "Vote NO in the active group vote.", ID: "Pilih TIDAK (devote) pada voting grup yang aktif."},
	"cekvote":    {EN: "Check the live tally of the active group vote.", ID: "Cek perolehan suara voting grup saat ini."},
	"delvote":    {EN: "Close and delete the active group vote.", ID: "Tutup dan hapus sesi voting grup yang aktif."},
	"poll":       {EN: "Create a native WhatsApp Poll. Usage: .poll Question | Opt1 | Opt2", ID: "Buat Polling WhatsApp asli. Contoh: .poll Pertanyaan | Opsi 1 | Opsi 2"},
	"list":       {EN: "View saved store/info list items in this group. Usage: .list [key]", ID: "Lihat daftar info/produk yang tersimpan di grup. Contoh: .list [nama]"},
	"addlist":    {EN: "Add an item to this group's list. Usage: .addlist key | response", ID: "Tambah item baru ke daftar grup. Contoh: .addlist kunci | isi"},
	"updatelist": {EN: "Update an item in this group's list. Usage: .updatelist key | new text", ID: "Perbarui item yang ada di daftar grup. Contoh: .updatelist kunci | isi baru"},
	"dellist":    {EN: "Delete an item from this group's list. Usage: .dellist <key>", ID: "Hapus item dari daftar grup. Contoh: .dellist <kunci>"},
	"resetlist":  {EN: "Clear all saved list items in this group.", ID: "Hapus semua item daftar di grup ini."},
	"proses":     {EN: "Send an 'Order In Progress' status card (reply to buyer).", ID: "Kirim kartu status 'Pesanan Diproses' (balas pesan pembeli)."},
	"done":       {EN: "Send an 'Order Completed' status card (reply to buyer).", ID: "Kirim kartu status 'Pesanan Selesai' (balas pesan pembeli)."},
	"batal":      {EN: "Send an 'Order Cancelled' status card (reply to buyer).", ID: "Kirim kartu status 'Pesanan Dibatalkan' (balas pesan pembeli)."},
	"listadmin":  {EN: "List all admins of this group.", ID: "Tampilkan daftar semua admin grup ini."},
	"kickme":     {EN: "Remove yourself from this group.", ID: "Keluarkan dirimu sendiri dari grup ini."},
	"group":      {EN: "Quickly open or close the group chat. Usage: .group <open|close>", ID: "Buka atau tutup chat grup dengan cepat. Contoh: .group <open|close>"},
	"ultah":      {EN: "View saved member birthdays in this group.", ID: "Lihat daftar ulang tahun anggota di grup ini."},
	"setultah":   {EN: "Save your birthday in this group. Usage: .setultah DD-MM", ID: "Simpan tanggal ulang tahunmu di grup ini. Contoh: .setultah DD-MM"},
	"delultah":   {EN: "Remove your saved birthday from this group.", ID: "Hapus tanggal ulang tahunmu dari grup ini."},
	"tag":        {EN: "Mention all members in a custom tag group. Usage: .tag <name>", ID: "Sebut semua anggota dalam tag grup khusus. Contoh: .tag <nama>"},
	"settag":     {EN: "Create or update a custom tag group. Usage: .settag <name> @user1 @user2", ID: "Buat atau perbarui tag grup khusus. Contoh: .settag <nama> @user1 @user2"},
	"deltag":     {EN: "Delete a custom tag group. Usage: .deltag <name>", ID: "Hapus tag grup khusus. Contoh: .deltag <nama>"},
	"listtag":    {EN: "List all custom tag groups in this group.", ID: "Lihat semua tag grup khusus yang tersimpan."},
	"savekontak": {EN: "Export all group members as a .vcf vCard file.", ID: "Ekspor semua anggota grup menjadi file kontak .vcf."},

	// Owner
	"broadcast":  {EN: "Broadcast a message to all joined groups. Usage: .broadcast <text>", ID: "Kirim pesan siaran ke semua grup yang diikuti bot. Contoh: .broadcast <teks>"},
	"react":      {EN: "React to a replied message with an emoji. Usage: .react 🔥", ID: "Beri reaksi emoji pada pesan yang dibalas. Contoh: .react 🔥"},
	"block":      {EN: "Block a WhatsApp contact. Usage: .block @user|phone", ID: "Blokir kontak WhatsApp. Contoh: .block @user|nomor"},
	"unblock":    {EN: "Unblock a WhatsApp contact. Usage: .unblock @user|phone", ID: "Buka blokir kontak WhatsApp. Contoh: .unblock @user|nomor"},
	"listblock":  {EN: "List all blocked WhatsApp contacts.", ID: "Lihat daftar semua kontak WhatsApp yang diblokir."},
	"kirim":      {EN: "Send a text message directly to a phone/JID. Usage: .kirim <phone> <msg>", ID: "Kirim pesan teks langsung ke nomor/JID tertentu. Contoh: .kirim <nomor> <pesan>"},
	"public":     {EN: "Switch bot back to public mode (disable self-mode).", ID: "Kembalikan bot ke mode publik (matikan self-mode)."},
	"addpoint":   {EN: "Add coins to a user's balance. Usage: .addpoint <phone|@user> <amt>", ID: "Tambahkan koin ke saldo pengguna. Contoh: .addpoint <nomor|@user> <jumlah>"},
	"delpoint":   {EN: "Subtract coins from a user's balance. Usage: .delpoint <phone|@user> <amt>", ID: "Kurangi koin dari saldo pengguna. Contoh: .delpoint <nomor|@user> <jumlah>"},
	"resetpoint": {EN: "Reset a user's coin balance to 0. Usage: .resetpoint <phone|@user>", ID: "Reset saldo koin pengguna menjadi 0. Contoh: .resetpoint <nomor|@user>"},
	"addexp":     {EN: "Add XP to a user. Usage: .addexp <phone|@user> <xp>", ID: "Tambahkan XP ke pengguna. Contoh: .addexp <nomor|@user> <xp>"},
	"delexp":     {EN: "Subtract XP from a user. Usage: .delexp <phone|@user> <xp>", ID: "Kurangi XP dari pengguna. Contoh: .delexp <nomor|@user> <xp>"},
}
