package dedupe

import (
	"context"
	"sync"
	"time"

	"gitlab.com/nevasik7/alerting/logger"
)

type memEntry struct {
	expireAt int64 // unix nano
}

type MemoryDedupe struct {
	log     logger.Logger
	ttl     time.Duration
	mu      sync.RWMutex
	items   map[string]memEntry
	stopCh  chan struct{}
	stopped bool
}

// for dev(one instance);
// ttl-how long to store see id;
// janitorEvery-how long clear expired key; 0-> don't run collector
func NewInMemoryDedupe(log logger.Logger, ttl, janitorEvery time.Duration) *MemoryDedupe {
	m := &MemoryDedupe{
		log:    log,
		ttl:    ttl,
		items:  make(map[string]memEntry, 1024),
		stopCh: make(chan struct{}),
	}

	if janitorEvery > 0 {
		go m.janitor(janitorEvery)
	}

	return m
}

func (m *MemoryDedupe) Seen(_ context.Context, id string) (bool, error) {
	now := time.Now().UnixNano()
	exp := now + m.ttl.Nanoseconds()

	m.mu.Lock()
	defer m.mu.Unlock()

	// if exists and not expired
	if e, ok := m.items[id]; ok && e.expireAt > now {
		return true, nil
	}

	// write/update
	m.items[id] = memEntry{
		expireAt: exp,
	}

	m.log.Debugf("Write to items by key=%s", id)

	return false, nil
}

func (m *MemoryDedupe) janitor(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-t.C:
			now := time.Now().UnixNano()
			m.mu.Lock()
			for k, e := range m.items {
				if e.expireAt <= now {
					m.log.Debugf("Removing expired item: %s", k)
					delete(m.items, k)
				}
			}
			m.mu.Unlock()
		}
	}
}

// Close garbage collector(if running)
func (m *MemoryDedupe) Close() {
	m.mu.Lock()
	if !m.stopped {
		close(m.stopCh)
		m.stopped = true
	}
	m.mu.Unlock()
}
