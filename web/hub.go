package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
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

	lidMu      sync.Mutex
	mergedLIDs map[string]string

	groupMu    sync.Mutex
	groupNames map[string]groupNameEntry

	mediaFetchMu      sync.Mutex
	mediaInFlight     map[string]chan struct{}
	mediaRetryWaiters map[string]chan *events.MediaRetry
}

type groupNameEntry struct {
	name    string
	expires time.Time
}

func NewHub(chatStore *db.ChatStore, sessionDB *sql.DB, accounts AccountManager) *Hub {
	return &Hub{
		store:             chatStore,
		sessionDB:         sessionDB,
		accounts:          accounts,
		startedAt:         time.Now(),
		auth:              newWebAuth(),
		subscribers:       make(map[chan SSEEvent]struct{}),
		qrStatus:          "idle",
		avatarMiss:        make(map[string]time.Time),
		mergedLIDs:        make(map[string]string),
		groupNames:        make(map[string]groupNameEntry),
		mediaInFlight:     make(map[string]chan struct{}),
		mediaRetryWaiters: make(map[string]chan *events.MediaRetry),
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
	case core.InstantImageMessage != nil:
		return "image", "", core.InstantImageMessage.GetCaption(), core.InstantImageMessage.GetMimetype()
	case core.VideoMessage != nil:
		return "video", "", core.VideoMessage.GetCaption(), core.VideoMessage.GetMimetype()
	case core.PtvMessage != nil:
		return "video", "", core.PtvMessage.GetCaption(), core.PtvMessage.GetMimetype()
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
	case core.Conversation != nil && strings.TrimSpace(core.GetConversation()) != "":
		return "text", core.GetConversation(), "", ""
	case core.ExtendedTextMessage != nil && strings.TrimSpace(core.ExtendedTextMessage.GetText()) != "":
		return "text", core.ExtendedTextMessage.GetText(), "", ""
	case core.LocationMessage != nil:
		loc := core.LocationMessage
		label := strings.TrimSpace(loc.GetName())
		if addr := strings.TrimSpace(loc.GetAddress()); addr != "" {
			if label != "" {
				label += " — " + addr
			} else {
				label = addr
			}
		}
		if label == "" {
			label = fmt.Sprintf("%.5f, %.5f", loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
		}
		return "text", "📍 Location: " + label, "", ""
	case core.LiveLocationMessage != nil:
		loc := core.LiveLocationMessage
		cap := strings.TrimSpace(loc.GetCaption())
		if cap == "" {
			cap = fmt.Sprintf("%.5f, %.5f", loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
		}
		return "text", "📍 Live Location: " + cap, "", ""
	case core.ContactMessage != nil:
		name := strings.TrimSpace(core.ContactMessage.GetDisplayName())
		if name == "" {
			name = "Contact"
		}
		return "text", "👤 Contact: " + name, "", ""
	case core.ContactsArrayMessage != nil:
		name := strings.TrimSpace(core.ContactsArrayMessage.GetDisplayName())
		if name == "" {
			name = fmt.Sprintf("%d contacts", len(core.ContactsArrayMessage.GetContacts()))
		}
		return "text", "👥 Contacts: " + name, "", ""
	case core.PollCreationMessage != nil:
		return "text", "📊 Poll: " + core.PollCreationMessage.GetName(), "", ""
	case core.PollCreationMessageV2 != nil:
		return "text", "📊 Poll: " + core.PollCreationMessageV2.GetName(), "", ""
	case core.PollCreationMessageV3 != nil:
		return "text", "📊 Poll: " + core.PollCreationMessageV3.GetName(), "", ""
	case core.PollCreationMessageV5 != nil:
		return "text", "📊 Poll: " + core.PollCreationMessageV5.GetName(), "", ""
	case core.PollCreationMessageV6 != nil:
		return "text", "📊 Poll: " + core.PollCreationMessageV6.GetName(), "", ""
	case core.EventMessage != nil:
		name := strings.TrimSpace(core.EventMessage.GetName())
		if name == "" {
			name = "Event"
		}
		return "text", "📅 Event: " + name, "", ""
	case core.AlbumMessage != nil:
		total := core.AlbumMessage.GetExpectedImageCount() + core.AlbumMessage.GetExpectedVideoCount()
		if total > 0 {
			return "text", fmt.Sprintf("🖼️ Album (%d items)", total), "", ""
		}
		return "text", "🖼️ Album", "", ""
	case core.ButtonsMessage != nil:
		txt := strings.TrimSpace(core.ButtonsMessage.GetContentText())
		if txt == "" {
			txt = strings.TrimSpace(core.ButtonsMessage.GetText())
		}
		return "text", txt, "", ""
	case core.ListMessage != nil:
		txt := strings.TrimSpace(core.ListMessage.GetTitle())
		if desc := strings.TrimSpace(core.ListMessage.GetDescription()); desc != "" {
			if txt != "" {
				txt += "\n" + desc
			} else {
				txt = desc
			}
		}
		return "text", txt, "", ""
	case core.InteractiveMessage != nil:
		txt := strings.TrimSpace(core.InteractiveMessage.GetBody().GetText())
		if txt == "" {
			txt = strings.TrimSpace(core.InteractiveMessage.GetHeader().GetTitle())
		}
		return "text", txt, "", ""
	case core.OrderMessage != nil:
		txt := strings.TrimSpace(core.OrderMessage.GetMessage())
		if txt == "" {
			txt = fmt.Sprintf("🛒 Order (%d items)", core.OrderMessage.GetItemCount())
		}
		return "text", txt, "", ""
	case core.ReactionMessage != nil:
		return "text", "", "", ""
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

// resolveCanonicalChatJID maps DM @lid JIDs to their canonical @s.whatsapp.net
// phone-number JID whenever available (via SenderAlt/RecipientAlt, HistorySync
// LID mappings, or whatsmeow's LIDStore), preventing DM threads from splitting
// across separate @lid and @s.whatsapp.net conversations in the Web UI.
func resolveCanonicalChatJID(
	client *whatsmeow.Client,
	chat, senderAlt, recipientAlt waTypes.JID,
	isGroup, isFromMe bool,
	extraLIDMap map[string]string,
) waTypes.JID {
	clean := chat.ToNonAD()
	if clean.Server == waTypes.HostedLIDServer {
		clean.Server = waTypes.HiddenUserServer
	} else if clean.Server == waTypes.HostedServer {
		clean.Server = waTypes.DefaultUserServer
	}
	if isGroup || clean.Server != waTypes.HiddenUserServer || clean.User == "" {
		return clean
	}

	// 1. Check live event alt JIDs first (most accurate for incoming/outgoing DMs).
	var candidates []waTypes.JID
	if isFromMe {
		candidates = []waTypes.JID{recipientAlt.ToNonAD(), senderAlt.ToNonAD()}
	} else {
		candidates = []waTypes.JID{senderAlt.ToNonAD(), recipientAlt.ToNonAD()}
	}
	for _, cand := range candidates {
		if cand.Server == waTypes.DefaultUserServer && cand.User != "" {
			return cand
		}
	}

	// 2. Check optional HistorySync PhoneNumberToLidMappings map.
	if len(extraLIDMap) > 0 {
		for _, key := range []string{clean.String(), clean.User} {
			if pnStr := strings.TrimSpace(extraLIDMap[key]); pnStr != "" {
				if parsed, err := waTypes.ParseJID(pnStr); err == nil && parsed.User != "" {
					if parsed.Server == "" {
						parsed.Server = waTypes.DefaultUserServer
					}
					return parsed.ToNonAD()
				}
			}
		}
	}

	// 3. Fall back to whatsmeow's persistent LID -> PN store.
	if client != nil && client.Store != nil && client.Store.LIDs != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pn, err := client.Store.LIDs.GetPNForLID(ctx, clean)
		cancel()
		if err == nil && pn.User != "" {
			if pn.Server == "" {
				pn.Server = waTypes.DefaultUserServer
			}
			return pn.ToNonAD()
		}
	}

	return clean
}

// maybeMergeLID merges any previously split @lid chat/messages into canonicalJID
// at most once per (account, lidJID) mapping during the process lifetime.
func (h *Hub) maybeMergeLID(acct, rawLID, canonicalJID string) {
	if h == nil || h.store == nil || acct == "" || rawLID == "" || canonicalJID == "" || rawLID == canonicalJID {
		return
	}
	if !strings.HasSuffix(strings.ToLower(rawLID), "@lid") {
		return
	}
	key := acct + "|" + rawLID
	h.lidMu.Lock()
	if h.mergedLIDs[key] == canonicalJID {
		h.lidMu.Unlock()
		return
	}
	h.mergedLIDs[key] = canonicalJID
	h.lidMu.Unlock()

	h.store.MergeChatJID(acct, rawLID, canonicalJID)
}

func (h *Hub) cacheGroupName(jid, name string) {
	if h == nil || jid == "" || strings.TrimSpace(name) == "" {
		return
	}
	h.groupMu.Lock()
	h.groupNames[jid] = groupNameEntry{
		name:    strings.TrimSpace(name),
		expires: time.Now().Add(15 * time.Minute),
	}
	h.groupMu.Unlock()
}

func (h *Hub) resolveWebChatName(client *whatsmeow.Client, info waTypes.MessageInfo, canonicalChat waTypes.JID) string {
	if info.IsGroup {
		key := canonicalChat.String()
		h.groupMu.Lock()
		entry, ok := h.groupNames[key]
		h.groupMu.Unlock()
		if ok && time.Now().Before(entry.expires) && entry.name != "" {
			return entry.name
		}
		if client != nil && client.IsConnected() {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if gi, err := client.GetGroupInfo(ctx, canonicalChat); err == nil && gi != nil && gi.Name != "" {
				h.cacheGroupName(key, gi.Name)
				return gi.Name
			}
		}
	}
	if !info.IsGroup {
		if saved := resolveContactName(client, canonicalChat); saved != "" {
			return saved
		}
		if canonicalChat != info.Chat.ToNonAD() {
			if saved := resolveContactName(client, info.Chat); saved != "" {
				return saved
			}
		}
		if !info.IsFromMe && strings.TrimSpace(info.PushName) != "" {
			return strings.TrimSpace(info.PushName)
		}
		if pn := utils.ResolvePhoneNumber(client, canonicalChat, info.RecipientAlt); pn != "" {
			return pn
		}
	}
	return canonicalChat.ToNonAD().User
}

// IngestMessage records live incoming/outgoing messages (including view-once
// media, sound stickers, edits, and revokes) into the sequential ChatStore and
// pushes real-time updates to connected WhatsApp Web tabs.
func (h *Hub) IngestMessage(client *whatsmeow.Client, msg *events.Message) {
	if h == nil || h.store == nil || msg == nil || msg.Message == nil {
		return
	}
	acct := accountKey(client)
	if acct == "" {
		return
	}

	rawChatJID := msg.Info.Chat
	if msg.Info.IsFromMe && !msg.Info.IsGroup && msg.Info.DeviceSentMeta != nil && strings.TrimSpace(msg.Info.DeviceSentMeta.DestinationJID) != "" {
		if rawChatJID.User == acct || rawChatJID.IsEmpty() {
			if dest, err := waTypes.ParseJID(strings.TrimSpace(msg.Info.DeviceSentMeta.DestinationJID)); err == nil && !dest.IsEmpty() {
				rawChatJID = dest
			}
		}
	}

	// Skip WhatsApp Channels (@newsletter) and Status/Broadcast lists (@broadcast).
	if rawChatJID.Server == waTypes.BroadcastServer ||
		rawChatJID.Server == waTypes.NewsletterServer ||
		db.IsIgnoredWebChatJID(rawChatJID.String()) {
		return
	}

	canonicalChat := resolveCanonicalChatJID(
		client,
		rawChatJID,
		msg.Info.SenderAlt,
		msg.Info.RecipientAlt,
		msg.Info.IsGroup,
		msg.Info.IsFromMe,
		nil,
	)
	rawChatStr := rawChatJID.ToNonAD().String()
	chatJIDStr := canonicalChat.String()
	if rawChatStr != chatJIDStr {
		h.maybeMergeLID(acct, rawChatStr, chatJIDStr)
	}

	core := utils.UnwrapMessage(msg.Message)
	if core == nil {
		return
	}

	// Handle revocations and edits BEFORE content classification.
	if pm := core.GetProtocolMessage(); pm != nil {
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			targetID := pm.GetKey().GetID()
			if targetID == "" {
				targetID = msg.Info.MsgMetaInfo.TargetID
			}
			if targetID != "" {
				h.store.MarkDeleted(acct, targetID)
				h.store.Flush()
				h.Broadcast("message_deleted", map[string]any{
					"accountId": acct,
					"chatJid":   chatJIDStr,
					"msgId":     targetID,
				})
			}
			return

		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			targetID := pm.GetKey().GetID()
			if targetID == "" {
				targetID = msg.Info.MsgMetaInfo.TargetID
			}
			if editedCore := utils.UnwrapMessage(pm.GetEditedMessage()); editedCore != nil && targetID != "" {
				_, newBody, newCaption, _ := classifyMessageForWeb(editedCore)
				if strings.TrimSpace(newBody) != "" || strings.TrimSpace(newCaption) != "" {
					h.store.UpdateMessageBody(acct, targetID, newBody, newCaption)
					h.store.Flush()
					h.Broadcast("message_edited", map[string]any{
						"accountId": acct,
						"chatJid":   chatJIDStr,
						"msgId":     targetID,
						"body":      newBody,
						"caption":   newCaption,
					})
				}
			}
			return
		}
	}

	// Classify actual content FIRST — do NOT reject messages merely because
	// SenderKeyDistributionMessage or ProtocolMessage is bundled alongside real
	// group/DM content (WhatsApp attaches SenderKeyDistributionMessage to normal
	// group messages on first send and after every key rotation!).
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
		} else if msg.Info.SenderAlt.User != "" {
			if saved := resolveContactName(client, msg.Info.SenderAlt); saved != "" {
				senderName = saved
			}
		}
		if senderName == "" {
			senderName = utils.ResolvePhoneNumber(client, msg.Info.Sender, msg.Info.SenderAlt)
		}
	}

	ts := db.NormalizeUnixSeconds(msg.Info.Timestamp.Unix())
	if ts <= 0 {
		ts = time.Now().Unix()
	}

	var mediaProto []byte
	var thumbPath string
	if msgType != "text" {
		mediaProto = marshalMediaProto(core)
		thumbPath = h.saveMediaThumb(acct, msg.Info.ID, core)
	}

	stored := db.StoredMessage{
		AccountID:   acct,
		MsgID:       msg.Info.ID,
		ChatJID:     chatJIDStr,
		SenderJID:   msg.Info.Sender.ToNonAD().String(),
		SenderName:  senderName,
		IsFromMe:    msg.Info.IsFromMe,
		MsgType:     msgType,
		Body:        body,
		Caption:     caption,
		IsViewOnce:  isVO,
		MediaMime:   mimeType,
		ThumbPath:   thumbPath,
		HasMedia:    thumbPath != "" || len(mediaProto) > 0,
		MediaProto:  mediaProto,
		Timestamp:   ts,
		ChatName:    h.resolveWebChatName(client, msg.Info, canonicalChat),
		ChatIsGroup: msg.Info.IsGroup,
	}

	h.store.RecordMessage(stored)
	h.store.Flush()
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
	h.store.Flush()
	h.Broadcast("media_ready", map[string]any{
		"accountId": acct,
		"chatJid":   chatJID,
		"msgId":     msgID,
		"mediaPath": mediaRel,
		"audioPath": audioRel,
		"mediaMime": mimeType,
	})
}

func extractMediaThumbnail(core *waE2E.Message) []byte {
	if core == nil {
		return nil
	}
	switch {
	case core.ImageMessage != nil && len(core.ImageMessage.GetJPEGThumbnail()) > 0:
		return core.ImageMessage.GetJPEGThumbnail()
	case core.InstantImageMessage != nil && len(core.InstantImageMessage.GetJPEGThumbnail()) > 0:
		return core.InstantImageMessage.GetJPEGThumbnail()
	case core.VideoMessage != nil && len(core.VideoMessage.GetJPEGThumbnail()) > 0:
		return core.VideoMessage.GetJPEGThumbnail()
	case core.PtvMessage != nil && len(core.PtvMessage.GetJPEGThumbnail()) > 0:
		return core.PtvMessage.GetJPEGThumbnail()
	case core.DocumentMessage != nil && len(core.DocumentMessage.GetJPEGThumbnail()) > 0:
		return core.DocumentMessage.GetJPEGThumbnail()
	default:
		return nil
	}
}

func extractMediaKey(core *waE2E.Message) []byte {
	if core == nil {
		return nil
	}
	switch {
	case core.ImageMessage != nil:
		return core.ImageMessage.GetMediaKey()
	case core.InstantImageMessage != nil:
		return core.InstantImageMessage.GetMediaKey()
	case core.VideoMessage != nil:
		return core.VideoMessage.GetMediaKey()
	case core.PtvMessage != nil:
		return core.PtvMessage.GetMediaKey()
	case core.AudioMessage != nil:
		return core.AudioMessage.GetMediaKey()
	case core.StickerMessage != nil:
		return core.StickerMessage.GetMediaKey()
	case core.DocumentMessage != nil:
		return core.DocumentMessage.GetMediaKey()
	default:
		return nil
	}
}

func updateMediaDirectPath(core *waE2E.Message, directPath string) {
	if core == nil || directPath == "" {
		return
	}
	switch {
	case core.ImageMessage != nil:
		core.ImageMessage.DirectPath = proto.String(directPath)
		core.ImageMessage.URL = nil
	case core.InstantImageMessage != nil:
		core.InstantImageMessage.DirectPath = proto.String(directPath)
		core.InstantImageMessage.URL = nil
	case core.VideoMessage != nil:
		core.VideoMessage.DirectPath = proto.String(directPath)
		core.VideoMessage.URL = nil
	case core.PtvMessage != nil:
		core.PtvMessage.DirectPath = proto.String(directPath)
		core.PtvMessage.URL = nil
	case core.AudioMessage != nil:
		core.AudioMessage.DirectPath = proto.String(directPath)
		core.AudioMessage.URL = nil
	case core.StickerMessage != nil:
		core.StickerMessage.DirectPath = proto.String(directPath)
		core.StickerMessage.URL = nil
	case core.DocumentMessage != nil:
		core.DocumentMessage.DirectPath = proto.String(directPath)
		core.DocumentMessage.URL = nil
	}
}

func marshalMediaProto(core *waE2E.Message) []byte {
	if core == nil || !utils.HasDownloadableMedia(core) {
		return nil
	}
	b, err := proto.Marshal(core)
	if err != nil {
		return nil
	}
	return b
}

func (h *Hub) saveMediaThumb(acct, msgID string, core *waE2E.Message) string {
	if h == nil || h.store == nil || acct == "" || msgID == "" {
		return ""
	}
	thumb := extractMediaThumbnail(core)
	if len(thumb) == 0 {
		return ""
	}
	rel, err := h.store.SaveMediaFile(acct, msgID, "_thumb.jpg", thumb)
	if err != nil {
		return ""
	}
	return rel
}

// HandleMediaRetry wakes any pending on-demand download waiting for WhatsApp's
// phone re-upload response (*events.MediaRetry) for expired historical media.
func (h *Hub) HandleMediaRetry(client *whatsmeow.Client, evt *events.MediaRetry) {
	if h == nil || evt == nil || evt.MessageID == "" {
		return
	}
	acct := accountKey(client)
	key := acct + "|" + evt.MessageID
	h.mediaFetchMu.Lock()
	ch, ok := h.mediaRetryWaiters[key]
	if ok {
		delete(h.mediaRetryWaiters, key)
	}
	h.mediaFetchMu.Unlock()
	if ok && ch != nil {
		select {
		case ch <- evt:
		default:
		}
	}
}

// DownloadMessageMediaOnDemand lazily downloads & decrypts an old/uncached media
// message from WhatsApp CDN when opened in the Web UI. If CDN returns 404/410,
// it requests a re-upload from the user's phone via SendMediaRetryReceipt, and
// falls back cleanly to the saved JPEG thumbnail if the phone cannot re-upload.
func (h *Hub) DownloadMessageMediaOnDemand(
	ctx context.Context,
	client *whatsmeow.Client,
	acct string,
	msg *db.StoredMessage,
	wantAudio bool,
) (relPath, mimeType string, isThumb bool, err error) {
	if h == nil || h.store == nil || msg == nil {
		return "", "", false, fmt.Errorf("missing message")
	}

	// Singleflight dedup per (account, msgID) so concurrent requests share one download.
	flightKey := acct + "|" + msg.MsgID
	h.mediaFetchMu.Lock()
	if waitCh, busy := h.mediaInFlight[flightKey]; busy {
		h.mediaFetchMu.Unlock()
		select {
		case <-waitCh:
		case <-ctx.Done():
			return "", "", false, ctx.Err()
		}
		refreshed, rErr := h.store.GetMessage(acct, msg.MsgID)
		if rErr == nil && refreshed != nil {
			if wantAudio && refreshed.AudioPath != "" {
				return refreshed.AudioPath, "audio/ogg; codecs=opus", false, nil
			}
			if !wantAudio && refreshed.MediaPath != "" {
				return refreshed.MediaPath, refreshed.MediaMime, false, nil
			}
			if !wantAudio && refreshed.ThumbPath != "" {
				return refreshed.ThumbPath, "image/jpeg", true, nil
			}
		}
		return "", "", false, fmt.Errorf("media unavailable")
	}
	doneCh := make(chan struct{})
	h.mediaInFlight[flightKey] = doneCh
	h.mediaFetchMu.Unlock()

	defer func() {
		h.mediaFetchMu.Lock()
		delete(h.mediaInFlight, flightKey)
		h.mediaFetchMu.Unlock()
		close(doneCh)
	}()

	if len(msg.MediaProto) == 0 || client == nil || !client.IsConnected() {
		if !wantAudio && msg.ThumbPath != "" {
			return msg.ThumbPath, "image/jpeg", true, nil
		}
		return "", "", false, fmt.Errorf("media not available for on-demand download")
	}

	var core waE2E.Message
	if uErr := proto.Unmarshal(msg.MediaProto, &core); uErr != nil {
		if !wantAudio && msg.ThumbPath != "" {
			return msg.ThumbPath, "image/jpeg", true, nil
		}
		return "", "", false, fmt.Errorf("decode media proto: %w", uErr)
	}

	dlCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	data, _, _, dlErr := utils.DownloadMedia(dlCtx, client, &core)
	if dlErr != nil && (errors.Is(dlErr, whatsmeow.ErrMediaDownloadFailedWith404) || errors.Is(dlErr, whatsmeow.ErrMediaDownloadFailedWith410)) {
		// Expired CDN URL (common for older history messages): ask phone to re-upload!
		if mediaKey := extractMediaKey(&core); len(mediaKey) > 0 {
			if parsedChat, pErr := waTypes.ParseJID(msg.ChatJID); pErr == nil {
				parsedSender, _ := waTypes.ParseJID(msg.SenderJID)
				if parsedSender.IsEmpty() {
					parsedSender = parsedChat
				}
				retryCh := make(chan *events.MediaRetry, 1)
				h.mediaFetchMu.Lock()
				h.mediaRetryWaiters[flightKey] = retryCh
				h.mediaFetchMu.Unlock()

				msgInfo := &waTypes.MessageInfo{
					MessageSource: waTypes.MessageSource{
						Chat:     parsedChat.ToNonAD(),
						Sender:   parsedSender.ToNonAD(),
						IsFromMe: msg.IsFromMe,
						IsGroup:  parsedChat.Server == waTypes.GroupServer,
					},
					ID:        msg.MsgID,
					Timestamp: time.Unix(msg.Timestamp, 0),
				}
				if sendErr := client.SendMediaRetryReceipt(dlCtx, msgInfo, mediaKey); sendErr == nil {
					select {
					case retryEvt := <-retryCh:
						if retryData, decErr := whatsmeow.DecryptMediaRetryNotification(retryEvt, mediaKey); decErr == nil &&
							retryData.GetResult() == waMmsRetry.MediaRetryNotification_SUCCESS &&
							retryData.GetDirectPath() != "" {
							updateMediaDirectPath(&core, retryData.GetDirectPath())
							data, _, _, dlErr = utils.DownloadMedia(dlCtx, client, &core)
						}
					case <-time.After(6 * time.Second):
					case <-dlCtx.Done():
					}
				}
				h.mediaFetchMu.Lock()
				delete(h.mediaRetryWaiters, flightKey)
				h.mediaFetchMu.Unlock()
			}
		}
	}

	if dlErr != nil || len(data) == 0 {
		if !wantAudio && msg.ThumbPath != "" {
			return msg.ThumbPath, "image/jpeg", true, nil
		}
		if dlErr != nil {
			return "", "", false, dlErr
		}
		return "", "", false, fmt.Errorf("empty media download")
	}

	mimeOut := msg.MediaMime
	ext := extensionForMime(mimeOut, ".bin")
	mediaRel, saveErr := h.store.SaveMediaFile(acct, msg.MsgID, ext, data)
	if saveErr != nil {
		return "", "", false, saveErr
	}

	var audioRel string
	if stk := core.GetStickerMessage(); stk != nil && stk.GetAudioMessage() != nil {
		if audBytes, aErr := client.Download(dlCtx, stk.GetAudioMessage()); aErr == nil && len(audBytes) > 0 {
			if rel, sErr := h.store.SaveMediaFile(acct, msg.MsgID, "_audio.ogg", audBytes); sErr == nil {
				audioRel = rel
			}
		}
	}

	h.store.UpdateMediaPaths(acct, msg.MsgID, mediaRel, audioRel, mimeOut)
	h.store.Flush()
	h.Broadcast("media_ready", map[string]any{
		"accountId": acct,
		"chatJid":   msg.ChatJID,
		"msgId":     msg.MsgID,
		"mediaPath": mediaRel,
		"audioPath": audioRel,
		"mediaMime": mimeOut,
	})

	if wantAudio {
		if audioRel != "" {
			return audioRel, "audio/ogg; codecs=opus", false, nil
		}
		return "", "", false, fmt.Errorf("audio track unavailable")
	}
	return mediaRel, mimeOut, false, nil
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

	// Index PhoneNumber <-> LID mappings and Pushnames included in this HistorySync chunk.
	lidToPN := make(map[string]string, len(evt.Data.GetPhoneNumberToLidMappings()))
	for _, m := range evt.Data.GetPhoneNumberToLidMappings() {
		lid := strings.TrimSpace(m.GetLidJID())
		pn := strings.TrimSpace(m.GetPnJID())
		if lid != "" && pn != "" {
			if !strings.Contains(pn, "@") {
				pn += "@" + waTypes.DefaultUserServer
			}
			lidToPN[lid] = pn
			if !strings.Contains(lid, "@") {
				lidToPN[lid+"@"+waTypes.HiddenUserServer] = pn
			} else {
				lidToPN[strings.Split(lid, "@")[0]] = pn
			}
		}
	}
	pushNames := make(map[string]string, len(evt.Data.GetPushnames()))
	for _, p := range evt.Data.GetPushnames() {
		id := strings.TrimSpace(p.GetID())
		name := strings.TrimSpace(p.GetPushname())
		if id != "" && name != "" {
			pushNames[id] = name
		}
	}

	importedMsgs := 0
	importedChats := 0
	var syncedChatJIDs []string

	for _, conv := range evt.Data.GetConversations() {
		rawID := strings.TrimSpace(conv.GetID())
		if rawID == "" || db.IsIgnoredWebChatJID(rawID) {
			continue
		}
		chatJIDStr := rawID
		lidAlias := strings.TrimSpace(conv.GetLidJID())
		if strings.HasSuffix(strings.ToLower(rawID), "@lid") && lidAlias == "" {
			lidAlias = rawID
		}
		// Prefer phone-number JID over duplicate LID thread when WhatsApp supplies PnJID.
		if pn := strings.TrimSpace(conv.GetPnJID()); pn != "" && !db.IsIgnoredWebChatJID(pn) {
			chatJIDStr = pn
		}
		chatJID, err := waTypes.ParseJID(chatJIDStr)
		if err != nil || chatJID.Server == waTypes.BroadcastServer || chatJID.Server == waTypes.NewsletterServer {
			continue
		}
		isGroup := chatJID.Server == waTypes.GroupServer
		canonicalChat := resolveCanonicalChatJID(client, chatJID, waTypes.EmptyJID, waTypes.EmptyJID, isGroup, false, lidToPN)
		normJID := canonicalChat.String()

		if lidAlias != "" {
			if parsedLID, lErr := waTypes.ParseJID(lidAlias); lErr == nil {
				lidNorm := parsedLID.ToNonAD().String()
				if lidNorm != normJID {
					h.maybeMergeLID(acct, lidNorm, normJID)
				}
			}
		}

		chatName := strings.TrimSpace(conv.GetName())
		if chatName == "" {
			chatName = strings.TrimSpace(conv.GetDisplayName())
		}
		if !isGroup && (chatName == "" || chatName == canonicalChat.User) {
			if saved := resolveContactName(client, canonicalChat); saved != "" {
				chatName = saved
			} else if pnName := pushNames[normJID]; pnName != "" {
				chatName = pnName
			} else if lidAlias != "" && pushNames[lidAlias] != "" {
				chatName = pushNames[lidAlias]
			}
		}
		if chatName == "" {
			chatName = canonicalChat.User
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
		syncedChatJIDs = append(syncedChatJIDs, normJID)

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
			if core == nil {
				continue
			}
			if pm := core.GetProtocolMessage(); pm != nil {
				if pm.GetType() == waE2E.ProtocolMessage_REVOKE {
					if targetID := pm.GetKey().GetID(); targetID != "" {
						h.store.MarkDeleted(acct, targetID)
					}
					continue
				}
				if pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
					if targetID := pm.GetKey().GetID(); targetID != "" {
						if editedCore := utils.UnwrapMessage(pm.GetEditedMessage()); editedCore != nil {
							_, nb, nc, _ := classifyMessageForWeb(editedCore)
							if strings.TrimSpace(nb) != "" || strings.TrimSpace(nc) != "" {
								h.store.UpdateMessageBody(acct, targetID, nb, nc)
							}
						}
					}
					continue
				}
			}

			msgType, body, caption, mimeType := classifyMessageForWeb(core)
			if msgType == "text" && strings.TrimSpace(body) == "" {
				continue
			}

			senderJID := key.GetParticipant()
			if senderJID == "" {
				senderJID = webMsg.GetParticipant()
			}
			if senderJID == "" {
				senderJID = normJID
			}
			senderName := strings.TrimSpace(webMsg.GetPushName())
			if key.GetFromMe() {
				senderName = "You"
			} else if senderName == "" {
				if pn := pushNames[senderJID]; pn != "" {
					senderName = pn
				} else {
					senderName = chatName
				}
			}

			var mediaProto []byte
			var thumbPath string
			if msgType != "text" {
				mediaProto = marshalMediaProto(core)
				thumbPath = h.saveMediaThumb(acct, key.GetID(), core)
			}

			h.store.RecordMessage(db.StoredMessage{
				AccountID:           acct,
				MsgID:               key.GetID(),
				ChatJID:             normJID,
				SenderJID:           senderJID,
				SenderName:          senderName,
				IsFromMe:            key.GetFromMe(),
				MsgType:             msgType,
				Body:                body,
				Caption:             caption,
				IsViewOnce:          utils.IsViewOnceMessage(nil, raw),
				MediaMime:           mimeType,
				ThumbPath:           thumbPath,
				HasMedia:            thumbPath != "" || len(mediaProto) > 0,
				MediaProto:          mediaProto,
				Timestamp:           db.NormalizeUnixSeconds(int64(webMsg.GetMessageTimestamp())),
				ChatName:            chatName,
				ChatIsGroup:         isGroup,
				SkipUnreadIncrement: true,
			})
			importedMsgs++
		}
	}

	if importedChats > 0 || importedMsgs > 0 {
		h.store.RepairChatSummaries(acct)
		h.store.Flush()
		fmt.Printf("📥 [WEBCHAT] Imported %d chats & %d historical messages (%s) into sequential SQLite for %s\n",
			importedChats, importedMsgs, evt.Data.GetSyncType().String(), acct)
		h.Broadcast("history_synced", map[string]any{
			"accountId": acct,
			"chats":     syncedChatJIDs,
			"count":     importedMsgs,
		})
		h.Broadcast("accounts_updated", nil)
	}
}

// HandleOfflineSyncCompleted flushes buffered offline messages and nudges open
// browser tabs to refresh so messages received during reconnect appear immediately.
func (h *Hub) HandleOfflineSyncCompleted(client *whatsmeow.Client) {
	if h == nil || h.store == nil {
		return
	}
	acct := accountKey(client)
	if acct == "" {
		return
	}
	h.store.Flush()
	h.Broadcast("history_synced", map[string]any{
		"accountId": acct,
	})
}

// HandleJoinedGroup immediately adds newly joined groups to the chat list.
func (h *Hub) HandleJoinedGroup(client *whatsmeow.Client, evt *events.JoinedGroup) {
	if h == nil || h.store == nil || evt == nil || evt.JID.IsEmpty() {
		return
	}
	acct := accountKey(client)
	if acct == "" {
		return
	}
	jidStr := evt.JID.ToNonAD().String()
	name := strings.TrimSpace(evt.Name)
	h.cacheGroupName(jidStr, name)
	h.store.UpsertChat(db.StoredChat{
		AccountID:     acct,
		ChatJID:       jidStr,
		Name:          name,
		IsGroup:       true,
		LastTimestamp: time.Now().Unix(),
	})
	h.store.Flush()
	h.Broadcast("accounts_updated", nil)
}

// HandleMarkChatAsRead syncs read-state changes made on the user's phone.
func (h *Hub) HandleMarkChatAsRead(client *whatsmeow.Client, evt *events.MarkChatAsRead) {
	if h == nil || h.store == nil || evt == nil || evt.Action == nil || !evt.Action.GetRead() {
		return
	}
	acct := accountKey(client)
	if acct == "" {
		return
	}
	canonical := resolveCanonicalChatJID(client, evt.JID, waTypes.EmptyJID, waTypes.EmptyJID, evt.JID.Server == waTypes.GroupServer, false, nil)
	h.store.MarkChatRead(acct, canonical.String())
	if canonical.String() != evt.JID.ToNonAD().String() {
		h.store.MarkChatRead(acct, evt.JID.ToNonAD().String())
	}
}

// HandleContactUpdate refreshes contact & DM chat names as PushName/Contact events arrive.
func (h *Hub) HandleContactUpdate(client *whatsmeow.Client, jid waTypes.JID, fullName, pushName string) {
	if h == nil || h.store == nil || jid.IsEmpty() {
		return
	}
	acct := accountKey(client)
	if acct == "" {
		return
	}
	if jid.Server == waTypes.NewsletterServer || jid.Server == waTypes.BroadcastServer || jid.Server == waTypes.GroupServer {
		return
	}
	canonical := resolveCanonicalChatJID(client, jid, waTypes.EmptyJID, waTypes.EmptyJID, false, false, nil)
	if db.IsIgnoredWebChatJID(canonical.String()) {
		return
	}
	name := strings.TrimSpace(fullName)
	if name == "" {
		name = strings.TrimSpace(pushName)
	}
	if name == "" {
		return
	}
	h.store.UpsertContact(db.StoredContact{
		AccountID: acct,
		JID:       canonical.String(),
		Phone:     canonical.User,
		Name:      name,
		FullName:  strings.TrimSpace(fullName),
		PushName:  strings.TrimSpace(pushName),
		UpdatedAt: time.Now().Unix(),
	})
}

// RequestChatHistory sends an ON_DEMAND history sync request to the user's
// primary phone for older messages in chatJID (returned asynchronously via *events.HistorySync).
func (h *Hub) RequestChatHistory(ctx context.Context, client *whatsmeow.Client, acct, chatJIDStr string, count int) error {
	if h == nil || h.store == nil || client == nil || !client.IsConnected() {
		return fmt.Errorf("WhatsApp account is not connected")
	}
	if count <= 0 || count > 100 {
		count = 50
	}
	oldest, err := h.store.GetOldestMessage(acct, chatJIDStr)
	if err != nil || oldest == nil {
		return fmt.Errorf("no existing anchor message in this chat yet")
	}
	parsedChat, err := waTypes.ParseJID(chatJIDStr)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}
	senderJID, _ := waTypes.ParseJID(oldest.SenderJID)
	if senderJID.IsEmpty() {
		senderJID = parsedChat
	}
	info := &waTypes.MessageInfo{
		MessageSource: waTypes.MessageSource{
			Chat:     parsedChat.ToNonAD(),
			Sender:   senderJID.ToNonAD(),
			IsFromMe: oldest.IsFromMe,
			IsGroup:  parsedChat.Server == waTypes.GroupServer,
		},
		ID:        oldest.MsgID,
		Timestamp: time.Unix(oldest.Timestamp, 0),
	}
	reqMsg := client.BuildHistorySyncRequest(info, count)
	_, err = client.SendPeerMessage(ctx, reqMsg)
	return err
}

// SyncAccount pulls all contacts and all joined groups from WhatsApp into the
// sequential ChatStore, merges any split @lid chats into their canonical
// @s.whatsapp.net threads, purges any legacy @newsletter/@broadcast rows, and
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

	// 0. Merge any previously split @lid chats into their canonical @s.whatsapp.net JIDs.
	if lidJIDs, err := h.store.ListLIDChatJIDs(acct); err == nil {
		merged := 0
		for _, rawLID := range lidJIDs {
			parsed, pErr := waTypes.ParseJID(rawLID)
			if pErr != nil {
				continue
			}
			canonical := resolveCanonicalChatJID(client, parsed, waTypes.EmptyJID, waTypes.EmptyJID, false, false, nil)
			if canonical.String() != rawLID && canonical.Server == waTypes.DefaultUserServer {
				h.store.MergeChatJID(acct, rawLID, canonical.String())
				h.lidMu.Lock()
				h.mergedLIDs[acct+"|"+rawLID] = canonical.String()
				h.lidMu.Unlock()
				merged++
			}
		}
		if merged > 0 {
			fmt.Printf("🔗 [WEBCHAT] Merged %d split @lid chats into canonical phone threads for %s\n", merged, acct)
		}
	}

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
				canonical := resolveCanonicalChatJID(client, jid, waTypes.EmptyJID, waTypes.EmptyJID, false, false, nil)
				jidStr := canonical.String()
				if db.IsIgnoredWebChatJID(jidStr) {
					continue
				}
				display := contactDisplayName(info, canonical.User)
				h.store.UpsertContact(db.StoredContact{
					AccountID:    acct,
					JID:          jidStr,
					Phone:        canonical.User,
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
				jidStr := g.JID.ToNonAD().String()
				name := strings.TrimSpace(g.Name)
				h.cacheGroupName(jidStr, name)
				h.store.UpsertChat(db.StoredChat{
					AccountID: acct,
					ChatJID:   jidStr,
					Name:      name,
					IsGroup:   true,
				})
				groupsCount++
			}
		}
	}

	h.store.RepairChatSummaries(acct)
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
