package clickhouse

import (
	"context"
	"dexcelerate/internal/config"
	"errors"
	"sync"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"gitlab.com/nevasik7/alerting"
)

type RawSwapRow struct {
	EventTime     time.Time
	ChainID       uint32
	TxHash        string
	LogIndex      uint32
	EventID       string
	TokenAddress  string
	TokenSymbol   string
	PoolAddress   string
	Side          string
	AmountToken   string // Decimal(38,18) — send string
	AmountUSD     string // Decimal(20,6)  — send string
	BlockNumber   uint64
	Removed       bool // convert to UInt8
	SchemaVersion uint16
}

type Writer struct {
	alert alerting.Alerting

	conn ch.Conn
	cfg  config.ClickHouseWriter

	inCh      chan RawSwapRow
	closedCh  chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func NewWriter(alert alerting.Alerting, conn ch.Conn, cfg config.ClickHouseWriter) *Writer {
	// sane defaults
	if cfg.BatchMaxRows <= 0 {
		cfg.BatchMaxRows = 1000
	}
	if cfg.BatchMaxInterval <= 0 {
		cfg.BatchMaxInterval = 200 * time.Millisecond
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 200 * time.Millisecond
	}

	w := &Writer{
		alert:    alert,
		conn:     conn,
		cfg:      cfg,
		inCh:     make(chan RawSwapRow, 8192), // ring buffer = expected EPS peak * time_to_level off
		closedCh: make(chan struct{}),
	}

	w.wg.Add(1)
	go w.loop()

	return w
}

func (w *Writer) Enqueue(row RawSwapRow) error {
	select {
	case <-w.closedCh:
		return errors.New("clickhouse writer closed")
	default:
	}

	select {
	case w.inCh <- row:
		return nil
	case <-w.closedCh:
		return errors.New("clickhouse writer closed")
	}
}

func (w *Writer) Close(ctx context.Context) error {
	w.closeOnce.Do(func() {
		close(w.closedCh)
	})
	close(w.inCh)

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Writer) loop() {
	defer w.wg.Done()

	batch := make([]RawSwapRow, 0, w.cfg.BatchMaxRows)
	ticker := time.NewTicker(w.cfg.BatchMaxInterval)
	defer ticker.Stop()

	_ = func() {
		if len(batch) == 0 {
			return
		}

		if err := w.insertBatch(context.Background(), batch); err != nil {
			// TODO прокинуть метрики
			w.alert.ErrorfLogAndAlert("Failed insert [%d] rows by batch to clickhouse, error=%v", len(batch), err)
		}
		batch = batch[:0]
	}
}

func (w *Writer) insertBatch(ctx context.Context, batch []RawSwapRow) error {
	return nil
}
