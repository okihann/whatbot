package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
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

// Hub coordinates the sequential ChatStore, WhatsApp accounts, QR state, owner auth, and SSE subscribers.
type Hub struct {
	store     *db.ChatStore
	sessionDB *sql.DB
	botStore  *db.BotStore
	pairQueue *pairing.Queue
	accounts  AccountManager
	startedAt time.Time
	auth      *webAuth

	mu          sync.RWMutex
	subscribers map[chan SSEEvent]struct{}
	qrCode      string
	qrStatus    string
	qrUpdatedAt int64

	avatarMu   sync.Mutex
	avatarMiss map[string]time.Time
}

func NewHub(chatStore *db.ChatStore, sessionDB *sql.DB, accounts AccountManager) *Hub {
	return &Hub{
		store:       chatStore,
		sessionDB:   sessionDB,
		accounts:    accounts,
		startedAt:   time.Now(),
		auth:        newWebAuth(),
		subscribers: make(map[chan SSEEvent]struct{}),
		qrStatus:    "idle",
		avatarMiss:  make(map[string]time.Time),
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

func contactDisplayName(info waTypes.ContactInfo, fallback string) string {
	for _, s := range []string{info.FullName, info.BusinessName, info.PushName, info.FirstName} {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return fallback
}

func resolveContactName(client *whatsmeow.Client, jid waTypes.JID) string {
	if client == nil || client.Store == nil || client.Store.Contacts == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if info, err := client.Store.Contacts.GetContact(ctx, jid.ToNonAD()); err == nil && info.Found {
		return contactDisplayName(info, "")
	}
	return ""
}

func resolveWebChatName(client *whatsmeow.Client, info waTypes.MessageInfo) string {
	if info.IsGroup && client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if gi, err := client.GetGroupInfo(ctx, info.Chat); err == nil && gi != nil && gi.Name != "" {
			return gi.Name
		}
	}
	if !info.IsGroup {
		if saved := resolveContactName(client, info.Chat); saved != "" {
			return saved
		}
		if !info.IsFromMe && strings.TrimSpace(info.PushName) != "" {
			return strings.TrimSpace(info.PushName)
		}
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

	// Skip WhatsApp Channels (@newsletter) and Status/Broadcast lists (@broadcast).
	if msg.Info.Chat.Server == waTypes.BroadcastServer ||
		msg.Info.Chat.Server == waTypes.NewsletterServer ||
		db.IsIgnoredWebChatJID(msg.Info.Chat.String()) {
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
	} else {
		if saved := resolveContactName(client, msg.Info.Sender); saved != "" {
			senderName = saved
		} else if senderName == "" {
			senderName = utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
		}
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

// IngestHistorySync imports all conversations and messages from WhatsApp's HistorySync
// payload into the sequential ChatStore (excluding Channels & Status broadcasts).
func (h *Hub) IngestHistorySync(client *whatsmeow.Client, evt *events.HistorySync) {
	if h == nil || h.store == nil || evt == nil || evt.Data == nil {
		return
	}
	acct := accountKey(client)
	if acct == "" {
		return
	}

	importedMsgs := 0
	importedChats := 0
	for _, conv := range evt.Data.GetConversations() {
		chatJIDStr := strings.TrimSpace(conv.GetID())
		if chatJIDStr == "" || db.IsIgnoredWebChatJID(chatJIDStr) {
			continue
		}
		// Prefer phone-number JID over duplicate LID thread when WhatsApp supplies PnJID.
		if pn := strings.TrimSpace(conv.GetPnJID()); pn != "" && !db.IsIgnoredWebChatJID(pn) {
			chatJIDStr = pn
		}
		chatJID, err := waTypes.ParseJID(chatJIDStr)
		if err != nil || chatJID.Server == waTypes.BroadcastServer || chatJID.Server == waTypes.NewsletterServer {
			continue
		}
		normJID := chatJID.ToNonAD().String()
		isGroup := chatJID.Server == waTypes.GroupServer

		chatName := strings.TrimSpace(conv.GetName())
		if chatName == "" {
			chatName = strings.TrimSpace(conv.GetDisplayName())
		}
		if !isGroup && (chatName == "" || chatName == chatJID.ToNonAD().User) {
			if saved := resolveContactName(client, chatJID); saved != "" {
				chatName = saved
			}
		}
		if chatName == "" {
			chatName = chatJID.ToNonAD().User
		}

		convTS := int64(conv.GetConversationTimestamp())
		if convTS == 0 {
			convTS = int64(conv.GetLastMsgTimestamp())
		}

		// Always record the conversation itself so even chats without inline message bodies appear.
		h.store.UpsertChat(db.StoredChat{
			AccountID:     acct,
			ChatJID:       normJID,
			Name:          chatName,
			IsGroup:       isGroup,
			LastTimestamp: convTS,
			UnreadCount:   int(conv.GetUnreadCount()),
		})
		importedChats++

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
				senderJID = normJID
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
				ChatJID:     normJID,
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
			importedMsgs++
		}
	}

	if importedChats > 0 || importedMsgs > 0 {
		fmt.Printf("📥 [WEBCHAT] Imported %d chats & %d historical messages into sequential SQLite for %s\n", importedChats, importedMsgs, acct)
		h.Broadcast("accounts_updated", nil)
	}
}

// SyncAccount pulls all contacts and all joined groups from WhatsApp into the
// sequential ChatStore, purges any legacy @newsletter/@broadcast rows, and
// refreshes contact/group names across all chats.
func (h *Hub) SyncAccount(client *whatsmeow.Client) (contactsCount, groupsCount int) {
	if h == nil || h.store == nil || client == nil || client.Store == nil {
		return 0, 0
	}
	acct := accountKey(client)
	if acct == "" {
		return 0, 0
	}

	h.store.PurgeIgnoredChats()

	// 1. Sync all contacts from whatsmeow's ContactStore.
	if client.Store.Contacts != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		allContacts, err := client.Store.Contacts.GetAllContacts(ctx)
		cancel()
		if err == nil {
			now := time.Now().Unix()
			for jid, info := range allContacts {
				if jid.Server == waTypes.NewsletterServer || jid.Server == waTypes.BroadcastServer || jid.Server == waTypes.GroupServer {
					continue
				}
				jidStr := jid.ToNonAD().String()
				if db.IsIgnoredWebChatJID(jidStr) {
					continue
				}
				display := contactDisplayName(info, jid.ToNonAD().User)
				h.store.UpsertContact(db.StoredContact{
					AccountID:    acct,
					JID:          jidStr,
					Phone:        jid.ToNonAD().User,
					Name:         display,
					FullName:     strings.TrimSpace(info.FullName),
					PushName:     strings.TrimSpace(info.PushName),
					BusinessName: strings.TrimSpace(info.BusinessName),
					UpdatedAt:    now,
				})
				contactsCount++
			}
		}
	}

	// 2. Sync all joined groups so every group appears in the chat list with its real subject.
	if client.IsConnected() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		groups, err := client.GetJoinedGroups(ctx)
		cancel()
		if err == nil {
			for _, g := range groups {
				if g == nil || g.JID.IsEmpty() || g.JID.Server != waTypes.GroupServer {
					continue
				}
				h.store.UpsertChat(db.StoredChat{
					AccountID: acct,
					ChatJID:   g.JID.ToNonAD().String(),
					Name:      strings.TrimSpace(g.Name),
					IsGroup:   true,
				})
				groupsCount++
			}
		}
	}

	h.store.Flush()
	h.Broadcast("accounts_updated", nil)
	fmt.Printf("🔄 [WEBCHAT] Synced %d contacts & %d groups for %s\n", contactsCount, groupsCount, acct)
	return contactsCount, groupsCount
}

// FetchAvatarBytes returns JPEG bytes for a contact or group profile photo,
// using a 6-hour disk cache under <mediaRoot>/avatars and a 30-min negative cache.
func (h *Hub) FetchAvatarBytes(ctx context.Context, client *whatsmeow.Client, acct string, targetJID waTypes.JID) ([]byte, error) {
	if h == nil || h.store == nil || client == nil || !client.IsConnected() {
		return nil, fmt.Errorf("client unavailable")
	}
	cleanJID := targetJID.ToNonAD()
	key := acct + "|" + cleanJID.String()

	replacer := strings.NewReplacer("/", "_", "\\", "_", "..", "_", ":", "_", "@", "_", " ", "_")
	cacheDir := filepath.Join(h.store.MediaRoot(), "avatars", replacer.Replace(acct))
	cachePath := filepath.Join(cacheDir, replacer.Replace(cleanJID.String())+".jpg")

	if st, err := os.Stat(cachePath); err == nil && time.Since(st.ModTime()) < 6*time.Hour && st.Size() > 0 {
		if b, err := os.ReadFile(cachePath); err == nil && len(b) > 0 {
			return b, nil
		}
	}

	h.avatarMu.Lock()
	if exp, missed := h.avatarMiss[key]; missed && time.Now().Before(exp) {
		h.avatarMu.Unlock()
		return nil, fmt.Errorf("no avatar")
	}
	h.avatarMu.Unlock()

	picInfo, err := client.GetProfilePictureInfo(ctx, cleanJID, &whatsmeow.GetProfilePictureParams{Preview: true})
	if err != nil || picInfo == nil || strings.TrimSpace(picInfo.URL) == "" {
		h.avatarMu.Lock()
		h.avatarMiss[key] = time.Now().Add(30 * time.Minute)
		h.avatarMu.Unlock()
		return nil, fmt.Errorf("avatar unavailable")
	}

	data, _, err := utils.DownloadHTTPBytes(ctx, picInfo.URL, nil)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("avatar download failed")
	}

	_ = os.MkdirAll(cacheDir, 0755)
	_ = os.WriteFile(cachePath, data, 0644)
	return data, nil
}

func writeJSON(w interface{ Write([]byte) (int, error) }, v any) {
	_ = json.NewEncoder(w).Encode(v)
}
