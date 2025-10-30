package window

import (
	"context"
	"dexcelerate/internal/config"
	"dexcelerate/internal/domain"
	"errors"
	"fmt"
	"sync"
	"time"

	"gitlab.com/nevasik7/alerting/logger"
)

/*
	Engine "alive" window(5m/1h/24h) on top on the minutes buckets
	Interface required for Redpanda and HTTP/WS
*/

type WindowEngine interface {
	Apply(ctx context.Context, ev *domain.SwapEvent) ([]*domain.TokenStatsPatch, error)
	GetWindows(ctx context.Context, key *domain.TokenKey) (*domain.Windows, bool)
	Snapshot(ctx context.Context) ([]byte, error)
	Restore(ctx context.Context, data []byte) error
	Tick(now time.Time)
}

var (
	// event oldest current watermark(don't apply)
	ErrTooLate = errors.New("event older than watermark")
)

type Window struct {
	Log           logger.Logger
	Grace         time.Duration // example, 120s for expired event
	BucketsPerDay int           // count minutes buckets (by default 1440)
	CoerceToUTC   bool          // if need cast time event to UTC

	mw        sync.RWMutex           // protection from race condition by concurrent access
	state     map[string]*tokenState // state all tokens (key = "chainID:address")
	watermark *Watermark             // lower limit permission age of event
}

func NewWindowEngine(log logger.Logger, cfg *config.WindowConfig) (WindowEngine, error) {
	if cfg == nil {
		return nil, errors.New("config is required to the window engine")
	}

	bucketsPerDay := cfg.BucketsPerDay
	if bucketsPerDay <= 0 {
		bucketsPerDay = 1440 // by default 24 hours * 60 minutes
	}

	grace := cfg.Grace
	if grace <= 0 {
		grace = 2 * time.Minute // by default 2 minutes grace period
	}

	watermark := newWatermark(grace)

	return &Window{
		Log:           log,
		Grace:         grace,
		BucketsPerDay: bucketsPerDay,
		CoerceToUTC:   cfg.CoerceToUTC,
		state:         make(map[string]*tokenState, 1024),
		watermark:     watermark,
	}, nil
}

// Processes new event swap and update sliding windows; Return patches for send to WS/NATS + batch to CH
func (w *Window) Apply(ctx context.Context, ev *domain.SwapEvent) ([]*domain.TokenStatsPatch, error) {
	eventTime := ev.EventTime
	if w.CoerceToUTC {
		eventTime = eventTime.UTC()
	}

	// check watermark
	if w.watermark.IsLate(eventTime) {
		w.Log.Debugf("Event %s is too late (ts=%s, watermark=%s)", ev.EventID, eventTime, w.watermark.current)
		return nil, ErrTooLate
	}

	tokenKey := domain.TokenKey{
		ChainID:      ev.ChainID,
		TokenAddress: ev.TokenAddress,
	}
	stateKey := makeStateKey(&tokenKey)

	volumeUSD, err := parseDecimal(ev.AmountUSD)
	if err != nil {
		w.Log.Errorf("Failed to parse amount_usd for event %s: %v", ev.EventID, err)
		return nil, fmt.Errorf("invalid amount_usd: %w", err)
	}

	delta := &deltaAgg{
		volUSD: volumeUSD,
		trades: 1,
	}

	switch ev.Side {
	case domain.SideBuy:
		delta.buys = 1
	case domain.SideSell:
		delta.sells = 1
	}

	// handle reorg: if event delete, invert delta
	if ev.Removed {
		delta.volUSD = -delta.volUSD
		delta.trades = -delta.trades
		delta.buys = -delta.buys
		delta.sells = -delta.sells
	}

	w.mw.Lock()
	defer w.mw.Unlock()

	// get or create state token
	ts, exists := w.state[stateKey]
	if !exists {
		ts = newTokenState(tokenKey, w.BucketsPerDay)
		w.state[stateKey] = ts
	}

	slotIdx := minuteIndex(eventTime)
	ts.applyDelta(slotIdx, delta, time.Now().UTC())

	// forming a patch for WebSocket/NATS client
	patch := &domain.TokenStatsPatch{
		Topic:       fmt.Sprintf("token:%s", ev.TokenSymbol),
		Token:       tokenKey,
		GeneratedAt: time.Now().UTC(),
	}

	// filling update window
	w5m := ts.w5m.toDomainAgg()
	w1h := ts.w1h.toDomainAgg()
	w24h := ts.w24h.toDomainAgg()

	patch.Windows.W5m = &w5m
	patch.Windows.W1h = &w1h
	patch.Windows.W24h = &w24h

	return []*domain.TokenStatsPatch{patch}, nil
}

// Get current value sliding windows for token; use get http handler
func (w *Window) GetWindows(ctx context.Context, key *domain.TokenKey) (*domain.Windows, bool) {
	// TODO: not use ctx
	stateKey := makeStateKey(key)

	w.mw.RLock()
	defer w.mw.RUnlock()

	ts, exists := w.state[stateKey]
	if !exists {
		return nil, false
	}

	return ts.getWindows(), true
}

// Serialize current state all window for save to Redis; Use for "warm start" after restart service
func (w *Window) Snapshot(ctx context.Context) ([]byte, error) {
	w.mw.RLock()
	defer w.mw.RUnlock()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context canceled: %w", ctx.Err())
	default:
	}

	// get current watermark
	var wm time.Time
	if w.watermark.initted {
		wm = w.watermark.current
	}

	data, err := marshalSnapshot(w.state, wm, w.Grace)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	w.Log.Infof("Created snapshot: %d tokens, %d bytes", len(w.state), len(data))
	return data, nil
}

// Restore state window from snapshot (from Redis); Run when start service for quick recovery
func (w *Window) Restore(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return errors.New("empty snapshot data")
	}

	state, wm, err := unmarshalSnapshot(data, w.BucketsPerDay)
	if err != nil {
		return fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	w.mw.Lock()
	defer w.mw.Unlock()

	w.state = state

	if !wm.IsZero() {
		w.watermark.current = wm
		w.watermark.initted = true
	}

	w.Log.Infof("Restored snapshot: %d tokens, watermark=%s", len(state), wm)
	return nil
}

// Promotes watermark and clears outdated slots
func (w *Window) Tick(now time.Time) {
	now = now.UTC()

	w.mw.Lock()
	defer w.mw.Unlock()

	w.watermark.Advance(now)

	currentMinute := minuteIndex(now) // clearing old slots that are outside the 24-hour window

	for _, ts := range w.state {
		ts.slots[currentMinute] = deltaAgg{}
		// recalc scaling window
		ts.recomputeWindows(now)
	}

	w.Log.Debugf("Tick: watermark=%s, tokens=%d", w.watermark.current, len(w.state))
}

// Create str key for save state token in map
func makeStateKey(key *domain.TokenKey) string {
	return fmt.Sprintf("%d:%s", key.ChainID, key.TokenAddress)
}

// Parses decimal string (example "123.456") в float64
func parseDecimal(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}

	var val float64
	_, err := fmt.Sscanf(s, "%f", &val)
	return val, err
}
