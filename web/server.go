package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	db "bot/databases"
	"bot/pairing"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"rsc.io/qr"
)

//go:embed static/index.html
var indexHTML []byte

//go:embed static/pair.html
var pairHTML []byte

//go:embed static/games.html
var gamesHTML []byte

const webHTMLCSP = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

func withSecurityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		next(w, r)
	}
}

// Mount registers the WhatsApp Web UI, Owner Auth, Sequential QR Pairing Queue, Arcade Games, and REST/SSE API onto mux.
func (h *Hub) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/", withSecurityHeaders(h.handleIndex))
	mux.HandleFunc("/pair/", withSecurityHeaders(h.handlePairPage))
	mux.HandleFunc("/games", withSecurityHeaders(h.handleGamesPage))
	mux.HandleFunc("/games/", withSecurityHeaders(h.handleGamesPage))

	// Owner-Only Authentication & WhatsApp 6-digit Verification endpoints
	mux.HandleFunc("/api/wa/auth/me", withSecurityHeaders(h.handleAuthMe))
	mux.HandleFunc("/api/wa/auth/request-code", withSecurityHeaders(h.handleAuthRequestCode))
	mux.HandleFunc("/api/wa/auth/verify", withSecurityHeaders(h.handleAuthVerify))
	mux.HandleFunc("/api/wa/auth/logout", withSecurityHeaders(h.handleAuthLogout))

	mux.HandleFunc("/api/wa/status", withSecurityHeaders(h.handleStatus))
	mux.HandleFunc("/api/wa/accounts", withSecurityHeaders(h.requireAuth(h.handleAccounts)))
	mux.HandleFunc("/api/wa/accounts/add", withSecurityHeaders(h.requireAuth(h.handleAddAccount)))
	mux.HandleFunc("/api/wa/accounts/remove", withSecurityHeaders(h.requireAuth(h.handleRemoveAccount)))
	mux.HandleFunc("/api/wa/qr.png", withSecurityHeaders(h.requireAuth(h.handleQRPNG)))
	mux.HandleFunc("/api/wa/pair/", withSecurityHeaders(h.handlePublicPairAPI))
	mux.HandleFunc("/api/wa/pairing/tickets", withSecurityHeaders(h.requireAuth(h.handlePairingTickets)))
	mux.HandleFunc("/api/wa/pairing/queue", withSecurityHeaders(h.requireAuth(h.handlePairingQueue)))
	mux.HandleFunc("/api/wa/pairing/cancel", withSecurityHeaders(h.requireAuth(h.handlePairingCancel)))
	mux.HandleFunc("/api/wa/games/score", withSecurityHeaders(h.handleGameScore))
	mux.HandleFunc("/api/wa/events", withSecurityHeaders(h.requireAuth(h.handleSSE)))
	mux.HandleFunc("/api/wa/chats", withSecurityHeaders(h.requireAuth(h.handleChats)))
	mux.HandleFunc("/api/wa/chats/read", withSecurityHeaders(h.requireAuth(h.handleMarkRead)))
	mux.HandleFunc("/api/wa/chats/history", withSecurityHeaders(h.requireAuth(h.handleRequestHistory)))
	mux.HandleFunc("/api/wa/contacts", withSecurityHeaders(h.requireAuth(h.handleContacts)))
	mux.HandleFunc("/api/wa/avatar", withSecurityHeaders(h.requireAuth(h.handleAvatar)))
	mux.HandleFunc("/api/wa/sync", withSecurityHeaders(h.requireAuth(h.handleSync)))
	mux.HandleFunc("/api/wa/messages", withSecurityHeaders(h.requireAuth(h.handleMessages)))
	mux.HandleFunc("/api/wa/media/", withSecurityHeaders(h.requireAuth(h.handleMedia)))
	mux.HandleFunc("/api/wa/send/text", withSecurityHeaders(h.requireAuth(h.handleSendText)))
	mux.HandleFunc("/api/wa/send/media", withSecurityHeaders(h.requireAuth(h.handleSendMedia)))
}

func (h *Hub) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if !checkSameOrigin(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			writeJSON(w, map[string]string{"error": "forbidden cross-origin request"})
			return
		}
		if !h.isAuthenticatedRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]string{"error": "unauthorized — owner login & verification required"})
			return
		}
		next(w, r)
	}
}

func (h *Hub) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", webHTMLCSP)
	_, _ = w.Write(indexHTML)
}

func (h *Hub) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	authed := h.isAuthenticatedRequest(r)
	resp := map[string]any{
		"ok":            true,
		"authRequired":  true,
		"otpRequired":   webOTPRequired(),
		"authenticated": authed,
	}
	if authed {
		resp["platform"] = "Android Companion (View-Once Ready)"
		resp["uptimeSec"] = int64(time.Since(h.startedAt).Seconds())
		resp["sqlite"] = map[string]any{
			"session": db.SQLiteStatus(h.sessionDB),
			"webchat": h.store.Status(),
		}
	}
	writeJSON(w, resp)
}

func (h *Hub) handleAccounts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	code, status, updatedAt := h.CurrentQR()
	var list []AccountInfo
	if h.accounts != nil {
		list = h.accounts.ListAccounts()
	}
	if list == nil {
		list = []AccountInfo{}
	}
	writeJSON(w, map[string]any{
		"accounts": list,
		"qr": map[string]any{
			"available": code != "",
			"status":    status,
			"updatedAt": updatedAt,
		},
	})
}

func (h *Hub) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if h.accounts == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "account manager unavailable"})
		return
	}
	if err := h.accounts.StartPairing(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "pairing_started"})
}

func (h *Hub) handleRemoveAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		AccountID string `json:"accountId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AccountID) == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "accountId required"})
		return
	}
	if err := h.accounts.RemoveAccount(strings.TrimSpace(req.AccountID)); err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	h.Broadcast("accounts_updated", nil)
	writeJSON(w, map[string]string{"status": "removed"})
}

func (h *Hub) handleQRPNG(w http.ResponseWriter, r *http.Request) {
	code, _, _ := h.CurrentQR()
	if code == "" {
		http.Error(w, "no active QR code", http.StatusNotFound)
		return
	}
	encoded, err := qr.Encode(code, qr.L)
	if err != nil {
		http.Error(w, "qr encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(encoded.PNG())
}

func (h *Hub) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := h.Subscribe()
	defer unsub()

	// Send initial connected ping.
	fmt.Fprintf(w, "event: ready\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case evt, open := <-ch:
			if !open {
				return
			}
			payload, _ := json.Marshal(evt.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, payload)
			flusher.Flush()
		}
	}
}

func (h *Hub) handleChats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	acct := strings.TrimSpace(r.URL.Query().Get("account"))
	if acct == "" && h.accounts != nil {
		if list := h.accounts.ListAccounts(); len(list) > 0 {
			acct = list[0].User
		}
	}
	if acct == "" {
		writeJSON(w, map[string]any{"chats": []db.StoredChat{}})
		return
	}
	chats, err := h.store.ListChats(acct, r.URL.Query().Get("q"), r.URL.Query().Get("filter"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"chats": chats})
}

func (h *Hub) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AccountID string `json:"accountId"`
		ChatJID   string `json:"chatJid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	h.store.MarkChatRead(req.AccountID, req.ChatJID)
	h.store.Flush()
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *Hub) handleRequestHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		AccountID string `json:"accountId"`
		ChatJID   string `json:"chatJid"`
		Count     int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid json"})
		return
	}
	acct := strings.TrimSpace(req.AccountID)
	if acct == "" {
		acct = h.resolveActiveAccount(r)
	}
	chatJID := strings.TrimSpace(req.ChatJID)
	if acct == "" || chatJID == "" || h.accounts == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "accountId and chatJid required"})
		return
	}
	cli, ok := h.accounts.GetClient(acct)
	if !ok || cli == nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "account not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := h.RequestChatHistory(ctx, cli, acct, chatJID, req.Count); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "status": "requested"})
}

func (h *Hub) resolveActiveAccount(r *http.Request) string {
	acct := strings.TrimSpace(r.URL.Query().Get("account"))
	if acct == "" && h.accounts != nil {
		if list := h.accounts.ListAccounts(); len(list) > 0 {
			acct = list[0].User
		}
	}
	return acct
}

func (h *Hub) handleContacts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	acct := h.resolveActiveAccount(r)
	if acct == "" {
		writeJSON(w, map[string]any{"contacts": []db.StoredContact{}})
		return
	}
	q := r.URL.Query().Get("q")
	contacts, err := h.store.ListContacts(acct, q)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	// If no contacts are cached yet in SQLite, pull them live once from whatsmeow ContactStore.
	if len(contacts) == 0 && q == "" && h.accounts != nil {
		if cli, ok := h.accounts.GetClient(acct); ok && cli != nil {
			h.SyncAccount(cli)
			contacts, _ = h.store.ListContacts(acct, q)
		}
	}
	if contacts == nil {
		contacts = []db.StoredContact{}
	}
	writeJSON(w, map[string]any{"contacts": contacts})
}

func (h *Hub) handleAvatar(w http.ResponseWriter, r *http.Request) {
	acct := h.resolveActiveAccount(r)
	rawJID := strings.TrimSpace(r.URL.Query().Get("jid"))
	if acct == "" || rawJID == "" || db.IsIgnoredWebChatJID(rawJID) || h.accounts == nil {
		http.NotFound(w, r)
		return
	}
	cli, ok := h.accounts.GetClient(acct)
	if !ok || cli == nil {
		http.NotFound(w, r)
		return
	}
	targetJID, err := parseTargetJID(rawJID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	data, err := h.FetchAvatarBytes(ctx, cli, acct, targetJID)
	if err != nil || len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

func (h *Hub) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		AccountID string `json:"accountId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	acct := strings.TrimSpace(req.AccountID)
	if acct == "" {
		acct = h.resolveActiveAccount(r)
	}
	if acct == "" || h.accounts == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "no connected account to sync"})
		return
	}
	cli, ok := h.accounts.GetClient(acct)
	if !ok || cli == nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "account not connected"})
		return
	}
	contactsCount, groupsCount := h.SyncAccount(cli)
	chats, _ := h.store.ListChats(acct, "", "all")
	writeJSON(w, map[string]any{
		"ok":            true,
		"contactsCount": contactsCount,
		"groupsCount":   groupsCount,
		"chatsCount":    len(chats),
	})
}

func (h *Hub) handleMessages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	acct := strings.TrimSpace(r.URL.Query().Get("account"))
	chat := strings.TrimSpace(r.URL.Query().Get("chat"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if acct == "" || chat == "" {
		writeJSON(w, map[string]any{"messages": []db.StoredMessage{}})
		return
	}
	msgs, err := h.store.ListMessages(acct, chat, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []db.StoredMessage{}
	}
	writeJSON(w, map[string]any{"messages": msgs})
}

func isSafeInlineMediaMIME(rawMIME string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(rawMIME, ";")[0]))
	switch base {
	case "image/jpeg", "image/jpg", "image/png", "image/gif", "image/webp", "image/avif",
		"video/mp4", "video/webm", "video/quicktime",
		"audio/ogg", "audio/mpeg", "audio/mp4", "audio/webm", "audio/wav", "audio/aac",
		"application/pdf":
		return true
	default:
		return false
	}
}

func (h *Hub) handleMedia(w http.ResponseWriter, r *http.Request) {
	// URL format: /api/wa/media/{account}/{msgID}
	rel := strings.TrimPrefix(r.URL.Path, "/api/wa/media/")
	parts := strings.SplitN(rel, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	acct, msgID := parts[0], parts[1]

	msg, err := h.store.GetMessage(acct, msgID)
	if err != nil || msg == nil {
		http.NotFound(w, r)
		return
	}

	targetRel := msg.MediaPath
	contentType := msg.MediaMime
	if r.URL.Query().Get("track") == "audio" {
		targetRel = msg.AudioPath
		contentType = "audio/ogg; codecs=opus"
	}
	if targetRel == "" {
		http.NotFound(w, r)
		return
	}

	rootAbs, err := filepath.Abs(h.store.MediaRoot())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	fullPath, err := filepath.Abs(filepath.Join(rootAbs, targetRel))
	if err != nil || !strings.HasPrefix(fullPath, rootAbs+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Prevent stored XSS from malicious WhatsApp attachments (e.g. .html, .svg, .js):
	// only allow safe non-scriptable media inline; force everything else to download.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	ext := strings.ToLower(filepath.Ext(fullPath))
	dangerousExt := ext == ".html" || ext == ".htm" || ext == ".xhtml" || ext == ".svg" || ext == ".xml" || ext == ".js" || ext == ".mjs"
	if !dangerousExt && isSafeInlineMediaMIME(contentType) {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", utils.SanitizeFilename(filepath.Base(fullPath))))
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, fullPath)
}

func parseTargetJID(raw string) (waTypes.JID, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return waTypes.EmptyJID, fmt.Errorf("chatJid is required")
	}
	if strings.Contains(s, "@") {
		return waTypes.ParseJID(s)
	}
	digits := utils.NormalizeTarget(s)
	if digits == "" {
		return waTypes.EmptyJID, fmt.Errorf("invalid phone number")
	}
	return waTypes.NewJID(digits, waTypes.DefaultUserServer), nil
}

func (h *Hub) handleSendText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var req struct {
		AccountID string `json:"accountId"`
		ChatJID   string `json:"chatJid"`
		Text      string `json:"text"`
		QuoteID   string `json:"quoteId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid json"})
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "text is required"})
		return
	}

	client, ok := h.accounts.GetClient(req.AccountID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "account not connected"})
		return
	}
	targetJID, err := parseTargetJID(req.ChatJID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	var waMsg *waE2E.Message
	if req.QuoteID != "" {
		waMsg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(req.Text),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID: proto.String(req.QuoteID),
				},
			},
		}
	} else {
		waMsg = &waE2E.Message{Conversation: proto.String(req.Text)}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := client.SendMessage(ctx, targetJID, waMsg)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	acct := accountKey(client)
	canonicalTarget := resolveCanonicalChatJID(client, targetJID, waTypes.EmptyJID, waTypes.EmptyJID, targetJID.Server == waTypes.GroupServer, true, nil)
	stored := db.StoredMessage{
		AccountID:   acct,
		MsgID:       resp.ID,
		ChatJID:     canonicalTarget.String(),
		SenderJID:   client.Store.ID.ToNonAD().String(),
		SenderName:  "You",
		IsFromMe:    true,
		MsgType:     "text",
		Body:        req.Text,
		Timestamp:   time.Now().Unix(),
		ChatName:    canonicalTarget.User,
		ChatIsGroup: canonicalTarget.Server == waTypes.GroupServer,
	}
	h.store.RecordMessage(stored)
	h.store.Flush()
	h.Broadcast("message", stored)

	writeJSON(w, map[string]any{"ok": true, "id": resp.ID, "message": stored})
}

func (h *Hub) handleSendMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid multipart upload"})
		return
	}

	accountID := strings.TrimSpace(r.FormValue("accountId"))
	chatRaw := strings.TrimSpace(r.FormValue("chatJid"))
	caption := strings.TrimSpace(r.FormValue("caption"))
	viewOnce := r.FormValue("viewOnce") == "true" || r.FormValue("viewOnce") == "1"

	client, ok := h.accounts.GetClient(accountID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "account not connected"})
		return
	}
	targetJID, err := parseTargetJID(chatRaw)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "file field required"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil || len(data) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "empty or unreadable file"})
		return
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	mimeType = strings.TrimSpace(strings.Split(mimeType, ";")[0])

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var (
		sendResp whatsmeow.SendResponse
		msgType  string
	)

	switch {
	case strings.HasPrefix(mimeType, "image/"):
		msgType = "image"
		up, upErr := client.Upload(ctx, data, whatsmeow.MediaImage)
		if upErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": upErr.Error()})
			return
		}
		imgMsg := &waE2E.ImageMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(mimeType),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			Caption:       proto.String(caption),
			ViewOnce:      proto.Bool(viewOnce),
		}
		out := &waE2E.Message{ImageMessage: imgMsg}
		if viewOnce {
			out = &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: out}}
		}
		sendResp, err = client.SendMessage(ctx, targetJID, out)

	case strings.HasPrefix(mimeType, "video/"):
		msgType = "video"
		up, upErr := client.Upload(ctx, data, whatsmeow.MediaVideo)
		if upErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": upErr.Error()})
			return
		}
		vidMsg := &waE2E.VideoMessage{
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(mimeType),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			Caption:       proto.String(caption),
			ViewOnce:      proto.Bool(viewOnce),
		}
		out := &waE2E.Message{VideoMessage: vidMsg}
		if viewOnce {
			out = &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: out}}
		}
		sendResp, err = client.SendMessage(ctx, targetJID, out)

	case strings.HasPrefix(mimeType, "audio/"):
		msgType = "audio"
		sendResp, err = utils.SendAudioData(ctx, client, targetJID, data, mimeType, 0, false)

	default:
		msgType = "document"
		fileName := utils.SanitizeFilename(header.Filename)
		sendResp, err = utils.SendDocumentData(ctx, client, targetJID, data, mimeType, fileName, caption)
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	acct := accountKey(client)
	canonicalTarget := resolveCanonicalChatJID(client, targetJID, waTypes.EmptyJID, waTypes.EmptyJID, targetJID.Server == waTypes.GroupServer, true, nil)
	mediaRel, _ := h.store.SaveMediaFile(acct, sendResp.ID, extensionForMime(mimeType, ".bin"), data)
	stored := db.StoredMessage{
		AccountID:   acct,
		MsgID:       sendResp.ID,
		ChatJID:     canonicalTarget.String(),
		SenderJID:   client.Store.ID.ToNonAD().String(),
		SenderName:  "You",
		IsFromMe:    true,
		MsgType:     msgType,
		Caption:     caption,
		IsViewOnce:  viewOnce,
		MediaMime:   mimeType,
		MediaPath:   mediaRel,
		Timestamp:   time.Now().Unix(),
		ChatName:    canonicalTarget.User,
		ChatIsGroup: canonicalTarget.Server == waTypes.GroupServer,
	}
	h.store.RecordMessage(stored)
	h.store.Flush()
	h.Broadcast("message", stored)

	writeJSON(w, map[string]any{"ok": true, "id": sendResp.ID, "message": stored})
}

func (h *Hub) handlePairPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", webHTMLCSP)
	_, _ = w.Write(pairHTML)
}

func (h *Hub) handleGamesPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", webHTMLCSP)
	_, _ = w.Write(gamesHTML)
}

// handlePublicPairAPI serves buyer-scoped ticket status and QR PNG without
// exposing admin endpoints or any other buyer's QR code.
func (h *Hub) handlePublicPairAPI(w http.ResponseWriter, r *http.Request) {
	q := h.getPairingQueue()
	if q == nil {
		http.Error(w, "pairing queue unavailable", http.StatusServiceUnavailable)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/api/wa/pair/")
	parts := strings.SplitN(rel, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	token, action := parts[0], parts[1]

	switch action {
	case "status":
		t, ok := q.Get(token)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, t)

	case "qr.png":
		code, ok := q.QRForToken(token)
		if !ok {
			http.Error(w, "QR not active for this ticket", http.StatusNotFound)
			return
		}
		encoded, err := qr.Encode(code, qr.L)
		if err != nil {
			http.Error(w, "qr encode failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(encoded.PNG())

	default:
		http.NotFound(w, r)
	}
}

func (h *Hub) handlePairingTickets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	q := h.getPairingQueue()
	if q == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "pairing queue unavailable"})
		return
	}
	var req struct {
		Label        string `json:"label"`
		BuyerJID     string `json:"buyerJid"`
		DurationDays int    `json:"durationDays"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid json"})
		return
	}
	t, err := q.Enqueue(pairing.EnqueueRequest{
		Label:        strings.TrimSpace(req.Label),
		BuyerJID:     utils.NormalizeTarget(req.BuyerJID),
		DurationDays: req.DurationDays,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"ticket":   t,
		"pairPath": "/pair/" + t.Token,
	})
}

func (h *Hub) handlePairingQueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := h.getPairingQueue()
	var tickets []pairing.Ticket
	if q != nil {
		tickets = q.List()
	}
	if tickets == nil {
		tickets = []pairing.Ticket{}
	}
	var tenants []db.BotTenant
	if bs := h.getBotStore(); bs != nil {
		tenants, _ = bs.ListTenants()
	}
	if tenants == nil {
		tenants = []db.BotTenant{}
	}
	writeJSON(w, map[string]any{
		"tickets": tickets,
		"tenants": tenants,
	})
}

func (h *Hub) handlePairingCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	q := h.getPairingQueue()
	if q == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "pairing queue unavailable"})
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "token required"})
		return
	}
	if err := q.Cancel(strings.TrimSpace(req.Token)); err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *Hub) handleGameScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		UserJID  string `json:"userJid"`
		GroupJID string `json:"groupJid"`
		Game     string `json:"game"`
		Points   int64  `json:"points"`
		Won      bool   `json:"won"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.UserJID) == "" || strings.TrimSpace(req.Game) == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "userJid and game required"})
		return
	}
	if bs := h.getBotStore(); bs != nil {
		_ = bs.RecordGameResult(strings.TrimSpace(req.UserJID), strings.TrimSpace(req.GroupJID), strings.ToLower(strings.TrimSpace(req.Game)), req.Won, false, req.Points)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *Hub) getPairingQueue() *pairing.Queue {
	h.mu.RLock()
	q := h.pairQueue
	h.mu.RUnlock()
	if q != nil {
		return q
	}
	return pairing.DefaultQueue()
}

func (h *Hub) getBotStore() *db.BotStore {
	h.mu.RLock()
	bs := h.botStore
	h.mu.RUnlock()
	if bs != nil {
		return bs
	}
	return db.GetBotStore()
}
