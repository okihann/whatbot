package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	db "bot/databases"

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
