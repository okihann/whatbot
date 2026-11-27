package pairing

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	db "bot/databases"
)

func TestSequentialQRQueueStrictFIFOAndIsolation(t *testing.T) {
	dir := t.TempDir()
	store, err := db.OpenBotStore(filepath.Join(dir, "botdata.db"))
	if err != nil {
		t.Fatalf("OpenBotStore: %v", err)
	}
	defer store.Close()

	var (
		inFlight      atomic.Int32
		maxConcurrent atomic.Int32
		orderMu       sync.Mutex
		servedOrder   []string
		release1      = make(chan struct{})
		release2      = make(chan struct{})
		qrReady1      = make(chan struct{}, 1)
		qrReady2      = make(chan struct{}, 1)
		callNum       atomic.Int32
	)

	dialer := func(ctx context.Context, emitQR func(code string)) (string, error) {
		cur := inFlight.Add(1)
		for {
			prev := maxConcurrent.Load()
			if cur <= prev || maxConcurrent.CompareAndSwap(prev, cur) {
				break
			}
		}
		defer inFlight.Add(-1)

		n := callNum.Add(1)
		orderMu.Lock()
		servedOrder = append(servedOrder, string(rune('0'+n)))
		orderMu.Unlock()

		if n == 1 {
			emitQR("QR-CODE-BUYER-1")
			qrReady1 <- struct{}{}
			select {
			case <-release1:
				return "6281110001", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		emitQR("QR-CODE-BUYER-2")
		qrReady2 <- struct{}{}
		select {
		case <-release2:
			return "6281110002", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	q := NewQueue(store, dialer, 10*time.Second, nil)
	defer q.Close()

	// Enqueue 3 buyers concurrently — buyer #2 will be canceled while queued,
	// so buyer #1 pairs first, then buyer #3 pairs second, strictly sequentially.
	t1, err := q.Enqueue(EnqueueRequest{Label: "Buyer 1", BuyerJID: "62890001", DurationDays: 30})
	if err != nil {
		t.Fatalf("Enqueue 1: %v", err)
	}
	t2, err := q.Enqueue(EnqueueRequest{Label: "Buyer 2", BuyerJID: "62890002", DurationDays: 7})
	if err != nil {
		t.Fatalf("Enqueue 2: %v", err)
	}
	t3, err := q.Enqueue(EnqueueRequest{Label: "Buyer 3", BuyerJID: "62890003", DurationDays: 14})
	if err != nil {
		t.Fatalf("Enqueue 3: %v", err)
	}

	select {
	case <-qrReady1:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for buyer 1 QR")
	}

	// Verify strict token isolation: buyer 1 sees QR, buyer 2 & 3 do NOT.
	if code, ok := q.QRForToken(t1.Token); !ok || code != "QR-CODE-BUYER-1" {
		t.Fatalf("expected buyer 1 QR, got ok=%v code=%q", ok, code)
	}
	if _, ok := q.QRForToken(t2.Token); ok {
		t.Fatalf("buyer 2 must NOT see buyer 1's QR while queued")
	}
	if _, ok := q.QRForToken(t3.Token); ok {
		t.Fatalf("buyer 3 must NOT see buyer 1's QR while queued")
	}

	// Cancel buyer 2 while queued.
	if err := q.Cancel(t2.Token); err != nil {
		t.Fatalf("Cancel buyer 2: %v", err)
	}

	// Complete buyer 1 and wait for buyer 3 to automatically advance to head.
	close(release1)
	select {
	case <-qrReady2:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for buyer 3 QR after buyer 1 finished")
	}
	if code, ok := q.QRForToken(t3.Token); !ok || code != "QR-CODE-BUYER-2" {
		t.Fatalf("expected buyer 3 QR, got ok=%v code=%q", ok, code)
	}
	close(release2)

	// Wait briefly for ticket 3 persistence.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap3, _ := q.Get(t3.Token)
		if snap3.Status == StatusConnected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if maxConcurrent.Load() != 1 {
		t.Fatalf("expected strictly sequential pairing (maxConcurrent=1), got %d", maxConcurrent.Load())
	}

	// Verify automatic buyer owner + subscription expiry binding in BotStore.
	tenant1, err := store.GetTenant("6281110001")
	if err != nil || tenant1.OwnerJID != "62890001" || tenant1.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("expected buyer 1 tenant bound with owner & expiry, got %+v", tenant1)
	}
}
