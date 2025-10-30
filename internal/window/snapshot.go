package window

import (
	"bytes"
	"dexcelerate/internal/domain"
	"encoding/gob"
	"errors"
	"fmt"
	"time"
)

// Represents a serializable snapshot of all windows to be saved in Redis
// This allows you to do a “warm start” after a service restart in milliseconds
type Snapshot struct {
	Version int
	TakenAt time.Time                // snapshot creation time
	GraceMs int64                    // grace period in millisecond (for validation during recovery)
	WM      time.Time                // watermark at the time of snapshot
	Tokens  map[string]snapshotToken // state of all tokens (key = "chainID:address")
}

// Contains a compact representation of the state of a single token
type snapshotToken struct {
	ChainID      uint32         // id network
	TokenAddress string         // address token
	Slots        []snapshotSlot // include not empty min slots
	LastUpdated  time.Time      // last update time
}

// Represents one minute data slot
type snapshotSlot struct {
	Minute int // index minute (0..1439)
	VolUSD float64
	Trades int64
	Buys   int64
	Sells  int64
}

// Use gob encoding for effective serialize struct
func marshalSnapshot(state map[string]*tokenState, watermark time.Time, grace time.Duration) ([]byte, error) {
	snap := Snapshot{
		Version: 1,
		TakenAt: time.Now().UTC(),
		GraceMs: grace.Milliseconds(),
		WM:      watermark,
		Tokens:  make(map[string]snapshotToken, len(state)),
	}

	for key, ts := range state {
		if ts == nil {
			continue
		}

		compactSlots := make([]snapshotSlot, 0, 1440)
		for i, slot := range ts.slots {
			if slot.trades == 0 && slot.volUSD == 0 && slot.buys == 0 && slot.sells == 0 { // empty slots
				continue
			}

			compactSlots = append(compactSlots, snapshotSlot{
				Minute: i,
				VolUSD: slot.volUSD,
				Trades: slot.trades,
				Buys:   slot.buys,
				Sells:  slot.sells,
			})
		}

		snap.Tokens[key] = snapshotToken{
			ChainID:      ts.key.ChainID,
			TokenAddress: ts.key.TokenAddress,
			Slots:        compactSlots,
			LastUpdated:  ts.lastUpdated,
		}
	}

	// serialize to gob
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(snap); err != nil {
		return nil, fmt.Errorf("failed to encode snapshot: %w", err)
	}

	return buf.Bytes(), nil
}

// Deserializes bytes from Redis back to windows state
func unmarshalSnapshot(data []byte, bucketsPerDay int) (map[string]*tokenState, time.Time, error) {
	if len(data) == 0 {
		return nil, time.Time{}, errors.New("empty snapshot data")
	}

	var snap Snapshot
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&snap); err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to decode snapshot: %w", err)
	}

	if snap.Version != 1 {
		return nil, time.Time{}, fmt.Errorf("unsupported snapshot version: %d", snap.Version)
	}

	state := make(map[string]*tokenState, len(snap.Tokens))

	for key, snapToken := range snap.Tokens {
		ts := newTokenState(
			domain.TokenKey{
				ChainID:      snapToken.ChainID,
				TokenAddress: snapToken.TokenAddress,
			},
			bucketsPerDay,
		)
		ts.lastUpdated = snapToken.LastUpdated

		for _, slot := range snapToken.Slots {
			if slot.Minute < 0 || slot.Minute >= bucketsPerDay {
				continue
			}

			ts.slots[slot.Minute] = deltaAgg{
				volUSD: slot.VolUSD,
				trades: slot.Trades,
				buys:   slot.Buys,
				sells:  slot.Sells,
			}
		}

		ts.recomputeWindows(snap.TakenAt)
		state[key] = ts
	}

	return state, snap.WM, nil
}
