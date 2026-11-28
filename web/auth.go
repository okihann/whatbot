package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/config"
	"bot/utils"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
)

const (
	webSessionCookieName = "nopebot_session"
	webSessionTTL        = 7 * 24 * time.Hour
	webOTPTTL            = 5 * time.Minute
	webOTPMaxAttempts    = 5
	webOTPResendCooldown = 15 * time.Second
	authRateWindow       = 10 * time.Minute
	authRequestMaxPerIP  = 5
	authVerifyMaxPerIP   = 10
	maxAuthJSONBodyBytes = 16 * 1024
)

var (
	defaultHubMu sync.RWMutex
	defaultHub   *Hub
)

// SetDefaultHub registers the active Web Hub so WhatsApp commands like .weblogin
// can issue owner verification codes.
func SetDefaultHub(h *Hub) {
	defaultHubMu.Lock()
	defaultHub = h
	defaultHubMu.Unlock()
}

// GetDefaultHub returns the active Web Hub if initialized.
func GetDefaultHub() *Hub {
	defaultHubMu.RLock()
	defer defaultHubMu.RUnlock()
	return defaultHub
}

type webAuth struct {
	mu              sync.Mutex
	secret          []byte
	secretLoaded    bool
	revokedLoaded   bool
	pendingCode     string
	pendingExpiry   time.Time
	pendingIssuedAt time.Time
	failedAttempts  int
	revoked         map[string]int64
	ipHits          map[string][]time.Time
}

func newWebAuth() *webAuth {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	return &webAuth{
		secret:  raw,
		revoked: make(map[string]int64),
		ipHits:  make(map[string][]time.Time),
	}
}

func (h *Hub) allowAuthRate(bucket, ip string, limit int, window time.Duration) bool {
	now := time.Now()
	cutoff := now.Add(-window)
	key := bucket + "|" + ip

	h.auth.mu.Lock()
	defer h.auth.mu.Unlock()

	if h.auth.ipHits == nil {
		h.auth.ipHits = make(map[string][]time.Time)
	}
	// Periodically prune stale buckets to keep memory bounded.
	if len(h.auth.ipHits) > 512 {
		for k, times := range h.auth.ipHits {
			kept := times[:0]
			for _, ts := range times {
				if ts.After(cutoff) {
					kept = append(kept, ts)
				}
			}
			if len(kept) == 0 {
				delete(h.auth.ipHits, k)
			} else {
				h.auth.ipHits[k] = kept
			}
		}
	}

	hits := h.auth.ipHits[key]
	kept := hits[:0]
	for _, ts := range hits {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= limit {
		h.auth.ipHits[key] = kept
		return false
	}
	h.auth.ipHits[key] = append(kept, now)
	return true
}

func (h *Hub) loadRevokedLocked() {
	if h.auth.revokedLoaded {
		return
	}
	h.auth.revokedLoaded = true
	bs := h.getBotStore()
	if bs == nil {
		return
	}
	val, ok, err := bs.GetKV("web_auth_revoked")
	if err != nil || !ok || val == "" {
		return
	}
	var loaded map[string]int64
	if err := json.Unmarshal([]byte(val), &loaded); err == nil {
		now := time.Now().Unix()
		for sid, exp := range loaded {
			if exp > now {
				h.auth.revoked[sid] = exp
			}
		}
	}
}

func (h *Hub) persistRevokedLocked() {
	now := time.Now().Unix()
	for sid, exp := range h.auth.revoked {
		if exp <= now {
			delete(h.auth.revoked, sid)
		}
	}
	if bs := h.getBotStore(); bs != nil {
		if raw, err := json.Marshal(h.auth.revoked); err == nil {
			_ = bs.SetKV("web_auth_revoked", string(raw))
		}
	}
}

func (h *Hub) authSecret() []byte {
	h.auth.mu.Lock()
	defer h.auth.mu.Unlock()
	if !h.auth.secretLoaded {
		if bs := h.getBotStore(); bs != nil {
			if val, ok, err := bs.GetKV("web_auth_secret"); err == nil && ok && len(val) == 64 {
				if decoded, err := hex.DecodeString(val); err == nil && len(decoded) == 32 {
					h.auth.secret = decoded
					h.auth.secretLoaded = true
					return h.auth.secret
				}
			}
			_ = bs.SetKV("web_auth_secret", hex.EncodeToString(h.auth.secret))
			h.auth.secretLoaded = true
		}
	}
	return h.auth.secret
}

func webPassword() string {
	return strings.TrimSpace(os.Getenv("WEB_PASSWORD"))
}

func webOTPRequired() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("WEB_OTP_REQUIRED")), "false")
}

func randomSixDigits() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func randomHexToken(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Hub) signSessionToken(sid string, exp int64) string {
	payload := sid + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, h.authSecret())
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

func (h *Hub) verifySessionToken(tok string) (string, int64, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return "", 0, false
	}
	sid, expStr := parts[0], parts[1]
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", 0, false
	}
	expected := h.signSessionToken(sid, exp)
	if subtle.ConstantTimeCompare([]byte(tok), []byte(expected)) != 1 {
		return "", 0, false
	}
	h.auth.mu.Lock()
	defer h.auth.mu.Unlock()
	h.loadRevokedLocked()
	if _, rev := h.auth.revoked[sid]; rev {
		return "", 0, false
	}
	return sid, exp, true
}

// checkSameOrigin enforces basic CSRF protection on mutating requests when a
// browser sends an Origin header, ensuring it matches the target Host.
func checkSameOrigin(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func (h *Hub) isAuthenticatedRequest(r *http.Request) bool {
	// 1. Verified owner session cookie (primary auth path).
	if c, err := r.Cookie(webSessionCookieName); err == nil && c.Value != "" {
		if _, _, ok := h.verifySessionToken(c.Value); ok {
			return true
		}
	}

	// 2. Explicit non-OTP header mode (only if owner opted out via WEB_OTP_REQUIRED=false).
	if !webOTPRequired() {
		pass := webPassword()
		if pass == "" {
			return true
		}
		provided := strings.TrimSpace(r.Header.Get("X-Web-Password"))
		if provided == "" {
			provided = strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
		}
		if provided == "" {
			provided = strings.TrimSpace(r.URL.Query().Get("password"))
		}
		if provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(pass)) == 1 {
			return true
		}
	}

	return false
}

func (h *Hub) connectedPrimaryClient() *whatsmeow.Client {
	if h.accounts == nil {
		return nil
	}
	for _, a := range h.accounts.ListAccounts() {
		if a.Connected {
			if c, ok := h.accounts.GetClient(a.User); ok && c != nil && c.IsConnected() {
				return c
			}
		}
	}
	return nil
}

func resolveOwnerTargetJID(client *whatsmeow.Client) (waTypes.JID, string) {
	if conf, err := config.LoadConfig(); err == nil && conf != nil {
		for _, raw := range conf.Settings.OwnerJIDs {
			digits := utils.NormalizeTarget(raw)
			if digits != "" {
				masked := maskPhone(digits)
				return waTypes.NewJID(digits, waTypes.DefaultUserServer), masked
			}
		}
	}
	if client != nil && client.Store != nil && client.Store.ID != nil {
		own := client.Store.ID.ToNonAD()
		return own, maskPhone(own.User)
	}
	return waTypes.EmptyJID, ""
}

func maskPhone(digits string) string {
	if len(digits) <= 6 {
		return "****"
	}
	return digits[:4] + "****" + digits[len(digits)-3:]
}

// IssueOwnerLoginCode creates a fresh 6-digit verification code valid for 5 minutes.
// Used by both POST /api/wa/auth/request-code and the WhatsApp .weblogin command.
func (h *Hub) IssueOwnerLoginCode(bypassCooldown bool) (code string, expiresInSec int, err error) {
	h.auth.mu.Lock()
	defer h.auth.mu.Unlock()

	now := time.Now()
	if !bypassCooldown && h.auth.pendingCode != "" && now.Sub(h.auth.pendingIssuedAt) < webOTPResendCooldown {
		rem := int((webOTPResendCooldown - now.Sub(h.auth.pendingIssuedAt)).Seconds()) + 1
		return "", 0, fmt.Errorf("please wait %ds before requesting another code", rem)
	}

	code = randomSixDigits()
	h.auth.pendingCode = code
	h.auth.pendingIssuedAt = now
	h.auth.pendingExpiry = now.Add(webOTPTTL)
	h.auth.failedAttempts = 0
	return code, int(webOTPTTL.Seconds()), nil
}

// GenerateOwnerLoginCodeForWhatsApp issues a fresh 6-digit code for the owner
// when they run .weblogin directly inside WhatsApp.
func GenerateOwnerLoginCodeForWhatsApp() (string, int, error) {
	h := GetDefaultHub()
	if h == nil {
		return "", 0, fmt.Errorf("web server not initialized")
	}
	return h.IssueOwnerLoginCode(true)
}

func (h *Hub) verifyOwnerOTP(code string) error {
	clean := strings.TrimSpace(code)
	h.auth.mu.Lock()
	defer h.auth.mu.Unlock()

	if h.auth.pendingCode == "" || time.Now().After(h.auth.pendingExpiry) {
		h.auth.pendingCode = ""
		return fmt.Errorf("verification code expired — request a new code")
	}
	if h.auth.failedAttempts >= webOTPMaxAttempts {
		h.auth.pendingCode = ""
		return fmt.Errorf("too many invalid attempts — request a new code")
	}
	if subtle.ConstantTimeCompare([]byte(clean), []byte(h.auth.pendingCode)) != 1 {
		h.auth.failedAttempts++
		return fmt.Errorf("invalid verification code (%d/%d attempts)", h.auth.failedAttempts, webOTPMaxAttempts)
	}
	// Single-use OTP: consume on success.
	h.auth.pendingCode = ""
	h.auth.failedAttempts = 0
	return nil
}

func (h *Hub) checkOptionalPassword(provided string) bool {
	pass := webPassword()
	if pass == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(provided)), []byte(pass)) == 1
}

// --- HTTP Handlers ---

func (h *Hub) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	authed := h.isAuthenticatedRequest(r)
	cli := h.connectedPrimaryClient()
	ownerHint := ""
	if authed {
		_, ownerHint = resolveOwnerTargetJID(cli)
	}
	writeJSON(w, map[string]any{
		"authenticated":     authed,
		"passwordRequired":  webPassword() != "",
		"otpRequired":       webOTPRequired(),
		"whatsappConnected": cli != nil,
		"ownerHint":         ownerHint,
	})
}

func (h *Hub) handleAuthRequestCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkSameOrigin(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	if !h.allowAuthRate("req", rateLimitIP(r), authRequestMaxPerIP, authRateWindow) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(w, map[string]string{"error": "too many verification code requests — try again later"})
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, maxAuthJSONBodyBytes)).Decode(&req)

	if !h.checkOptionalPassword(req.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "incorrect owner password"})
		return
	}

	code, expSec, err := h.IssueOwnerLoginCode(false)
	if err != nil {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Deliver to Owner's WhatsApp if a bot client is connected; otherwise print in server console.
	delivery := "console"
	targetHint := "Server Console"
	if cli := h.connectedPrimaryClient(); cli != nil {
		targetJID, hint := resolveOwnerTargetJID(cli)
		if !targetJID.IsEmpty() {
			msgText := fmt.Sprintf(
				"🔐 *NopeBot Web Login Verification*\n\nYour owner verification code is:\n\n*%s*\n\n• Expires in 5 minutes\n• Request IP: `%s`\n• If this wasn't you, ignore this message.",
				code, clientIP(r),
			)
			_, sendErr := utils.SendReply(cli, targetJID, msgText)
			if sendErr == nil {
				delivery = "whatsapp"
				targetHint = hint
			} else {
				fmt.Printf("⚠️ [WEB AUTH] Failed sending OTP via WhatsApp (%v), falling back to console.\n", sendErr)
			}
		}
	}

	if delivery == "console" {
		fmt.Printf("\n===================================================\n")
		fmt.Printf("🔐 NOPEBOT WEB OWNER VERIFICATION CODE: %s\n", code)
		fmt.Printf("   Valid for 5 minutes (requested from %s)\n", clientIP(r))
		fmt.Printf("===================================================\n\n")
	}

	writeJSON(w, map[string]any{
		"ok":        true,
		"delivery":  delivery,
		"target":    targetHint,
		"expiresIn": expSec,
	})
}

func (h *Hub) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkSameOrigin(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	if !h.allowAuthRate("verify", rateLimitIP(r), authVerifyMaxPerIP, authRateWindow) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(w, map[string]string{"error": "too many login attempts — try again later"})
		return
	}

	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxAuthJSONBodyBytes)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid json"})
		return
	}

	if !h.checkOptionalPassword(req.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "incorrect owner password"})
		return
	}

	if webOTPRequired() {
		if err := h.verifyOwnerOTP(req.Code); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
	}

	sid := randomHexToken(16)
	exp := time.Now().Add(webSessionTTL).Unix()
	token := h.signSessionToken(sid, exp)

	http.SetCookie(w, &http.Cookie{
		Name:     webSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Unix(exp, 0),
		MaxAge:   int(webSessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})

	writeJSON(w, map[string]any{
		"ok":            true,
		"authenticated": true,
	})
}

func (h *Hub) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !checkSameOrigin(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	if c, err := r.Cookie(webSessionCookieName); err == nil && c.Value != "" {
		if sid, exp, ok := h.verifySessionToken(c.Value); ok {
			h.auth.mu.Lock()
			h.loadRevokedLocked()
			h.auth.revoked[sid] = exp
			h.persistRevokedLocked()
			h.auth.mu.Unlock()
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     webSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]bool{"ok": true})
}

func rateLimitIP(r *http.Request) string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TRUST_PROXY")), "true") {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func clientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return r.RemoteAddr
}
