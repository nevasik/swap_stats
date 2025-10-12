//go:build ignore

// Run: go run ./build-tools/loadgen.go -brokers localhost:9092 -topic raw-swaps -rps 1000 -duration 60s -tokens USDC,ETH,WBTC

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	mrand "math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/IBM/sarama"
)

type SwapEvent struct {
	ChainID      uint32 `json:"chain_id"`
	TxHash       string `json:"tx_hash"`
	LogIndex     uint32 `json:"log_index"`
	EventID      string `json:"event_id"`
	TokenAddress string `json:"token_address"`
	TokenSymbol  string `json:"token_symbol"`
	PoolAddress  string `json:"pool_address"`
	Side         string `json:"side"`
	AmountToken  string `json:"amount_token"`
	AmountUSD    string `json:"amount_usd"`
	EventTime    string `json:"event_time"` // RFC3339
	BlockNumber  uint64 `json:"block_number"`
	Removed      bool   `json:"removed"`
	SchemaVer    uint16 `json:"schema_version"`
}

func main() {
	var (
		brokers  = flag.String("brokers", "localhost:9092", "comma-separated list of brokers")
		topic    = flag.String("topic", "raw-swaps", "topic name")
		rps      = flag.Int("rps", 1000, "events per second target")
		duration = flag.Duration("duration", 30*time.Second, "how long to run")
		tokens   = flag.String("tokens", "USDC,ETH,WBTC,DAI", "comma-separated token symbols")
		chainID  = flag.Uint("chain", 1, "chain id")
	)
	flag.Parse()

	tokenSymbols := splitTrim(*tokens)
	if len(tokenSymbols) == 0 {
		fmt.Println("no tokens provided")
		os.Exit(1)
	}

	cfg := sarama.NewConfig()
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Compression = sarama.CompressionSnappy
	cfg.Producer.Return.Successes = false
	cfg.Producer.Return.Errors = true
	cfg.Producer.Idempotent = false
	cfg.Producer.Flush.Frequency = 20 * time.Millisecond
	cfg.Producer.Flush.MaxMessages = 1000
	cfg.Producer.Partitioner = sarama.NewHashPartitioner
	cfg.Version = sarama.V2_3_0_0

	cli, err := sarama.NewAsyncProducer(strings.Split(*brokers, ","), cfg)
	if err != nil {
		fmt.Printf("producer init error: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = cli.Close() }()

	// error logger
	go func() {
		for e := range cli.Errors() {
			fmt.Printf("produce error: %v\n", e.Err)
		}
	}()

	fmt.Printf("loadgen → brokers=%s topic=%s rps=%d duration=%s\n", *brokers, *topic, *rps, duration.String())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	end := start.Add(*duration)

	// steady pace with a little drift
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	perTick := float64(*rps) / 10.0 // 10 ticket in sec
	accum := 0.0

loop:
	for {
		select {
		case <-ctx.Done():
			fmt.Println("signal received, stopping…")
			break loop
		case now := <-tick.C:
			if now.After(end) {
				break loop
			}

			accum += perTick
			batch := int(math.Floor(accum))
			if batch <= 0 {
				continue
			}
			accum -= float64(batch)

			// send batch message
			for i := 0; i < batch; i++ {
				ev := randomEvent(uint32(*chainID), tokenSymbols)
				key := sarama.StringEncoder(ev.EventID)
				val, _ := json.Marshal(ev)
				cli.Input() <- &sarama.ProducerMessage{
					Topic: *topic,
					Key:   key,
					Value: sarama.ByteEncoder(val),
				}
			}
		}
	}

	// drain and close
	fmt.Println("flushing…")
	time.Sleep(500 * time.Millisecond)
	fmt.Println("done")
}

func randomEvent(chainID uint32, tokens []string) *SwapEvent {
	now := time.Now().UTC()
	token := tokens[mrand.Intn(len(tokens))]

	// imitation what exists pool and tokens
	tx := "0x" + randHex(64)
	logIndex := uint32(mrand.Intn(20))
	eventID := fmt.Sprintf("%d:%s:%d", chainID, tx, logIndex)
	pool := "0x" + randHex(40)
	tokenAddr := "0x" + randHex(40)

	side := "buy"
	if mrand.Intn(2) == 0 {
		side = "sell"
	}

	amountToken := fmt.Sprintf("%.6f", 10+mrand.Float64()*1000)
	amountUSD := fmt.Sprintf("%.2f", 10+mrand.Float64()*10000)

	return &SwapEvent{
		ChainID:      chainID,
		TxHash:       tx,
		LogIndex:     logIndex,
		EventID:      eventID,
		TokenAddress: tokenAddr,
		TokenSymbol:  token,
		PoolAddress:  pool,
		Side:         side,
		AmountToken:  amountToken,
		AmountUSD:    amountUSD,
		EventTime:    now.Format(time.RFC3339Nano),
		BlockNumber:  uint64(20_000_000 + mrand.Intn(1_000_000)),
		Removed:      false,
		SchemaVer:    1,
	}
}

func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func randHex(n int) string {
	b := make([]byte, n/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
