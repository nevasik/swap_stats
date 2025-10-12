package clickhouse

import (
	"context"
	"dexcelerate/internal/config"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

type Conn struct {
	Native ch.Conn
}

func New(ctx context.Context, cfg *config.ClickHouseConfig) (*Conn, error) {
	if cfg == nil {
		return nil, fmt.Errorf("clickhouse config cannot be nil")
	}
	opts, err := ch.ParseDSN(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed parse DSN ch, error=%w", err)
	}

	if opts.DialTimeout == 0 {
		opts.DialTimeout = 5 * time.Second
	}

	if opts.Compression == nil {
		opts.Compression = &ch.Compression{Method: ch.CompressionLZ4}
	}

	opts.ClientInfo = ch.ClientInfo{
		Products: []struct{ Name, Version string }{
			{
				Name:    "swap-stats",
				Version: "0.1.0",
			},
		},
	}

	conn, err := ch.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed Open ch, error=%w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err = conn.Ping(pingCtx); err != nil {
		return nil, fmt.Errorf("failed ping ch, error=%w", err)
	}

	return &Conn{Native: conn}, nil
}

func (c *Conn) Close() error {
	return c.Native.Close()
}
