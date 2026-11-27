package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// BotTenant holds per-linked-account settings & optional subscription expiry
// for multi-tenant ("sell the bot") deployments.
type BotTenant struct {
	AccountID   string `json:"accountId"`
	OwnerJID    string `json:"ownerJid"`
	BotName     string `json:"botName"`
	Prefix      string `json:"prefix"`
	SelfMode    bool   `json:"selfMode"`
	GroupMode   bool   `json:"groupMode"`
	AutoRead    bool   `json:"autoRead"`
	RejectCall  bool   `json:"rejectCall"`
	AnswerCall  string `json:"answerCall"`
	NSFWEnabled bool   `json:"nsfwEnabled"`
	PacingMS    int    `json:"pacingMs"`
	ExpiresAt   int64  `json:"expiresAt"` // 0 = never expires
	CreatedAt   int64  `json:"createdAt"`
	Notes       string `json:"notes"`
}

// PairingTicketRecord persists a sequential QR pairing queue ticket.
type PairingTicketRecord struct {
	Token        string `json:"token"`
	Label        string `json:"label"`
	BuyerJID     string `json:"buyerJid"`
	DurationDays int    `json:"durationDays"`
	Status       string `json:"status"` // queued, pairing, connected, expired, canceled, failed
	AccountID    string `json:"accountId"`
	ErrorMsg     string `json:"errorMsg"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// UserProfile stores persistent profile, economy, AFK, and XP state per user.
type UserProfile struct {
	JID          string `json:"jid"`
	Name         string `json:"name"`
	Age          int    `json:"age"`
	Gender       string `json:"gender"`
	Bio          string `json:"bio"`
	CustomPrefix string `json:"customPrefix"`
	XP           int64  `json:"xp"`
	Level        int    `json:"level"`
	Balance      int64  `json:"balance"`
	DailyLast    int64  `json:"dailyLast"`
	AFKReason    string `json:"afkReason"`
	AFKSince     int64  `json:"afkSince"`
	PremiumUntil int64  `json:"premiumUntil"`
	Banned       bool   `json:"banned"`
	MsgCount     int64  `json:"msgCount"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// BalanceEntry records a user's balance transaction for .BalanceHistory.
type BalanceEntry struct {
	ID        int64  `json:"id"`
	UserJID   string `json:"userJid"`
	Delta     int64  `json:"delta"`
	Reason    string `json:"reason"`
	CreatedAt int64  `json:"createdAt"`
}

// GroupConfig stores all Group Admin & Group Security settings in SQLite.
type GroupConfig struct {
	GroupJID           string            `json:"groupJid"`
	WelcomeEnabled     bool              `json:"welcomeEnabled"`
	WelcomeMessage     string            `json:"welcomeMessage"`
	GoodbyeEnabled     bool              `json:"goodbyeEnabled"`
	GoodbyeMessage     string            `json:"goodbyeMessage"`
	AdminOnly          bool              `json:"adminOnly"`
	Prefix             string            `json:"prefix"`
	Lang               string            `json:"lang"`
	Timezone           string            `json:"timezone"`
	NSFWEnabled        bool              `json:"nsfwEnabled"`
	AntiLink           bool              `json:"antiLink"`
	AntiLinkAction     string            `json:"antiLinkAction"` // delete, warn, kick
	AntiMedia          bool              `json:"antiMedia"`
	AntiSpam           bool              `json:"antiSpam"`
	AntiVO             bool              `json:"antiVo"`
	AntiHideTag        bool              `json:"antiHideTag"`
	AntiSW             bool              `json:"antiSw"`
	AntiTagSW          bool              `json:"antiTagSw"`
	AntiBot            bool              `json:"antiBot"`
	AntiKudeta         bool              `json:"antiKudeta"`
	AntiBug            bool              `json:"antiBug"`
	AntiCall           bool              `json:"antiCall"`
	BadWords           []string          `json:"badWords"`
	BadWordEnabled     bool              `json:"badWordEnabled"`
	AutoRespon         map[string]string `json:"autoRespon"`
	DisabledCategories map[string]bool   `json:"disabledCategories"`
	DisabledCommands   map[string]bool   `json:"disabledCommands"`
	ScheduleOpen       string            `json:"scheduleOpen"`  // "HH:MM"
	ScheduleClose      string            `json:"scheduleClose"` // "HH:MM"
	AdzanCity          string            `json:"adzanCity"`
	AdzanEnabled       bool              `json:"adzanEnabled"`
	WarnLimit          int               `json:"warnLimit"`
	Banned             bool              `json:"banned"`
}

// GroupMemberState tracks per-group activity, warns, mute, and ban state.
type GroupMemberState struct {
	GroupJID   string `json:"groupJid"`
	UserJID    string `json:"userJid"`
	MsgCount   int64  `json:"msgCount"`
	LastActive int64  `json:"lastActive"`
	Warns      int    `json:"warns"`
	MutedUntil int64  `json:"mutedUntil"` // 0 = not muted, -1 = permanent, >0 = unix timestamp
	Banned     bool   `json:"banned"`
}

// AttendanceRecord represents a group's active roll-call session.
type AttendanceRecord struct {
	GroupJID string            `json:"groupJid"`
	Title    string            `json:"title"`
	DateKey  string            `json:"dateKey"`
	Present  map[string]string `json:"present"` // userJID -> displayName
}

// GameStatRow stores aggregate game statistics for leaderboards & profiles.
type GameStatRow struct {
	UserJID    string `json:"userJid"`
	GroupJID   string `json:"groupJid"`
	Game       string `json:"game"`
	Wins       int    `json:"wins"`
	Losses     int    `json:"losses"`
	Draws      int    `json:"draws"`
	Points     int64  `json:"points"`
	Streak     int    `json:"streak"`
	BestStreak int    `json:"bestStreak"`
	UpdatedAt  int64  `json:"updatedAt"`
}

// RedeemCode represents a redeemable code for balance/XP/premium.
type RedeemCode struct {
	Code          string   `json:"code"`
	RewardBalance int64    `json:"rewardBalance"`
	RewardXP      int64    `json:"rewardXp"`
	PremiumDays   int      `json:"premiumDays"`
	MaxUses       int      `json:"maxUses"`
	UsedBy        []string `json:"usedBy"`
	CreatedAt     int64    `json:"createdAt"`
}

// BotStore provides fast sequential WAL SQLite persistence for all bot features.
type BotStore struct {
	db        *sql.DB
	mu        sync.Mutex
	closeOnce sync.Once
}

var (
	defaultBotStoreMu sync.RWMutex
	defaultBotStore   *BotStore
)

// SetBotStore sets the process-wide BotStore used by command packages.
func SetBotStore(s *BotStore) {
	defaultBotStoreMu.Lock()
	defaultBotStore = s
	defaultBotStoreMu.Unlock()
}

// GetBotStore returns the active BotStore, or nil if not initialized yet.
func GetBotStore() *BotStore {
	defaultBotStoreMu.RLock()
	defer defaultBotStoreMu.RUnlock()
	return defaultBotStore
}

// OpenBotStore opens db/botdata.db with sequential WAL pragmas and creates all tables.
func OpenBotStore(dbPath string) (*BotStore, error) {
	conn, err := OpenSequentialSQLite(dbPath)
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS bot_tenants (
		account_id   TEXT PRIMARY KEY,
		owner_jid    TEXT NOT NULL DEFAULT '',
		bot_name     TEXT NOT NULL DEFAULT 'Whatbot',
		prefix       TEXT NOT NULL DEFAULT '',
		self_mode    INTEGER NOT NULL DEFAULT 0,
		group_mode   INTEGER NOT NULL DEFAULT 0,
		auto_read    INTEGER NOT NULL DEFAULT 0,
		reject_call  INTEGER NOT NULL DEFAULT 0,
		answer_call  TEXT NOT NULL DEFAULT '',
		nsfw_enabled INTEGER NOT NULL DEFAULT 0,
		pacing_ms    INTEGER NOT NULL DEFAULT 0,
		expires_at   INTEGER NOT NULL DEFAULT 0,
		created_at   INTEGER NOT NULL DEFAULT 0,
		notes        TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS pairing_tickets (
		token         TEXT PRIMARY KEY,
		label         TEXT NOT NULL DEFAULT '',
		buyer_jid     TEXT NOT NULL DEFAULT '',
		duration_days INTEGER NOT NULL DEFAULT 0,
		status        TEXT NOT NULL DEFAULT 'queued',
		account_id    TEXT NOT NULL DEFAULT '',
		error_msg     TEXT NOT NULL DEFAULT '',
		created_at    INTEGER NOT NULL DEFAULT 0,
		updated_at    INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS users (
		jid           TEXT PRIMARY KEY,
		name          TEXT NOT NULL DEFAULT '',
		age           INTEGER NOT NULL DEFAULT 0,
		gender        TEXT NOT NULL DEFAULT '',
		bio           TEXT NOT NULL DEFAULT '',
		custom_prefix TEXT NOT NULL DEFAULT '',
		xp            INTEGER NOT NULL DEFAULT 0,
		level         INTEGER NOT NULL DEFAULT 1,
		balance       INTEGER NOT NULL DEFAULT 100,
		daily_last    INTEGER NOT NULL DEFAULT 0,
		afk_reason    TEXT NOT NULL DEFAULT '',
		afk_since     INTEGER NOT NULL DEFAULT 0,
		premium_until INTEGER NOT NULL DEFAULT 0,
		banned        INTEGER NOT NULL DEFAULT 0,
		msg_count     INTEGER NOT NULL DEFAULT 0,
		updated_at    INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS balance_history (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		user_jid   TEXT NOT NULL,
		delta      INTEGER NOT NULL,
		reason     TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_balance_user ON balance_history (user_jid, id DESC);

	CREATE TABLE IF NOT EXISTS group_configs (
		group_jid   TEXT PRIMARY KEY,
		config_json TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS group_members (
		group_jid   TEXT NOT NULL,
		user_jid    TEXT NOT NULL,
		msg_count   INTEGER NOT NULL DEFAULT 0,
		last_active INTEGER NOT NULL DEFAULT 0,
		warns       INTEGER NOT NULL DEFAULT 0,
		muted_until INTEGER NOT NULL DEFAULT 0,
		banned      INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (group_jid, user_jid)
	);
	CREATE INDEX IF NOT EXISTS idx_gmembers_active ON group_members (group_jid, msg_count DESC);

	CREATE TABLE IF NOT EXISTS attendance (
		group_jid    TEXT PRIMARY KEY,
		title        TEXT NOT NULL DEFAULT '',
		date_key     TEXT NOT NULL DEFAULT '',
		present_json TEXT NOT NULL DEFAULT '{}'
	);

	CREATE TABLE IF NOT EXISTS game_stats (
		user_jid    TEXT NOT NULL,
		group_jid   TEXT NOT NULL DEFAULT '',
		game        TEXT NOT NULL,
		wins        INTEGER NOT NULL DEFAULT 0,
		losses      INTEGER NOT NULL DEFAULT 0,
		draws       INTEGER NOT NULL DEFAULT 0,
		points      INTEGER NOT NULL DEFAULT 0,
		streak      INTEGER NOT NULL DEFAULT 0,
		best_streak INTEGER NOT NULL DEFAULT 0,
		updated_at  INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (user_jid, group_jid, game)
	);
	CREATE INDEX IF NOT EXISTS idx_gstats_points ON game_stats (points DESC);
	CREATE INDEX IF NOT EXISTS idx_gstats_group ON game_stats (group_jid, points DESC);

	CREATE TABLE IF NOT EXISTS redeem_codes (
		code           TEXT PRIMARY KEY,
		reward_balance INTEGER NOT NULL DEFAULT 0,
		reward_xp      INTEGER NOT NULL DEFAULT 0,
		premium_days   INTEGER NOT NULL DEFAULT 0,
		max_uses       INTEGER NOT NULL DEFAULT 1,
		used_by_json   TEXT NOT NULL DEFAULT '[]',
		created_at     INTEGER NOT NULL DEFAULT 0
	);
	`
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init botstore schema: %w", err)
	}

	return &BotStore{db: conn}, nil
}

// Close checkpoints the WAL and closes the SQLite connection cleanly.
func (s *BotStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = CheckpointAndClose(s.db)
	})
	return err
}

// Status returns live SQLite pragmas for diagnostics.
func (s *BotStore) Status() map[string]string {
	if s == nil {
		return map[string]string{}
	}
	return SQLiteStatus(s.db)
}

// --- BotTenant methods ---

func (s *BotStore) GetTenant(accountID string) (*BotTenant, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT account_id, owner_jid, bot_name, prefix, self_mode, group_mode, auto_read, reject_call, answer_call, nsfw_enabled, pacing_ms, expires_at, created_at, notes FROM bot_tenants WHERE account_id = ?`, accountID)
	var t BotTenant
	var sm, gm, ar, rc, nsfw int
	err := row.Scan(&t.AccountID, &t.OwnerJID, &t.BotName, &t.Prefix, &sm, &gm, &ar, &rc, &t.AnswerCall, &nsfw, &t.PacingMS, &t.ExpiresAt, &t.CreatedAt, &t.Notes)
	if err == sql.ErrNoRows {
		return &BotTenant{AccountID: accountID, BotName: "Whatbot", CreatedAt: time.Now().Unix()}, nil
	}
	if err != nil {
		return nil, err
	}
	t.SelfMode = sm != 0
	t.GroupMode = gm != 0
	t.AutoRead = ar != 0
	t.RejectCall = rc != 0
	t.NSFWEnabled = nsfw != 0
	return &t, nil
}

func (s *BotStore) SaveTenant(t *BotTenant) error {
	if s == nil || t == nil || t.AccountID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if t.CreatedAt == 0 {
		t.CreatedAt = time.Now().Unix()
	}
	_, err := s.db.Exec(`
		INSERT INTO bot_tenants (account_id, owner_jid, bot_name, prefix, self_mode, group_mode, auto_read, reject_call, answer_call, nsfw_enabled, pacing_ms, expires_at, created_at, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			owner_jid = excluded.owner_jid,
			bot_name = excluded.bot_name,
			prefix = excluded.prefix,
			self_mode = excluded.self_mode,
			group_mode = excluded.group_mode,
			auto_read = excluded.auto_read,
			reject_call = excluded.reject_call,
			answer_call = excluded.answer_call,
			nsfw_enabled = excluded.nsfw_enabled,
			pacing_ms = excluded.pacing_ms,
			expires_at = excluded.expires_at,
			notes = excluded.notes
	`, t.AccountID, t.OwnerJID, t.BotName, t.Prefix, boolInt(t.SelfMode), boolInt(t.GroupMode),
		boolInt(t.AutoRead), boolInt(t.RejectCall), t.AnswerCall, boolInt(t.NSFWEnabled),
		t.PacingMS, t.ExpiresAt, t.CreatedAt, t.Notes)
	return err
}

func (s *BotStore) ListTenants() ([]BotTenant, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT account_id, owner_jid, bot_name, prefix, self_mode, group_mode, auto_read, reject_call, answer_call, nsfw_enabled, pacing_ms, expires_at, created_at, notes FROM bot_tenants ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BotTenant
	for rows.Next() {
		var t BotTenant
		var sm, gm, ar, rc, nsfw int
		if err := rows.Scan(&t.AccountID, &t.OwnerJID, &t.BotName, &t.Prefix, &sm, &gm, &ar, &rc, &t.AnswerCall, &nsfw, &t.PacingMS, &t.ExpiresAt, &t.CreatedAt, &t.Notes); err != nil {
			continue
		}
		t.SelfMode = sm != 0
		t.GroupMode = gm != 0
		t.AutoRead = ar != 0
		t.RejectCall = rc != 0
		t.NSFWEnabled = nsfw != 0
		out = append(out, t)
	}
	return out, nil
}

func (s *BotStore) DeleteTenant(accountID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM bot_tenants WHERE account_id = ?`, accountID)
	return err
}

// --- PairingTicket methods ---

func (s *BotStore) SavePairingTicket(r PairingTicketRecord) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO pairing_tickets (token, label, buyer_jid, duration_days, status, account_id, error_msg, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET
			status = excluded.status,
			account_id = excluded.account_id,
			error_msg = excluded.error_msg,
			updated_at = excluded.updated_at
	`, r.Token, r.Label, r.BuyerJID, r.DurationDays, r.Status, r.AccountID, r.ErrorMsg, r.CreatedAt, r.UpdatedAt)
	return err
}

func (s *BotStore) ListPairingTickets(limit int) ([]PairingTicketRecord, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT token, label, buyer_jid, duration_days, status, account_id, error_msg, created_at, updated_at FROM pairing_tickets ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PairingTicketRecord
	for rows.Next() {
		var r PairingTicketRecord
		if err := rows.Scan(&r.Token, &r.Label, &r.BuyerJID, &r.DurationDays, &r.Status, &r.AccountID, &r.ErrorMsg, &r.CreatedAt, &r.UpdatedAt); err == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// --- UserProfile & Economy methods ---

func (s *BotStore) GetUser(jid string) (*UserProfile, error) {
	if s == nil {
		return &UserProfile{JID: jid, Level: 1, Balance: 100}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT jid, name, age, gender, bio, custom_prefix, xp, level, balance, daily_last, afk_reason, afk_since, premium_until, banned, msg_count, updated_at FROM users WHERE jid = ?`, jid)
	var u UserProfile
	var banned int
	err := row.Scan(&u.JID, &u.Name, &u.Age, &u.Gender, &u.Bio, &u.CustomPrefix, &u.XP, &u.Level, &u.Balance, &u.DailyLast, &u.AFKReason, &u.AFKSince, &u.PremiumUntil, &banned, &u.MsgCount, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return &UserProfile{JID: jid, Level: 1, Balance: 100}, nil
	}
	if err != nil {
		return nil, err
	}
	u.Banned = banned != 0
	return &u, nil
}

func (s *BotStore) SaveUser(u *UserProfile) error {
	if s == nil || u == nil || u.JID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u.UpdatedAt = time.Now().Unix()
	if u.Level < 1 {
		u.Level = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO users (jid, name, age, gender, bio, custom_prefix, xp, level, balance, daily_last, afk_reason, afk_since, premium_until, banned, msg_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			name = excluded.name,
			age = excluded.age,
			gender = excluded.gender,
			bio = excluded.bio,
			custom_prefix = excluded.custom_prefix,
			xp = excluded.xp,
			level = excluded.level,
			balance = excluded.balance,
			daily_last = excluded.daily_last,
			afk_reason = excluded.afk_reason,
			afk_since = excluded.afk_since,
			premium_until = excluded.premium_until,
			banned = excluded.banned,
			msg_count = excluded.msg_count,
			updated_at = excluded.updated_at
	`, u.JID, u.Name, u.Age, u.Gender, u.Bio, u.CustomPrefix, u.XP, u.Level, u.Balance, u.DailyLast, u.AFKReason, u.AFKSince, u.PremiumUntil, boolInt(u.Banned), u.MsgCount, u.UpdatedAt)
	return err
}

// AddUserActivity increments message count and XP, returning (newLevel, leveledUp).
func (s *BotStore) AddUserActivity(jid, displayName string, xpGain int64) (int, bool) {
	if s == nil || jid == "" {
		return 1, false
	}
	u, err := s.GetUser(jid)
	if err != nil {
		return 1, false
	}
	if u.Name == "" && displayName != "" {
		u.Name = displayName
	}
	u.MsgCount++
	u.XP += xpGain
	newLevel := int(u.XP/100) + 1
	leveledUp := newLevel > u.Level
	u.Level = newLevel
	_ = s.SaveUser(u)
	return newLevel, leveledUp
}

func (s *BotStore) AddBalance(jid string, delta int64, reason string) (int64, error) {
	u, err := s.GetUser(jid)
	if err != nil {
		return 0, err
	}
	u.Balance += delta
	if u.Balance < 0 {
		u.Balance = 0
	}
	if err := s.SaveUser(u); err != nil {
		return 0, err
	}
	if s != nil {
		s.mu.Lock()
		_, _ = s.db.Exec(`INSERT INTO balance_history (user_jid, delta, reason, created_at) VALUES (?, ?, ?, ?)`, jid, delta, reason, time.Now().Unix())
		s.mu.Unlock()
	}
	return u.Balance, nil
}

func (s *BotStore) GetBalanceHistory(jid string, limit int) ([]BalanceEntry, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 15
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, user_jid, delta, reason, created_at FROM balance_history WHERE user_jid = ? ORDER BY id DESC LIMIT ?`, jid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BalanceEntry
	for rows.Next() {
		var e BalanceEntry
		if err := rows.Scan(&e.ID, &e.UserJID, &e.Delta, &e.Reason, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *BotStore) TopUsers(orderBy string, limit int) ([]UserProfile, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	col := "balance"
	switch strings.ToLower(orderBy) {
	case "xp", "level":
		col = "xp"
	case "msg", "messages", "active":
		col = "msg_count"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(fmt.Sprintf(`SELECT jid, name, age, gender, bio, custom_prefix, xp, level, balance, daily_last, afk_reason, afk_since, premium_until, banned, msg_count, updated_at FROM users ORDER BY %s DESC LIMIT ?`, col), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserProfile
	for rows.Next() {
		var u UserProfile
		var banned int
		if err := rows.Scan(&u.JID, &u.Name, &u.Age, &u.Gender, &u.Bio, &u.CustomPrefix, &u.XP, &u.Level, &u.Balance, &u.DailyLast, &u.AFKReason, &u.AFKSince, &u.PremiumUntil, &banned, &u.MsgCount, &u.UpdatedAt); err == nil {
			u.Banned = banned != 0
			out = append(out, u)
		}
	}
	return out, nil
}

func (s *BotStore) TotalMessageCount() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var total sql.NullInt64
	_ = s.db.QueryRow(`SELECT SUM(msg_count) FROM users`).Scan(&total)
	return total.Int64
}

// --- GroupConfig methods ---

func DefaultGroupConfig(groupJID string) *GroupConfig {
	return &GroupConfig{
		GroupJID:           groupJID,
		WelcomeMessage:     "Welcome @user to {group}! 🎉 We now have {count} members.",
		GoodbyeMessage:     "Goodbye @user from {group} 👋",
		Lang:               "id",
		Timezone:           "Asia/Jakarta",
		AntiLinkAction:     "delete",
		WarnLimit:          3,
		BadWords:           []string{},
		AutoRespon:         map[string]string{},
		DisabledCategories: map[string]bool{},
		DisabledCommands:   map[string]bool{},
	}
}

func (s *BotStore) GetGroupConfig(groupJID string) (*GroupConfig, error) {
	if s == nil {
		return DefaultGroupConfig(groupJID), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	err := s.db.QueryRow(`SELECT config_json FROM group_configs WHERE group_jid = ?`, groupJID).Scan(&raw)
	if err == sql.ErrNoRows {
		return DefaultGroupConfig(groupJID), nil
	}
	if err != nil {
		return nil, err
	}
	cfg := DefaultGroupConfig(groupJID)
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return DefaultGroupConfig(groupJID), nil
	}
	cfg.GroupJID = groupJID
	if cfg.AutoRespon == nil {
		cfg.AutoRespon = map[string]string{}
	}
	if cfg.DisabledCategories == nil {
		cfg.DisabledCategories = map[string]bool{}
	}
	if cfg.DisabledCommands == nil {
		cfg.DisabledCommands = map[string]bool{}
	}
	if cfg.WarnLimit <= 0 {
		cfg.WarnLimit = 3
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Jakarta"
	}
	return cfg, nil
}

func (s *BotStore) SaveGroupConfig(cfg *GroupConfig) error {
	if s == nil || cfg == nil || cfg.GroupJID == "" {
		return nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(`
		INSERT INTO group_configs (group_jid, config_json) VALUES (?, ?)
		ON CONFLICT(group_jid) DO UPDATE SET config_json = excluded.config_json
	`, cfg.GroupJID, string(data))
	return err
}

func (s *BotStore) ListAllGroupConfigs() ([]*GroupConfig, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT group_jid, config_json FROM group_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*GroupConfig
	for rows.Next() {
		var jid, raw string
		if err := rows.Scan(&jid, &raw); err != nil {
			continue
		}
		cfg := DefaultGroupConfig(jid)
		if json.Unmarshal([]byte(raw), cfg) == nil {
			cfg.GroupJID = jid
			out = append(out, cfg)
		}
	}
	return out, nil
}

// --- GroupMember methods ---

func (s *BotStore) GetGroupMember(groupJID, userJID string) (*GroupMemberState, error) {
	if s == nil {
		return &GroupMemberState{GroupJID: groupJID, UserJID: userJID}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT group_jid, user_jid, msg_count, last_active, warns, muted_until, banned FROM group_members WHERE group_jid = ? AND user_jid = ?`, groupJID, userJID)
	var m GroupMemberState
	var banned int
	err := row.Scan(&m.GroupJID, &m.UserJID, &m.MsgCount, &m.LastActive, &m.Warns, &m.MutedUntil, &banned)
	if err == sql.ErrNoRows {
		return &GroupMemberState{GroupJID: groupJID, UserJID: userJID}, nil
	}
	if err != nil {
		return nil, err
	}
	m.Banned = banned != 0
	return &m, nil
}

func (s *BotStore) SaveGroupMember(m *GroupMemberState) error {
	if s == nil || m == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO group_members (group_jid, user_jid, msg_count, last_active, warns, muted_until, banned)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(group_jid, user_jid) DO UPDATE SET
			msg_count = excluded.msg_count,
			last_active = excluded.last_active,
			warns = excluded.warns,
			muted_until = excluded.muted_until,
			banned = excluded.banned
	`, m.GroupJID, m.UserJID, m.MsgCount, m.LastActive, m.Warns, m.MutedUntil, boolInt(m.Banned))
	return err
}

func (s *BotStore) TouchGroupMember(groupJID, userJID string) {
	if s == nil || groupJID == "" || userJID == "" {
		return
	}
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`
		INSERT INTO group_members (group_jid, user_jid, msg_count, last_active)
		VALUES (?, ?, 1, ?)
		ON CONFLICT(group_jid, user_jid) DO UPDATE SET
			msg_count = msg_count + 1,
			last_active = excluded.last_active
	`, groupJID, userJID, now)
}

func (s *BotStore) ListGroupMembers(groupJID string) ([]GroupMemberState, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT group_jid, user_jid, msg_count, last_active, warns, muted_until, banned FROM group_members WHERE group_jid = ? ORDER BY msg_count DESC`, groupJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMemberState
	for rows.Next() {
		var m GroupMemberState
		var banned int
		if err := rows.Scan(&m.GroupJID, &m.UserJID, &m.MsgCount, &m.LastActive, &m.Warns, &m.MutedUntil, &banned); err == nil {
			m.Banned = banned != 0
			out = append(out, m)
		}
	}
	return out, nil
}

// --- Attendance methods ---

func (s *BotStore) GetAttendance(groupJID string) (*AttendanceRecord, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var rec AttendanceRecord
	var presentJSON string
	err := s.db.QueryRow(`SELECT group_jid, title, date_key, present_json FROM attendance WHERE group_jid = ?`, groupJID).Scan(&rec.GroupJID, &rec.Title, &rec.DateKey, &presentJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Present = map[string]string{}
	_ = json.Unmarshal([]byte(presentJSON), &rec.Present)
	return &rec, nil
}

func (s *BotStore) SaveAttendance(rec *AttendanceRecord) error {
	if s == nil || rec == nil {
		return nil
	}
	data, _ := json.Marshal(rec.Present)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO attendance (group_jid, title, date_key, present_json)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(group_jid) DO UPDATE SET
			title = excluded.title,
			date_key = excluded.date_key,
			present_json = excluded.present_json
	`, rec.GroupJID, rec.Title, rec.DateKey, string(data))
	return err
}

func (s *BotStore) ClearAttendance(groupJID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM attendance WHERE group_jid = ?`, groupJID)
	return err
}

// --- GameStats methods ---

func (s *BotStore) RecordGameResult(userJID, groupJID, game string, won, draw bool, points int64) error {
	if s == nil || userJID == "" || game == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var cur GameStatRow
	err := s.db.QueryRow(`SELECT wins, losses, draws, points, streak, best_streak FROM game_stats WHERE user_jid = ? AND group_jid = ? AND game = ?`, userJID, groupJID, game).
		Scan(&cur.Wins, &cur.Losses, &cur.Draws, &cur.Points, &cur.Streak, &cur.BestStreak)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if draw {
		cur.Draws++
	} else if won {
		cur.Wins++
		cur.Streak++
		if cur.Streak > cur.BestStreak {
			cur.BestStreak = cur.Streak
		}
	} else {
		cur.Losses++
		cur.Streak = 0
	}
	cur.Points += points
	now := time.Now().Unix()
	_, err = s.db.Exec(`
		INSERT INTO game_stats (user_jid, group_jid, game, wins, losses, draws, points, streak, best_streak, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_jid, group_jid, game) DO UPDATE SET
			wins = excluded.wins,
			losses = excluded.losses,
			draws = excluded.draws,
			points = excluded.points,
			streak = excluded.streak,
			best_streak = excluded.best_streak,
			updated_at = excluded.updated_at
	`, userJID, groupJID, game, cur.Wins, cur.Losses, cur.Draws, cur.Points, cur.Streak, cur.BestStreak, now)
	return err
}

func (s *BotStore) GetUserGameStats(userJID string) ([]GameStatRow, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT user_jid, group_jid, game, wins, losses, draws, points, streak, best_streak, updated_at FROM game_stats WHERE user_jid = ? ORDER BY points DESC`, userJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GameStatRow
	for rows.Next() {
		var r GameStatRow
		if err := rows.Scan(&r.UserJID, &r.GroupJID, &r.Game, &r.Wins, &r.Losses, &r.Draws, &r.Points, &r.Streak, &r.BestStreak, &r.UpdatedAt); err == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// LeaderboardEntry aggregates total wins/points per user or group.
type LeaderboardEntry struct {
	ID     string `json:"id"`
	Wins   int    `json:"wins"`
	Losses int    `json:"losses"`
	Draws  int    `json:"draws"`
	Points int64  `json:"points"`
	Best   int    `json:"bestStreak"`
}

func (s *BotStore) GameLeaderboard(groupJID string, limit int) ([]LeaderboardEntry, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		rows *sql.Rows
		err  error
	)
	if groupJID != "" {
		rows, err = s.db.Query(`SELECT user_jid, SUM(wins), SUM(losses), SUM(draws), SUM(points), MAX(best_streak) FROM game_stats WHERE group_jid = ? GROUP BY user_jid ORDER BY SUM(points) DESC LIMIT ?`, groupJID, limit)
	} else {
		rows, err = s.db.Query(`SELECT user_jid, SUM(wins), SUM(losses), SUM(draws), SUM(points), MAX(best_streak) FROM game_stats GROUP BY user_jid ORDER BY SUM(points) DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.ID, &e.Wins, &e.Losses, &e.Draws, &e.Points, &e.Best); err == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *BotStore) GroupGameSummary(limit int) ([]LeaderboardEntry, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT group_jid, SUM(wins), SUM(losses), SUM(draws), SUM(points), MAX(best_streak) FROM game_stats WHERE group_jid != '' GROUP BY group_jid ORDER BY SUM(points) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.ID, &e.Wins, &e.Losses, &e.Draws, &e.Points, &e.Best); err == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// --- RedeemCode methods ---

func (s *BotStore) SaveRedeemCode(rc *RedeemCode) error {
	if s == nil || rc == nil {
		return nil
	}
	rc.Code = strings.ToUpper(strings.TrimSpace(rc.Code))
	usedJSON, _ := json.Marshal(rc.UsedBy)
	if rc.CreatedAt == 0 {
		rc.CreatedAt = time.Now().Unix()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO redeem_codes (code, reward_balance, reward_xp, premium_days, max_uses, used_by_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			reward_balance = excluded.reward_balance,
			reward_xp = excluded.reward_xp,
			premium_days = excluded.premium_days,
			max_uses = excluded.max_uses,
			used_by_json = excluded.used_by_json
	`, rc.Code, rc.RewardBalance, rc.RewardXP, rc.PremiumDays, rc.MaxUses, string(usedJSON), rc.CreatedAt)
	return err
}

func (s *BotStore) GetRedeemCode(code string) (*RedeemCode, error) {
	if s == nil {
		return nil, nil
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	s.mu.Lock()
	defer s.mu.Unlock()
	var rc RedeemCode
	var usedJSON string
	err := s.db.QueryRow(`SELECT code, reward_balance, reward_xp, premium_days, max_uses, used_by_json, created_at FROM redeem_codes WHERE code = ?`, code).
		Scan(&rc.Code, &rc.RewardBalance, &rc.RewardXP, &rc.PremiumDays, &rc.MaxUses, &usedJSON, &rc.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(usedJSON), &rc.UsedBy)
	return &rc, nil
}
