package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App       AppConfig       `yaml:"app"`
	Logging   LoggingConfig   `yaml:"logging"`
	Alerting  AlertingConfig  `yaml:"alerting"`
	Security  SecurityConfig  `yaml:"security"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	Ingest    IngestConfig    `yaml:"ingest"`
	Window    WindowConfig    `yaml:"window"`
	Dedupe    DedupeConfig    `yaml:"dedupe"`
	Stores    StoresConfig    `yaml:"stores"`
	PubSub    PubSubConfig    `yaml:"pubsub"`
	API       APIConfig       `yaml:"api"`
	Metrics   MetricsConfig   `yaml:"metrics"`
}

type AppConfig struct {
	InstanceID string `yaml:"instance_id"`
	//SnapshotInterval time.Duration `yaml:"snapshot_interval"`
	//ShutdownTimeout  time.Duration `yaml:"shutdown_timeout"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug|info|warn|error
	Format string `yaml:"format"` // json|console
}

type AlertingConfig struct {
	AppName string `yaml:"app_name"`
	Token   string `yaml:"token"`
	ChatID  string `yaml:"chat_id"`
}
type SecurityConfig struct {
	JWT JWTConfig `yaml:"jwt"`
}

type JWTConfig struct {
	Enabled        bool          `yaml:"enabled"`
	PublicKeyPath  string        `yaml:"public_key_path"`
	PrivateKeyPath string        `yaml:"private_key_path"`
	Audience       string        `yaml:"audience"`
	Issuer         string        `yaml:"issuer"`
	Leeway         time.Duration `yaml:"leeway_timeout"`
}

type RateLimitConfig struct {
	ByJWT              RateBucket `yaml:"by_jwt"`
	ByIP               RateBucket `yaml:"by_ip"`
	TrustedProxiesList []string   `yaml:"trusted_proxies_list"`
}

type RateBucket struct {
	RefillPerSec int           `yaml:"refill_per_sec"`
	Burst        int           `yaml:"burst"` // max len bucket
	TTL          time.Duration `yaml:"ttl"`   // how long should you keep a key if it isn't use
}

type IngestConfig struct {
	BrokerType       string        `yaml:"broker_type"` // redpanda|kafka
	Brokers          []string      `yaml:"brokers"`
	Topic            string        `yaml:"topic"`
	GroupID          string        `yaml:"group_id"`
	Start            string        `yaml:"start"`
	MaxBytes         int           `yaml:"max_bytes"`
	MaxInflight      int           `yaml:"max_inflight"`
	SessionTimeout   time.Duration `yaml:"session_timeout"`
	RebalanceTimeout time.Duration `yaml:"rebalance_timeout"`
	TLS              TLSConfig     `yaml:"tls"`
}

type WindowConfig struct {
	Grace         time.Duration `yaml:"grace"`
	BucketsPerDay int           `yaml:"buckets_per_day"`
	CoerceToUTC   bool          `yaml:"coerce_to_utc"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CAFile   string `yaml:"ca_file"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type DedupeConfig struct {
	Prefix       string        `yaml:"prefix"`
	TTL          time.Duration `yaml:"ttl"`
	JanitorEvery time.Duration `yaml:"janitor_every"`
	Bloom        BloomConfig   `yaml:"bloom"`
}

type BloomConfig struct {
	Enabled  bool
	Key      string
	Capacity int64
	ErrRate  float64
}

type StoresConfig struct {
	Redis      RedisConfig      `yaml:"redis"`
	ClickHouse ClickHouseConfig `yaml:"clickhouse"`
}

type RedisConfig struct {
	Addr         string        `yaml:"addr"`
	Username     string        `yaml:"username"`
	Password     string        `yaml:"password"`
	DB           int           `yaml:"db"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type ClickHouseConfig struct {
	DSN    string                 `yaml:"dsn"`
	Writer ClickHouseWriterConfig `yaml:"writer"`
}

type ClickHouseWriterConfig struct {
	BatchMaxRows     int           `yaml:"batch_max_rows"`
	BatchMaxInterval time.Duration `yaml:"batch_max_interval"`
	MaxRetries       int           `yaml:"max_retries"`
	RetryBackoff     time.Duration `yaml:"retry_backoff"`
}

type PubSubConfig struct {
	NATS NATSConfig `yaml:"nats"`
}

type NATSConfig struct {
	URL             string `yaml:"url"`
	BroadcastPrefix string `yaml:"broadcast_prefix"`
}

type APIConfig struct {
	HTTP HTTPConfig `yaml:"http"`
	WS   WSConfig   `yaml:"ws"`
}

type HTTPConfig struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	CORS         CORSConfig    `yaml:"cors"`
}

type CORSConfig struct {
	Enabled bool     `yaml:"enabled"`
	Origins []string `yaml:"origins"`
	Methods []string `yaml:"methods"`
	Headers []string `yaml:"headers"`
}

type WSConfig struct {
	CoalesceMS        int           `yaml:"coalesce_ms"`
	MaxConn           int           `yaml:"max_conn"`
	ReadLimitBytes    int64         `yaml:"read_limit_bytes"`
	WriteTimeout      time.Duration `yaml:"write_timeout"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
}

type MetricsConfig struct {
	Prometheus string          `yaml:"prometheus"` // ":9091"
	Pyroscope  PyroscopeConfig `yaml:"pyroscope"`
}

type PyroscopeConfig struct {
	Enabled    bool              `yaml:"enabled"`
	ServerAddr string            `yaml:"server_addr"`
	AppName    string            `yaml:"app_name"`
	AuthToken  string            `yaml:"auth_token"`
	Tags       map[string]string `yaml:"tags"`
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
