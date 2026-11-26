package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	AccountID   string `json:"accountId"`
	MsgID       string `json:"msgId"`
	ChatJID     string `json:"chatJid"`
	SenderJID   string `json:"senderJid"`
	SenderName  string `json:"senderName"`
	IsFromMe    bool   `json:"isFromMe"`
	MsgType     string `json:"msgType"` // text, image, video, audio, sticker, sound_sticker, document
	Body        string `json:"body"`
	Caption     string `json:"caption"`
	IsViewOnce  bool   `json:"isViewOnce"`
	IsDeleted   bool   `json:"isDeleted"`
	MediaMime   string `json:"mediaMime"`
	MediaPath   string `json:"mediaPath"`
	AudioPath   string `json:"audioPath"`
	Timestamp   int64  `json:"timestamp"`
	ChatName    string `json:"-"`
	ChatIsGroup bool   `json:"-"`
}

type writeOp struct {
	fn   func(tx *sql.Tx) error
	done chan struct{}
}

// ChatStore persists chats and messages into a dedicated sequential-WAL SQLite
// database with a batched background writer goroutine.
type ChatStore struct {
	db        *sql.DB
	mediaRoot string
	writes    chan writeOp
	stopCh    chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
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
		timestamp    INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (account_id, msg_id)
	);
	CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages (account_id, chat_jid, timestamp DESC);
	`
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init webchat schema: %w", err)
	}

	cs := &ChatStore{
		db:        conn,
		mediaRoot: mediaRoot,
		writes:    make(chan writeOp, writeQueueSize),
		stopCh:    make(chan struct{}),
	}
	cs.wg.Add(1)
	go cs.writeLoop()
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
		tx, err := cs.db.Begin()
		if err == nil {
			for _, op := range batch {
				if op.fn != nil {
					if opErr := op.fn(tx); opErr != nil {
						fmt.Printf("[WEBCHAT] write error: %v\n", opErr)
					}
				}
			}
			if commitErr := tx.Commit(); commitErr != nil {
				fmt.Printf("[WEBCHAT] commit error: %v\n", commitErr)
			}
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
	select {
	case cs.writes <- writeOp{fn: fn}:
	default:
		// Queue saturated: run synchronously so messages are never lost.
		tx, err := cs.db.Begin()
		if err == nil {
			_ = fn(tx)
			_ = tx.Commit()
		}
	}
}

// Flush blocks until all queued sequential writes have been committed to WAL.
func (cs *ChatStore) Flush() {
	done := make(chan struct{})
	cs.writes <- writeOp{done: done}
	<-done
}

// Close drains pending writes, checkpoints the WAL, and closes SQLite.
func (cs *ChatStore) Close() error {
	var err error
	cs.closeOnce.Do(func() {
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
	if m.AccountID == "" || m.MsgID == "" || m.ChatJID == "" {
		return
	}
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
	if m.IsFromMe {
		unreadDelta = 0
	}

	cs.enqueue(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO messages (
				account_id, msg_id, chat_jid, sender_jid, sender_name, is_from_me,
				msg_type, body, caption, is_view_once, is_deleted,
				media_mime, media_path, audio_path, timestamp
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, msg_id) DO UPDATE SET
				body = CASE WHEN excluded.body != '' THEN excluded.body ELSE messages.body END,
				caption = CASE WHEN excluded.caption != '' THEN excluded.caption ELSE messages.caption END,
				media_mime = CASE WHEN excluded.media_mime != '' THEN excluded.media_mime ELSE messages.media_mime END,
				media_path = CASE WHEN excluded.media_path != '' THEN excluded.media_path ELSE messages.media_path END,
				audio_path = CASE WHEN excluded.audio_path != '' THEN excluded.audio_path ELSE messages.audio_path END,
				is_view_once = MAX(messages.is_view_once, excluded.is_view_once)
		`,
			m.AccountID, m.MsgID, m.ChatJID, m.SenderJID, m.SenderName, boolToInt(m.IsFromMe),
			m.MsgType, m.Body, m.Caption, boolToInt(m.IsViewOnce), boolToInt(m.IsDeleted),
			m.MediaMime, m.MediaPath, m.AudioPath, m.Timestamp,
		)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`
			INSERT INTO chats (account_id, chat_jid, name, is_group, last_message, last_timestamp, unread_count, has_view_once)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_id, chat_jid) DO UPDATE SET
				name = CASE WHEN excluded.name != '' AND (chats.name = '' OR chats.name = chats.chat_jid OR excluded.is_group = 1) THEN excluded.name ELSE chats.name END,
				is_group = MAX(chats.is_group, excluded.is_group),
				last_message = CASE WHEN excluded.last_timestamp >= chats.last_timestamp THEN excluded.last_message ELSE chats.last_message END,
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
	query := `
		SELECT account_id, chat_jid, name, is_group, last_message, last_timestamp, unread_count, has_view_once
		FROM chats
		WHERE account_id = ?
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
	query += ` ORDER BY last_timestamp DESC LIMIT 200`

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
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := cs.db.Query(`
		SELECT account_id, msg_id, chat_jid, sender_jid, sender_name, is_from_me,
		       msg_type, body, caption, is_view_once, is_deleted,
		       media_mime, media_path, audio_path, timestamp
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
		var fromMe, vo, del int
		if err := rows.Scan(
			&m.AccountID, &m.MsgID, &m.ChatJID, &m.SenderJID, &m.SenderName, &fromMe,
			&m.MsgType, &m.Body, &m.Caption, &vo, &del,
			&m.MediaMime, &m.MediaPath, &m.AudioPath, &m.Timestamp,
		); err != nil {
			return nil, err
		}
		m.IsFromMe = fromMe == 1
		m.IsViewOnce = vo == 1
		m.IsDeleted = del == 1
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

func (cs *ChatStore) GetMessage(accountID, msgID string) (*StoredMessage, error) {
	var m StoredMessage
	var fromMe, vo, del int
	err := cs.db.QueryRow(`
		SELECT account_id, msg_id, chat_jid, sender_jid, sender_name, is_from_me,
		       msg_type, body, caption, is_view_once, is_deleted,
		       media_mime, media_path, audio_path, timestamp
		FROM messages
		WHERE account_id = ? AND msg_id = ?
	`, accountID, msgID).Scan(
		&m.AccountID, &m.MsgID, &m.ChatJID, &m.SenderJID, &m.SenderName, &fromMe,
		&m.MsgType, &m.Body, &m.Caption, &vo, &del,
		&m.MediaMime, &m.MediaPath, &m.AudioPath, &m.Timestamp,
	)
	if err != nil {
		return nil, err
	}
	m.IsFromMe = fromMe == 1
	m.IsViewOnce = vo == 1
	m.IsDeleted = del == 1
	return &m, nil
}
