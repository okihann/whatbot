package market

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	db "bot/databases"
	"bot/i18n"
)

func TestNormalizeMarketSymbol(t *testing.T) {
	cases := []struct {
		in        string
		wantYahoo string
		wantDisp  string
		wantType  string
	}{
		{"btc", "BTC-USD", "BTC", "Crypto"},
		{"$ETH", "ETH-USD", "ETH", "Crypto"},
		{"SOLUSDT", "SOL-USD", "SOL", "Crypto"},
		{"gold", "GC=F", "GOLD", "Commodity"},
		{"emas", "GC=F", "GOLD", "Commodity"},
		{"usdidr", "USDIDR=X", "USD/IDR", "Forex"},
		{"ihsg", "^JKSE", "IHSG", "Index"},
		{"bbca", "BBCA.JK", "BBCA.JK", "IDX Stock"},
		{"BBRI.JK", "BBRI.JK", "BBRI.JK", "IDX Stock"},
		{"nvda", "NVDA", "NVDA", "Stock"},
	}

	for _, tc := range cases {
		y, d, typ := NormalizeMarketSymbol(tc.in)
		if y != tc.wantYahoo || d != tc.wantDisp || typ != tc.wantType {
			t.Errorf("NormalizeMarketSymbol(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.in, y, d, typ, tc.wantYahoo, tc.wantDisp, tc.wantType)
		}
	}
}

func TestComputeTechnicalsAndQuantAnalysis(t *testing.T) {
	// Strong uptrend series
	upCloses := []float64{100, 101, 102, 101.5, 103, 104, 105, 106, 107.5, 108, 109, 111, 112, 114, 115, 117, 118, 120, 121, 123}
	techUp := ComputeTechnicals(upCloses, 125)
	if techUp.Signal != "BULLISH" {
		t.Fatalf("expected BULLISH signal on uptrend, got %+v", techUp)
	}
	if techUp.RSI14 <= 60 || techUp.SMA7 <= techUp.SMA20 {
		t.Fatalf("unexpected uptrend technicals: %+v", techUp)
	}
	if techUp.Sparkline == "" || techUp.Support != 101 || techUp.Resistance != 125 {
		t.Fatalf("unexpected support/resistance/sparkline: %+v", techUp)
	}

	// Strong downtrend series
	downCloses := []float64{125, 123, 121, 120, 118, 117, 115, 114, 112, 111, 109, 108, 107, 105, 104, 102, 101, 99, 98, 96}
	techDown := ComputeTechnicals(downCloses, 94)
	if techDown.Signal != "BEARISH" {
		t.Fatalf("expected BEARISH signal on downtrend, got %+v", techDown)
	}
	if techDown.RSI14 >= 40 || techDown.SMA7 >= techDown.SMA20 {
		t.Fatalf("unexpected downtrend technicals: %+v", techDown)
	}

	snap := &Snapshot{
		Display:   "BTC",
		Name:      "Bitcoin USD",
		AssetType: "Crypto",
		Currency:  "USD",
		Price:     125,
		ChangePct: 3.4,
		Closes:    upCloses,
	}
	outID := BuildQuantitativeAnalysis(snap, techUp, i18n.LangID)
	if !strings.Contains(outID, "RSI-14") || !strings.Contains(outID, "Support") {
		t.Fatalf("unexpected Indonesian quant analysis: %s", outID)
	}
	outEN, engine := GenerateMarketAIAnalysis(context.Background(), snap, techUp, i18n.LangEN)
	if outEN == "" || engine == "" {
		t.Fatalf("expected non-empty analysis and engine, got engine=%q out=%q", engine, outEN)
	}

	// UP/DOWN Digest formatting
	digest := FormatDigest([]*Snapshot{
		{Display: "BTC", Currency: "USD", Price: 92000, ChangePct: 4.5},
		{Display: "NVDA", Currency: "USD", Price: 140, ChangePct: -2.1},
		{Display: "SOL", Currency: "USD", Price: 210, ChangePct: 7.2},
	}, i18n.LangID)
	if !strings.Contains(digest, "NAIK (UP)") || !strings.Contains(digest, "TURUN (DOWN)") {
		t.Fatalf("expected UP/DOWN sections in digest:\n%s", digest)
	}
	if strings.Index(digest, "SOL") > strings.Index(digest, "BTC") {
		t.Fatalf("expected SOL (+7.2%%) sorted ahead of BTC (+4.5%%):\n%s", digest)
	}
}

func TestMarketAlertsAndRemindersSQLite(t *testing.T) {
	dir := t.TempDir()
	store, err := db.OpenBotStore(filepath.Join(dir, "botdata.db"))
	if err != nil {
		t.Fatalf("OpenBotStore: %v", err)
	}
	defer store.Close()

	id, err := store.AddMarketAlert(db.MarketAlert{
		ChatJID:    "120363000@g.us",
		CreatorJID: "628111@s.whatsapp.net",
		Symbol:     "BTC",
		Condition:  "above",
		Threshold:  100000,
	})
	if err != nil || id <= 0 {
		t.Fatalf("AddMarketAlert failed: id=%d err=%v", id, err)
	}

	alerts, err := store.ListMarketAlerts("120363000@g.us")
	if err != nil || len(alerts) != 1 || alerts[0].Symbol != "BTC" {
		t.Fatalf("ListMarketAlerts unexpected: %+v err=%v", alerts, err)
	}

	if err := store.MarkMarketAlertFired(id, true); err != nil {
		t.Fatalf("MarkMarketAlertFired: %v", err)
	}
	afterFire, _ := store.ListMarketAlerts("120363000@g.us")
	if len(afterFire) != 0 {
		t.Fatalf("expected 0 active alerts after one-shot fire, got %d", len(afterFire))
	}

	// Reminders
	if err := store.SaveMarketReminder(db.MarketReminder{
		ChatJID:     "120363000@g.us",
		Enabled:     true,
		IntervalMin: 30,
		SymbolsCSV:  "BTC,ETH,GOLD,BBCA.JK",
	}); err != nil {
		t.Fatalf("SaveMarketReminder: %v", err)
	}
	enabled, err := store.ListEnabledMarketReminders()
	if err != nil || len(enabled) != 1 || enabled[0].IntervalMin != 30 {
		t.Fatalf("ListEnabledMarketReminders unexpected: %+v err=%v", enabled, err)
	}
}
