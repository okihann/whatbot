package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
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

func newVerifiedOwnerClient(t *testing.T, srvURL string, hub *Hub, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	cli := &http.Client{Jar: jar}

	code, _, err := hub.IssueOwnerLoginCode(true)
	if err != nil {
		t.Fatalf("IssueOwnerLoginCode: %v", err)
	}

	payload, _ := json.Marshal(map[string]string{
		"password": password,
		"code":     code,
	})
	resp, err := cli.Post(srvURL+"/api/wa/auth/verify", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /api/wa/auth/verify: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on owner verify, got %d", resp.StatusCode)
	}
	return cli
}

func TestWebServerEndpointsOwnerAuthContactsAndChannelFilter(t *testing.T) {
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
		SenderName: "628222",
		MsgType:    "image",
		Caption:    "secret photo",
		IsViewOnce: true,
		MediaMime:  "image/jpeg",
		MediaPath:  mediaRel,
		Timestamp:  1700000100,
		ChatName:   "628222",
	})
	// Channels (@newsletter) and Status broadcasts must NEVER enter chats
	chatStore.RecordMessage(db.StoredMessage{
		AccountID: "628111",
		MsgID:     "CHAN-1",
		ChatJID:   "120363999@newsletter",
		MsgType:   "text",
		Body:      "Channel announcement",
		Timestamp: 1700000200,
		ChatName:  "Some Channel",
	})
	chatStore.RecordMessage(db.StoredMessage{
		AccountID: "628111",
		MsgID:     "STAT-1",
		ChatJID:   "status@broadcast",
		MsgType:   "text",
		Body:      "Status update",
		Timestamp: 1700000300,
	})
	// Syncing contact upgrades raw phone chat name to saved contact name
	chatStore.UpsertContact(db.StoredContact{
		AccountID: "628111",
		JID:       "628222@s.whatsapp.net",
		Phone:     "628222",
		Name:      "Bob Marley",
		FullName:  "Bob Marley",
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

	t.Setenv("WEB_PASSWORD", "ownerpass123")

	mux := http.NewServeMux()
	hub.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 1. Index HTML loads, but all /api/wa/* data routes are strictly 401 before verification
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Owner Verification") {
		t.Fatalf("unexpected index response: status=%d", resp.StatusCode)
	}

	for _, protected := range []string{"/api/wa/accounts", "/api/wa/chats?account=628111", "/api/wa/contacts?account=628111", "/api/wa/qr.png", "/api/wa/media/628111/VO-1"} {
		r, err := http.Get(srv.URL + protected)
		if err != nil {
			t.Fatalf("GET %s: %v", protected, err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 on %s without owner auth, got %d", protected, r.StatusCode)
		}
	}

	// 2. Wrong password rejected on request-code
	resp, _ = http.Post(srv.URL+"/api/wa/auth/request-code", "application/json", strings.NewReader(`{"password":"wrong"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", resp.StatusCode)
	}

	// 3. Valid password + wrong OTP rejected on verify
	resp, _ = http.Post(srv.URL+"/api/wa/auth/request-code", "application/json", strings.NewReader(`{"password":"ownerpass123"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on request-code, got %d", resp.StatusCode)
	}
	resp, _ = http.Post(srv.URL+"/api/wa/auth/verify", "application/json", strings.NewReader(`{"password":"ownerpass123","code":"000000"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong OTP, got %d", resp.StatusCode)
	}

	// 4. Complete Owner Login + 6-digit OTP Verification
	ownerClient := newVerifiedOwnerClient(t, srv.URL, hub, "ownerpass123")

	// Status reports WAL & authenticated=true
	resp, err = ownerClient.Get(srv.URL + "/api/wa/status")
	if err != nil {
		t.Fatalf("GET /api/wa/status: %v", err)
	}
	var statusPayload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&statusPayload)
	resp.Body.Close()
	if statusPayload["ok"] != true || statusPayload["authenticated"] != true {
		t.Fatalf("expected ok=true & authenticated=true, got %+v", statusPayload)
	}

	// QR PNG returns valid PNG magic header for authenticated owner
	resp, err = ownerClient.Get(srv.URL + "/api/wa/qr.png")
	if err != nil {
		t.Fatalf("GET /api/wa/qr.png: %v", err)
	}
	pngBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(pngBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("expected valid PNG, status=%d len=%d", resp.StatusCode, len(pngBytes))
	}

	// Chats excludes @newsletter & @broadcast AND shows upgraded contact name "Bob Marley"
	resp, err = ownerClient.Get(srv.URL + "/api/wa/chats?account=628111")
	if err != nil {
		t.Fatalf("GET /api/wa/chats: %v", err)
	}
	var chatsPayload struct {
		Chats []db.StoredChat `json:"chats"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&chatsPayload)
	resp.Body.Close()
	if len(chatsPayload.Chats) != 1 || !chatsPayload.Chats[0].HasViewOnce || chatsPayload.Chats[0].Name != "Bob Marley" {
		t.Fatalf("expected 1 non-channel chat named 'Bob Marley', got %+v", chatsPayload.Chats)
	}

	// Contacts endpoint returns synced contacts
	resp, err = ownerClient.Get(srv.URL + "/api/wa/contacts?account=628111&q=bob")
	if err != nil {
		t.Fatalf("GET /api/wa/contacts: %v", err)
	}
	var contactsPayload struct {
		Contacts []db.StoredContact `json:"contacts"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&contactsPayload)
	resp.Body.Close()
	if len(contactsPayload.Contacts) != 1 || contactsPayload.Contacts[0].Name != "Bob Marley" {
		t.Fatalf("expected 1 contact 'Bob Marley', got %+v", contactsPayload.Contacts)
	}

	// View-Once media download succeeds for authenticated owner
	resp, err = ownerClient.Get(srv.URL + "/api/wa/media/628111/VO-1")
	if err != nil {
		t.Fatalf("GET /api/wa/media: %v", err)
	}
	mediaBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(mediaBytes) != "fake-jpeg-bytes" {
		t.Fatalf("unexpected media response: status=%d body=%q", resp.StatusCode, string(mediaBytes))
	}

	// 5. Logout revokes cookie session immediately
	resp, _ = ownerClient.Post(srv.URL+"/api/wa/auth/logout", "application/json", nil)
	resp.Body.Close()
	resp, _ = ownerClient.Get(srv.URL + "/api/wa/accounts")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", resp.StatusCode)
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

	// Create ticket via verified owner session
	ownerClient := newVerifiedOwnerClient(t, srv.URL, hub, "")
	resp, err := ownerClient.Post(srv.URL+"/api/wa/pairing/tickets", "application/json", strings.NewReader(`{"label":"Buyer A","buyerJid":"628555","durationDays":30}`))
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

	// Buyer pairing page HTML & buyer-scoped QR PNG remain publicly accessible for the buyer token
	resp, err = http.Get(srv.URL + createRes.PairPath)
	if err != nil {
		t.Fatalf("GET pair page: %v", err)
	}
	pairBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(pairBody), "Sequential QR") {
		t.Fatalf("unexpected pair page: status=%d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/api/wa/pair/" + createRes.Ticket.Token + "/qr.png")
	if err != nil {
		t.Fatalf("GET buyer qr.png: %v", err)
	}
	pngBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(pngBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("expected valid PNG for buyer token, status=%d", resp.StatusCode)
	}

	// Arcade games page + score submission remain public for group players
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

func TestWebSecurityHardening(t *testing.T) {
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

	// Save a malicious HTML attachment to verify stored-XSS protection on /api/wa/media/.
	htmlRel, err := chatStore.SaveMediaFile("628111", "XSS-1", ".html", []byte("<script>alert(1)</script>"))
	if err != nil {
		t.Fatalf("SaveMediaFile: %v", err)
	}
	chatStore.RecordMessage(db.StoredMessage{
		AccountID: "628111",
		MsgID:     "XSS-1",
		ChatJID:   "628222@s.whatsapp.net",
		MsgType:   "document",
		MediaMime: "text/html",
		MediaPath: htmlRel,
		Timestamp: 1700000400,
	})
	chatStore.Flush()

	hub := NewHub(chatStore, sessionDB, &mockAccountManager{})
	hub.SetBotStore(botStore)
	t.Setenv("WEB_PASSWORD", "strongpass999")

	mux := http.NewServeMux()
	hub.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 1. Security headers + CSP on index HTML
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	resp.Body.Close()
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" ||
		resp.Header.Get("X-Frame-Options") != "DENY" ||
		resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("missing security headers on /: %+v", resp.Header)
	}

	// 2. Pre-auth /api/wa/auth/me and /api/wa/status hide sensitive metadata
	resp, _ = http.Get(srv.URL + "/api/wa/auth/me")
	var mePayload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&mePayload)
	resp.Body.Close()
	if mePayload["ownerHint"] != "" {
		t.Fatalf("expected empty ownerHint before auth, got %v", mePayload["ownerHint"])
	}

	resp, _ = http.Get(srv.URL + "/api/wa/status")
	var stPayload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&stPayload)
	resp.Body.Close()
	if _, leaked := stPayload["sqlite"]; leaked {
		t.Fatalf("expected sqlite metadata hidden before auth, got %+v", stPayload)
	}

	// 3. GET /api/wa/auth/logout rejected (405)
	resp, _ = http.Get(srv.URL + "/api/wa/auth/logout")
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET logout, got %d", resp.StatusCode)
	}

	// 4. Cross-origin POST rejected (403 CSRF check)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/wa/auth/request-code", strings.NewReader(`{"password":"strongpass999"}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin POST, got %d", resp.StatusCode)
	}

	// 5. Stored XSS protection on /api/wa/media/ for .html attachment
	ownerClient := newVerifiedOwnerClient(t, srv.URL, hub, "strongpass999")
	resp, err = ownerClient.Get(srv.URL + "/api/wa/media/628111/XSS-1")
	if err != nil {
		t.Fatalf("GET media XSS-1: %v", err)
	}
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("expected application/octet-stream for html attachment, got %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("expected attachment Content-Disposition for html attachment, got %q", cd)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Fatalf("expected sandbox CSP on media response, got %q", csp)
	}

	// 6. Per-IP rate limiting triggers 429 on repeated request-code calls
	var lastCode int
	for i := 0; i < authRequestMaxPerIP+2; i++ {
		r, _ := http.Post(srv.URL+"/api/wa/auth/request-code", "application/json", strings.NewReader(`{"password":"wrong"}`))
		lastCode = r.StatusCode
		r.Body.Close()
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding per-IP auth rate limit, got %d", lastCode)
	}
}
