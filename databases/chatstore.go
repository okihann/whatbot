package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	writeQueueSize  = 2048
	writeBatchLimit = 64
	writeFlushDelay = 30 * time.Millisecond
)

// StoredChat represents a conversation row in the sequential webchat SQLite DB.
type StoredChat struct {
	AccountID     string `json:"accountId"`
	ChatJID       string `json:"chatJid"`
	Name          string `json:"name"`
	IsGroup       bool   `json:"isGroup"`
	LastMessage   string `json:"lastMessage"`
	LastTimestamp int64  `json:"lastTimestamp"`
	UnreadCount   int    `json:"unreadCount"`
	HasViewOnce   bool   `json:"hasViewOnce"`
}

// StoredMessage represents a persisted message (including recovered view-once
// media and anti-delete history) rendered by the WhatsApp Web UI.
type StoredMessage struct {
	AccountID           string `json:"accountId"`
	MsgID               string `json:"msgId"`
	ChatJID             string `json:"chatJid"`
	SenderJID           string `json:"senderJid"`
	SenderName          string `json:"senderName"`
	IsFromMe            bool   `json:"isFromMe"`
	MsgType             string `json:"msgType"` // text, image, video, audio, sticker, sound_sticker, document
	Body                string `json:"body"`
	Caption             string `json:"caption"`
	IsViewOnce          bool   `json:"isViewOnce"`
	IsDeleted           bool   `json:"isDeleted"`
	MediaMime           string `json:"mediaMime"`
	MediaPath           string `json:"mediaPath"`
	AudioPath           string `json:"audioPath"`
	ThumbPath           string `json:"thumbPath,omitempty"`
	HasMedia            bool   `json:"hasMedia"`
	Timestamp           int64  `json:"timestamp"`
	ChatName            string `json:"-"`
	ChatIsGroup         bool   `json:"-"`
	SkipUnreadIncrement bool   `json:"-"`
	MediaProto          []byte `json:"-"`
}

// StoredContact represents a synced WhatsApp contact stored for the Web UI.
type StoredContact struct {
	AccountID    string `json:"accountId"`
	JID          string `json:"jid"`
	Phone        string `json:"phone"`
	Name         string `json:"name"`
	FullName     string `json:"fullName"`
	PushName     string `json:"pushName"`
	BusinessName string `json:"businessName"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// IsIgnoredWebChatJID returns true for WhatsApp Channels (@newsletter) and
// Status/Broadcast lists (@broadcast) that should never appear in the chat list.
func IsIgnoredWebChatJID(jid string) bool {
	j := strings.ToLower(strings.TrimSpace(jid))
	if j == "" {
		return true
	}
	return strings.HasSuffix(j, "@newsletter") ||
		strings.HasSuffix(j, "@broadcast") ||
		j == "status@broadcast"
}

// NormalizeUnixSeconds converts millisecond Unix timestamps (> 1e12) into
// seconds so mixed-unit history timestamps never break ordering or previews.
func NormalizeUnixSeconds(ts int64) int64 {
	for ts > 1_000_000_000_000 {
		ts /= 1000
	}
	if ts < 0 {
		return 0
	}
	return ts
}

type writeOp struct {
	fn   func(tx *sql.Tx) error
	done chan struct{}
}

// ChatStore persists chats, contacts, and messages into a dedicated sequential-WAL
// SQLite database with a batched background writer goroutine.
type ChatStore struct {
	db            *sql.DB
	mediaRoot     string
	writes        chan writeOp
	stopCh        chan struct{}
	wg            sync.WaitGroup
	closeOnce     sync.Once
	closed        atomic.Bool
	pendingWrites atomic.Int64
}

// OpenChatStore initializes db/webchat.db and its sequential batch writer.
func OpenChatStore(dbPath, mediaRoot string) (*ChatStore, error) {
	conn, err := OpenSequentialSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(mediaRoot, 0755); err != nil {
		conn.Close()
		return nil, fmt.Errorf("create web media dir: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS chats (
		account_id     TEXT NOT NULL,
		chat_jid       TEXT NOT NULL,
		name           TEXT NOT NULL DEFAULT '',
		is_group       INTEGER NOT NULL DEFAULT 0,
		last_message   TEXT NOT NULL DEFAULT '',
		last_timestamp INTEGER NOT NULL DEFAULT 0,
		unread_count   INTEGER NOT NULL DEFAULT 0,
		has_view_once  INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (account_id, chat_jid)
	);
	CREATE INDEX IF NOT EXISTS idx_chats_account_ts ON chats (account_id, last_timestamp DESC);

	CREATE TABLE IF NOT EXISTS messages (
		account_id   TEXT NOT NULL,
		msg_id       TEXT NOT NULL,
		chat_jid     TEXT NOT NULL,
		sender_jid   TEXT NOT NULL DEFAULT '',
		sender_name  TEXT NOT NULL DEFAULT '',
		is_from_me   INTEGER NOT NULL DEFAULT 0,
		msg_type     TEXT NOT NULL DEFAULT 'text',
		body         TEXT NOT NULL DEFAULT '',
		caption      TEXT NOT NULL DEFAULT '',
		is_view_once INTEGER NOT NULL DEFAULT 0,
		is_deleted   INTEGER NOT NULL DEFAULT 0,
		media_mime   TEXT NOT NULL DEFAULT '',
		media_path   TEXT NOT NULL DEFAULT '',
		audio_path   TEXT NOT NULL DEFAULT '',
		thumb_path   TEXT NOT NULL DEFAULT '',
		media_proto  BLOB,
		timestamp    INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (account_id, msg_id)
	);
	CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages (account_id, chat_jid, timestamp DESC);

	CREATE TABLE IF NOT EXISTS contacts (
		account_id    TEXT NOT NULL,
		jid           TEXT NOT NULL,
		phone         TEXT NOT NULL DEFAULT '',
		name          TEXT NOT NULL DEFAULT '',
		full_name     TEXT NOT NULL DEFAULT '',
		push_name     TEXT NOT NULL DEFAULT '',
		business_name TEXT NOT NULL DEFAULT '',
		updated_at    INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (account_id, jid)
	);
	CREATE INDEX IF NOT EXISTS idx_contacts_name ON contacts (account_id, name COLLATE NOCASE);

	DELETE FROM chats WHERE LOWER(chat_jid) LIKE '%@newsletter' OR LOWER(chat_jid) LIKE '%@broadcast';
	DELETE FROM messages WHERE LOWER(chat_jid) LIKE '%@newsletter' OR LOWER(chat_jid) LIKE '%@broadcast';
	`
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init webchat schema: %w", err)
	}

	// Idempotent migrations for existing db/webchat.db files created by older versions.
	_, _ = conn.Exec(`ALTER TABLE messages ADD COLUMN thumb_path TEXT NOT NULL DEFAULT ''`)
	_, _ = conn.Exec(`ALTER TABLE messages ADD COLUMN media_proto BLOB`)
	// Repair any legacy millisecond timestamps that would pin old chats above new ones.
	_, _ = conn.Exec(`UPDATE messages SET timestamp = timestamp / 1000 WHERE timestamp > 1000000000000`)
	_, _ = conn.Exec(`UPDATE chats SET last_timestamp = last_timestamp / 1000 WHERE last_timestamp > 1000000000000`)

	cs := &ChatStore{
		db:        conn,
		mediaRoot: mediaRoot,
		writes:    make(chan writeOp, writeQueueSize),
		stopCh:    make(chan struct{}),
	}
	cs.wg.Add(1)
	go cs.writeLoop()
	cs.RepairChatSummaries("")
	return cs, nil
}

func (cs *ChatStore) writeLoop() {
	defer cs.wg.Done()
	batch := make([]writeOp, 0, writeBatchLimit)
	timer := time.NewTimer(writeFlushDelay)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		var drained int64
		tx, err := cs.db.Begin()
		if err == nil {
			for _, op := range batch {
				if op.fn != nil {
					drained++
					if opErr := op.fn(tx); opErr != nil {
						fmt.Printf("[WEBCHAT] write error: %v\n", opErr)
					}
				}
			}
			if commitErr := tx.Commit(); commitErr != nil {
				fmt.Printf("[WEBCHAT] commit error: %v\n", commitErr)
			}
		} else {
			for _, op := range batch {
				if op.fn != nil {
					drained++
				}
			}
		}
		if drained > 0 {
			cs.pendingWrites.Add(-drained)
		}
		for _, op := range batch {
			if op.done != nil {
				close(op.done)
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case op := <-cs.writes:
			batch = append(batch, op)
			// Immediately flush explicit sync/flush markers or full batches.
			if op.done != nil || len(batch) >= writeBatchLimit {
				flush()
				continue
			}
			timer.Reset(writeFlushDelay)

		case <-timer.C:
			flush()

		case <-cs.stopCh:
			// Drain remaining queued writes before shutting down.
			for {
				select {
				case op := <-cs.writes:
					batch = append(batch, op)
					if len(batch) >= writeBatchLimit {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (cs *ChatStore) enqueue(fn func(tx *sql.Tx) error) {
	if cs.closed.Load() {
		return
	}
	cs.pendingWrites.Add(1)
	select {
	case cs.writes <- writeOp{fn: fn}:
	default:
		// Queue saturated: run synchronously so messages are never lost.
		tx, err := cs.db.Begin()
		if err == nil {
			if opErr := fn(tx); opErr != nil {
				fmt.Printf("[WEBCHAT] sync write error: %v\n", opErr)
			}
			_ = tx.Commit()
		}
		cs.pendingWrites.Add(-1)
	}
}

// Flush blocks until all queued sequential writes have been committed to WAL.
func (cs *ChatStore) Flush() {
	if cs == nil || cs.closed.Load() || cs.pendingWrites.Load() <= 0 {
		return
	}
	done := make(chan struct{})
	select {
	case cs.writes <- writeOp{done: done}:
		<-done
	case <-cs.stopCh:
	}
}

// Close drains pending writes, checkpoints the WAL, and closes SQLite.
func (cs *ChatStore) Close() error {
	var err error
	cs.closeOnce.Do(func() {
		cs.closed.Store(true)
		close(cs.stopCh)
		cs.wg.Wait()
		err = CheckpointAndClose(cs.db)
	})
	return err
}

func (cs *ChatStore) Status() map[string]string {
	return SQLiteStatus(cs.db)
}

func (cs *ChatStore) MediaRoot() string {
	return cs.mediaRoot
}

func (cs *ChatStore) SaveMediaFile(accountID, msgID, suffix string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty media")
	}
	safeAcct := sanitizePathSegment(accountID)
	safeID := sanitizePathSegment(msgID)
	dir := filepath.Join(cs.mediaRoot, safeAcct)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	rel := filepath.Join(safeAcct, safeID+suffix)
	full := filepath.Join(cs.mediaRoot, rel)
	if err := os.WriteFile(full, data, 0644); err != nil {
		return "", err
	}
	return rel, nil
}

func sanitizePathSegment(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", "..", "_", ":", "_", " ", "_")
	out := r.Replace(strings.TrimSpace(s))
	if out == "" {
		return "unknown"
	}
	return out
}

func previewForMessage(m StoredMessage) string {
	if m.Body != "" {
		return m.Body
	}
	prefix := ""
	if m.IsViewOnce {
		prefix = "👁️ View-Once "
	}
	switch m.MsgType {
	case "image":
		if m.Caption != "" {
			return prefix + "📷 " + m.Caption
		}
		return prefix + "📷 Photo"
	case "video":
		if m.Caption != "" {
			return prefix + "🎬 " + m.Caption
		}
		return prefix + "🎬 Video"
	case "audio":
		return prefix + "🎤 Audio"
	case "sound_sticker":
		return "🔊 Sound Sticker"
	case "sticker":
		return "🖼️ Sticker"
	case "document":
		if m.Caption != "" {
			return "📄 " + m.Caption
		}
		return "📄 Document"
	default:
		return "[Message]"
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// RecordMessage persists a message and updates its chat summary sequentially.
func (cs *ChatStore) RecordMessage(m StoredMessage) {
	if m.AccountID == "" || m.MsgID == "" || m.ChatJID == "" || IsIgnoredWebChatJID(m.ChatJID) {
		return
	}
	m.Timestamp = NormalizeUnixSeconds(m.Timestamp)
	if m.Timestamp == 0 {
		m.Timestamp = time.Now().Unix()
	}
	preview := previewForMessage(m)
	chatName := strings.TrimSpace(m.ChatName)
	if chatName == "" {
		chatName = m.SenderName
	}
	if chatName == "" {
		chatName = strings.Split(m.ChatJID, "@")[0]
	}

	unreadDelta := 1
	if m.IsFromMe || m.SkipUnreadIncrement {
		unreadDelta = 0
	}

	cs.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO messages (
				account_id, msg_id, chat_jid, sender_jid, sender_name, is_from_me,
				msg_type, body, caption, is_view_once, is_deleted,
				media_mime, media_path, audio_path, thumb_path, media_proto, timestamp
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, msg_id) DO UPDATE SET
				chat_jid = CASE WHEN messages.chat_jid LIKE '%@lid' AND excluded.chat_jid NOT LIKE '%@lid' THEN excluded.chat_jid ELSE messages.chat_jid END,
				body = CASE WHEN excluded.body != '' THEN excluded.body ELSE messages.body END,
				caption = CASE WHEN excluded.caption != '' THEN excluded.caption ELSE messages.caption END,
				media_mime = CASE WHEN excluded.media_mime != '' THEN excluded.media_mime ELSE messages.media_mime END,
				media_path = CASE WHEN excluded.media_path != '' THEN excluded.media_path ELSE messages.media_path END,
				audio_path = CASE WHEN excluded.audio_path != '' THEN excluded.audio_path ELSE messages.audio_path END,
				thumb_path = CASE WHEN excluded.thumb_path != '' THEN excluded.thumb_path ELSE messages.thumb_path END,
				media_proto = CASE WHEN excluded.media_proto IS NOT NULL AND LENGTH(excluded.media_proto) > 0 THEN excluded.media_proto ELSE messages.media_proto END,
				is_view_once = MAX(messages.is_view_once, excluded.is_view_once)
		`,
			m.AccountID, m.MsgID, m.ChatJID, m.SenderJID, m.SenderName, boolToInt(m.IsFromMe),
			m.MsgType, m.Body, m.Caption, boolToInt(m.IsViewOnce), boolToInt(m.IsDeleted),
			m.MediaMime, m.MediaPath, m.AudioPath, m.ThumbPath, m.MediaProto, m.Timestamp,
		)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`
			INSERT INTO chats (account_id, chat_jid, name, is_group, last_message, last_timestamp, unread_count, has_view_once)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, chat_jid) DO UPDATE SET
				name = CASE WHEN excluded.name != '' AND (chats.name = '' OR chats.name = chats.chat_jid OR chats.name = SUBSTR(chats.chat_jid, 1, INSTR(chats.chat_jid, '@') - 1) OR excluded.is_group = 1) THEN excluded.name ELSE chats.name END,
				is_group = MAX(chats.is_group, excluded.is_group),
				last_message = CASE
					WHEN excluded.last_timestamp >= chats.last_timestamp THEN excluded.last_message
					WHEN chats.last_message = '' THEN excluded.last_message
					ELSE chats.last_message
				END,
				last_timestamp = MAX(chats.last_timestamp, excluded.last_timestamp),
				unread_count = chats.unread_count + excluded.unread_count,
				has_view_once = MAX(chats.has_view_once, excluded.has_view_once)
		`,
			m.AccountID, m.ChatJID, chatName, boolToInt(m.ChatIsGroup),
			preview, m.Timestamp, unreadDelta, boolToInt(m.IsViewOnce),
		)
		return err
	})
}

// UpsertChat ensures a conversation (e.g. from HistorySync, joined groups, or contacts)
// exists in the chats table and refreshes its display name without overwriting newer messages.
func (cs *ChatStore) UpsertChat(c StoredChat) {
	if c.AccountID == "" || c.ChatJID == "" || IsIgnoredWebChatJID(c.ChatJID) {
		return
	}
	c.LastTimestamp = NormalizeUnixSeconds(c.LastTimestamp)
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = strings.Split(c.ChatJID, "@")[0]
	}
	cs.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO chats (account_id, chat_jid, name, is_group, last_message, last_timestamp, unread_count, has_view_once)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, chat_jid) DO UPDATE SET
				name = CASE
					WHEN excluded.name != ''
					 AND excluded.name != excluded.chat_jid
					 AND excluded.name != SUBSTR(excluded.chat_jid, 1, INSTR(excluded.chat_jid, '@') - 1)
					THEN excluded.name
					WHEN chats.name = '' THEN excluded.name
					ELSE chats.name
				END,
				is_group = MAX(chats.is_group, excluded.is_group),
				last_message = CASE
					WHEN excluded.last_message != '' AND excluded.last_timestamp >= chats.last_timestamp THEN excluded.last_message
					WHEN chats.last_message = '' AND excluded.last_message != '' THEN excluded.last_message
					ELSE chats.last_message
				END,
				last_timestamp = MAX(chats.last_timestamp, excluded.last_timestamp),
				unread_count = MAX(chats.unread_count, excluded.unread_count)
		`,
			c.AccountID, c.ChatJID, name, boolToInt(c.IsGroup),
			c.LastMessage, c.LastTimestamp, c.UnreadCount, boolToInt(c.HasViewOnce),
		)
		return err
	})
}

// UpsertContact persists a synced WhatsApp contact and also upgrades any matching chat's display name.
func (cs *ChatStore) UpsertContact(c StoredContact) {
	if c.AccountID == "" || c.JID == "" || IsIgnoredWebChatJID(c.JID) {
		return
	}
	if c.UpdatedAt == 0 {
		c.UpdatedAt = time.Now().Unix()
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = strings.TrimSpace(c.FullName)
	}
	if name == "" {
		name = strings.TrimSpace(c.PushName)
	}
	if name == "" {
		name = strings.TrimSpace(c.BusinessName)
	}
	if name == "" {
		name = c.Phone
	}
	if name == "" {
		name = strings.Split(c.JID, "@")[0]
	}

	cs.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO contacts (account_id, jid, phone, name, full_name, push_name, business_name, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, jid) DO UPDATE SET
				phone = CASE WHEN excluded.phone != '' THEN excluded.phone ELSE contacts.phone END,
				name = CASE WHEN excluded.name != '' THEN excluded.name ELSE contacts.name END,
				full_name = CASE WHEN excluded.full_name != '' THEN excluded.full_name ELSE contacts.full_name END,
				push_name = CASE WHEN excluded.push_name != '' THEN excluded.push_name ELSE contacts.push_name END,
				business_name = CASE WHEN excluded.business_name != '' THEN excluded.business_name ELSE contacts.business_name END,
				updated_at = excluded.updated_at
		`, c.AccountID, c.JID, c.Phone, name, c.FullName, c.PushName, c.BusinessName, c.UpdatedAt)
		if err != nil {
			return err
		}
		// Upgrade existing DM chat name if it was just a phone number or empty.
		if name != "" && name != c.Phone && name != c.JID {
			_, _ = tx.Exec(`
				UPDATE chats
				SET name = ?
				WHERE account_id = ? AND chat_jid = ? AND is_group = 0
				  AND (name = '' OR name = chat_jid OR name = SUBSTR(chat_jid, 1, INSTR(chat_jid, '@') - 1))
			`, name, c.AccountID, c.JID)
		}
		return nil
	})
}

// ListContacts returns all synced contacts for an account, optionally filtered by query.
func (cs *ChatStore) ListContacts(accountID, search string) ([]StoredContact, error) {
	cs.Flush()
	query := `
		SELECT account_id, jid, phone, name, full_name, push_name, business_name, updated_at
		FROM contacts
		WHERE account_id = ?
		  AND LOWER(jid) NOT LIKE '%@newsletter'
		  AND LOWER(jid) NOT LIKE '%@broadcast'
	`
	args := []any{accountID}
	if q := strings.TrimSpace(search); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		query += ` AND (LOWER(name) LIKE ? OR LOWER(full_name) LIKE ? OR LOWER(push_name) LIKE ? OR LOWER(phone) LIKE ? OR LOWER(jid) LIKE ?)`
		args = append(args, like, like, like, like, like)
	}
	query += ` ORDER BY name COLLATE NOCASE ASC LIMIT 2000`

	rows, err := cs.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]StoredContact, 0)
	for rows.Next() {
		var c StoredContact
		if err := rows.Scan(&c.AccountID, &c.JID, &c.Phone, &c.Name, &c.FullName, &c.PushName, &c.BusinessName, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PurgeIgnoredChats deletes any WhatsApp Channel (@newsletter) or Broadcast rows.
func (cs *ChatStore) PurgeIgnoredChats() {
	cs.enqueue(func(tx *sql.Tx) error {
		_, _ = tx.Exec(`DELETE FROM chats WHERE LOWER(chat_jid) LIKE '%@newsletter' OR LOWER(chat_jid) LIKE '%@broadcast'`)
		_, _ = tx.Exec(`DELETE FROM messages WHERE LOWER(chat_jid) LIKE '%@newsletter' OR LOWER(chat_jid) LIKE '%@broadcast'`)
		return nil
	})
}

func (cs *ChatStore) UpdateMediaPaths(accountID, msgID, mediaPath, audioPath, mime string) {
	cs.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE messages
			SET media_path = CASE WHEN ? != '' THEN ? ELSE media_path END,
			    audio_path = CASE WHEN ? != '' THEN ? ELSE audio_path END,
			    media_mime = CASE WHEN ? != '' THEN ? ELSE media_mime END
			WHERE account_id = ? AND msg_id = ?
		`, mediaPath, mediaPath, audioPath, audioPath, mime, mime, accountID, msgID)
		return err
	})
}

// UpdateMessageBody updates the text or caption of an edited message.
func (cs *ChatStore) UpdateMessageBody(accountID, msgID, newBody, newCaption string) {
	if accountID == "" || msgID == "" {
		return
	}
	cs.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE messages
			SET body = CASE WHEN ? != '' THEN ? ELSE body END,
			    caption = CASE WHEN ? != '' THEN ? ELSE caption END
			WHERE account_id = ? AND msg_id = ?
		`, newBody, newBody, newCaption, newCaption, accountID, msgID)
		return err
	})
}

// ListLIDChatJIDs returns all @lid chat JIDs stored in either chats or messages
// for an account so SyncAccount can merge them into canonical @s.whatsapp.net JIDs.
func (cs *ChatStore) ListLIDChatJIDs(accountID string) ([]string, error) {
	cs.Flush()
	rows, err := cs.db.Query(`
		SELECT chat_jid FROM chats
		WHERE account_id = ? AND LOWER(chat_jid) LIKE '%@lid'
		UNION
		SELECT DISTINCT chat_jid FROM messages
		WHERE account_id = ? AND LOWER(chat_jid) LIKE '%@lid'
	`, accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var jid string
		if err := rows.Scan(&jid); err != nil {
			return nil, err
		}
		out = append(out, jid)
	}
	return out, rows.Err()
}

// RepairChatSummaries fixes legacy millisecond timestamps, creates missing chat
// rows for any orphaned messages, and backfills blank last_message/last_timestamp
// values from the newest stored message in each chat. Pass accountID="" to repair all accounts.
func (cs *ChatStore) RepairChatSummaries(accountID string) {
	if cs == nil {
		return
	}
	cs.enqueue(func(tx *sql.Tx) error {
		if accountID == "" {
			_, _ = tx.Exec(`UPDATE messages SET timestamp = timestamp / 1000 WHERE timestamp > 1000000000000`)
			_, _ = tx.Exec(`UPDATE chats SET last_timestamp = last_timestamp / 1000 WHERE last_timestamp > 1000000000000`)
		} else {
			_, _ = tx.Exec(`UPDATE messages SET timestamp = timestamp / 1000 WHERE account_id = ? AND timestamp > 1000000000000`, accountID)
			_, _ = tx.Exec(`UPDATE chats SET last_timestamp = last_timestamp / 1000 WHERE account_id = ? AND last_timestamp > 1000000000000`, accountID)
		}

		// Ensure every chat_jid that has stored messages also has a row in chats.
		orphanSQL := `
			INSERT OR IGNORE INTO chats (account_id, chat_jid, name, is_group, last_message, last_timestamp, unread_count, has_view_once)
			SELECT m.account_id,
			       m.chat_jid,
			       SUBSTR(m.chat_jid, 1, CASE WHEN INSTR(m.chat_jid, '@') > 0 THEN INSTR(m.chat_jid, '@') - 1 ELSE LENGTH(m.chat_jid) END),
			       CASE WHEN LOWER(m.chat_jid) LIKE '%@g.us' THEN 1 ELSE 0 END,
			       '',
			       MAX(m.timestamp),
			       0,
			       MAX(m.is_view_once)
			FROM messages m
			WHERE LOWER(m.chat_jid) NOT LIKE '%@newsletter'
			  AND LOWER(m.chat_jid) NOT LIKE '%@broadcast'
		`
		if accountID != "" {
			_, _ = tx.Exec(orphanSQL+` AND m.account_id = ? GROUP BY m.account_id, m.chat_jid`, accountID)
		} else {
			_, _ = tx.Exec(orphanSQL + ` GROUP BY m.account_id, m.chat_jid`)
		}

		// Backfill last_timestamp, last_message (when empty or older), and has_view_once from newest message.
		whereAcct := ""
		var args []any
		if accountID != "" {
			whereAcct = " WHERE account_id = ?"
			args = append(args, accountID)
		}
		_, _ = tx.Exec(`
			UPDATE chats
			SET last_timestamp = MAX(
				last_timestamp,
				COALESCE((SELECT MAX(m.timestamp) FROM messages m WHERE m.account_id = chats.account_id AND m.chat_jid = chats.chat_jid), 0)
			),
			has_view_once = MAX(
				has_view_once,
				COALESCE((SELECT MAX(m.is_view_once) FROM messages m WHERE m.account_id = chats.account_id AND m.chat_jid = chats.chat_jid), 0)
			),
			last_message = CASE
				WHEN last_message = '' THEN COALESCE((
					SELECT CASE
						WHEN m.body != '' THEN m.body
						WHEN m.caption != '' THEN m.caption
						ELSE '[' || m.msg_type || ']'
					END
					FROM messages m
					WHERE m.account_id = chats.account_id AND m.chat_jid = chats.chat_jid
					ORDER BY m.timestamp DESC
					LIMIT 1
				), '')
				ELSE last_message
			END
		`+whereAcct, args...)
		return nil
	})
}

// MergeChatJID moves all messages from an alias/LID chat JID (fromJID) into its
// canonical chat JID (toJID) and consolidates the chat summary row so messages
// are never split across duplicate @lid and @s.whatsapp.net threads.
func (cs *ChatStore) MergeChatJID(accountID, fromJID, toJID string) {
	fromJID = strings.TrimSpace(fromJID)
	toJID = strings.TrimSpace(toJID)
	if accountID == "" || fromJID == "" || toJID == "" || fromJID == toJID || IsIgnoredWebChatJID(toJID) {
		return
	}
	cs.enqueue(func(tx *sql.Tx) error {
		// Reassign messages from fromJID to toJID.
		if _, err := tx.Exec(`
			UPDATE messages
			SET chat_jid = ?
			WHERE account_id = ? AND chat_jid = ?
		`, toJID, accountID, fromJID); err != nil {
			return err
		}

		// Read the legacy/LID chat row if it exists.
		var fromName, fromLastMsg string
		var fromIsGroup, fromUnread, fromHasVO int
		var fromLastTS int64
		err := tx.QueryRow(`
			SELECT name, is_group, last_message, last_timestamp, unread_count, has_view_once
			FROM chats
			WHERE account_id = ? AND chat_jid = ?
		`, accountID, fromJID).Scan(&fromName, &fromIsGroup, &fromLastMsg, &fromLastTS, &fromUnread, &fromHasVO)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}

		// Don't copy a raw LID number as the display name of the PN chat.
		fromPrefix := strings.Split(fromJID, "@")[0]
		if fromName == fromJID || fromName == fromPrefix {
			fromName = ""
		}

		// Upsert/merge into the canonical chat row.
		_, err = tx.Exec(`
			INSERT INTO chats (account_id, chat_jid, name, is_group, last_message, last_timestamp, unread_count, has_view_once)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, chat_jid) DO UPDATE SET
				name = CASE
					WHEN (chats.name = '' OR chats.name = chats.chat_jid OR chats.name = SUBSTR(chats.chat_jid, 1, INSTR(chats.chat_jid, '@') - 1))
					 AND excluded.name != ''
					THEN excluded.name
					ELSE chats.name
				END,
				is_group = MAX(chats.is_group, excluded.is_group),
				last_message = CASE
					WHEN excluded.last_timestamp > chats.last_timestamp AND excluded.last_message != '' THEN excluded.last_message
					WHEN chats.last_message = '' AND excluded.last_message != '' THEN excluded.last_message
					ELSE chats.last_message
				END,
				last_timestamp = MAX(chats.last_timestamp, excluded.last_timestamp),
				unread_count = MAX(chats.unread_count, excluded.unread_count),
				has_view_once = MAX(chats.has_view_once, excluded.has_view_once)
		`, accountID, toJID, fromName, fromIsGroup, fromLastMsg, fromLastTS, fromUnread, fromHasVO)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`DELETE FROM chats WHERE account_id = ? AND chat_jid = ?`, accountID, fromJID)
		return err
	})
}

func (cs *ChatStore) MarkDeleted(accountID, msgID string) {
	cs.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE messages SET is_deleted = 1 WHERE account_id = ? AND msg_id = ?`, accountID, msgID)
		return err
	})
}

func (cs *ChatStore) MarkChatRead(accountID, chatJID string) {
	cs.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE chats SET unread_count = 0 WHERE account_id = ? AND chat_jid = ?`, accountID, chatJID)
		return err
	})
}

func (cs *ChatStore) ListChats(accountID, search, filter string) ([]StoredChat, error) {
	cs.Flush()
	query := `
		SELECT account_id, chat_jid, name, is_group, last_message, last_timestamp, unread_count, has_view_once
		FROM chats
		WHERE account_id = ?
		  AND LOWER(chat_jid) NOT LIKE '%@newsletter'
		  AND LOWER(chat_jid) NOT LIKE '%@broadcast'
	`
	args := []any{accountID}

	if q := strings.TrimSpace(search); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		query += ` AND (LOWER(name) LIKE ? OR LOWER(chat_jid) LIKE ? OR LOWER(last_message) LIKE ?)`
		args = append(args, like, like, like)
	}
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "unread":
		query += ` AND unread_count > 0`
	case "groups":
		query += ` AND is_group = 1`
	case "viewonce":
		query += ` AND has_view_once = 1`
	}
	query += ` ORDER BY last_timestamp DESC, name COLLATE NOCASE ASC LIMIT 1000`

	rows, err := cs.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]StoredChat, 0)
	for rows.Next() {
		var c StoredChat
		var isGroup, hasVO int
		if err := rows.Scan(&c.AccountID, &c.ChatJID, &c.Name, &isGroup, &c.LastMessage, &c.LastTimestamp, &c.UnreadCount, &hasVO); err != nil {
			return nil, err
		}
		c.IsGroup = isGroup == 1
		c.HasViewOnce = hasVO == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (cs *ChatStore) ListMessages(accountID, chatJID string, limit int) ([]StoredMessage, error) {
	cs.Flush()
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := cs.db.Query(`
		SELECT account_id, msg_id, chat_jid, sender_jid, sender_name, is_from_me,
		       msg_type, body, caption, is_view_once, is_deleted,
		       media_mime, media_path, audio_path, thumb_path,
		       CASE WHEN media_path != '' OR thumb_path != '' OR (media_proto IS NOT NULL AND LENGTH(media_proto) > 0) THEN 1 ELSE 0 END,
		       timestamp
		FROM messages
		WHERE account_id = ? AND chat_jid = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, accountID, chatJID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rev []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var fromMe, vo, del, hasMedia int
		if err := rows.Scan(
			&m.AccountID, &m.MsgID, &m.ChatJID, &m.SenderJID, &m.SenderName, &fromMe,
			&m.MsgType, &m.Body, &m.Caption, &vo, &del,
			&m.MediaMime, &m.MediaPath, &m.AudioPath, &m.ThumbPath, &hasMedia, &m.Timestamp,
		); err != nil {
			return nil, err
		}
		m.IsFromMe = fromMe == 1
		m.IsViewOnce = vo == 1
		m.IsDeleted = del == 1
		m.HasMedia = hasMedia == 1
		rev = append(rev, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse into chronological order (oldest -> newest) for chat UI rendering.
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, nil
}

// GetOldestMessage returns the earliest stored message in a chat, used as the
// anchor for on-demand WhatsApp history sync requests.
func (cs *ChatStore) GetOldestMessage(accountID, chatJID string) (*StoredMessage, error) {
	cs.Flush()
	var m StoredMessage
	var fromMe, vo, del int
	err := cs.db.QueryRow(`
		SELECT account_id, msg_id, chat_jid, sender_jid, sender_name, is_from_me,
		       msg_type, body, caption, is_view_once, is_deleted,
		       media_mime, media_path, audio_path, thumb_path, timestamp
		FROM messages
		WHERE account_id = ? AND chat_jid = ?
		ORDER BY timestamp ASC
		LIMIT 1
	`, accountID, chatJID).Scan(
		&m.AccountID, &m.MsgID, &m.ChatJID, &m.SenderJID, &m.SenderName, &fromMe,
		&m.MsgType, &m.Body, &m.Caption, &vo, &del,
		&m.MediaMime, &m.MediaPath, &m.AudioPath, &m.ThumbPath, &m.Timestamp,
	)
	if err != nil {
		return nil, err
	}
	m.IsFromMe = fromMe == 1
	m.IsViewOnce = vo == 1
	m.IsDeleted = del == 1
	m.HasMedia = m.MediaPath != "" || m.ThumbPath != ""
	return &m, nil
}

func (cs *ChatStore) GetMessage(accountID, msgID string) (*StoredMessage, error) {
	cs.Flush()
	var m StoredMessage
	var fromMe, vo, del int
	err := cs.db.QueryRow(`
		SELECT account_id, msg_id, chat_jid, sender_jid, sender_name, is_from_me,
		       msg_type, body, caption, is_view_once, is_deleted,
		       media_mime, media_path, audio_path, thumb_path, media_proto, timestamp
		FROM messages
		WHERE account_id = ? AND msg_id = ?
	`, accountID, msgID).Scan(
		&m.AccountID, &m.MsgID, &m.ChatJID, &m.SenderJID, &m.SenderName, &fromMe,
		&m.MsgType, &m.Body, &m.Caption, &vo, &del,
		&m.MediaMime, &m.MediaPath, &m.AudioPath, &m.ThumbPath, &m.MediaProto, &m.Timestamp,
	)
	if err != nil {
		return nil, err
	}
	m.IsFromMe = fromMe == 1
	m.IsViewOnce = vo == 1
	m.IsDeleted = del == 1
	m.HasMedia = m.MediaPath != "" || m.ThumbPath != "" || len(m.MediaProto) > 0
	return &m, nil
}
