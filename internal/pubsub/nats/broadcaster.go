package nats

import (
	"dexcelerate/internal/config"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"gitlab.com/nevasik7/alerting/logger"
)

type Client struct {
	nc  *nats.Conn
	log logger.Logger
}

func New(log logger.Logger, cfg *config.NATSConfig) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("nats config is required")
	}

	url := cfg.URL
	if url == "" {
		return nil, errors.New("nats url is required")
	}

	opts := []nats.Option{
		nats.Name("swap-stats"),
		nats.Timeout(5 * time.Second),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1), // endless reconnected
		nats.ReconnectWait(2 * time.Second),
	}

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &Client{
		nc:  nc,
		log: log,
	}, nil
}

func (c *Client) Ready() bool {
	if c.nc == nil {
		return false
	}
	return c.nc.Status() == nats.CONNECTED
}

func (c *Client) Status() nats.Status {
	if c.nc == nil {
		return nats.DISCONNECTED
	}
	return c.nc.Status()
}

func (c *Client) Close() error {
	if c.nc == nil {
		return nil
	}

	// check not close this conn
	if c.nc.Status() == nats.CLOSED {
		return nil
	}

	if err := c.nc.Drain(); err != nil {
		c.log.Errorf("Failed to drain connection to NATS, error=%v", err)
		c.nc.Close()
		return fmt.Errorf("failed to drain connection to NATS: %w", err)
	}

	c.nc.Close()
	c.log.Infof("NATS connection closed gracefully")
	return nil
}
