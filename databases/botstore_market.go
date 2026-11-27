package db

import (
	"database/sql"
	"strings"
	"time"
)

// MarketAlert represents a user/group real-world price threshold alert.
type MarketAlert struct {
	ID          int64   `json:"id"`
	ChatJID     string  `json:"chatJid"`
	CreatorJID  string  `json:"creatorJid"`
	Symbol      string  `json:"symbol"`    // e.g. BTC, ETH, NVDA, BBCA.JK, GOLD
	Condition   string  `json:"condition"` // above, below, pct_up, pct_down
	Threshold   float64 `json:"threshold"`
	Triggered   bool    `json:"triggered"`
	CreatedAt   int64   `json:"createdAt"`
	LastFiredAt int64   `json:"lastFiredAt"`
}

// MarketReminder represents a recurring scheduled real-world market digest for a chat.
type MarketReminder struct {
	ChatJID     string `json:"chatJid"`
	Enabled     bool   `json:"enabled"`
	IntervalMin int    `json:"intervalMin"` // e.g. 60 = hourly
	SymbolsCSV  string `json:"symbolsCsv"`  // comma-separated watchlist
	LastSentAt  int64  `json:"lastSentAt"`
}

func initMarketAndWorldSchema(conn *sql.DB) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS kv_state (
		key        TEXT PRIMARY KEY,
		val_json   TEXT NOT NULL,
		updated_at INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS market_alerts (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_jid      TEXT NOT NULL,
		creator_jid   TEXT NOT NULL DEFAULT '',
		symbol        TEXT NOT NULL,
		condition     TEXT NOT NULL,
		threshold     REAL NOT NULL,
		triggered     INTEGER NOT NULL DEFAULT 0,
		created_at    INTEGER NOT NULL DEFAULT 0,
		last_fired_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_malerts_active ON market_alerts (triggered, chat_jid);

	CREATE TABLE IF NOT EXISTS market_reminders (
		chat_jid     TEXT PRIMARY KEY,
		enabled      INTEGER NOT NULL DEFAULT 0,
		interval_min INTEGER NOT NULL DEFAULT 60,
		symbols_csv  TEXT NOT NULL DEFAULT 'BTC,ETH,SOL,GOLD,NVDA,BBCA.JK',
		last_sent_at INTEGER NOT NULL DEFAULT 0
	);
	`
	_, err := conn.Exec(ddl)
	return err
}

// --- Generic KV state (used by shared RPG World Boss, etc.) ---

func (s *BotStore) SetKV(key, valJSON string) error {
	if s == nil || key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO kv_state (key, val_json, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET val_json = excluded.val_json, updated_at = excluded.updated_at
	`, key, valJSON, time.Now().Unix())
	return err
}

func (s *BotStore) GetKV(key string) (string, bool, error) {
	if s == nil || key == "" {
		return "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var val string
	err := s.db.QueryRow(`SELECT val_json FROM kv_state WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// --- Real-World Market Alerts ---

func (s *BotStore) AddMarketAlert(a MarketAlert) (int64, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`
		INSERT INTO market_alerts (chat_jid, creator_jid, symbol, condition, threshold, triggered, created_at)
		VALUES (?, ?, ?, ?, ?, 0, ?)
	`, a.ChatJID, a.CreatorJID, strings.ToUpper(strings.TrimSpace(a.Symbol)), strings.ToLower(a.Condition), a.Threshold, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *BotStore) ListMarketAlerts(chatJID string) ([]MarketAlert, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var (
		rows *sql.Rows
		err  error
	)
	if chatJID != "" {
		rows, err = s.db.Query(`SELECT id, chat_jid, creator_jid, symbol, condition, threshold, triggered, created_at, last_fired_at FROM market_alerts WHERE chat_jid = ? AND triggered = 0 ORDER BY id ASC`, chatJID)
	} else {
		rows, err = s.db.Query(`SELECT id, chat_jid, creator_jid, symbol, condition, threshold, triggered, created_at, last_fired_at FROM market_alerts WHERE triggered = 0 ORDER BY id ASC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MarketAlert
	for rows.Next() {
		var a MarketAlert
		var trig int
		if rows.Scan(&a.ID, &a.ChatJID, &a.CreatorJID, &a.Symbol, &a.Condition, &a.Threshold, &trig, &a.CreatedAt, &a.LastFiredAt) == nil {
			a.Triggered = trig != 0
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *BotStore) MarkMarketAlertFired(id int64, oneShot bool) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	trig := 0
	if oneShot {
		trig = 1
	}
	_, err := s.db.Exec(`UPDATE market_alerts SET triggered = ?, last_fired_at = ? WHERE id = ?`, trig, time.Now().Unix(), id)
	return err
}

func (s *BotStore) DeleteMarketAlert(id int64, chatJID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM market_alerts WHERE id = ? AND chat_jid = ?`, id, chatJID)
	return err
}

// --- Real-World Market Scheduled Reminders ---

func (s *BotStore) SaveMarketReminder(r MarketReminder) error {
	if s == nil || r.ChatJID == "" {
		return nil
	}
	if r.IntervalMin < 5 {
		r.IntervalMin = 60
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO market_reminders (chat_jid, enabled, interval_min, symbols_csv, last_sent_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(chat_jid) DO UPDATE SET
			enabled = excluded.enabled,
			interval_min = excluded.interval_min,
			symbols_csv = excluded.symbols_csv,
			last_sent_at = excluded.last_sent_at
	`, r.ChatJID, boolInt(r.Enabled), r.IntervalMin, r.SymbolsCSV, r.LastSentAt)
	return err
}

func (s *BotStore) GetMarketReminder(chatJID string) (*MarketReminder, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var r MarketReminder
	var en int
	err := s.db.QueryRow(`SELECT chat_jid, enabled, interval_min, symbols_csv, last_sent_at FROM market_reminders WHERE chat_jid = ?`, chatJID).
		Scan(&r.ChatJID, &en, &r.IntervalMin, &r.SymbolsCSV, &r.LastSentAt)
	if err == sql.ErrNoRows {
		return &MarketReminder{ChatJID: chatJID, Enabled: false, IntervalMin: 60, SymbolsCSV: "BTC,ETH,SOL,GOLD,NVDA,BBCA.JK"}, nil
	}
	if err != nil {
		return nil, err
	}
	r.Enabled = en != 0
	return &r, nil
}

func (s *BotStore) ListEnabledMarketReminders() ([]MarketReminder, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT chat_jid, enabled, interval_min, symbols_csv, last_sent_at FROM market_reminders WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MarketReminder
	for rows.Next() {
		var r MarketReminder
		var en int
		if rows.Scan(&r.ChatJID, &en, &r.IntervalMin, &r.SymbolsCSV, &r.LastSentAt) == nil {
			r.Enabled = en != 0
			out = append(out, r)
		}
	}
	return out, nil
}
