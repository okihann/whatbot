package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	db "bot/databases"
	"bot/pairing"

	"go.mau.fi/whatsmeow"
)

type mockAccountManager struct {
	accounts      []AccountInfo
	pairingCalled bool
}

func (m *mockAccountManager) ListAccounts() []AccountInfo {
	return m.accounts
}

func (m *mockAccountManager) GetClient(accountID string) (*whatsmeow.Client, bool) {
	return nil, false
}

func (m *mockAccountManager) StartPairing() error {
	m.pairingCalled = true
	return nil
}

func (m *mockAccountManager) RemoveAccount(accountID string) error {
	return nil
}

func TestWebServerEndpointsAndAuth(t *testing.T) {
	dir := t.TempDir()
	sessionDB, err := db.OpenSequentialSQLite(filepath.Join(dir, "session.db"))
	if err != nil {
		t.Fatalf("OpenSequentialSQLite: %v", err)
	}
	defer db.CheckpointAndClose(sessionDB)

	chatStore, err := db.OpenChatStore(filepath.Join(dir, "webchat.db"), filepath.Join(dir, "webmedia"))
	if err != nil {
		t.Fatalf("OpenChatStore: %v", err)
	}
	defer chatStore.Close()

	mediaRel, err := chatStore.SaveMediaFile("628111", "VO-1", ".jpg", []byte("fake-jpeg-bytes"))
	if err != nil {
		t.Fatalf("SaveMediaFile: %v", err)
	}
	chatStore.RecordMessage(db.StoredMessage{
		AccountID:  "628111",
		MsgID:      "VO-1",
		ChatJID:    "628222@s.whatsapp.net",
		SenderJID:  "628222@s.whatsapp.net",
		SenderName: "Bob",
		MsgType:    "image",
		Caption:    "secret photo",
		IsViewOnce: true,
		MediaMime:  "image/jpeg",
		MediaPath:  mediaRel,
		Timestamp:  1700000100,
		ChatName:   "Bob",
	})
	chatStore.Flush()

	mgr := &mockAccountManager{
		accounts: []AccountInfo{{
			ID:        "628111:1@s.whatsapp.net",
			User:      "628111",
			PushName:  "TestBot",
			Connected: true,
			Platform:  "Android Companion",
		}},
	}
	hub := NewHub(chatStore, sessionDB, mgr)
	hub.PublishQR("2@mock-android-companion-qr-code")

	mux := http.NewServeMux()
	hub.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 1. Index HTML
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Android Companion") {
		t.Fatalf("unexpected index response: status=%d", resp.StatusCode)
	}

	// 2. Status reports WAL & Android Companion
	resp, err = http.Get(srv.URL + "/api/wa/status")
	if err != nil {
		t.Fatalf("GET /api/wa/status: %v", err)
	}
	var statusPayload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&statusPayload)
	resp.Body.Close()
	if statusPayload["ok"] != true {
		t.Fatalf("expected ok=true, got %+v", statusPayload)
	}

	// 3. QR PNG returns valid PNG magic header
	resp, err = http.Get(srv.URL + "/api/wa/qr.png")
	if err != nil {
		t.Fatalf("GET /api/wa/qr.png: %v", err)
	}
	pngBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(pngBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("expected valid PNG, status=%d len=%d", resp.StatusCode, len(pngBytes))
	}

	// 4. Chats & Messages & View-Once Media download
	resp, err = http.Get(srv.URL + "/api/wa/chats?account=628111&filter=viewonce")
	if err != nil {
		t.Fatalf("GET /api/wa/chats: %v", err)
	}
	var chatsPayload struct {
		Chats []db.StoredChat `json:"chats"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&chatsPayload)
	resp.Body.Close()
	if len(chatsPayload.Chats) != 1 || !chatsPayload.Chats[0].HasViewOnce {
		t.Fatalf("expected 1 view-once chat, got %+v", chatsPayload.Chats)
	}

	resp, err = http.Get(srv.URL + "/api/wa/media/628111/VO-1")
	if err != nil {
		t.Fatalf("GET /api/wa/media: %v", err)
	}
	mediaBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(mediaBytes) != "fake-jpeg-bytes" {
		t.Fatalf("unexpected media response: status=%d body=%q", resp.StatusCode, string(mediaBytes))
	}

	// 5. Optional WEB_PASSWORD enforcement
	t.Setenv("WEB_PASSWORD", "supersecret")
	resp, err = http.Get(srv.URL + "/api/wa/accounts")
	if err != nil {
		t.Fatalf("GET /api/wa/accounts without password: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without password, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/wa/accounts", nil)
	req.Header.Set("X-Web-Password", "supersecret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/wa/accounts with password: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with valid password, got %d", resp.StatusCode)
	}
}

func TestSequentialPairingAndGamesWebRoutes(t *testing.T) {
	dir := t.TempDir()
	sessionDB, err := db.OpenSequentialSQLite(filepath.Join(dir, "session.db"))
	if err != nil {
		t.Fatalf("OpenSequentialSQLite: %v", err)
	}
	defer db.CheckpointAndClose(sessionDB)

	chatStore, err := db.OpenChatStore(filepath.Join(dir, "webchat.db"), filepath.Join(dir, "webmedia"))
	if err != nil {
		t.Fatalf("OpenChatStore: %v", err)
	}
	defer chatStore.Close()

	botStore, err := db.OpenBotStore(filepath.Join(dir, "botdata.db"))
	if err != nil {
		t.Fatalf("OpenBotStore: %v", err)
	}
	defer botStore.Close()

	qrReady := make(chan struct{}, 1)
	release := make(chan struct{})
	q := pairing.NewQueue(botStore, func(ctx context.Context, emitQR func(code string)) (string, error) {
		emitQR("2@buyer-sequential-qr")
		select {
		case qrReady <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return "628777001", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}, 5*time.Second, nil)
	defer func() {
		close(release)
		q.Close()
	}()

	hub := NewHub(chatStore, sessionDB, &mockAccountManager{})
	hub.SetBotStore(botStore)
	hub.SetPairingQueue(q)

	mux := http.NewServeMux()
	hub.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Create ticket via admin API
	resp, err := http.Post(srv.URL+"/api/wa/pairing/tickets", "application/json", strings.NewReader(`{"label":"Buyer A","buyerJid":"628555","durationDays":30}`))
	if err != nil {
		t.Fatalf("POST /api/wa/pairing/tickets: %v", err)
	}
	var createRes struct {
		OK       bool           `json:"ok"`
		PairPath string         `json:"pairPath"`
		Ticket   pairing.Ticket `json:"ticket"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&createRes)
	resp.Body.Close()
	if !createRes.OK || createRes.Ticket.Token == "" {
		t.Fatalf("unexpected ticket create response: %+v", createRes)
	}

	select {
	case <-qrReady:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for ticket QR")
	}

	// Buyer pairing page HTML
	resp, err = http.Get(srv.URL + createRes.PairPath)
	if err != nil {
		t.Fatalf("GET pair page: %v", err)
	}
	pairBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(pairBody), "Sequential QR") {
		t.Fatalf("unexpected pair page: status=%d", resp.StatusCode)
	}

	// Buyer-scoped QR PNG
	resp, err = http.Get(srv.URL + "/api/wa/pair/" + createRes.Ticket.Token + "/qr.png")
	if err != nil {
		t.Fatalf("GET buyer qr.png: %v", err)
	}
	pngBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(pngBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("expected valid PNG for buyer token, status=%d", resp.StatusCode)
	}

	// Arcade games page + score submission
	resp, err = http.Get(srv.URL + "/games/snake")
	if err != nil {
		t.Fatalf("GET /games/snake: %v", err)
	}
	gameBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(gameBody), "NopeBot Arcade") {
		t.Fatalf("unexpected games page: status=%d", resp.StatusCode)
	}

	resp, err = http.Post(srv.URL+"/api/wa/games/score", "application/json", strings.NewReader(`{"userJid":"628555","groupJid":"1203@g.us","game":"snake","points":120,"won":true}`))
	if err != nil {
		t.Fatalf("POST /api/wa/games/score: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on score post, got %d", resp.StatusCode)
	}
	lb, _ := botStore.GameLeaderboard("1203@g.us", 5)
	if len(lb) != 1 || lb[0].Points != 120 {
		t.Fatalf("expected recorded arcade score 120, got %+v", lb)
	}
}
