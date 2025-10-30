package window

import (
	"dexcelerate/internal/domain"
	"time"
)

// View delta for one minutes slots in the ring buffer; Use for incremental update sliding window O(1)
type deltaAgg struct {
	volUSD float64 // volume in USD for minute
	trades int64   // count transaction for minute
	buys   int64   // count buy for minute
	sells  int64   // count sales for minute
}

// Present aggregate statistics for period; Use for sliding window (5m/1h/24h)
type agg struct {
	VolUSD float64 `json:"vol_usd"`
	Trades int64   `json:"trades"`
	Buys   int64   `json:"buy"`
	Sells  int64   `json:"sell"`
}

// Add delta to aggregate (incremental update)
func (a *agg) add(d *deltaAgg) {
	a.VolUSD += d.volUSD
	a.Trades += d.trades
	a.Buys += d.buys
	a.Sells += d.sells
}

// Subtract delta from aggregate (when the old slot falls out of the window)
func (a *agg) sub(d *deltaAgg) {
	a.VolUSD -= d.volUSD
	a.Trades -= d.trades
	a.Buys -= d.buys
	a.Sells -= d.sells
}

// Convert internal agg to domain.Agg for external API
func (a *agg) toDomainAgg() domain.Agg {
	return domain.Agg{
		VolumeUSD: a.VolUSD,
		Trades:    uint64(a.Trades),
		Buy:       uint64(a.Buys),
		Sell:      uint64(a.Sells),
	}
}

// Store full state sliding window for one token; it's ring buffer from 1440 minutes slots (24 hour) + increment sum
type tokenState struct {
	key domain.TokenKey // token identifier (ChainID + Address)

	slots []deltaAgg
	head  int
	w5m   agg
	w1h   agg
	w24h  agg

	lastUpdated time.Time
}

// Create new state for token with empty slots
func newTokenState(key domain.TokenKey, bucketsPerDay int) *tokenState {
	return &tokenState{
		key:         key,
		slots:       make([]deltaAgg, bucketsPerDay),
		head:        0,
		lastUpdated: time.Now().UTC(),
	}
}

func (ts *tokenState) applyDelta(slotIdx int, delta *deltaAgg, now time.Time) {
	slot := &ts.slots[slotIdx]
	slot.volUSD += delta.volUSD
	slot.trades += delta.trades
	slot.buys += delta.buys
	slot.sells += delta.sells

	ts.w24h.add(delta)

	// check 1 hours window
	if ts.isInWindow(slotIdx, now, 60) {
		ts.w1h.add(delta)
	}

	// check 5 min window
	if ts.isInWindow(slotIdx, now, 5) {
		ts.w5m.add(delta)
	}

	ts.lastUpdated = now
}

func (ts *tokenState) isInWindow(slotIdx int, now time.Time, windowMinutes int) bool {
	nowMinute := minuteIndex(now)
	// calc distance from current min to slot (by module ring)
	dist := (nowMinute - slotIdx + len(ts.slots)) % len(ts.slots)
	return dist < windowMinutes
}

// Get current value all sliding windows
func (ts *tokenState) getWindows() *domain.Windows {
	return &domain.Windows{
		W5m:  ts.w5m.toDomainAgg(),
		W1h:  ts.w1h.toDomainAgg(),
		W24h: ts.w24h.toDomainAgg(),
	}
}

// Calc index minute slot for time
func minuteIndex(t time.Time) int {
	t = t.UTC()
	return (t.Hour()*60 + t.Minute()) % 1440
}

// Full recalculate sliding windows from slots; Use after Tick or recover from snapshot
func (ts *tokenState) recomputeWindows(now time.Time) {
	ts.w5m = agg{}
	ts.w1h = agg{}
	ts.w24h = agg{}

	nowMinute := minuteIndex(now)

	for i := 0; i < len(ts.slots); i++ {
		slot := &ts.slots[i]
		if slot.trades == 0 && slot.volUSD == 0 {
			continue
		}

		dist := (nowMinute - i + len(ts.slots)) % len(ts.slots)

		ts.w24h.add(slot)
		if dist < 60 {
			ts.w1h.add(slot)
		}
		if dist < 5 {
			ts.w5m.add(slot)
		}
	}
}
