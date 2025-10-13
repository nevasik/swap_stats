package dedupe

import (
	"context"
	"sync"
	"testing"
	"time"

	loggerCfg "gitlab.com/nevasik7/alerting/config"
	"gitlab.com/nevasik7/alerting/logger"
)

// --- helpers ---

func newTestLogger() logger.Logger {
	return logger.New(loggerCfg.LoggerCfg{
		Level:  "error",
		Format: "json",
	})
}

// --- tests ---

// First call Seen -> false (first), second -> true (exists).
func TestMemoryDedupe_FirstSeenThenDuplicate(t *testing.T) {
	t.Parallel()

	lg := newTestLogger()
	m := NewInMemoryDedupe(lg, 200*time.Millisecond, 0)
	defer m.Close()

	ctx := context.Background()
	const id = "tx:1:log:1"

	seen, err := m.Seen(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen {
		t.Fatalf("expected first Seen=false, got true")
	}

	seen, err = m.Seen(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !seen {
		t.Fatalf("expected second Seen=true (duplicate), got false")
	}
}

// ttl key: after TTL pair is expired and call Seen return false (first after expired)
func TestMemoryDedupe_Expiration(t *testing.T) {
	t.Parallel()

	lg := newTestLogger()
	ttl := 50 * time.Millisecond
	m := NewInMemoryDedupe(lg, ttl, 0)
	defer m.Close()

	ctx := context.Background()
	const id = "tx:2:log:7"

	seen, _ := m.Seen(ctx, id)
	if seen {
		t.Fatalf("first Seen must be false")
	}

	time.Sleep(ttl + 20*time.Millisecond)

	seen, _ = m.Seen(ctx, id)
	if seen {
		t.Fatalf("after TTL expired, Seen must be false again (reinsert), got true")
	}
}

// check clear map
func TestMemoryDedupe_JanitorCleansUp(t *testing.T) {
	t.Parallel()

	lg := newTestLogger()
	ttl := 20 * time.Millisecond
	janitorEvery := 15 * time.Millisecond

	m := NewInMemoryDedupe(lg, ttl, janitorEvery)
	defer m.Close()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _ = m.Seen(ctx, "k-"+time.Now().String())
	}

	// Ждём, чтобы элементы протухли и чтобы janitor успел сработать.
	time.Sleep(ttl + 2*janitorEvery)

	m.mu.RLock()
	size := len(m.items)
	m.mu.RUnlock()

	if size != 0 {
		t.Fatalf("expected janitor to clean expired items, but map size=%d", size)
	}
}

// Идемпотентность Close() и отсутствие гонок при остановке.
func TestMemoryDedupe_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	lg := newTestLogger()
	m := NewInMemoryDedupe(lg, 50*time.Millisecond, 10*time.Millisecond)

	// дважды без паники
	m.Close()
	m.Close()
}

// Cuncurrency tests
func TestMemoryDedupe_ConcurrentSameID(t *testing.T) {
	t.Parallel()

	lg := newTestLogger()
	m := NewInMemoryDedupe(lg, 500*time.Millisecond, 0)
	defer m.Close()

	ctx := context.Background()
	const id = "same-id"
	const workers = 64

	var wg sync.WaitGroup
	wg.Add(workers)

	var firstCount int64 // how false
	var dupCount int64   // how true

	var mu sync.Mutex
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			seen, err := m.Seen(ctx, id)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			mu.Lock()
			if seen {
				dupCount++
			} else {
				firstCount++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if firstCount != 1 {
		t.Fatalf("expected exactly one first insert (false), got %d", firstCount)
	}
	if dupCount != workers-1 {
		t.Fatalf("expected %d duplicates (true), got %d", workers-1, dupCount)
	}
}

// Not race and panic
func TestMemoryDedupe_ConcurrentDifferentIDs(t *testing.T) {
	t.Parallel()

	lg := newTestLogger()
	m := NewInMemoryDedupe(lg, 500*time.Millisecond, 0)
	defer m.Close()

	ctx := context.Background()

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		id := time.Now().Format(time.RFC3339Nano) + "-" + randomSuffix(i)
		go func(k string) {
			defer wg.Done()
			seen, err := m.Seen(ctx, k)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if seen {
				t.Errorf("first Seen for %s must be false", k)
			}
		}(id)
		time.Sleep(100 * time.Microsecond) // min
	}
	wg.Wait()
}

// little stable suffix don't rand
func randomSuffix(i int) string {
	return time.Duration(i).String()
}
