package db

import (
	"database/sql"
	"strings"
	"time"
)

// ConfessionRecord stores an anonymous two-way confession thread.
type ConfessionRecord struct {
	ID            string `json:"id"`
	SenderJID     string `json:"senderJid"`
	TargetJID     string `json:"targetJid"`
	Alias         string `json:"alias"`
	Message       string `json:"message"`
	Status        string `json:"status"` // open, closed
	LastTargetMsg string `json:"lastTargetMsg"`
	LastSenderMsg string `json:"lastSenderMsg"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}

func initExtraSchema(conn *sql.DB) error {
	extra := `
	CREATE TABLE IF NOT EXISTS rpg_players (
		player_id  TEXT PRIMARY KEY,
		name       TEXT NOT NULL DEFAULT '',
		level      INTEGER NOT NULL DEFAULT 1,
		net_worth  INTEGER NOT NULL DEFAULT 0,
		power      INTEGER NOT NULL DEFAULT 0,
		data_json  TEXT NOT NULL,
		updated_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_rpg_networth ON rpg_players (net_worth DESC);
	CREATE INDEX IF NOT EXISTS idx_rpg_level ON rpg_players (level DESC);
	CREATE INDEX IF NOT EXISTS idx_rpg_power ON rpg_players (power DESC);

	CREATE TABLE IF NOT EXISTS rpg_guilds (
		guild_id   TEXT PRIMARY KEY,
		name       TEXT NOT NULL UNIQUE,
		leader_id  TEXT NOT NULL,
		level      INTEGER NOT NULL DEFAULT 1,
		treasury   INTEGER NOT NULL DEFAULT 0,
		data_json  TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS confessions (
		id              TEXT PRIMARY KEY,
		sender_jid      TEXT NOT NULL,
		target_jid      TEXT NOT NULL,
		alias           TEXT NOT NULL DEFAULT '',
		message         TEXT NOT NULL DEFAULT '',
		status          TEXT NOT NULL DEFAULT 'open',
		last_target_msg TEXT NOT NULL DEFAULT '',
		last_sender_msg TEXT NOT NULL DEFAULT '',
		created_at      INTEGER NOT NULL DEFAULT 0,
		updated_at      INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_confess_target ON confessions (target_jid, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_confess_sender ON confessions (sender_jid, updated_at DESC);

	CREATE TABLE IF NOT EXISTS confess_blocks (
		blocker_jid TEXT NOT NULL,
		blocked_jid TEXT NOT NULL,
		created_at  INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (blocker_jid, blocked_jid)
	);
	`
	if _, err := conn.Exec(extra); err != nil {
		return err
	}

	// Safe idempotent column migrations for existing db/botdata.db files.
	migrations := []string{
		`ALTER TABLE bot_tenants ADD COLUMN jadibot_locked INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE users ADD COLUMN lang TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN confess_opt_out INTEGER NOT NULL DEFAULT 0`,
	}
	for _, stmt := range migrations {
		_, _ = conn.Exec(stmt) // ignore duplicate-column errors
	}
	return nil
}

// --- RPG Player & Guild Persistence ---

func (s *BotStore) SaveRPGPlayerJSON(playerID, name string, level int, netWorth, power int64, rawJSON string) error {
	if s == nil || playerID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO rpg_players (player_id, name, level, net_worth, power, data_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(player_id) DO UPDATE SET
			name = excluded.name,
			level = excluded.level,
			net_worth = excluded.net_worth,
			power = excluded.power,
			data_json = excluded.data_json,
			updated_at = excluded.updated_at
	`, playerID, name, level, netWorth, power, rawJSON, time.Now().Unix())
	return err
}

func (s *BotStore) GetRPGPlayerJSON(playerID string) (string, bool, error) {
	if s == nil || playerID == "" {
		return "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	err := s.db.QueryRow(`SELECT data_json FROM rpg_players WHERE player_id = ?`, playerID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return raw, true, nil
}

func (s *BotStore) ListRPGPlayersJSON(orderBy string, limit int) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	col := "net_worth"
	switch strings.ToLower(orderBy) {
	case "level", "lvl":
		col = "level"
	case "power", "atk", "combat":
		col = "power"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT data_json FROM rpg_players ORDER BY `+col+` DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) == nil {
			out = append(out, raw)
		}
	}
	return out, nil
}

func (s *BotStore) SaveRPGGuildJSON(guildID, name, leaderID string, level int, treasury int64, rawJSON string) error {
	if s == nil || guildID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO rpg_guilds (guild_id, name, leader_id, level, treasury, data_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(guild_id) DO UPDATE SET
			name = excluded.name,
			leader_id = excluded.leader_id,
			level = excluded.level,
			treasury = excluded.treasury,
			data_json = excluded.data_json
	`, guildID, name, leaderID, level, treasury, rawJSON, time.Now().Unix())
	return err
}

func (s *BotStore) GetRPGGuildByNameJSON(name string) (string, bool, error) {
	if s == nil || name == "" {
		return "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	err := s.db.QueryRow(`SELECT data_json FROM rpg_guilds WHERE LOWER(name) = LOWER(?)`, strings.TrimSpace(name)).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return raw, true, nil
}

func (s *BotStore) GetRPGGuildByIDJSON(id string) (string, bool, error) {
	if s == nil || id == "" {
		return "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	err := s.db.QueryRow(`SELECT data_json FROM rpg_guilds WHERE guild_id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return raw, true, nil
}

func (s *BotStore) ListRPGGuildsJSON(limit int) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 15
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT data_json FROM rpg_guilds ORDER BY treasury DESC, level DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) == nil {
			out = append(out, raw)
		}
	}
	return out, nil
}

// --- Anonymous Confess / Menfess Persistence ---

func (s *BotStore) SaveConfession(rec *ConfessionRecord) error {
	if s == nil || rec == nil || rec.ID == "" {
		return nil
	}
	now := time.Now().Unix()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	if rec.Status == "" {
		rec.Status = "open"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO confessions (id, sender_jid, target_jid, alias, message, status, last_target_msg, last_sender_msg, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			last_target_msg = excluded.last_target_msg,
			last_sender_msg = excluded.last_sender_msg,
			updated_at = excluded.updated_at
	`, rec.ID, rec.SenderJID, rec.TargetJID, rec.Alias, rec.Message, rec.Status, rec.LastTargetMsg, rec.LastSenderMsg, rec.CreatedAt, rec.UpdatedAt)
	return err
}

func (s *BotStore) GetConfession(id string) (*ConfessionRecord, error) {
	if s == nil || id == "" {
		return nil, nil
	}
	id = strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(id), "#"))
	s.mu.Lock()
	defer s.mu.Unlock()
	var r ConfessionRecord
	err := s.db.QueryRow(`SELECT id, sender_jid, target_jid, alias, message, status, last_target_msg, last_sender_msg, created_at, updated_at FROM confessions WHERE id = ?`, id).
		Scan(&r.ID, &r.SenderJID, &r.TargetJID, &r.Alias, &r.Message, &r.Status, &r.LastTargetMsg, &r.LastSenderMsg, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *BotStore) GetConfessionByMsgID(stanzaID string) (*ConfessionRecord, error) {
	if s == nil || stanzaID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var r ConfessionRecord
	err := s.db.QueryRow(`SELECT id, sender_jid, target_jid, alias, message, status, last_target_msg, last_sender_msg, created_at, updated_at FROM confessions WHERE last_target_msg = ? OR last_sender_msg = ? LIMIT 1`, stanzaID, stanzaID).
		Scan(&r.ID, &r.SenderJID, &r.TargetJID, &r.Alias, &r.Message, &r.Status, &r.LastTargetMsg, &r.LastSenderMsg, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *BotStore) ListUserConfessions(userNum string, limit int) ([]ConfessionRecord, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, sender_jid, target_jid, alias, message, status, last_target_msg, last_sender_msg, created_at, updated_at FROM confessions WHERE target_jid = ? OR sender_jid = ? ORDER BY updated_at DESC LIMIT ?`, userNum, userNum, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConfessionRecord
	for rows.Next() {
		var r ConfessionRecord
		if rows.Scan(&r.ID, &r.SenderJID, &r.TargetJID, &r.Alias, &r.Message, &r.Status, &r.LastTargetMsg, &r.LastSenderMsg, &r.CreatedAt, &r.UpdatedAt) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *BotStore) BlockConfessSender(blocker, blocked string) error {
	if s == nil || blocker == "" || blocked == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT OR IGNORE INTO confess_blocks (blocker_jid, blocked_jid, created_at) VALUES (?, ?, ?)`, blocker, blocked, time.Now().Unix())
	return err
}

func (s *BotStore) IsConfessBlocked(blocker, sender string) bool {
	if s == nil || blocker == "" || sender == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM confess_blocks WHERE blocker_jid = ? AND blocked_jid = ?`, blocker, sender).Scan(&count)
	return count > 0
}
