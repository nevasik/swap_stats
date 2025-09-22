package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App struct {
		InstanceID       string        `yaml:"instance_id"`
		SnapshotInterval time.Duration `yaml:"snapshot_interval"`
		Grace            time.Duration `yaml:"grace"`
		DedupeTTL        time.Duration `yaml:"dedupe_ttl"`
	} `yaml:"app"`

	Logger struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"logging"`

	Alerting struct {
		AppName string `yaml:"app_name"`
		Token   string `yaml:"token"`
		ChatID  string `yaml:"chat_id"`
	} `yaml:"alerting"`

	Security struct {
		JWT JWT `yaml:"jwt"`
	} `yaml:"security"`

	RateLimit struct {
		ByJWT RL `yaml:"by_jwt"`
		ByIP  RL `yaml:"by_ip"`
	} `yaml:"rate_limit"`

	Ingest struct {
		BrokerType       string        `yaml:"broker_type"`
		Brokers          []string      `yaml:"brokers"`
		Topic            string        `yaml:"topic"`
		GroupID          string        `yaml:"group_id"`
		Start            string        `yaml:"start"`
		MaxBytes         int           `yaml:"max_bytes"`
		MaxInflight      int           `yaml:"max_inflight"`
		SessionTimeout   time.Duration `yaml:"session_timeout"`
		RebalanceTimeout time.Duration `yaml:"rebalance_timeout"`
	} `yaml:"ingest"`

	Stores struct {
		Redis      Redis      `yaml:"redis"`
		ClickHouse ClickHouse `yaml:"clickhouse"`
	} `yaml:"stores"`

	PubSub struct {
		Nats Nats `yaml:"nats"`
	} `yaml:"pubsub"`

	API struct {
		HTTP struct {
			Addr string `yaml:"addr"`
		} `yaml:"http"`
		WS struct {
			CoalesceMS int `yaml:"coalesce_ms"`
			MaxConn    int `yaml:"max_conn"`
		} `yaml:"ws"`
	} `yaml:"api"`

	Metrics struct {
		Prometheus string `yaml:"prometheus"`
		Pprof      string `yaml:"pprof"`
	} `yaml:"metrics"`
}

type RL struct {
	RefillPerSec int `yaml:"refill_per_sec"`
	Burst        int `yaml:"burst"`
}

type JWT struct {
	Enabled       bool     `yaml:"enabled"`
	Alg           string   `yaml:"alg"`
	PublicKeyPath string   `yaml:"public_key_path"`
	Audiences     []string `yaml:"audiences"`
}

type Redis struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	Prefix   string `yaml:"prefix"`
}

type ClickHouse struct {
	DSN    string           `yaml:"dsn"`
	Writer ClickHouseWriter `yaml:"writer"`
}

type ClickHouseWriter struct {
	BatchMaxRows     int           `yaml:"batch_max_rows"`
	BatchMaxInterval time.Duration `yaml:"batch_max_interval"`
	MaxRetries       int           `yaml:"max_retries"`
	RetryBackoff     time.Duration `yaml:"retry_backoff"`
}

type Nats struct {
	URL      string `yaml:"url"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Stream   string `yaml:"stream"`
	EnableJS bool   `yaml:"enable_js"`
}

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
