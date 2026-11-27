package market

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/ai"
	"bot/config"
	db "bot/databases"
	"bot/handlers"
	"bot/i18n"
	"bot/types"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// DefaultWatchlist contains major crypto, commodities, US stocks, IDX stocks, and forex.
var DefaultWatchlist = []string{"BTC", "ETH", "SOL", "GOLD", "NVDA", "AAPL", "TSLA", "IHSG", "BBCA.JK", "USDIDR"}

// Snapshot holds live real-world market data + recent daily closes for a symbol.
type Snapshot struct {
	InputSymbol string
	YahooSymbol string
	Display     string
	Name        string
	AssetType   string
	Currency    string
	Price       float64
	PrevClose   float64
	Change      float64
	ChangePct   float64
	DayHigh     float64
	DayLow      float64
	Closes      []float64
	Source      string
	FetchedAt   time.Time
}

// Technicals holds quantitative technical indicators computed from historical daily closes.
type Technicals struct {
	SMA7          float64
	SMA20         float64
	RSI14         float64
	Support       float64
	Resistance    float64
	VolatilityPct float64
	Sparkline     string
	Signal        string // "BULLISH", "BEARISH", "NEUTRAL"
	Score         int
}

type cacheEntry struct {
	snap      *Snapshot
	expiresAt time.Time
}

var (
	marketHTTP = &http.Client{Timeout: 10 * time.Second}
	cacheMu    sync.Mutex
	quoteCache = map[string]cacheEntry{}
)

func init() {
	cmds := []types.Command{
		{
			Name:        "market",
			Aliases:     []string{"price", "harga", "quote", "saham", "crypto"},
			Category:    "market",
			Description: "Check live real-world market price (Crypto, Stocks, IDX, Gold, Forex). Usage: .market <BTC|ETH|NVDA|BBCA|GOLD|USDIDR>",
			Execute:     cmdMarket,
		},
		{
			Name:        "markettop",
			Aliases:     []string{"movers", "marketmovers", "updown"},
			Category:    "market",
			Description: "See what real-world assets are UP 📈 and DOWN 📉 right now across Crypto, Stocks, Gold & IDX.",
			Execute:     cmdMarketTop,
		},
		{
			Name:        "marketalert",
			Aliases:     []string{"alert", "pricealert"},
			Category:    "market",
			Description: "Set real-world price/percent alerts. Usage: .marketalert add <SYM> <above|below|pct_up|pct_down> <val> | list | del <id>",
			Execute:     cmdMarketAlert,
		},
		{
			Name:        "marketremind",
			Aliases:     []string{"mremind", "marketreminder"},
			Category:    "market",
			Description: "Schedule automatic UP/DOWN real-world market reminders for this chat. Usage: .marketremind on [min] [SYM1,SYM2] | off | status | now",
			Execute:     cmdMarketRemind,
		},
		{
			Name:        "marketai",
			Aliases:     []string{"analisa", "aianalysis", "marketanalysis"},
			Category:    "market",
			Description: "AI + Quantitative Technical Analysis (RSI-14, SMA-7/20, Support/Resistance, Trend Bias). Usage: .marketai <symbol>",
			Execute:     cmdMarketAI,
		},
	}
	for _, c := range cmds {
		handlers.RegisterCommand(c)
	}
}

func isID(client *whatsmeow.Client, msg *events.Message) bool {
	return i18n.ResolveLang(client, msg) == i18n.LangID
}

func pickLang(client *whatsmeow.Client, msg *events.Message, idText, enText string) string {
	if isID(client, msg) {
		return idText
	}
	return enText
}

var cryptoShortcuts = map[string]bool{
	"BTC": true, "ETH": true, "SOL": true, "BNB": true, "XRP": true,
	"DOGE": true, "ADA": true, "TON": true, "PEPE": true, "AVAX": true,
	"LINK": true, "SUI": true, "TRX": true, "SHIB": true, "LTC": true,
	"DOT": true, "NEAR": true, "APT": true, "ARB": true, "OP": true,
	"POL": true, "MATIC": true, "UNI": true, "BCH": true, "XLM": true,
}

var idxBluechips = map[string]bool{
	"BBCA": true, "BBRI": true, "BMRI": true, "BBNI": true, "TLKM": true,
	"ASII": true, "GOTO": true, "ANTM": true, "ADRO": true, "UNVR": true,
	"ICBP": true, "INDF": true, "BREN": true, "AMMN": true, "PGAS": true,
	"PTBA": true, "MDKA": true, "KLBF": true, "BRIS": true, "CPIN": true,
}

// NormalizeMarketSymbol maps friendly user inputs (BTC, GOLD, BBCA, USDIDR, IHSG, NVDA)
// to Yahoo Finance symbols, clean display names, and asset classes.
func NormalizeMarketSymbol(raw string) (yahooSym, displaySym, assetType string) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "$")
	if s == "" {
		return "BTC-USD", "BTC", "Crypto"
	}

	switch s {
	case "GOLD", "XAU", "XAUUSD", "EMAS":
		return "GC=F", "GOLD", "Commodity"
	case "SILVER", "XAG", "PERAK":
		return "SI=F", "SILVER", "Commodity"
	case "OIL", "WTI", "CRUDE":
		return "CL=F", "WTI OIL", "Commodity"
	case "BRENT":
		return "BZ=F", "BRENT", "Commodity"
	case "USDIDR", "USD", "DOLAR", "USD/IDR":
		return "USDIDR=X", "USD/IDR", "Forex"
	case "EURUSD", "EUR/USD":
		return "EURUSD=X", "EUR/USD", "Forex"
	case "GBPUSD", "GBP/USD":
		return "GBPUSD=X", "GBP/USD", "Forex"
	case "USDJPY", "USD/JPY":
		return "USDJPY=X", "USD/JPY", "Forex"
	case "IHSG", "JKSE", "IDX":
		return "^JKSE", "IHSG", "Index"
	case "SP500", "SPX", "S&P500":
		return "^GSPC", "S&P 500", "Index"
	case "NASDAQ", "IXIC":
		return "^IXIC", "NASDAQ", "Index"
	case "DOW", "DJI":
		return "^DJI", "DOW", "Index"
	}

	if cryptoShortcuts[s] {
		return s + "-USD", s, "Crypto"
	}
	if strings.HasSuffix(s, "-USD") {
		return s, strings.TrimSuffix(s, "-USD"), "Crypto"
	}
	if strings.HasSuffix(s, "USDT") && len(s) > 4 {
		base := strings.TrimSuffix(s, "USDT")
		return base + "-USD", base, "Crypto"
	}
	if idxBluechips[s] {
		return s + ".JK", s + ".JK", "IDX Stock"
	}
	if strings.HasSuffix(s, ".JK") {
		return s, s, "IDX Stock"
	}
	if strings.HasSuffix(s, "=X") {
		return s, strings.TrimSuffix(s, "=X"), "Forex"
	}
	if strings.HasSuffix(s, "=F") {
		return s, s, "Commodity"
	}
	if strings.HasPrefix(s, "^") {
		return s, s, "Index"
	}
	return s, s, "Stock"
}

// FetchMarketSnapshot fetches live price + 1-month daily closes with a 45s in-memory cache.
func FetchMarketSnapshot(ctx context.Context, rawSymbol string) (*Snapshot, error) {
	yahooSym, displaySym, assetType := NormalizeMarketSymbol(rawSymbol)

	cacheMu.Lock()
	if ce, ok := quoteCache[yahooSym]; ok && time.Now().Before(ce.expiresAt) {
		snap := *ce.snap
		cacheMu.Unlock()
		return &snap, nil
	}
	cacheMu.Unlock()

	snap, err := fetchYahooChart(ctx, rawSymbol, yahooSym, displaySym, assetType)
	if err != nil && assetType == "Crypto" {
		if bSnap, bErr := fetchBinanceCrypto(ctx, rawSymbol, displaySym); bErr == nil {
			snap = bSnap
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}

	cacheMu.Lock()
	copySnap := *snap
	quoteCache[yahooSym] = cacheEntry{snap: &copySnap, expiresAt: time.Now().Add(45 * time.Second)}
	cacheMu.Unlock()
	return snap, nil
}

type yahooChartResp struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Currency                   string  `json:"currency"`
				Symbol                     string  `json:"symbol"`
				ShortName                  string  `json:"shortName"`
				LongName                   string  `json:"longName"`
				RegularMarketPrice         float64 `json:"regularMarketPrice"`
				RegularMarketDayHigh       float64 `json:"regularMarketDayHigh"`
				RegularMarketDayLow        float64 `json:"regularMarketDayLow"`
				PreviousClose              float64 `json:"previousClose"`
				RegularMarketPreviousClose float64 `json:"regularMarketPreviousClose"`
				ChartPreviousClose         float64 `json:"chartPreviousClose"`
			} `json:"meta"`
			Indicators struct {
				Quote []struct {
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func fetchYahooChart(ctx context.Context, rawSymbol, yahooSym, displaySym, assetType string) (*Snapshot, error) {
	endpoint := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1mo&interval=1d", url.PathEscape(yahooSym))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := marketHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo http %d for %s", resp.StatusCode, yahooSym)
	}

	var parsed yahooChartResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Chart.Error != nil {
		return nil, fmt.Errorf("%s: %s", parsed.Chart.Error.Code, parsed.Chart.Error.Description)
	}
	if len(parsed.Chart.Result) == 0 {
		return nil, fmt.Errorf("symbol '%s' not found", rawSymbol)
	}

	res := parsed.Chart.Result[0]
	meta := res.Meta

	var closes []float64
	if len(res.Indicators.Quote) > 0 {
		for _, ptr := range res.Indicators.Quote[0].Close {
			if ptr != nil && !math.IsNaN(*ptr) && *ptr > 0 {
				closes = append(closes, *ptr)
			}
		}
	}

	price := meta.RegularMarketPrice
	if price <= 0 && len(closes) > 0 {
		price = closes[len(closes)-1]
	}
	if price <= 0 {
		return nil, fmt.Errorf("no valid price for %s", yahooSym)
	}

	// Determine accurate 1-day previous close (avoiding 1-month chartPreviousClose skew).
	prev := meta.RegularMarketPreviousClose
	if prev <= 0 {
		prev = meta.PreviousClose
	}
	if prev <= 0 && len(closes) >= 2 {
		prev = closes[len(closes)-2]
	}
	if prev <= 0 {
		prev = meta.ChartPreviousClose
	}

	change := 0.0
	changePct := 0.0
	if prev > 0 {
		change = price - prev
		changePct = (change / prev) * 100.0
	}

	name := strings.TrimSpace(meta.ShortName)
	if name == "" {
		name = strings.TrimSpace(meta.LongName)
	}
	if name == "" {
		name = displaySym
	}

	currency := meta.Currency
	if currency == "" {
		currency = "USD"
	}

	dayHigh := meta.RegularMarketDayHigh
	dayLow := meta.RegularMarketDayLow
	if dayHigh <= 0 {
		dayHigh = price
	}
	if dayLow <= 0 {
		dayLow = price
	}

	return &Snapshot{
		InputSymbol: strings.ToUpper(strings.TrimSpace(rawSymbol)),
		YahooSymbol: yahooSym,
		Display:     displaySym,
		Name:        name,
		AssetType:   assetType,
		Currency:    currency,
		Price:       price,
		PrevClose:   prev,
		Change:      change,
		ChangePct:   changePct,
		DayHigh:     dayHigh,
		DayLow:      dayLow,
		Closes:      closes,
		Source:      "Yahoo Finance",
		FetchedAt:   time.Now(),
	}, nil
}

func fetchBinanceCrypto(ctx context.Context, rawSymbol, displaySym string) (*Snapshot, error) {
	pair := strings.ToUpper(displaySym) + "USDT"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.binance.com/api/v3/ticker/24hr?symbol="+url.QueryEscape(pair), nil)
	if err != nil {
		return nil, err
	}
	resp, err := marketHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance http %d for %s", resp.StatusCode, pair)
	}
	var tick struct {
		LastPrice          string `json:"lastPrice"`
		PrevClosePrice     string `json:"prevClosePrice"`
		PriceChange        string `json:"priceChange"`
		PriceChangePercent string `json:"priceChangePercent"`
		HighPrice          string `json:"highPrice"`
		LowPrice           string `json:"lowPrice"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&tick); err != nil {
		return nil, err
	}
	price, _ := strconv.ParseFloat(tick.LastPrice, 64)
	prev, _ := strconv.ParseFloat(tick.PrevClosePrice, 64)
	chg, _ := strconv.ParseFloat(tick.PriceChange, 64)
	pct, _ := strconv.ParseFloat(tick.PriceChangePercent, 64)
	high, _ := strconv.ParseFloat(tick.HighPrice, 64)
	low, _ := strconv.ParseFloat(tick.LowPrice, 64)
	if price <= 0 {
		return nil, fmt.Errorf("binance empty price for %s", pair)
	}

	// Best-effort 30d daily closes from Binance klines.
	var closes []float64
	kReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.binance.com/api/v3/klines?symbol="+url.QueryEscape(pair)+"&interval=1d&limit=30", nil)
	if err == nil {
		if kResp, err := marketHTTP.Do(kReq); err == nil {
			defer kResp.Body.Close()
			if kResp.StatusCode == http.StatusOK {
				var rows [][]json.RawMessage
				if json.NewDecoder(io.LimitReader(kResp.Body, 256*1024)).Decode(&rows) == nil {
					for _, row := range rows {
						if len(row) >= 5 {
							var cStr string
							if json.Unmarshal(row[4], &cStr) == nil {
								if v, err := strconv.ParseFloat(cStr, 64); err == nil && v > 0 {
									closes = append(closes, v)
								}
							}
						}
					}
				}
			}
		}
	}

	return &Snapshot{
		InputSymbol: strings.ToUpper(strings.TrimSpace(rawSymbol)),
		YahooSymbol: displaySym + "-USD",
		Display:     displaySym,
		Name:        displaySym + " / USDT",
		AssetType:   "Crypto",
		Currency:    "USD",
		Price:       price,
		PrevClose:   prev,
		Change:      chg,
		ChangePct:   pct,
		DayHigh:     high,
		DayLow:      low,
		Closes:      closes,
		Source:      "Binance",
		FetchedAt:   time.Now(),
	}, nil
}

// ComputeTechnicals computes SMA-7, SMA-20, RSI-14, 20d Support/Resistance, Volatility, Sparkline, and Trend Bias.
func ComputeTechnicals(closes []float64, currentPrice float64) Technicals {
	series := make([]float64, 0, len(closes)+1)
	for _, c := range closes {
		if !math.IsNaN(c) && c > 0 {
			series = append(series, c)
		}
	}
	if currentPrice > 0 {
		if len(series) == 0 || math.Abs(series[len(series)-1]-currentPrice)/currentPrice > 0.0001 {
			series = append(series, currentPrice)
		}
	}
	if len(series) == 0 {
		return Technicals{Signal: "NEUTRAL"}
	}

	t := Technicals{
		SMA7:      sma(series, 7),
		SMA20:     sma(series, 20),
		RSI14:     rsi(series, 14),
		Sparkline: buildSparkline(series, 14),
	}

	// Support & Resistance over last 20 candles.
	win := series
	if len(win) > 20 {
		win = win[len(win)-20:]
	}
	minV, maxV := win[0], win[0]
	for _, v := range win[1:] {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	t.Support = minV
	t.Resistance = maxV

	// Daily return standard deviation (%) as volatility metric.
	if len(win) >= 3 {
		rets := make([]float64, 0, len(win)-1)
		sum := 0.0
		for i := 1; i < len(win); i++ {
			if win[i-1] > 0 {
				r := ((win[i] - win[i-1]) / win[i-1]) * 100.0
				rets = append(rets, r)
				sum += r
			}
		}
		if len(rets) > 0 {
			mean := sum / float64(len(rets))
			varSq := 0.0
			for _, r := range rets {
				d := r - mean
				varSq += d * d
			}
			t.VolatilityPct = math.Sqrt(varSq / float64(len(rets)))
		}
	}

	// Composite directional bias score.
	last := series[len(series)-1]
	score := 0
	if t.SMA7 > 0 {
		if last > t.SMA7 {
			score++
		} else if last < t.SMA7 {
			score--
		}
	}
	if t.SMA20 > 0 {
		if last > t.SMA20 {
			score++
		} else if last < t.SMA20 {
			score--
		}
	}
	if t.SMA7 > 0 && t.SMA20 > 0 {
		if t.SMA7 > t.SMA20 {
			score++
		} else if t.SMA7 < t.SMA20 {
			score--
		}
	}
	switch {
	case t.RSI14 >= 55 && t.RSI14 <= 72:
		score++
	case t.RSI14 <= 45 && t.RSI14 >= 28:
		score--
	}

	t.Score = score
	switch {
	case score >= 2:
		t.Signal = "BULLISH"
	case score <= -2:
		t.Signal = "BEARISH"
	default:
		t.Signal = "NEUTRAL"
	}
	return t
}

func sma(series []float64, period int) float64 {
	if len(series) == 0 {
		return 0
	}
	if len(series) < period {
		period = len(series)
	}
	sum := 0.0
	for _, v := range series[len(series)-period:] {
		sum += v
	}
	return sum / float64(period)
}

func rsi(series []float64, period int) float64 {
	if len(series) < 2 {
		return 50
	}
	n := len(series) - 1
	if n > period {
		series = series[len(series)-(period+1):]
		n = period
	}
	var gains, losses float64
	for i := 1; i <= n; i++ {
		diff := series[i] - series[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses -= diff
		}
	}
	avgGain := gains / float64(n)
	avgLoss := losses / float64(n)
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50
		}
		return 100
	}
	rs := avgGain / avgLoss
	return 100.0 - (100.0 / (1.0 + rs))
}

func buildSparkline(series []float64, maxBars int) string {
	if len(series) == 0 {
		return ""
	}
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	win := series
	if len(win) > maxBars {
		win = win[len(win)-maxBars:]
	}
	minV, maxV := win[0], win[0]
	for _, v := range win[1:] {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	span := maxV - minV
	var b strings.Builder
	for _, v := range win {
		idx := 3
		if span > 0 {
			idx = int(math.Round(((v - minV) / span) * float64(len(blocks)-1)))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(blocks) {
				idx = len(blocks) - 1
			}
		}
		b.WriteRune(blocks[idx])
	}
	return b.String()
}

// FormatPrice formats crypto/stock/IDR prices cleanly depending on magnitude & currency.
func FormatPrice(price float64, currency string) string {
	prefix := "$"
	switch strings.ToUpper(currency) {
	case "IDR":
		prefix = "Rp"
	case "EUR":
		prefix = "€"
	case "GBP":
		prefix = "£"
	case "JPY":
		prefix = "¥"
	case "":
		prefix = ""
	default:
		if strings.ToUpper(currency) != "USD" {
			prefix = strings.ToUpper(currency) + " "
		}
	}

	abs := math.Abs(price)
	switch {
	case strings.ToUpper(currency) == "IDR" || abs >= 1000:
		return fmt.Sprintf("%s%.2f", prefix, price)
	case abs >= 1:
		return fmt.Sprintf("%s%.2f", prefix, price)
	case abs >= 0.01:
		return fmt.Sprintf("%s%.4f", prefix, price)
	default:
		return fmt.Sprintf("%s%.6f", prefix, price)
	}
}

func trendBadge(pct float64) string {
	if pct > 0.05 {
		return "🟢 UP"
	}
	if pct < -0.05 {
		return "🔴 DOWN"
	}
	return "⚪ FLAT"
}

func signalLabel(sig string) string {
	switch sig {
	case "BULLISH":
		return "📈 BULLISH"
	case "BEARISH":
		return "📉 BEARISH"
	default:
		return "⚖️ NEUTRAL"
	}
}

// --- Commands ---

func cmdMarket(client *whatsmeow.Client, msg *events.Message, args []string) {
	if len(args) == 0 {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			"📈 *NOPEBOT REAL-WORLD MARKET*\n\nContoh penggunaan:\n• `.market BTC` — Harga Bitcoin + RSI/SMA\n• `.market GOLD` / `.market NVDA` / `.market BBCA` / `.market USDIDR`\n• `.markettop` — Lihat aset yang sedang *NAIK 📈* & *TURUN 📉*\n• `.marketai BTC` — Analisa teknikal + AI lengkap\n• `.marketalert add BTC above 100000` — Notifikasi harga otomatis\n• `.marketremind on 60 BTC,ETH,GOLD,BBCA` — Reminder market berkala",
			"📈 *NOPEBOT REAL-WORLD MARKET*\n\nExamples:\n• `.market BTC` — Live Bitcoin quote + RSI/SMA\n• `.market GOLD` / `.market NVDA` / `.market BBCA` / `.market USDIDR`\n• `.markettop` — Live *UP 📈* & *DOWN 📉* movers\n• `.marketai BTC` — Technical + AI market analysis\n• `.marketalert add BTC above 100000` — Price threshold alert\n• `.marketremind on 60 BTC,ETH,GOLD,NVDA` — Scheduled UP/DOWN digest",
		))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	snap, err := FetchMarketSnapshot(ctx, args[0])
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Failed to fetch market data for `%s`: %v", args[0], err))
		return
	}
	tech := ComputeTechnicals(snap.Closes, snap.Price)

	sign := "+"
	if snap.ChangePct < 0 {
		sign = ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s *%s* (`%s` • %s)\n", trendBadge(snap.ChangePct), snap.Display, snap.YahooSymbol, snap.AssetType)
	if snap.Name != "" && !strings.EqualFold(snap.Name, snap.Display) {
		fmt.Fprintf(&b, "_%s_\n", snap.Name)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "💵 Price: *%s*\n", FormatPrice(snap.Price, snap.Currency))
	fmt.Fprintf(&b, "📊 24h: `%s%.2f%%` (`%s%s`)\n", sign, snap.ChangePct, sign, FormatPrice(snap.Change, snap.Currency))
	fmt.Fprintf(&b, "↕️ Range: `%s` — `%s`\n", FormatPrice(snap.DayLow, snap.Currency), FormatPrice(snap.DayHigh, snap.Currency))
	if tech.Sparkline != "" {
		fmt.Fprintf(&b, "📉 14D Trend: `%s`\n", tech.Sparkline)
	}
	fmt.Fprintf(&b, "🧮 RSI-14: `%.1f` | SMA-7: `%s` | SMA-20: `%s`\n", tech.RSI14, FormatPrice(tech.SMA7, snap.Currency), FormatPrice(tech.SMA20, snap.Currency))
	fmt.Fprintf(&b, "🧭 Bias: *%s* | Source: _%s_\n", signalLabel(tech.Signal), snap.Source)
	b.WriteString(pickLang(client, msg,
		fmt.Sprintf("\n💡 Ketik `.marketai %s` untuk analisa AI lengkap!", snap.Display),
		fmt.Sprintf("\n💡 Run `.marketai %s` for full AI + technical analysis!", snap.Display),
	))
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

func fetchWatchlistConcurrent(ctx context.Context, symbols []string) []*Snapshot {
	if len(symbols) > 15 {
		symbols = symbols[:15]
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out = make([]*Snapshot, 0, len(symbols))
		sem = make(chan struct{}, 5)
	)
	for _, sym := range symbols {
		s := strings.TrimSpace(sym)
		if s == "" {
			continue
		}
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if snap, err := FetchMarketSnapshot(ctx, sym); err == nil && snap != nil {
				mu.Lock()
				out = append(out, snap)
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	return out
}

// FormatDigest renders a clean UP / DOWN market summary from a slice of snapshots.
func FormatDigest(snaps []*Snapshot, lang string) string {
	var ups, downs []*Snapshot
	for _, s := range snaps {
		if s.ChangePct >= 0 {
			ups = append(ups, s)
		} else {
			downs = append(downs, s)
		}
	}
	sort.Slice(ups, func(i, j int) bool { return ups[i].ChangePct > ups[j].ChangePct })
	sort.Slice(downs, func(i, j int) bool { return downs[i].ChangePct < downs[j].ChangePct })

	var b strings.Builder
	if lang == i18n.LangID {
		fmt.Fprintf(&b, "📊 *NOPEBOT MARKET REMINDER* (%s)\n", time.Now().Format("02 Jan 15:04"))
		b.WriteString("\n🟢 *NAIK (UP)*\n")
	} else {
		fmt.Fprintf(&b, "📊 *NOPEBOT MARKET DIGEST* (%s)\n", time.Now().Format("02 Jan 15:04"))
		b.WriteString("\n🟢 *MOVING UP*\n")
	}
	if len(ups) == 0 {
		b.WriteString("• _None_\n")
	}
	for _, s := range ups {
		fmt.Fprintf(&b, "• *%s*: `%s` (`+%.2f%%`) 📈\n", s.Display, FormatPrice(s.Price, s.Currency), s.ChangePct)
	}

	if lang == i18n.LangID {
		b.WriteString("\n🔴 *TURUN (DOWN)*\n")
	} else {
		b.WriteString("\n🔴 *MOVING DOWN*\n")
	}
	if len(downs) == 0 {
		b.WriteString("• _None_\n")
	}
	for _, s := range downs {
		fmt.Fprintf(&b, "• *%s*: `%s` (`%.2f%%`) 📉\n", s.Display, FormatPrice(s.Price, s.Currency), s.ChangePct)
	}

	if lang == i18n.LangID {
		b.WriteString("\n💡 _Ketik `.marketai <simbol>` untuk analisa AI atau `.marketalert` untuk pasang alarm harga._")
	} else {
		b.WriteString("\n💡 _Run `.marketai <symbol>` for AI analysis or `.marketalert` to set price alerts._")
	}
	return strings.TrimSpace(b.String())
}

func cmdMarketTop(client *whatsmeow.Client, msg *events.Message, args []string) {
	symbols := DefaultWatchlist
	if len(args) > 0 {
		custom := parseSymbolsCSV(strings.Join(args, ","))
		if len(custom) > 0 {
			symbols = custom
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	snaps := fetchWatchlistConcurrent(ctx, symbols)
	if len(snaps) == 0 {
		utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch live market quotes right now. Please try again in a moment.")
		return
	}
	utils.SendReply(client, msg.Info.Chat, FormatDigest(snaps, i18n.ResolveLang(client, msg)))
}

func cmdMarketAlert(client *whatsmeow.Client, msg *events.Message, args []string) {
	bs := db.GetBotStore()
	if bs == nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Database not initialized.")
		return
	}
	chatJID := msg.Info.Chat.ToNonAD().String()
	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 0 || strings.EqualFold(fields[0], "list") {
		alerts, err := bs.ListMarketAlerts(chatJID)
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
			return
		}
		if len(alerts) == 0 {
			utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
				"🔔 Belum ada alarm market aktif di chat ini.\n\nTambah alarm:\n• `.marketalert add BTC above 100000`\n• `.marketalert add ETH below 2500`\n• `.marketalert add NVDA pct_up 5`\n• `.marketalert add BBCA pct_down 3`",
				"🔔 No active market alerts in this chat.\n\nAdd one:\n• `.marketalert add BTC above 100000`\n• `.marketalert add ETH below 2500`\n• `.marketalert add NVDA pct_up 5`\n• `.marketalert add BBCA pct_down 3`",
			))
			return
		}
		var b strings.Builder
		b.WriteString(pickLang(client, msg, "🔔 *ALARM MARKET AKTIF*\n\n", "🔔 *ACTIVE MARKET ALERTS*\n\n"))
		for _, a := range alerts {
			fmt.Fprintf(&b, "• `#%d` *%s* `%s` `%.4g`\n", a.ID, a.Symbol, a.Condition, a.Threshold)
		}
		b.WriteString(pickLang(client, msg, "\nHapus dengan `.marketalert del <id>`", "\nDelete with `.marketalert del <id>`"))
		utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
		return
	}

	sub := strings.ToLower(fields[0])
	switch sub {
	case "add", "set", "new":
		if len(fields) < 4 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.marketalert add <SYMBOL> <above|below|pct_up|pct_down> <value>`")
			return
		}
		_, display, _ := NormalizeMarketSymbol(fields[1])
		cond := strings.ToLower(fields[2])
		switch cond {
		case "above", "below", "pct_up", "pct_down":
		case ">", "up":
			cond = "above"
		case "<", "down":
			cond = "below"
		default:
			utils.SendReply(client, msg.Info.Chat, "❌ Condition must be `above`, `below`, `pct_up`, or `pct_down`.")
			return
		}
		val, err := strconv.ParseFloat(fields[3], 64)
		if err != nil || val <= 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ Value must be a positive number.")
			return
		}
		id, err := bs.AddMarketAlert(db.MarketAlert{
			ChatJID:    chatJID,
			CreatorJID: msg.Info.Sender.ToNonAD().String(),
			Symbol:     display,
			Condition:  cond,
			Threshold:  val,
		})
		if err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
			return
		}
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			fmt.Sprintf("✅ Alarm `#%d` aktif! Kamu akan diberi notifikasi saat *%s* `%s %.4g`.", id, display, cond, val),
			fmt.Sprintf("✅ Alert `#%d` saved! You will be notified when *%s* triggers `%s %.4g`.", id, display, cond, val),
		))

	case "del", "delete", "rm", "remove":
		if len(fields) < 2 {
			utils.SendReply(client, msg.Info.Chat, "Usage: `.marketalert del <id>`")
			return
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(fields[1], "#"), 10, 64)
		if err != nil || id <= 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ Invalid alert ID.")
			return
		}
		_ = bs.DeleteMarketAlert(id, chatJID)
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("🗑️ Deleted market alert `#%d`.", id))

	default:
		utils.SendReply(client, msg.Info.Chat, "Usage: `.marketalert add <SYM> <above|below|pct_up|pct_down> <val>` | `list` | `del <id>`")
	}
}

func cmdMarketRemind(client *whatsmeow.Client, msg *events.Message, args []string) {
	bs := db.GetBotStore()
	if bs == nil {
		utils.SendReply(client, msg.Info.Chat, "❌ Database not initialized.")
		return
	}
	chatJID := msg.Info.Chat.ToNonAD().String()
	rem, err := bs.GetMarketReminder(chatJID)
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
		return
	}

	fields := strings.Fields(strings.Join(args, " "))
	if len(fields) == 0 || strings.EqualFold(fields[0], "status") {
		state := "OFF 🔴"
		if rem.Enabled {
			state = "ON 🟢"
		}
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			fmt.Sprintf("⏰ *MARKET REMINDER*\n• Status: *%s*\n• Interval: setiap `%d menit`\n• Watchlist: `%s`\n\nPerintah:\n• `.marketremind on 60 BTC,ETH,GOLD,NVDA,BBCA`\n• `.marketremind off`\n• `.marketremind now` — Kirim laporan sekarang", state, rem.IntervalMin, rem.SymbolsCSV),
			fmt.Sprintf("⏰ *MARKET REMINDER*\n• Status: *%s*\n• Interval: every `%d min`\n• Watchlist: `%s`\n\nCommands:\n• `.marketremind on 60 BTC,ETH,GOLD,NVDA,BBCA`\n• `.marketremind off`\n• `.marketremind now` — Send digest right now", state, rem.IntervalMin, rem.SymbolsCSV),
		))
		return
	}

	switch strings.ToLower(fields[0]) {
	case "on", "enable":
		interval := rem.IntervalMin
		if interval < 5 {
			interval = 60
		}
		symStart := 1
		if len(fields) >= 2 {
			if n, err := strconv.Atoi(fields[1]); err == nil && n >= 5 && n <= 1440 {
				interval = n
				symStart = 2
			}
		}
		syms := rem.SymbolsCSV
		if len(fields) > symStart {
			parsed := parseSymbolsCSV(strings.Join(fields[symStart:], ","))
			if len(parsed) > 0 {
				syms = strings.Join(parsed, ",")
			}
		}
		if syms == "" {
			syms = strings.Join(DefaultWatchlist[:6], ",")
		}
		rem.Enabled = true
		rem.IntervalMin = interval
		rem.SymbolsCSV = syms
		rem.LastSentAt = time.Now().Unix()
		if err := bs.SaveMarketReminder(*rem); err != nil {
			utils.SendReply(client, msg.Info.Chat, "❌ "+err.Error())
			return
		}
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			fmt.Sprintf("✅ *Market Reminder AKTIF!*\n• Interval: setiap `%d menit`\n• Aset: `%s`\n\nKetik `.marketremind now` untuk lihat contoh laporan langsung.", interval, syms),
			fmt.Sprintf("✅ *Market Reminder ENABLED!*\n• Interval: every `%d min`\n• Watchlist: `%s`\n\nRun `.marketremind now` to preview the digest immediately.", interval, syms),
		))

	case "off", "disable":
		rem.Enabled = false
		_ = bs.SaveMarketReminder(*rem)
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg, "🔴 Market Reminder dimatikan untuk chat ini.", "🔴 Market Reminder disabled for this chat."))

	case "now", "test":
		syms := parseSymbolsCSV(rem.SymbolsCSV)
		if len(syms) == 0 {
			syms = DefaultWatchlist[:6]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		snaps := fetchWatchlistConcurrent(ctx, syms)
		if len(snaps) == 0 {
			utils.SendReply(client, msg.Info.Chat, "❌ Could not fetch market data right now.")
			return
		}
		utils.SendReply(client, msg.Info.Chat, FormatDigest(snaps, i18n.ResolveLang(client, msg)))

	default:
		utils.SendReply(client, msg.Info.Chat, "Usage: `.marketremind on [minutes] [SYM1,SYM2,...]` | `off` | `status` | `now`")
	}
}

func cmdMarketAI(client *whatsmeow.Client, msg *events.Message, args []string) {
	if len(args) == 0 {
		utils.SendReply(client, msg.Info.Chat, pickLang(client, msg,
			"🤖 Usage: `.marketai <simbol>` (contoh: `.marketai BTC`, `.marketai NVDA`, `.marketai GOLD`, `.marketai BBCA`)",
			"🤖 Usage: `.marketai <symbol>` (e.g. `.marketai BTC`, `.marketai NVDA`, `.marketai GOLD`, `.marketai BBCA`)",
		))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 28*time.Second)
	defer cancel()

	snap, err := FetchMarketSnapshot(ctx, args[0])
	if err != nil {
		utils.SendReply(client, msg.Info.Chat, fmt.Sprintf("❌ Could not fetch data for `%s`: %v", args[0], err))
		return
	}
	tech := ComputeTechnicals(snap.Closes, snap.Price)
	lang := i18n.ResolveLang(client, msg)

	analysis, engine := GenerateMarketAIAnalysis(ctx, snap, tech, lang)

	sign := "+"
	if snap.ChangePct < 0 {
		sign = ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🤖 *NOPEBOT AI MARKET ANALYSIS — %s*\n", snap.Display)
	fmt.Fprintf(&b, "_%s (%s) • %s_\n\n", snap.Name, snap.YahooSymbol, snap.AssetType)
	fmt.Fprintf(&b, "💵 *Price:* `%s` (%s `%s%.2f%%`)\n", FormatPrice(snap.Price, snap.Currency), trendBadge(snap.ChangePct), sign, snap.ChangePct)
	fmt.Fprintf(&b, "📐 *Indicators:* RSI-14 `%.1f` | SMA-7 `%s` | SMA-20 `%s`\n", tech.RSI14, FormatPrice(tech.SMA7, snap.Currency), FormatPrice(tech.SMA20, snap.Currency))
	fmt.Fprintf(&b, "🧱 *20D Range:* Support `%s` | Resistance `%s` | Vol `%.2f%%/d`\n", FormatPrice(tech.Support, snap.Currency), FormatPrice(tech.Resistance, snap.Currency), tech.VolatilityPct)
	if tech.Sparkline != "" {
		fmt.Fprintf(&b, "📊 *14D Chart:* `%s` | Bias: *%s*\n", tech.Sparkline, signalLabel(tech.Signal))
	}
	fmt.Fprintf(&b, "\n🧠 *Analysis (%s):*\n%s", engine, strings.TrimSpace(analysis))
	utils.SendReply(client, msg.Info.Chat, strings.TrimSpace(b.String()))
}

// GenerateMarketAIAnalysis queries any configured OpenAI-compatible LLM provider
// (ai.AppConfig.Providers, Groq, or OpenRouter) and falls back cleanly to a
// built-in quantitative technical engine if no API key is set or the call fails.
func GenerateMarketAIAnalysis(ctx context.Context, snap *Snapshot, tech Technicals, lang string) (string, string) {
	langHint := "English"
	if lang == i18n.LangID {
		langHint = "Indonesian (Bahasa Indonesia)"
	}

	prompt := fmt.Sprintf(
		"Analyze this real-world market asset concisely in %s (max 140 words, WhatsApp markdown with bullet points):\n"+
			"Asset: %s (%s, %s)\n"+
			"Live Price: %s (24h Change: %.2f%%)\n"+
			"Day Range: %s - %s\n"+
			"RSI-14: %.1f | SMA-7: %s | SMA-20: %s\n"+
			"20D Support: %s | 20D Resistance: %s | Daily Volatility: %.2f%%\n"+
			"Quantitative Bias: %s\n\n"+
			"Provide:\n1. Trend & momentum verdict\n2. Key support/resistance zones to watch\n3. Short-term bullish vs bearish scenario\n4. End with a short 'Not financial advice' note.",
		langHint,
		snap.Display, snap.Name, snap.AssetType,
		FormatPrice(snap.Price, snap.Currency), snap.ChangePct,
		FormatPrice(snap.DayLow, snap.Currency), FormatPrice(snap.DayHigh, snap.Currency),
		tech.RSI14, FormatPrice(tech.SMA7, snap.Currency), FormatPrice(tech.SMA20, snap.Currency),
		FormatPrice(tech.Support, snap.Currency), FormatPrice(tech.Resistance, snap.Currency), tech.VolatilityPct,
		tech.Signal,
	)

	// 1. Try dynamic providers configured in .env via ai.AppConfig.Providers.
	for i := range ai.AppConfig.Providers {
		p := &ai.AppConfig.Providers[i]
		key := p.GetNextKey()
		if p.BaseURL == "" || key == "" {
			continue
		}
		model := pickDefaultModelForProvider(p.Name)
		if text, err := callOpenAICompat(ctx, p.BaseURL, key, model, prompt); err == nil && text != "" {
			return text, fmt.Sprintf("AI • %s", p.Name)
		}
	}

	// 2. Try Groq or OpenRouter from config.json if valid keys are present.
	if conf, err := config.LoadConfig(); err == nil && conf != nil {
		groq := strings.TrimSpace(conf.APIs.GroqAPIKey)
		if groq != "" && groq != "YOUR_GROQ_API_KEY_HERE" {
			if text, err := callOpenAICompat(ctx, "https://api.groq.com/openai/v1", groq, "llama-3.3-70b-versatile", prompt); err == nil && text != "" {
				return text, "AI • Groq"
			}
		}
		orKey := strings.TrimSpace(conf.APIs.OpenRouterAPIKey)
		if orKey != "" && orKey != "YOUR_OPENROUTER_API_KEY_HERE" {
			if text, err := callOpenAICompat(ctx, "https://openrouter.ai/api/v1", orKey, "openai/gpt-4o-mini", prompt); err == nil && text != "" {
				return text, "AI • OpenRouter"
			}
		}
	}

	// 3. Built-in quantitative fallback (always works with zero API keys!).
	return BuildQuantitativeAnalysis(snap, tech, lang), "Quant Engine"
}

func pickDefaultModelForProvider(providerName string) string {
	n := strings.ToLower(providerName)
	switch {
	case strings.Contains(n, "groq"):
		return "llama-3.3-70b-versatile"
	case strings.Contains(n, "deepseek"):
		return "deepseek-chat"
	case strings.Contains(n, "gemini") || strings.Contains(n, "google"):
		return "gemini-2.0-flash"
	case strings.Contains(n, "openrouter"):
		return "openai/gpt-4o-mini"
	default:
		return "gpt-4o-mini"
	}
}

func callOpenAICompat(ctx context.Context, baseURL, apiKey, model, userPrompt string) (string, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are NopeBot's concise quantitative market analyst. Be objective, structured, and never guarantee profits."},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.4,
		"max_tokens":  360,
	}
	raw, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := marketHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm http %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 128*1024)).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty llm response")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// BuildQuantitativeAnalysis creates a clear, structured technical outlook directly
// from live RSI-14, SMA-7/20, Support/Resistance, and Volatility metrics.
func BuildQuantitativeAnalysis(snap *Snapshot, tech Technicals, lang string) string {
	rsiNoteEN := "Neutral momentum"
	rsiNoteID := "Momentum netral"
	switch {
	case tech.RSI14 >= 70:
		rsiNoteEN = "Overbought — watch for short-term pullback"
		rsiNoteID = "Jenuh beli (Overbought) — waspadai koreksi jangka pendek"
	case tech.RSI14 <= 30:
		rsiNoteEN = "Oversold — potential technical bounce zone"
		rsiNoteID = "Jenuh jual (Oversold) — berpotensi rebound teknikal"
	case tech.RSI14 >= 55:
		rsiNoteEN = "Healthy bullish momentum"
		rsiNoteID = "Momentum naik (bullish) cukup sehat"
	case tech.RSI14 <= 45:
		rsiNoteEN = "Selling pressure dominant"
		rsiNoteID = "Tekanan jual masih dominan"
	}

	maNoteEN := "Price consolidating near moving averages"
	maNoteID := "Harga sedang konsolidasi di sekitar rata-rata pergerakan"
	if tech.SMA7 > 0 && tech.SMA20 > 0 {
		if snap.Price > tech.SMA7 && tech.SMA7 > tech.SMA20 {
			maNoteEN = "Bullish alignment (Price > SMA-7 > SMA-20)"
			maNoteID = "Struktur tren naik kuat (Harga > SMA-7 > SMA-20)"
		} else if snap.Price < tech.SMA7 && tech.SMA7 < tech.SMA20 {
			maNoteEN = "Bearish alignment (Price < SMA-7 < SMA-20)"
			maNoteID = "Struktur tren turun (Harga < SMA-7 < SMA-20)"
		}
	}

	volNoteEN := "Moderate"
	volNoteID := "Sedang"
	if tech.VolatilityPct >= 4.0 {
		volNoteEN = "High (elevated swing risk)"
		volNoteID = "Tinggi (risiko fluktuasi tajam)"
	} else if tech.VolatilityPct < 1.5 {
		volNoteEN = "Low (tight range / possible breakout)"
		volNoteID = "Rendah (rentang sempit / potensi breakout)"
	}

	sup := FormatPrice(tech.Support, snap.Currency)
	res := FormatPrice(tech.Resistance, snap.Currency)

	if lang == i18n.LangID {
		return fmt.Sprintf(
			"• *Tren:* %s.\n"+
				"• *RSI-14 (%.1f):* %s.\n"+
				"• *Volatilitas:* %s (`%.2f%%/hari`).\n"+
				"• *Area Kunci:* Support di `%s` | Resistance di `%s`.\n"+
				"• *Skenario:* Tembus di atas `%s` mengonfirmasi kelanjutan naik; jebol di bawah `%s` membuka risiko koreksi lanjut.\n\n"+
				"_⚠️ Bukan saran finansial (DYOR). Tambahkan API key AI di `.env`/`config.json` untuk narasi LLM mendalam._",
			maNoteID, tech.RSI14, rsiNoteID, volNoteID, tech.VolatilityPct, sup, res, res, sup,
		)
	}
	return fmt.Sprintf(
		"• *Trend:* %s.\n"+
			"• *RSI-14 (%.1f):* %s.\n"+
			"• *Volatility:* %s (`%.2f%%/day`).\n"+
			"• *Key Levels:* Support `%s` | Resistance `%s`.\n"+
			"• *Outlook:* A breakout above `%s` favors bullish continuation; losing `%s` opens downside risk.\n\n"+
			"_⚠️ Not financial advice (DYOR). Configure an AI API key in `.env`/`config.json` for deeper LLM commentary._",
		maNoteEN, tech.RSI14, rsiNoteEN, volNoteEN, tech.VolatilityPct, sup, res, res, sup,
	)
}

// --- Background Monitor (Alerts + Scheduled UP/DOWN Reminders) ---

// StartMarketMonitor launches a lightweight background ticker that checks
// SQLite market_alerts and market_reminders every 2 minutes.
func StartMarketMonitor(stopCh <-chan struct{}, getClients func() []*whatsmeow.Client) {
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				runMarketMonitorTick(getClients)
			}
		}
	}()
}

func runMarketMonitorTick(getClients func() []*whatsmeow.Client) {
	bs := db.GetBotStore()
	if bs == nil || getClients == nil {
		return
	}
	clients := getClients()
	if len(clients) == 0 {
		return
	}
	primary := clients[0]

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Evaluate all active price/percent alerts.
	alerts, _ := bs.ListMarketAlerts("")
	if len(alerts) > 0 {
		snaps := map[string]*Snapshot{}
		for _, a := range alerts {
			if _, seen := snaps[a.Symbol]; !seen {
				if s, err := FetchMarketSnapshot(ctx, a.Symbol); err == nil {
					snaps[a.Symbol] = s
				} else {
					snaps[a.Symbol] = nil
				}
			}
			s := snaps[a.Symbol]
			if s == nil {
				continue
			}
			fired := false
			switch a.Condition {
			case "above":
				fired = s.Price >= a.Threshold
			case "below":
				fired = s.Price <= a.Threshold
			case "pct_up":
				fired = s.ChangePct >= a.Threshold
			case "pct_down":
				fired = s.ChangePct <= -math.Abs(a.Threshold)
			}
			if fired {
				_ = bs.MarkMarketAlertFired(a.ID, true)
				if jid, err := waTypes.ParseJID(a.ChatJID); err == nil {
					sign := "+"
					if s.ChangePct < 0 {
						sign = ""
					}
					text := fmt.Sprintf(
						"🚨 *NOPEBOT MARKET ALERT #%d*\n\n%s *%s* hit `%s %.4g`!\n• Live Price: *%s*\n• 24h Change: `%s%.2f%%`\n\n💡 Run `.marketai %s` for instant AI analysis.",
						a.ID, trendBadge(s.ChangePct), s.Display, a.Condition, a.Threshold,
						FormatPrice(s.Price, s.Currency), sign, s.ChangePct, s.Display,
					)
					_, _ = utils.SendReply(primary, jid, text)
				}
			}
		}
	}

	// 2. Send due recurring market reminders.
	reminders, _ := bs.ListEnabledMarketReminders()
	now := time.Now()
	for _, r := range reminders {
		interval := time.Duration(r.IntervalMin) * time.Minute
		if interval < 5*time.Minute {
			interval = 60 * time.Minute
		}
		if r.LastSentAt > 0 && now.Sub(time.Unix(r.LastSentAt, 0)) < interval {
			continue
		}
		syms := parseSymbolsCSV(r.SymbolsCSV)
		if len(syms) == 0 {
			syms = DefaultWatchlist[:6]
		}
		snaps := fetchWatchlistConcurrent(ctx, syms)
		if len(snaps) == 0 {
			continue
		}
		r.LastSentAt = now.Unix()
		_ = bs.SaveMarketReminder(r)
		if jid, err := waTypes.ParseJID(r.ChatJID); err == nil {
			lang := i18n.LangID
			if strings.HasSuffix(r.ChatJID, "@g.us") {
				if gc, err := bs.GetGroupConfig(r.ChatJID); err == nil && gc != nil && gc.Lang != "" {
					lang = gc.Lang
				}
			}
			_, _ = utils.SendReply(primary, jid, FormatDigest(snaps, lang))
		}
	}
}

func parseSymbolsCSV(raw string) []string {
	raw = strings.ReplaceAll(raw, " ", ",")
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		s := strings.ToUpper(strings.TrimSpace(p))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
