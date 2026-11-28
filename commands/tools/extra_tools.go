package toolscmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/handlers"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type activeReminder struct {
	ID     string
	ChatID string
	Owner  string
	Text   string
	FireAt time.Time
	Timer  *time.Timer
}

var (
	remMu     sync.Mutex
	reminders = map[string][]*activeReminder{}
	remSeq    int
)

func init() {
	cmds := []types.Command{
		{Name: "alquran", Aliases: []string{"quran"}, Category: "tools", Description: "Read an Al-Quran verse (Arabic + translation). Usage: .alquran <surah> [ayah]", Execute: cmdAlQuran},
		{Name: "alkitab", Aliases: []string{"bible"}, Category: "tools", Description: "Look up a Bible verse. Usage: .alkitab <book chapter:verse>", Execute: cmdAlkitab},
		{Name: "wiki", Aliases: []string{"wikipedia"}, Category: "tools", Description: "Search Wikipedia summary (ID/EN). Usage: .wiki <topic>", Execute: cmdWiki},
		{Name: "infounsur", Aliases: []string{"periodictable", "unsur"}, Category: "tools", Description: "Look up a chemical element by symbol, name, or atomic number. Usage: .infounsur Au", Execute: cmdInfoUnsur},
		{Name: "ipchecker", Aliases: []string{"ipinfo", "cekip"}, Category: "tools", Description: "Look up IP address geolocation and ISP info. Usage: .ipchecker <ip>", Execute: cmdIPChecker},
		{Name: "shortlink", Aliases: []string{"shorturl", "tinyurl"}, Category: "tools", Description: "Shorten a long URL via TinyURL. Usage: .shortlink <url>", Execute: cmdShortlink},
		{Name: "ssweb", Aliases: []string{"screenshot"}, Category: "tools", Description: "Capture a screenshot of a website URL. Usage: .ssweb <url>", Execute: cmdSSWeb},
		{Name: "nulis", Aliases: []string{"folio"}, Category: "tools", Description: "Render text onto a lined notebook paper image. Usage: .nulis <text>", Execute: cmdNulis},
		{Name: "kodebahasa", Aliases: []string{"langcodes"}, Category: "tools", Description: "List common ISO language codes for .translate and .tts.", Execute: cmdKodeBahasa},
		{Name: "reminder", Aliases: []string{"alarm"}, Category: "tools", Description: "Set a timed reminder in this chat. Usage: .reminder <5m|1h30m> <message> | list", Execute: cmdReminder},
		{Name: "delreminder", Aliases: []string{"delalarm"}, Category: "tools", Description: "Delete a reminder by ID. Usage: .delreminder <id>", Execute: cmdDelReminder},
		{Name: "resetreminder", Aliases: []string{"resetalarm"}, Category: "tools", Description: "Clear all active reminders in this chat.", Execute: cmdResetReminder},
		{Name: "cekperangkat", Aliases: []string{"cekdevice"}, Category: "tools", Description: "Detect the WhatsApp client platform (Android/iOS/Web) of a replied message.", Execute: cmdCekPerangkat},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func cmdAlQuran(client *whatsmeow.Client, msg *events.Message, args []string) {
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 0 {
		utils.SendReply(client, msg.Info.Chat, "📖 Usage: `.alquran <surah> [ayah]`\nContoh: `.alquran 1 1` atau `.alquran 2:255`")
		return
	}
	surahStr := fields[0]
	ayahStr := "1"
	if strings.Contains(surahStr, ":") {
		p := strings.SplitN(surahStr, ":", 2)
		surahStr, ayahStr = p[0], p[1]
	} else if len(fields) >= 2 {
		ayahStr = fields[1]
	}
	surah, err1 := strconv.Atoi(surahStr)
	ayah, err2 := strconv.Atoi(ayahStr)
	if err1 != nil || err2 != nil || surah < 1 || surah > 114 || ayah < 1 {
		utils.SendReply(client, msg.Info.Chat, "❌ Invalid surah/ayah. Surah must be 1–114 (e.g. `.alquran 1 1` or `.alquran 2:255`).")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	endpoint := fmt.Sprintf("https://api.alquran.cloud/v1/ayah/%d:%d/editions/quran-uthmani,id.indonesian,en.asad", surah, ayah)
	body, _, err := utils.HTTPGetBytes(ctx, endpoint, nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed to fetch Al-Quran verse: "+err.Error())
		return
	}
	var res struct {
		Status string `json:"status"`
		Data   []struct {
			Text  string `json:"text"`
			Surah struct {
				EnglishName string `json:"englishName"`
				Name        string `json:"name"`
			} `json:"surah"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil || len(res.Data) < 2 {
		utils.SendReply(client, msg.Info.Chat, "❌ Verse not found. Check surah & ayah number.")
		return
	}
	arabic := res.Data[0].Text
	idText := res.Data[1].Text
	enText := ""
	if len(res.Data) >= 3 {
		enText = "\n\n*EN:* " + res.Data[2].Text
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"📖 *QS. %s (%d:%d)* — %s\n\n%s\n\n*ID:* %s%s",
		res.Data[0].Surah.EnglishName, surah, ayah, res.Data[0].Surah.Name, arabic, idText, enText,
	))
}

func cmdAlkitab(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "✝️ Usage: `.alkitab <book chapter:verse>`\nExample: `.alkitab John 3:16`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	body, _, err := utils.HTTPGetBytes(ctx, "https://bible-api.com/"+url.PathEscape(q), nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch Bible verse: "+err.Error())
		return
	}
	var res struct {
		Reference       string `json:"reference"`
		Text            string `json:"text"`
		TranslationName string `json:"translation_name"`
	}
	if err := json.Unmarshal(body, &res); err != nil || strings.TrimSpace(res.Text) == "" {
		utils.SendReply(client, msg.Info.Chat, "❌ Verse not found. Try English book names like `John 3:16` or `Psalm 23:1`.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✝️ *%s* (%s)\n\n%s", res.Reference, res.TranslationName, strings.TrimSpace(res.Text)))
}

func cmdWiki(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.TrimSpace(strings.Join(args, " "))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "📚 Usage: `.wiki <topic>` (e.g. `.wiki Indonesia`)")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	for _, host := range []string{"id.wikipedia.org", "en.wikipedia.org"} {
		u := fmt.Sprintf("https://%s/api/rest_v1/page/summary/%s", host, url.PathEscape(strings.ReplaceAll(q, " ", "_")))
		body, _, err := utils.HTTPGetBytes(ctx, u, nil)
		if err != nil {
			continue
		}
		var res struct {
			Title       string `json:"title"`
			Extract     string `json:"extract"`
			ContentURLs struct {
				Desktop struct{ Page string } `json:"desktop"`
			} `json:"content_urls"`
		}
		if json.Unmarshal(body, &res) == nil && res.Extract != "" {
			ext := res.Extract
			if len(ext) > 900 {
				ext = ext[:900] + "..."
			}
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📚 *Wikipedia: %s*\n\n%s\n\n🔗 %s", res.Title, ext, res.ContentURLs.Desktop.Page))
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, "❌ Article not found on Wikipedia (ID/EN).")
}

type chemElem struct {
	Num    int
	Sym    string
	Name   string
	Mass   string
	Series string
}

var commonElements = []chemElem{
	{1, "H", "Hydrogen / Hidrogen", "1.008", "Nonmetal"},
	{2, "He", "Helium", "4.0026", "Noble Gas"},
	{3, "Li", "Lithium / Litium", "6.94", "Alkali Metal"},
	{6, "C", "Carbon / Karbon", "12.011", "Nonmetal"},
	{7, "N", "Nitrogen", "14.007", "Nonmetal"},
	{8, "O", "Oxygen / Oksigen", "15.999", "Nonmetal"},
	{9, "F", "Fluorine / Fluorin", "18.998", "Halogen"},
	{10, "Ne", "Neon", "20.180", "Noble Gas"},
	{11, "Na", "Sodium / Natrium", "22.990", "Alkali Metal"},
	{12, "Mg", "Magnesium", "24.305", "Alkaline Earth"},
	{13, "Al", "Aluminum / Aluminium", "26.982", "Post-Transition"},
	{14, "Si", "Silicon / Silikon", "28.085", "Metalloid"},
	{15, "P", "Phosphorus / Fosfor", "30.974", "Nonmetal"},
	{16, "S", "Sulfur / Belerang", "32.06", "Nonmetal"},
	{17, "Cl", "Chlorine / Klorin", "35.45", "Halogen"},
	{18, "Ar", "Argon", "39.948", "Noble Gas"},
	{19, "K", "Potassium / Kalium", "39.098", "Alkali Metal"},
	{20, "Ca", "Calcium / Kalsium", "40.078", "Alkaline Earth"},
	{22, "Ti", "Titanium", "47.867", "Transition Metal"},
	{24, "Cr", "Chromium / Kromium", "51.996", "Transition Metal"},
	{26, "Fe", "Iron / Besi", "55.845", "Transition Metal"},
	{28, "Ni", "Nickel / Nikel", "58.693", "Transition Metal"},
	{29, "Cu", "Copper / Tembaga", "63.546", "Transition Metal"},
	{30, "Zn", "Zinc / Seng", "65.38", "Transition Metal"},
	{47, "Ag", "Silver / Perak", "107.87", "Transition Metal"},
	{50, "Sn", "Tin / Timah", "118.71", "Post-Transition"},
	{53, "I", "Iodine / Yodium", "126.90", "Halogen"},
	{74, "W", "Tungsten / Wolfram", "183.84", "Transition Metal"},
	{78, "Pt", "Platinum / Platina", "195.08", "Transition Metal"},
	{79, "Au", "Gold / Emas", "196.97", "Transition Metal"},
	{80, "Hg", "Mercury / Raksa", "200.59", "Transition Metal"},
	{82, "Pb", "Lead / Timbal", "207.2", "Post-Transition"},
	{92, "U", "Uranium", "238.03", "Actinide"},
}

func cmdInfoUnsur(client *whatsmeow.Client, msg *events.Message, args []string) {
	q := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	if q == "" {
		utils.SendReply(client, msg.Info.Chat, "⚗️ Usage: `.infounsur <symbol|name|number>` (e.g. `.infounsur Au`, `.infounsur 79`, `.infounsur emas`)")
		return
	}
	numQ, numErr := strconv.Atoi(q)
	for _, e := range commonElements {
		if (numErr == nil && e.Num == numQ) || strings.EqualFold(e.Sym, q) || strings.Contains(strings.ToLower(e.Name), q) {
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
				"⚗️ *Unsur Kimia: %s (%s)*\n• *Nomor Atom:* `%d`\n• *Massa Atom:* `%s u`\n• *Golongan:* %s",
				e.Name, e.Sym, e.Num, e.Mass, e.Series,
			))
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, "❌ Element not found. Try symbols like `H`, `O`, `Fe`, `Au`, `Ag`, `Cu`, `U`, or atomic numbers.")
}

func cmdIPChecker(client *whatsmeow.Client, msg *events.Message, args []string) {
	ip := strings.TrimSpace(strings.Join(args, ""))
	if ip == "" {
		utils.SendReply(client, msg.Info.Chat, "🌐 Usage: `.ipchecker <ip_or_domain>` (e.g. `.ipchecker 1.1.1.1`)")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	body, _, err := utils.HTTPGetBytes(ctx, "http://ip-api.com/json/"+url.PathEscape(ip)+"?fields=status,message,country,regionName,city,isp,org,as,query,timezone", nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ IP lookup failed: "+err.Error())
		return
	}
	var res struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Query    string `json:"query"`
		Country  string `json:"country"`
		Region   string `json:"regionName"`
		City     string `json:"city"`
		ISP      string `json:"isp"`
		Org      string `json:"org"`
		AS       string `json:"as"`
		Timezone string `json:"timezone"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Status != "success" {
		utils.SendReply(client, msg.Info.Chat, "❌ Invalid IP/domain: "+res.Message)
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf(
		"🌐 *IP Info: `%s`*\n• *Location:* %s, %s, %s\n• *Timezone:* %s\n• *ISP:* %s\n• *Org:* %s\n• *ASN:* %s",
		res.Query, res.City, res.Region, res.Country, res.Timezone, res.ISP, res.Org, res.AS,
	))
}

func cmdShortlink(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawURL := strings.TrimSpace(strings.Join(args, " "))
	if rawURL == "" || (!strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://")) {
		utils.SendReply(client, msg.Info.Chat, "🔗 Usage: `.shortlink https://example.com/very/long/url`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	body, _, err := utils.HTTPGetBytes(ctx, "https://tinyurl.com/api-create.php?url="+url.QueryEscape(rawURL), nil)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Shortlink failed: "+err.Error())
		return
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🔗 *Shortened URL:*\n%s", strings.TrimSpace(string(body))))
}

func cmdSSWeb(client *whatsmeow.Client, msg *events.Message, args []string) {
	rawURL := strings.TrimSpace(strings.Join(args, " "))
	if rawURL == "" {
		utils.SendReply(client, msg.Info.Chat, "📸 Usage: `.ssweb https://example.com`")
		return
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	imgBytes, _, err := utils.HTTPGetBytes(ctx, "https://image.thum.io/get/width/1280/crop/720/noanimate/"+rawURL, nil)
	if err != nil || len(imgBytes) < 1024 {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not capture screenshot of "+rawURL)
		return
	}
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, imgBytes, "image/png", "📸 *Screenshot:* "+rawURL)
}

func cmdNulis(client *whatsmeow.Client, msg *events.Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		utils.SendReply(client, msg.Info.Chat, "📝 Usage: `.nulis <text>`")
		return
	}
	const W, H = 820, 1080
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	paper := color.RGBA{252, 250, 240, 255}
	lineCol := color.RGBA{185, 210, 235, 255}
	marginCol := color.RGBA{230, 130, 130, 255}
	ink := color.RGBA{25, 35, 85, 255}

	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			img.SetRGBA(x, y, paper)
		}
	}
	// Red vertical margin line
	for y := 0; y < H; y++ {
		img.SetRGBA(92, y, marginCol)
		img.SetRGBA(93, y, marginCol)
	}
	// Horizontal ruled lines
	for y := 90; y < H-40; y += 34 {
		for x := 0; x < W; x++ {
			img.SetRGBA(x, y, lineCol)
		}
	}

	// Word wrap text cleanly (~88 chars per line for basicfont)
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := ""
		for _, w := range words {
			if len(cur)+len(w)+1 > 85 && cur != "" {
				lines = append(lines, cur)
				cur = w
			} else if cur == "" {
				cur = w
			} else {
				cur += " " + w
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
	}

	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(ink),
		Face: basicfont.Face7x13,
	}
	for i, line := range lines {
		y := 84 + i*34
		if y > H-45 {
			break
		}
		d.Dot = fixed.P(106, y)
		d.DrawString(line)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Failed rendering notebook page.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = utils.SendImageData(ctx, client, msg.Info.Chat, buf.Bytes(), "image/png", "📝 *Nulis*")
}

func cmdKodeBahasa(client *whatsmeow.Client, msg *events.Message, args []string) {
	utils.SendReply(client, msg.Info.Chat,
		"🌐 *Kode Bahasa / Language Codes* (`.translate` & `.tts`)\n\n"+
			"• `id` — Indonesian\n• `en` — English\n• `ja` — Japanese\n• `ko` — Korean\n"+
			"• `zh` — Chinese\n• `ar` — Arabic\n• `es` — Spanish\n• `fr` — French\n"+
			"• `de` — German\n• `ru` — Russian\n• `th` — Thai\n• `vi` — Vietnamese\n• `ms` — Malay\n• `jw` — Javanese\n• `su` — Sundanese",
	)
}

func cmdReminder(client *whatsmeow.Client, msg *events.Message, args []string) {
	raw := strings.TrimSpace(strings.Join(args, " "))
	chatID := msg.Info.Chat.ToNonAD().String()
	if raw == "" || strings.EqualFold(raw, "list") {
		remMu.Lock()
		list := append([]*activeReminder(nil), reminders[chatID]...)
		remMu.Unlock()
		if len(list) == 0 {
			utils.SendReply(client, msg.Info.Chat, "⏰ *Reminders*\nNo active reminders in this chat.\nUsage: `.reminder 10m Drink water`")
			return
		}
		var b strings.Builder
		b.WriteString("⏰ *Active Reminders*\n\n")
		for _, r := range list {
			rem := time.Until(r.FireAt).Round(time.Second)
			if rem < 0 {
				rem = 0
			}
			fmt.Fprintf(&b, "• `%s` — %s (in `%s`)\n", r.ID, r.Text, rem)
		}
		utils.SendReply(client, msg.Info.Chat, b.String())
		return
	}

	parts := strings.SplitN(raw, " ", 2)
	if len(parts) < 2 {
		utils.SendReply(client, msg.Info.Chat, "⏰ Usage: `.reminder <duration> <message>` (e.g. `.reminder 15m Meeting`)")
		return
	}
	dur, err := time.ParseDuration(parts[0])
	if err != nil || dur < 5*time.Second || dur > 7*24*time.Hour {
		utils.SendReply(client, msg.Info.Chat, "❌ Invalid duration (min 5s, max 168h). Examples: `30s`, `10m`, `1h30m`.")
		return
	}
	note := strings.TrimSpace(parts[1])
	senderJID := msg.Info.Sender.ToNonAD()
	chatJID := msg.Info.Chat

	remMu.Lock()
	remSeq++
	id := fmt.Sprintf("R%d", remSeq)
	item := &activeReminder{
		ID:     id,
		ChatID: chatID,
		Owner:  senderJID.User,
		Text:   note,
		FireAt: time.Now().Add(dur),
	}
	item.Timer = time.AfterFunc(dur, func() {
		remMu.Lock()
		arr := reminders[chatID]
		for i, x := range arr {
			if x.ID == id {
				reminders[chatID] = append(arr[:i], arr[i+1:]...)
				break
			}
		}
		remMu.Unlock()
		utils.SendMentionReply(client, chatJID, fmt.Sprintf("⏰ *REMINDER* (`%s`)\n@%s — %s", id, senderJID.User, note), []string{senderJID.String()})
	})
	reminders[chatID] = append(reminders[chatID], item)
	remMu.Unlock()

	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("✅ Reminder `%s` set for `%s`: _%s_", id, dur, note))
}

func cmdDelReminder(client *whatsmeow.Client, msg *events.Message, args []string) {
	id := strings.ToUpper(strings.TrimSpace(strings.Join(args, "")))
	if id == "" {
		utils.SendReply(client, msg.Info.Chat, "Usage: `.delreminder <ID>`")
		return
	}
	chatID := msg.Info.Chat.ToNonAD().String()
	remMu.Lock()
	defer remMu.Unlock()
	arr := reminders[chatID]
	for i, x := range arr {
		if strings.EqualFold(x.ID, id) {
			x.Timer.Stop()
			reminders[chatID] = append(arr[:i], arr[i+1:]...)
			utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗑️ Deleted reminder `%s`.", x.ID))
			return
		}
	}
	utils.SendReply(client, msg.Info.Chat, "❌ Reminder not found.")
}

func cmdResetReminder(client *whatsmeow.Client, msg *events.Message, args []string) {
	chatID := msg.Info.Chat.ToNonAD().String()
	remMu.Lock()
	for _, x := range reminders[chatID] {
		x.Timer.Stop()
	}
	n := len(reminders[chatID])
	delete(reminders, chatID)
	remMu.Unlock()
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗑️ Cleared `%d` active reminder(s) in this chat.", n))
}

func cmdCekPerangkat(client *whatsmeow.Client, msg *events.Message, args []string) {
	id := msg.Info.ID
	if ctxInfo := utils.ExtractContextInfo(msg.Message); ctxInfo != nil && ctxInfo.GetStanzaID() != "" {
		id = ctxInfo.GetStanzaID()
	}
	device := "Android 📱"
	switch {
	case strings.HasPrefix(id, "3EB0") && len(id) >= 20:
		device = "WhatsApp Web / Multi-Device Bot 💻"
	case strings.HasPrefix(id, "3A") && len(id) == 20:
		device = "iOS (iPhone / iPad) 🍎"
	case strings.HasPrefix(id, "3F"):
		device = "WhatsApp Desktop 🖥️"
	case len(id) == 18:
		device = "WhatsApp Business / API 💼"
	}
	utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("📱 *Device Detection*\n• *Message ID:* `%s`\n• *Detected Platform:* *%s*", id, device))
}
