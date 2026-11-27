package pairing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	db "bot/databases"
)

const (
	StatusQueued    = "queued"
	StatusPairing   = "pairing"
	StatusConnected = "connected"
	StatusExpired   = "expired"
	StatusCanceled  = "canceled"
	StatusFailed    = "failed"
)

// DefaultPairingTimeout is how long a single buyer holds the front of the QR
// queue before the worker automatically advances to the next queued buyer.
const DefaultPairingTimeout = 90 * time.Second

// Ticket represents one sequential QR pairing slot in the FIFO queue.
type Ticket struct {
	Token        string `json:"token"`
	Label        string `json:"label"`
	BuyerJID     string `json:"buyerJid"`
	DurationDays int    `json:"durationDays"`
	Status       string `json:"status"`
	Position     int    `json:"position"` // 0 = active pairing, >=1 = waiting in FIFO queue, -1 = finished
	AccountID    string `json:"accountId,omitempty"`
	ErrorMsg     string `json:"errorMsg,omitempty"`
	QRAvailable  bool   `json:"qrAvailable"`
	QRUpdatedAt  int64  `json:"qrUpdatedAt,omitempty"`
	DeadlineUnix int64  `json:"deadlineUnix,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`

	// Internal fields not exposed via JSON
	qrCode string
	cancel context.CancelFunc
	onQR   func(code string)
	onDone func(t Ticket)
}

// PairSessionFunc connects a new Android Companion WhatsApp device, calling
// emitQR whenever a QR string arrives, and returns the paired account ID (phone
// number) on success or an error on timeout/cancellation/failure.
type PairSessionFunc func(ctx context.Context, emitQR func(code string)) (accountID string, err error)

// Queue enforces strict FIFO sequential QR pairing so multiple customers can
// buy and pair bot sessions without clobbering each other's QR codes.
type Queue struct {
	store    *db.BotStore
	dialer   PairSessionFunc
	timeout  time.Duration
	onChange func()
	notifyCh chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	mu      sync.RWMutex
	tickets map[string]*Ticket
	order   []string // FIFO queue of pending/active tokens
	active  string   // currently pairing token (at most one at any instant)
}

var (
	defaultQueueMu sync.RWMutex
	defaultQueue   *Queue
)

func SetDefaultQueue(q *Queue) {
	defaultQueueMu.Lock()
	defaultQueue = q
	defaultQueueMu.Unlock()
}

func DefaultQueue() *Queue {
	defaultQueueMu.RLock()
	defer defaultQueueMu.RUnlock()
	return defaultQueue
}

// NewQueue starts a sequential QR pairing worker backed by store and dialer.
func NewQueue(store *db.BotStore, dialer PairSessionFunc, timeout time.Duration, onChange func()) *Queue {
	if timeout <= 0 {
		timeout = DefaultPairingTimeout
	}
	q := &Queue{
		store:    store,
		dialer:   dialer,
		timeout:  timeout,
		onChange: onChange,
		notifyCh: make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		tickets:  make(map[string]*Ticket),
	}
	q.wg.Add(1)
	go q.worker()
	return q
}

// Close cancels any active pairing session and stops the sequential worker.
func (q *Queue) Close() {
	if q == nil {
		return
	}
	q.stopOnce.Do(func() {
		close(q.stopCh)
		q.mu.Lock()
		if cur, ok := q.tickets[q.active]; ok && cur.cancel != nil {
			cur.cancel()
		}
		q.mu.Unlock()
	})
	q.wg.Wait()
}

func randomToken() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// EnqueueRequest describes a new customer/admin pairing ticket.
type EnqueueRequest struct {
	Label        string
	BuyerJID     string
	DurationDays int
	OnQR         func(code string)
	OnDone       func(t Ticket)
}

// Enqueue appends a new pairing ticket to the FIFO sequential queue.
func (q *Queue) Enqueue(req EnqueueRequest) (Ticket, error) {
	if q == nil {
		return Ticket{}, fmt.Errorf("pairing queue unavailable")
	}
	now := time.Now().Unix()
	tok := randomToken()
	label := req.Label
	if label == "" {
		label = "Buyer Bot"
	}

	t := &Ticket{
		Token:        tok,
		Label:        label,
		BuyerJID:     req.BuyerJID,
		DurationDays: req.DurationDays,
		Status:       StatusQueued,
		CreatedAt:    now,
		UpdatedAt:    now,
		onQR:         req.OnQR,
		onDone:       req.OnDone,
	}

	q.mu.Lock()
	q.tickets[tok] = t
	q.order = append(q.order, tok)
	snap := q.snapshotLocked(t)
	q.mu.Unlock()

	q.persist(snap)
	q.signal()
	q.fireChange()
	return snap, nil
}

// Cancel removes a queued ticket or aborts the currently active pairing ticket.
func (q *Queue) Cancel(token string) error {
	if q == nil {
		return fmt.Errorf("pairing queue unavailable")
	}
	q.mu.Lock()
	t, ok := q.tickets[token]
	if !ok {
		q.mu.Unlock()
		return fmt.Errorf("ticket not found")
	}
	if t.Status != StatusQueued && t.Status != StatusPairing {
		q.mu.Unlock()
		return fmt.Errorf("ticket is already %s", t.Status)
	}
	t.Status = StatusCanceled
	t.qrCode = ""
	t.QRAvailable = false
	t.UpdatedAt = time.Now().Unix()
	if t.cancel != nil {
		t.cancel()
	}
	q.removeFromOrderLocked(token)
	snap := q.snapshotLocked(t)
	cb := t.onDone
	q.mu.Unlock()

	q.persist(snap)
	q.fireChange()
	if cb != nil {
		go cb(snap)
	}
	return nil
}

// Get returns a snapshot of a ticket by its secret token.
func (q *Queue) Get(token string) (Ticket, bool) {
	if q == nil {
		return Ticket{}, false
	}
	q.mu.RLock()
	defer q.mu.RUnlock()
	t, ok := q.tickets[token]
	if !ok {
		return Ticket{}, false
	}
	return q.snapshotLocked(t), true
}

// QRForToken returns the active QR string ONLY if token is currently at the front
// of the sequential pairing queue.
func (q *Queue) QRForToken(token string) (string, bool) {
	if q == nil {
		return "", false
	}
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.active != token {
		return "", false
	}
	t, ok := q.tickets[token]
	if !ok || t.qrCode == "" || t.Status != StatusPairing {
		return "", false
	}
	return t.qrCode, true
}

// ActiveQR returns the QR string and token of whichever ticket is currently pairing.
func (q *Queue) ActiveQR() (token, code, status string, updatedAt int64) {
	if q == nil {
		return "", "", "idle", 0
	}
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.active == "" {
		return "", "", "idle", 0
	}
	t, ok := q.tickets[q.active]
	if !ok {
		return "", "", "idle", 0
	}
	st := t.Status
	if t.qrCode != "" {
		st = "code"
	}
	return t.Token, t.qrCode, st, t.QRUpdatedAt
}

// List returns active/queued tickets in FIFO order followed by recent completed tickets.
func (q *Queue) List() []Ticket {
	if q == nil {
		return nil
	}
	q.mu.RLock()
	defer q.mu.RUnlock()

	seen := make(map[string]bool, len(q.order))
	out := make([]Ticket, 0, len(q.tickets))
	for _, tok := range q.order {
		if t, ok := q.tickets[tok]; ok {
			out = append(out, q.snapshotLocked(t))
			seen[tok] = true
		}
	}
	for tok, t := range q.tickets {
		if !seen[tok] {
			out = append(out, q.snapshotLocked(t))
		}
	}
	return out
}

func (q *Queue) snapshotLocked(t *Ticket) Ticket {
	cp := *t
	cp.qrCode = ""
	cp.cancel = nil
	cp.onQR = nil
	cp.onDone = nil
	cp.Position = -1
	for idx, tok := range q.order {
		if tok == t.Token {
			cp.Position = idx // 0 when active at head, >=1 when waiting
			break
		}
	}
	return cp
}

func (q *Queue) removeFromOrderLocked(token string) {
	for i, tok := range q.order {
		if tok == token {
			q.order = append(q.order[:i], q.order[i+1:]...)
			return
		}
	}
}

func (q *Queue) signal() {
	select {
	case q.notifyCh <- struct{}{}:
	default:
	}
}

func (q *Queue) fireChange() {
	if q.onChange != nil {
		q.onChange()
	}
}

func (q *Queue) persist(t Ticket) {
	if q.store == nil {
		return
	}
	_ = q.store.SavePairingTicket(db.PairingTicketRecord{
		Token:        t.Token,
		Label:        t.Label,
		BuyerJID:     t.BuyerJID,
		DurationDays: t.DurationDays,
		Status:       t.Status,
		AccountID:    t.AccountID,
		ErrorMsg:     t.ErrorMsg,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	})
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		tok := q.nextQueuedToken()
		if tok == "" {
			select {
			case <-q.stopCh:
				return
			case <-q.notifyCh:
				continue
			}
		}
		q.runTicket(tok)
	}
}

func (q *Queue) nextQueuedToken() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, tok := range q.order {
		if t, ok := q.tickets[tok]; ok && t.Status == StatusQueued {
			return tok
		}
	}
	return ""
}

func (q *Queue) runTicket(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), q.timeout)
	defer cancel()

	q.mu.Lock()
	t, ok := q.tickets[token]
	if !ok || t.Status != StatusQueued {
		q.mu.Unlock()
		return
	}
	q.active = token
	t.Status = StatusPairing
	t.cancel = cancel
	t.DeadlineUnix = time.Now().Add(q.timeout).Unix()
	t.UpdatedAt = time.Now().Unix()
	onQR := t.onQR
	onDone := t.onDone
	snap := q.snapshotLocked(t)
	q.mu.Unlock()

	q.persist(snap)
	q.fireChange()

	emitQR := func(code string) {
		q.mu.Lock()
		if cur, exists := q.tickets[token]; exists && cur.Status == StatusPairing {
			cur.qrCode = code
			cur.QRAvailable = code != ""
			cur.QRUpdatedAt = time.Now().Unix()
			cur.UpdatedAt = cur.QRUpdatedAt
		}
		q.mu.Unlock()
		q.fireChange()
		if onQR != nil && code != "" {
			onQR(code)
		}
	}

	var (
		accountID string
		err       error
	)
	if q.dialer != nil {
		accountID, err = q.dialer(ctx, emitQR)
	} else {
		err = fmt.Errorf("no pairing dialer configured")
	}

	q.mu.Lock()
	if q.active == token {
		q.active = ""
	}
	t = q.tickets[token]
	if t != nil {
		t.qrCode = ""
		t.QRAvailable = false
		t.cancel = nil
		t.UpdatedAt = time.Now().Unix()
		if t.Status != StatusCanceled {
			if err == nil && accountID != "" {
				t.Status = StatusConnected
				t.AccountID = accountID
			} else if ctx.Err() == context.DeadlineExceeded {
				t.Status = StatusExpired
				t.ErrorMsg = "QR pairing timed out"
			} else {
				t.Status = StatusFailed
				if err != nil {
					t.ErrorMsg = err.Error()
				}
			}
		}
		q.removeFromOrderLocked(token)
		snap = q.snapshotLocked(t)
	}
	q.mu.Unlock()

	// Automatically bind buyer owner & subscription expiry in BotStore.
	if snap.Status == StatusConnected && snap.AccountID != "" && q.store != nil {
		tenant, _ := q.store.GetTenant(snap.AccountID)
		if tenant == nil {
			tenant = &db.BotTenant{AccountID: snap.AccountID, BotName: "NopeBot"}
		}
		if snap.BuyerJID != "" {
			tenant.OwnerJID = snap.BuyerJID
		}
		if snap.Label != "" && tenant.Notes == "" {
			tenant.Notes = snap.Label
		}
		if snap.DurationDays > 0 {
			base := time.Now()
			if tenant.ExpiresAt > base.Unix() {
				base = time.Unix(tenant.ExpiresAt, 0)
			}
			tenant.ExpiresAt = base.Add(time.Duration(snap.DurationDays) * 24 * time.Hour).Unix()
		}
		_ = q.store.SaveTenant(tenant)
	}

	q.persist(snap)
	q.fireChange()
	if onDone != nil && snap.Status != StatusCanceled {
		go onDone(snap)
	}
}
