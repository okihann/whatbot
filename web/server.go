package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	db "bot/databases"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"rsc.io/qr"
)

//go:embed static/index.html
var indexHTML []byte

// Mount registers the WhatsApp Web UI and Android Companion REST/SSE API onto mux.
func (h *Hub) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/api/wa/status", h.handleStatus)
	mux.HandleFunc("/api/wa/accounts", h.requireAuth(h.handleAccounts))
	mux.HandleFunc("/api/wa/accounts/add", h.requireAuth(h.handleAddAccount))
	mux.HandleFunc("/api/wa/accounts/remove", h.requireAuth(h.handleRemoveAccount))
	mux.HandleFunc("/api/wa/qr.png", h.requireAuth(h.handleQRPNG))
	mux.HandleFunc("/api/wa/events", h.requireAuth(h.handleSSE))
	mux.HandleFunc("/api/wa/chats", h.requireAuth(h.handleChats))
	mux.HandleFunc("/api/wa/chats/read", h.requireAuth(h.handleMarkRead))
	mux.HandleFunc("/api/wa/messages", h.requireAuth(h.handleMessages))
	mux.HandleFunc("/api/wa/media/", h.requireAuth(h.handleMedia))
	mux.HandleFunc("/api/wa/send/text", h.requireAuth(h.handleSendText))
	mux.HandleFunc("/api/wa/send/media", h.requireAuth(h.handleSendMedia))
}

func webPassword() string {
	return strings.TrimSpace(os.Getenv("WEB_PASSWORD"))
}

func (h *Hub) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Web-Password")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		pass := webPassword()
		if pass != "" {
			provided := strings.TrimSpace(r.Header.Get("X-Web-Password"))
			if provided == "" {
				provided = strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
			}
			if provided == "" {
				provided = strings.TrimSpace(r.URL.Query().Get("password"))
			}
			if provided != pass {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, map[string]string{"error": "unauthorized"})
				return
			}
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
	_, _ = w.Write(indexHTML)
}

func (h *Hub) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"ok":           true,
		"platform":     "Android Companion (View-Once Ready)",
		"uptimeSec":    int64(time.Since(h.startedAt).Seconds()),
		"authRequired": webPassword() != "",
		"sqlite": map[string]any{
			"session": db.SQLiteStatus(h.sessionDB),
			"webchat": h.store.Status(),
		},
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
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

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
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *Hub) handleMessages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
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

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
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
	stored := db.StoredMessage{
		AccountID:   acct,
		MsgID:       resp.ID,
		ChatJID:     targetJID.ToNonAD().String(),
		SenderJID:   client.Store.ID.ToNonAD().String(),
		SenderName:  "You",
		IsFromMe:    true,
		MsgType:     "text",
		Body:        req.Text,
		Timestamp:   time.Now().Unix(),
		ChatName:    targetJID.ToNonAD().User,
		ChatIsGroup: targetJID.Server == waTypes.GroupServer,
	}
	h.store.RecordMessage(stored)
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
	mediaRel, _ := h.store.SaveMediaFile(acct, sendResp.ID, extensionForMime(mimeType, ".bin"), data)
	stored := db.StoredMessage{
		AccountID:   acct,
		MsgID:       sendResp.ID,
		ChatJID:     targetJID.ToNonAD().String(),
		SenderJID:   client.Store.ID.ToNonAD().String(),
		SenderName:  "You",
		IsFromMe:    true,
		MsgType:     msgType,
		Caption:     caption,
		IsViewOnce:  viewOnce,
		MediaMime:   mimeType,
		MediaPath:   mediaRel,
		Timestamp:   time.Now().Unix(),
		ChatName:    targetJID.ToNonAD().User,
		ChatIsGroup: targetJID.Server == waTypes.GroupServer,
	}
	h.store.RecordMessage(stored)
	h.Broadcast("message", stored)

	writeJSON(w, map[string]any{"ok": true, "id": sendResp.ID, "message": stored})
}
