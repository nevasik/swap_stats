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
	cfg  config.ClickHouseConfig

	inCh      chan RawSwapRow
	closedCh  chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func NewWriter(alert alerting.Alerting, conn ch.Conn, cfg config.ClickHouseConfig) *Writer {
	// sane defaults
	if cfg.Writer.BatchMaxRows <= 0 {
		cfg.Writer.BatchMaxRows = 1000
	}
	if cfg.Writer.BatchMaxInterval <= 0 {
		cfg.Writer.BatchMaxInterval = 200 * time.Millisecond
	}
	if cfg.Writer.MaxRetries < 0 {
		cfg.Writer.MaxRetries = 0
	}
	if cfg.Writer.RetryBackoff <= 0 {
		cfg.Writer.RetryBackoff = 200 * time.Millisecond
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

	batch := make([]RawSwapRow, 0, w.cfg.Writer.BatchMaxRows)
	ticker := time.NewTicker(w.cfg.Writer.BatchMaxInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		if err := w.insertBatch(context.Background(), batch); err != nil {
			// TODO прокинуть метрики
			w.alert.ErrorfLogAndAlert("Failed insert [%d] rows by batch to clickhouse, error=%v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case row, ok := <-w.inCh:
			if !ok {
				flush()
				return
			}

			batch = append(batch, row)
			if len(batch) >= w.cfg.Writer.BatchMaxRows {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.closedCh:
		}
	}
}

func (w *Writer) insertBatch(ctx context.Context, rows []RawSwapRow) error {
	if len(rows) == 0 {
		return nil
	}

	// repeat wuth exponential delay
	backoff := w.cfg.Writer.RetryBackoff

	var lastErr error

	for attempt := 0; attempt <= w.cfg.Writer.MaxRetries; attempt++ {
		batch, err := w.conn.PrepareBatch(ctx, `
			INSERT INTO raw_swaps (
				event_time,
				chain_id,
				tx_hash,
				log_index,
				event_id,
				token_address,
				token_symbol,
				pool_address,
				side,
				amount_token,
				amount_usd,
				block_number,
				removed,
				schema_version
			)
		`)
		if err != nil {
			lastErr = err
			goto retry
		}

		for i := range rows {
			r := &rows[i]
			var removed uint8
			if r.Removed {
				removed = 1
			}

			if err = batch.Append(
				r.EventTime,
				r.ChainID,
				r.TxHash,
				r.LogIndex,
				r.EventID,
				r.TokenAddress,
				r.TokenSymbol,
				r.PoolAddress,
				r.Side,
				r.AmountToken,
				r.AmountUSD,
				r.BlockNumber,
				removed,
				r.SchemaVersion,
			); err != nil {
				lastErr = err
				_ = batch.Abort()
				goto retry
			}
		}

		if err = batch.Send(); err != nil {
			lastErr = err
			goto retry
		}
		// success
		return nil

	retry:
		if attempt == w.cfg.Writer.MaxRetries {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
	}

	return lastErr
}
