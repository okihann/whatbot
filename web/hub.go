package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime"
	"strings"
	"sync"
	"time"

	db "bot/databases"
	"bot/pairing"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const maxWebAutoDownloadBytes = 15 * 1024 * 1024 // 15 MB auto-capture limit for web UI

// AccountInfo describes a connected WhatsApp account exposed to the Web UI.
type AccountInfo struct {
	ID        string `json:"id"`
	User      string `json:"user"`
	PushName  string `json:"pushName"`
	Connected bool   `json:"connected"`
	Platform  string `json:"platform"`
}

// AccountManager decouples the web package from main's client registry.
type AccountManager interface {
	ListAccounts() []AccountInfo
	GetClient(accountID string) (*whatsmeow.Client, bool)
	StartPairing() error
	RemoveAccount(accountID string) error
}

// SSEEvent is broadcast to connected browser tabs in real time.
type SSEEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Hub coordinates the sequential ChatStore, WhatsApp accounts, QR state, and SSE subscribers.
type Hub struct {
	store     *db.ChatStore
	sessionDB *sql.DB
	botStore  *db.BotStore
	pairQueue *pairing.Queue
	accounts  AccountManager
	startedAt time.Time

	mu          sync.RWMutex
	subscribers map[chan SSEEvent]struct{}
	qrCode      string
	qrStatus    string
	qrUpdatedAt int64
}

func NewHub(chatStore *db.ChatStore, sessionDB *sql.DB, accounts AccountManager) *Hub {
	return &Hub{
		store:       chatStore,
		sessionDB:   sessionDB,
		accounts:    accounts,
		startedAt:   time.Now(),
		subscribers: make(map[chan SSEEvent]struct{}),
		qrStatus:    "idle",
	}
}

func (h *Hub) SetBotStore(bs *db.BotStore) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.botStore = bs
	h.mu.Unlock()
}

func (h *Hub) SetPairingQueue(q *pairing.Queue) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.pairQueue = q
	h.mu.Unlock()
}

func (h *Hub) Subscribe() (chan SSEEvent, func()) {
	ch := make(chan SSEEvent, 64)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Broadcast(evtType string, data any) {
	evt := SSEEvent{Type: evtType, Data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- evt:
		default:
			// Slow subscriber; drop oldest notification rather than blocking.
		}
	}
}

func (h *Hub) PublishQR(code string) {
	h.mu.Lock()
	h.qrCode = code
	h.qrStatus = "code"
	h.qrUpdatedAt = time.Now().Unix()
	h.mu.Unlock()
	h.Broadcast("qr", map[string]any{
		"status":    "code",
		"updatedAt": time.Now().Unix(),
	})
}

func (h *Hub) PublishQREvent(status string) {
	h.mu.Lock()
	if status == "success" || status == "timeout" || status == "idle" {
		h.qrCode = ""
	}
	h.qrStatus = status
	h.qrUpdatedAt = time.Now().Unix()
	h.mu.Unlock()
	h.Broadcast("qr", map[string]any{
		"status":    status,
		"updatedAt": time.Now().Unix(),
	})
	if status == "success" {
		h.Broadcast("accounts_updated", nil)
	}
}

func (h *Hub) CurrentQR() (code, status string, updatedAt int64) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.qrCode, h.qrStatus, h.qrUpdatedAt
}

func accountKey(client *whatsmeow.Client) string {
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return ""
	}
	return client.Store.ID.ToNonAD().User
}

func extensionForMime(mimeType, fallback string) string {
	if mimeType != "" {
		clean := strings.TrimSpace(strings.Split(mimeType, ";")[0])
		switch clean {
		case "image/jpeg":
			return ".jpg"
		case "image/png":
			return ".png"
		case "image/webp":
			return ".webp"
		case "image/gif":
			return ".gif"
		case "video/mp4":
			return ".mp4"
		case "audio/ogg":
			return ".ogg"
		case "audio/mpeg":
			return ".mp3"
		case "audio/mp4":
			return ".m4a"
		case "application/pdf":
			return ".pdf"
		}
		if exts, err := mime.ExtensionsByType(clean); err == nil && len(exts) > 0 {
			return exts[0]
		}
	}
	return fallback
}

func classifyMessageForWeb(core *waE2E.Message) (msgType, body, caption, mimeType string) {
	if core == nil {
		return "text", "", "", ""
	}
	switch {
	case core.ImageMessage != nil:
		return "image", "", core.ImageMessage.GetCaption(), core.ImageMessage.GetMimetype()
	case core.VideoMessage != nil:
		return "video", "", core.VideoMessage.GetCaption(), core.VideoMessage.GetMimetype()
	case core.AudioMessage != nil:
		return "audio", "", "", core.AudioMessage.GetMimetype()
	case core.StickerMessage != nil:
		if core.StickerMessage.GetAudioMessage() != nil {
			return "sound_sticker", "", "", core.StickerMessage.GetMimetype()
		}
		return "sticker", "", "", core.StickerMessage.GetMimetype()
	case core.DocumentMessage != nil:
		cap := core.DocumentMessage.GetCaption()
		if cap == "" {
			cap = core.DocumentMessage.GetFileName()
		}
		return "document", "", cap, core.DocumentMessage.GetMimetype()
	default:
		return "text", utils.GetTextFromMessage(core), "", ""
	}
}

func resolveWebChatName(client *whatsmeow.Client, info waTypes.MessageInfo) string {
	if info.IsGroup && client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if gi, err := client.GetGroupInfo(ctx, info.Chat); err == nil && gi != nil && gi.Name != "" {
			return gi.Name
		}
	}
	if !info.IsGroup && !info.IsFromMe && strings.TrimSpace(info.PushName) != "" {
		return strings.TrimSpace(info.PushName)
	}
	if !info.IsGroup {
		if pn := utils.ResolvePhoneNumber(client, info.Chat, info.RecipientAlt); pn != "" {
			return pn
		}
	}
	return info.Chat.ToNonAD().User
}

// IngestMessage records live incoming/outgoing messages (including view-once
// media, sound stickers, and revokes) into the sequential ChatStore and pushes
// real-time updates to connected WhatsApp Web tabs.
func (h *Hub) IngestMessage(client *whatsmeow.Client, msg *events.Message) {
	if h == nil || h.store == nil || msg == nil || msg.Message == nil {
		return
	}
	acct := accountKey(client)
	if acct == "" {
		return
	}

	core := utils.UnwrapMessage(msg.Message)
	if core == nil {
		return
	}

	// Skip WhatsApp status broadcasts from cluttering the DM/group sidebar.
	if msg.Info.Chat.Server == waTypes.BroadcastServer {
		return
	}

	// Handle revocations so deleted messages remain visible with a badge.
	if pm := core.GetProtocolMessage(); pm != nil && pm.GetType() == waE2E.ProtocolMessage_REVOKE {
		targetID := pm.GetKey().GetID()
		if targetID == "" {
			targetID = msg.Info.MsgMetaInfo.TargetID
		}
		if targetID != "" {
			h.store.MarkDeleted(acct, targetID)
			h.Broadcast("message_deleted", map[string]any{
				"accountId": acct,
				"chatJid":   msg.Info.Chat.ToNonAD().String(),
				"msgId":     targetID,
			})
		}
		return
	}

	// Ignore pure protocol/reaction noise without user content.
	if core.ProtocolMessage != nil || core.ReactionMessage != nil || core.SenderKeyDistributionMessage != nil {
		return
	}

	msgType, body, caption, mimeType := classifyMessageForWeb(core)
	if msgType == "text" && strings.TrimSpace(body) == "" {
		return
	}

	isVO := utils.IsViewOnceMessage(msg, nil)
	senderName := strings.TrimSpace(msg.Info.PushName)
	if msg.Info.IsFromMe {
		senderName = "You"
	} else if senderName == "" {
		senderName = utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
	}

	ts := msg.Info.Timestamp.Unix()
	if ts <= 0 {
		ts = time.Now().Unix()
	}

	stored := db.StoredMessage{
		AccountID:   acct,
		MsgID:       msg.Info.ID,
		ChatJID:     msg.Info.Chat.ToNonAD().String(),
		SenderJID:   msg.Info.Sender.ToNonAD().String(),
		SenderName:  senderName,
		IsFromMe:    msg.Info.IsFromMe,
		MsgType:     msgType,
		Body:        body,
		Caption:     caption,
		IsViewOnce:  isVO,
		MediaMime:   mimeType,
		Timestamp:   ts,
		ChatName:    resolveWebChatName(client, msg.Info),
		ChatIsGroup: msg.Info.IsGroup,
	}

	h.store.RecordMessage(stored)
	h.Broadcast("message", stored)

	// Automatically download and persist media (especially view-once & sound stickers)
	// asynchronously so the Web UI can display them permanently.
	if utils.HasDownloadableMedia(core) {
		fileLen := utils.MediaFileLength(core)
		if isVO || fileLen == 0 || fileLen <= maxWebAutoDownloadBytes {
			go h.captureMediaForWeb(client, acct, stored.ChatJID, stored.MsgID, core, mimeType)
		}
	}
}

func (h *Hub) captureMediaForWeb(client *whatsmeow.Client, acct, chatJID, msgID string, core *waE2E.Message, mimeType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Check if View-Once pre-cache already downloaded the bytes.
	var data []byte
	if cached, ok := utils.DefaultMessageCache.Get(acct, msgID); ok && cached != nil && len(cached.MediaData) > 0 {
		data = cached.MediaData
	}
	if len(data) == 0 {
		var err error
		data, _, _, err = utils.DownloadMedia(ctx, client, core)
		if err != nil {
			fmt.Printf("  [WEBCHAT] media capture skipped for %s: %v\n", msgID, err)
			return
		}
	}

	ext := extensionForMime(mimeType, ".bin")
	mediaRel, err := h.store.SaveMediaFile(acct, msgID, ext, data)
	if err != nil {
		fmt.Printf("  [WEBCHAT] save media error for %s: %v\n", msgID, err)
		return
	}

	var audioRel string
	if stk := core.GetStickerMessage(); stk != nil && stk.GetAudioMessage() != nil {
		if audBytes, err := client.Download(ctx, stk.GetAudioMessage()); err == nil && len(audBytes) > 0 {
			if rel, saveErr := h.store.SaveMediaFile(acct, msgID, "_audio.ogg", audBytes); saveErr == nil {
				audioRel = rel
			}
		}
	}

	h.store.UpdateMediaPaths(acct, msgID, mediaRel, audioRel, mimeType)
	h.Broadcast("media_ready", map[string]any{
		"accountId": acct,
		"chatJid":   chatJID,
		"msgId":     msgID,
		"mediaPath": mediaRel,
		"audioPath": audioRel,
		"mediaMime": mimeType,
	})
}

// IngestHistorySync imports recent conversations from WhatsApp's HistorySync
// payload into the sequential ChatStore so the Web UI is populated on login.
func (h *Hub) IngestHistorySync(client *whatsmeow.Client, evt *events.HistorySync) {
	if h == nil || h.store == nil || evt == nil || evt.Data == nil {
		return
	}
	acct := accountKey(client)
	if acct == "" {
		return
	}

	imported := 0
	for _, conv := range evt.Data.GetConversations() {
		chatJIDStr := conv.GetID()
		if chatJIDStr == "" || strings.HasSuffix(chatJIDStr, "@broadcast") {
			continue
		}
		chatJID, err := waTypes.ParseJID(chatJIDStr)
		if err != nil {
			continue
		}
		chatName := strings.TrimSpace(conv.GetName())
		if chatName == "" {
			chatName = strings.TrimSpace(conv.GetDisplayName())
		}
		if chatName == "" {
			chatName = chatJID.ToNonAD().User
		}
		isGroup := chatJID.Server == waTypes.GroupServer

		for _, histMsg := range conv.GetMessages() {
			webMsg := histMsg.GetMessage()
			if webMsg == nil || webMsg.GetMessage() == nil {
				continue
			}
			key := webMsg.GetKey()
			if key == nil || key.GetID() == "" {
				continue
			}
			raw := webMsg.GetMessage()
			core := utils.UnwrapMessage(raw)
			if core == nil || core.ProtocolMessage != nil || core.ReactionMessage != nil {
				continue
			}
			msgType, body, caption, mimeType := classifyMessageForWeb(core)
			if msgType == "text" && strings.TrimSpace(body) == "" {
				continue
			}

			senderJID := key.GetParticipant()
			if senderJID == "" {
				senderJID = chatJID.ToNonAD().String()
			}
			senderName := strings.TrimSpace(webMsg.GetPushName())
			if key.GetFromMe() {
				senderName = "You"
			} else if senderName == "" {
				senderName = chatName
			}

			h.store.RecordMessage(db.StoredMessage{
				AccountID:   acct,
				MsgID:       key.GetID(),
				ChatJID:     chatJID.ToNonAD().String(),
				SenderJID:   senderJID,
				SenderName:  senderName,
				IsFromMe:    key.GetFromMe(),
				MsgType:     msgType,
				Body:        body,
				Caption:     caption,
				IsViewOnce:  utils.IsViewOnceMessage(nil, raw),
				MediaMime:   mimeType,
				Timestamp:   int64(webMsg.GetMessageTimestamp()),
				ChatName:    chatName,
				ChatIsGroup: isGroup,
			})
			imported++
		}
	}

	if imported > 0 {
		fmt.Printf("📥 [WEBCHAT] Imported %d historical messages into sequential SQLite for %s\n", imported, acct)
		h.Broadcast("accounts_updated", nil)
	}
}

func writeJSON(w interface{ Write([]byte) (int, error) }, v any) {
	_ = json.NewEncoder(w).Encode(v)
}
