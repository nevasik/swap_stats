package domain

import "time"

// Raw swap event from the stream
type SwapEvent struct {
	ChainID      uint32    `json:"chain_id"`
	TxHash       string    `json:"tx_hash"` // 0x-prefixed 66 chars
	LogIndex     uint32    `json:"log_index"`
	EventID      string    `json:"event_id"`      // chain:tx_hash:logIndex(canon)
	TokenAddress string    `json:"token_address"` // 0x-prefixed 43 chars
	TokenSymbol  string    `json:"token_symbol"`  // lowCardinality in CH
	PoolAddress  string    `json:"pool_address"`  // 0x-prefixed 42 chars
	Side         Side      `json:"side"`          // buy|sell
	AmountToken  string    `json:"amount_token"`  // decimal(38,18) as string
	AmountUSD    string    `json:"amount_usd"`    // decimal(20,6) as string
	EventTime    time.Time `json:"event_time"`    // RFC3339/UTC
	BlockNumber  uint64    `json:"block_number"`
	Removed      bool      `json:"removed"` // reorg compensation flag
	SchemaVer    uint16    `json:"schema_version"`
}

type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// Canon key token for window/aggregate
type TokenKey struct {
	ChainID      uint32
	TokenAddress string // 0x42
}

// Min kit aggregate for window
type Agg struct {
	VolumeUSD float64
	Trades    uint64
	Buy       uint64
	Sell      uint64
}

// Snapshot current rolling-window
type Windows struct {
	W5m  Agg
	W1h  Agg
	W24h Agg
}

// Patch(delta/slice) for WS/cluster fan-out. We usually only send changed windows for a specific token
type TokenStatsPatch struct {
	Topic       string    `json:"topic"` // example: "token:<symbol>" or "token:<address>"
	Token       TokenKey  `json:"token"`
	GeneratedAt time.Time `json:"ts"`
	Windows     struct {
		W5m  *Agg `json:"w5m,omitempty"`
		W1h  *Agg `json:"w1h,omitempty"`
		W24h *Agg `json:"w24h,omitempty"`
	} `json:"windows"`
}
