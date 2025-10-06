package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App struct {
		InstanceID       string        `yaml:"instance_id"`
		Grace            time.Duration `yaml:"grace"`
		SnapshotInterval time.Duration `yaml:"snapshot_interval"`
		ShutdownTimeout  time.Duration `yaml:"shutdown_timeout"`
	} `yaml:"app"`

	Logging struct {
		Level  string `yaml:"level"`  // debug|info|warn|error
		Format string `yaml:"format"` // json|console
	} `yaml:"logging"`

	Alerting struct {
		AppName string `yaml:"app_name"`
		Token   string `yaml:"token"`
		ChatID  string `yaml:"chat_id"`
	} `yaml:"alerting"`

	Security struct {
		JWT struct {
			Enabled        bool   `yaml:"enabled"`
			Alg            string `yaml:"alg"` // RS256
			PublicKeyPath  string `yaml:"public_key_path"`
			PrivateKeyPath string `yaml:"private_key_path"`
			Audience       string `yaml:"audience"`
			Issuer         string `yaml:"issuer"`
		} `yaml:"jwt"`
	} `yaml:"security"`

	RateLimit struct {
		ByJWT struct {
			RefillPerSec int `yaml:"refill_per_sec"`
			Burst        int `yaml:"burst"`
		} `yaml:"by_jwt"`
		ByIP struct {
			RefillPerSec int `yaml:"refill_per_sec"`
			Burst        int `yaml:"burst"`
		} `yaml:"by_ip"`
	} `yaml:"rate_limit"`

	Ingest struct {
		BrokerType       string        `yaml:"broker_type"` // redpanda|kafka
		Brokers          []string      `yaml:"brokers"`
		Topic            string        `yaml:"topic"`
		GroupID          string        `yaml:"group_id"`
		Start            string        `yaml:"start"`
		MaxBytes         int           `yaml:"max_bytes"`
		MaxInflight      int           `yaml:"max_inflight"`
		SessionTimeout   time.Duration `yaml:"session_timeout"`
		RebalanceTimeout time.Duration `yaml:"rebalance_timeout"`
		TLS              struct {
			Enabled  bool   `yaml:"enabled"`
			CAFile   string `yaml:"ca_file"`
			CertFile string `yaml:"cert_file"`
			KeyFile  string `yaml:"key_file"`
		} `yaml:"tls"`
	} `yaml:"ingest"`

	Dedupe struct {
		TTL time.Duration `yaml:"ttl"`
	} `yaml:"dedupe"`

	Stores struct {
		Redis struct {
			Addr         string        `yaml:"addr"`
			Username     string        `yaml:"username"`
			Password     string        `yaml:"password"`
			DB           int           `yaml:"db"`
			Prefix       string        `yaml:"prefix"`
			DialTimeout  time.Duration `yaml:"dial_timeout"`
			ReadTimeout  time.Duration `yaml:"read_timeout"`
			WriteTimeout time.Duration `yaml:"write_timeout"`
		} `yaml:"redis"`
		ClickHouse struct {
			DSN    string `yaml:"dsn"`
			Writer struct {
				BatchMaxRows     int           `yaml:"batch_max_rows"`
				BatchMaxInterval time.Duration `yaml:"batch_max_interval"`
				MaxRetries       int           `yaml:"max_retries"`
				RetryBackoff     time.Duration `yaml:"retry_backoff"`
			} `yaml:"writer"`
		} `yaml:"clickhouse"`
	} `yaml:"stores"`

	PubSub struct {
		NATS struct {
			URL             string `yaml:"url"`
			BroadcastPrefix string `yaml:"broadcast_prefix"`
		} `yaml:"nats"`
	} `yaml:"pubsub"`

	API struct {
		HTTP struct {
			Addr         string        `yaml:"addr"`
			ReadTimeout  time.Duration `yaml:"read_timeout"`
			WriteTimeout time.Duration `yaml:"write_timeout"`
			IdleTimeout  time.Duration `yaml:"idle_timeout"`
			CORS         struct {
				Enabled bool     `yaml:"enabled"`
				Origins []string `yaml:"origins"`
				Methods []string `yaml:"methods"`
				Headers []string `yaml:"headers"`
			} `yaml:"cors"`
		} `yaml:"http"`
		WS struct {
			CoalesceMS        int           `yaml:"coalesce_ms"`
			MaxConn           int           `yaml:"max_conn"`
			ReadLimitBytes    int64         `yaml:"read_limit_bytes"`
			WriteTimeout      time.Duration `yaml:"write_timeout"`
			HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
		} `yaml:"ws"`
	} `yaml:"api"`

	Metrics struct {
		Prometheus string `yaml:"prometheus"`
		PPROF      string `yaml:"pprof"`
	} `yaml:"metrics"`
}

// FIXME rewrite read secret from Vault engine
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err = yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
