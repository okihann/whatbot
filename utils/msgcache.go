package utils

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
)

const (
	MaxCachedViewOnceBytes = 20 * 1024 * 1024  // 20 MB per view-once item
	maxTotalMediaBytes     = 150 * 1024 * 1024 // 150 MB total pre-cached view-once budget
	maxCacheEntries        = 5000
	messageCacheTTL        = 24 * time.Hour
	cacheCleanupInterval   = 10 * time.Minute
)

// CachedMessage stores an incoming message (and optionally pre-downloaded bytes
// for view-once media) so anti-delete, auto-get, and /get share a single source
// of truth.
type CachedMessage struct {
	Info                waTypes.MessageInfo
	Message             *waE2E.Message
	IsViewOnce          bool
	UnavailableViewOnce bool
	MediaData           []byte
	StoredAt            time.Time
}

// MessageCache is a thread-safe, TTL-evicted, memory-bounded message cache.
type MessageCache struct {
	mu              sync.Mutex
	entries         map[string]*CachedMessage
	autoGetDone     map[string]time.Time
	totalMediaBytes int64
	startOnce       sync.Once
}

// DefaultMessageCache is shared across automations (anti-delete / auto-get)
// and /get.
var DefaultMessageCache = NewMessageCache()

func NewMessageCache() *MessageCache {
	c := &MessageCache{
		entries:     make(map[string]*CachedMessage),
		autoGetDone: make(map[string]time.Time),
	}
	return c
}

func (c *MessageCache) startCleanup() {
	c.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(cacheCleanupInterval)
			defer ticker.Stop()
			for range ticker.C {
				c.Prune(time.Now())
			}
		}()
	})
}

func cacheKey(botID, msgID string) string {
	if botID == "" {
		return msgID
	}
	return botID + ":" + msgID
}

// Put stores a fresh incoming message.
func (c *MessageCache) Put(botID, msgID string, entry *CachedMessage) {
	if msgID == "" || entry == nil {
		return
	}
	c.startCleanup()

	key := cacheKey(botID, msgID)
	now := time.Now()
	if entry.StoredAt.IsZero() {
		entry.StoredAt = now
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if prev, ok := c.entries[key]; ok {
		c.totalMediaBytes -= int64(len(prev.MediaData))
		if len(entry.MediaData) == 0 && len(prev.MediaData) > 0 {
			entry.MediaData = prev.MediaData
		}
	}
	c.entries[key] = entry
	c.totalMediaBytes += int64(len(entry.MediaData))

	if len(c.entries) > maxCacheEntries {
		c.evictOldestLocked()
	}
	c.enforceMediaBudgetLocked()
}

// PutQuotedIfBetter records a quoted message ONLY when no cached entry exists
// yet or when the existing entry lacks downloadable media while the quoted
// message has downloadable media. This prevents replies from overwriting a
// complete original message with a stripped WhatsApp quote stub.
func (c *MessageCache) PutQuotedIfBetter(botID, quotedID string, entry *CachedMessage) {
	if quotedID == "" || entry == nil || entry.Message == nil {
		return
	}
	c.startCleanup()

	key := cacheKey(botID, quotedID)
	if entry.StoredAt.IsZero() {
		entry.StoredAt = time.Now()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existing, found := c.entries[key]
	if !found {
		c.entries[key] = entry
		if len(c.entries) > maxCacheEntries {
			c.evictOldestLocked()
		}
		return
	}

	// Only upgrade if the existing cached message has no downloadable media
	// and no text, or lacks downloadable media while the quote actually has it.
	if !HasDownloadableMedia(existing.Message) && HasDownloadableMedia(entry.Message) {
		existing.Message = entry.Message
		if entry.IsViewOnce {
			existing.IsViewOnce = true
		}
		if existing.Info.Sender.IsEmpty() && !entry.Info.Sender.IsEmpty() {
			existing.Info.Sender = entry.Info.Sender
		}
		if existing.Info.SenderAlt.IsEmpty() && !entry.Info.SenderAlt.IsEmpty() {
			existing.Info.SenderAlt = entry.Info.SenderAlt
		}
	}
}

// SetMediaData attaches pre-downloaded bytes (e.g. from an incoming view-once
// message) to an existing cache entry if within the size budget.
func (c *MessageCache) SetMediaData(botID, msgID string, data []byte) {
	if msgID == "" || len(data) == 0 || len(data) > MaxCachedViewOnceBytes {
		return
	}
	key := cacheKey(botID, msgID)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.entries[key]
	if !found {
		return
	}
	c.totalMediaBytes -= int64(len(entry.MediaData))
	copied := make([]byte, len(data))
	copy(copied, data)
	entry.MediaData = copied
	c.totalMediaBytes += int64(len(copied))
	c.enforceMediaBudgetLocked()
}

// MarkUnavailableViewOnce records that WhatsApp sent an <unavailable type="view_once">
// placeholder for this message ID instead of ciphertext.
func (c *MessageCache) MarkUnavailableViewOnce(botID, msgID string, info waTypes.MessageInfo) {
	if msgID == "" {
		return
	}
	c.startCleanup()
	key := cacheKey(botID, msgID)

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.entries[key]; ok {
		existing.IsViewOnce = true
		existing.UnavailableViewOnce = true
		return
	}
	c.entries[key] = &CachedMessage{
		Info:                info,
		IsViewOnce:          true,
		UnavailableViewOnce: true,
		StoredAt:            time.Now(),
	}
	if len(c.entries) > maxCacheEntries {
		c.evictOldestLocked()
	}
}

// Get returns a snapshot copy of the cached message for (botID, msgID).
func (c *MessageCache) Get(botID, msgID string) (*CachedMessage, bool) {
	if msgID == "" {
		return nil, false
	}
	key := cacheKey(botID, msgID)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.entries[key]
	if !found {
		return nil, false
	}
	snap := *entry
	return &snap, true
}

// TryMarkAutoGet atomically checks and marks a message as processed by auto-get.
// Returns true if the caller should proceed, false if already handled.
func (c *MessageCache) TryMarkAutoGet(botID, msgID string) bool {
	if msgID == "" {
		return false
	}
	c.startCleanup()
	key := cacheKey(botID, msgID)

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, done := c.autoGetDone[key]; done {
		return false
	}
	c.autoGetDone[key] = time.Now()
	return true
}

// UnmarkAutoGet releases an auto-get mark if processing failed transiently and
// may succeed later (e.g., when a reply later supplies media keys).
func (c *MessageCache) UnmarkAutoGet(botID, msgID string) {
	if msgID == "" {
		return
	}
	key := cacheKey(botID, msgID)

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.autoGetDone, key)
}

// Prune removes expired entries and expired auto-get markers.
func (c *MessageCache) Prune(now time.Time) {
	cutoff := now.Add(-messageCacheTTL)

	c.mu.Lock()
	defer c.mu.Unlock()

	for k, e := range c.entries {
		if e.StoredAt.Before(cutoff) {
			c.totalMediaBytes -= int64(len(e.MediaData))
			delete(c.entries, k)
		}
	}
	for k, ts := range c.autoGetDone {
		if ts.Before(cutoff) {
			delete(c.autoGetDone, k)
		}
	}
}

func (c *MessageCache) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	for k, e := range c.entries {
		if oldestKey == "" || e.StoredAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.StoredAt
		}
	}
	if oldestKey != "" {
		c.totalMediaBytes -= int64(len(c.entries[oldestKey].MediaData))
		delete(c.entries, oldestKey)
	}
}

func (c *MessageCache) enforceMediaBudgetLocked() {
	for c.totalMediaBytes > maxTotalMediaBytes {
		var oldestKey string
		var oldestTime time.Time
		for k, e := range c.entries {
			if len(e.MediaData) == 0 {
				continue
			}
			if oldestKey == "" || e.StoredAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.StoredAt
			}
		}
		if oldestKey == "" {
			break
		}
		c.totalMediaBytes -= int64(len(c.entries[oldestKey].MediaData))
		c.entries[oldestKey].MediaData = nil
	}
}
