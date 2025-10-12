package dedupe

import (
	"context"
	"sync"
	"time"
)

type memEntry struct {
	expireAt int64 // unix nano
}

type Memory struct {
	ttl     time.Duration
	mu      sync.RWMutex
	items   map[string]memEntry
	stopCh  chan struct{}
	stopped bool
}

// for dev(one instance);
// ttl-how long to store see id;
// janitorEvery-how long clear expired key; 0-> don't run collector
func NewMemory(ttl, janitorEvery time.Duration) *Memory {
	m := &Memory{
		ttl:    ttl,
		items:  make(map[string]memEntry, 1024),
		stopCh: make(chan struct{}),
	}

	if janitorEvery > 0 {
		go m.janitor(janitorEvery)
	}

	return m
}

func (m *Memory) Seen(_ context.Context, id string) (bool, error) {
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
	return false, nil
}

func (m *Memory) janitor(every time.Duration) {
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
					delete(m.items, k)
				}
			}
			m.mu.Unlock()
		}
	}
}

// Close garbage collector(if running)
func (m *Memory) Close() {
	m.mu.Lock()
	if !m.stopped {
		close(m.stopCh)
		m.stopped = true
	}
	m.mu.Unlock()
}
