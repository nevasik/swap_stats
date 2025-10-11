## Project Overview

**dexcelerate** is a real-time swap statistics aggregation system built in Go. It processes high-throughput swap events (1000+ events/second) from decentralized exchanges, calculates rolling window statistics (5m/1h/24h volumes, transaction counts), and serves this data via HTTP API and WebSocket with minimal latency.

The system is designed for high availability with zero data loss, handling duplicates, out-of-order events, and graceful restarts.

## Architecture

### Core Components

**Application Layer** (`internal/app/`)
- `app.go`: Main application struct that manages lifecycle (start/shutdown)
- `run.go`: Entry point that builds container, starts app, and handles OS signals
- `wiring.go`: Dependency injection container (Build function) that wires all components together

**Configuration** (`internal/config/`)
- YAML-based configuration with structured types for all subsystems
- Default config path: `cmd/aggregator/config.yaml`
- Override via `CONFIG` environment variable

**Domain Models** (`internal/domain/`)
- Core domain types for swap events and statistics
- Validation logic for domain objects

**Data Stores** (`internal/stores/`)
- `redis/`: Redis client for deduplication and rate limiting state
- `clickhouse/`: ClickHouse connection and batched writer for time-series data
    - Writer uses buffered channels (8192 capacity) with batching (1000 rows or 200ms interval)
    - Automatic retries with exponential backoff

**API Layer** (`internal/api/http/`)
- `server.go`: HTTP server implementation
- `routes.go`: Router with middleware chain (chi-based)
- `handlers/`: Request handlers (health, readiness, token stats, overview)
- `mw/`: Middleware for JWT auth, CORS, gzip, logging, rate limiting

**Security** (`internal/security/`)
- `jwt_verifier.go`: RS256 JWT verification with audience/issuer validation
- `jwt_signer.go`: RS256 JWT signing for token generation
- Public/private key paths configured in config.yaml

**Rate Limiting** (`internal/ratelimit/`)
- Token bucket algorithm implemented with Redis backend
- Dual-layer rate limiting: by JWT subject (user) and by IP address
- Configurable refill rates, burst sizes, and TTLs

**PubSub** (`internal/pubsub/nats/`)
- NATS client for broadcasting statistics updates across multiple instances
- Enables horizontal scaling with fan-out pattern
- Subject prefix: `ws.broadcast.token.*` (e.g., `ws.broadcast.token.USDC`)

**Ingest Pipeline** (`internal/ingest/`)
- Kafka/Redpanda consumer for swap events
- Consumer group management with rebalancing support
- Handles deduplication, watermarking, and event ordering

**Window Engine** (`internal/window/`)
- Rolling time window calculations for statistics
- Manages snapshots at configurable intervals
- Watermark tracking for late event handling

**Deduplication** (`internal/dedupe/`)
- Redis-based duplicate detection using event IDs
- Configurable TTL (default 24h)

**Metrics** (`internal/metrics/`)
- Prometheus metrics endpoint at `/metrics` (port 9091)
- pprof endpoints for profiling (port 4040)

### Request Flow

**HTTP/WebSocket Requests:**
1. Request → Chi Router → Middleware Chain (RequestID → Recoverer → RealIP → Logging → Gzip → CORS)
2. Protected routes: → RateLimit MW (by IP/JWT) → JWT MW (auth) → Handler
3. Public routes (healthz, readiness, metrics): Skip rate limit and JWT checks

**Data Ingestion:**
1. Kafka/Redpanda → Consumer → Deduplication (Redis) → Window Engine → Statistics
2. Statistics → Broadcaster (NATS) → WebSocket Clients
3. Raw Events → ClickHouse Writer (batched)

## Common Development Commands

### Build and Run
```bash
# Run with default config
go run cmd/aggregator/main.go

# Run with custom config
CONFIG=/path/to/config.yaml go run cmd/aggregator/main.go

# Build binary
go build -o bin/aggregator cmd/aggregator/main.go
```

### Testing
```bash
# Run all tests
make tests
# or
go test ./...

# Run specific test
go test ./test/unit/ratelimit_test.go
go test ./test/integration/clickhouse_writer_test.go

# Run tests with coverage
go test -cover ./...
```

### Linting
```bash
# Install linter (if not present)
make .install-linter

# Run linter
make lint
# or
golangci-lint run ./...
```

### Docker
```bash
# Infrastructure in infra/
docker-compose -f infra/docker-compose.yml up -d

# Build Docker image
docker build -f infra/Dockerfile -t dexcelerate .
```

## Code Patterns and Conventions

### Dependency Injection
All dependencies are wired in `internal/app/wiring.go` using the Build function. This returns a Container with cleanup function. Never initialize infrastructure clients (Redis, ClickHouse, NATS) directly in handlers or business logic.

### Error Handling
- Log errors with context using the logger (`lg.Errorf`, `lg.Panicf`)
- Return errors up the stack, don't swallow them
- Use `fmt.Errorf` with `%w` for error wrapping
- Critical initialization errors should panic or return from Build

### Graceful Shutdown
The app listens for SIGINT, SIGTERM, SIGHUP signals. Cleanup function in Container closes all connections with context timeout (10s). Always implement proper Close/Shutdown methods for new components.

### Configuration
- All configuration lives in YAML files and is parsed into strongly-typed structs
- Use duration strings (e.g., "30s", "5m") parsed to `time.Duration`
- Validate configuration during Load or Build phase, not at runtime

### Security
- JWT verification is mandatory on protected routes when `security.jwt.enabled: true`
- Rate limiting applies to both authenticated (by JWT subject) and unauthenticated (by IP) requests
- Never commit private keys or credentials; use config paths that point to secrets outside the repo

### Testing Structure
- Unit tests: `test/unit/` - test individual components in isolation
- Integration tests: `test/integration/` - test with real infrastructure (Redis, ClickHouse, NATS)
- Use table-driven tests where applicable

## Important Implementation Notes

### ClickHouse Writer
- Batches writes (1000 rows or 200ms interval) to optimize throughput
- Uses buffered channel (8192 capacity) to handle bursts
- Implements retry logic with exponential backoff
- Decimal values (amounts) must be passed as strings

### Rate Limiting
- Implemented as token bucket with Redis backend
- Keys have TTL to prevent memory leaks
- Two separate buckets: by JWT subject and by IP
- Both limits are checked on protected endpoints

### NATS Broadcasting
- Used for distributing statistics updates across multiple service instances
- Enables horizontal scaling without state synchronization issues
- Endless reconnection with exponential backoff

### JWT Authentication
- RS256 (asymmetric) only for security
- Validates audience, issuer, expiration, and issued-at
- Leeway timeout for clock skew tolerance
- Extract subject from claims for rate limiting

### Middleware Ordering
Critical that middleware is applied in correct order:
1. RequestID, Recoverer, RealIP (first)
2. Logging, Gzip, CORS
3. RateLimit, JWT (last before handler)

## Key Configuration Parameters

- `app.grace`: How late events are accepted before being dropped
- `app.snapshot_interval`: Frequency of state snapshots for restart recovery
- `stores.clickhouse.writer.batch_max_rows`: ClickHouse batch size (tune for throughput)
- `stores.clickhouse.writer.batch_max_interval`: Max time to hold a batch
- `rate_limit.by_jwt.refill_per_sec`: Token bucket refill rate for authenticated users
- `rate_limit.by_ip.burst`: Max burst capacity for IP-based rate limiting
- `api.ws.coalesce_ms`: WebSocket update batching interval (reduce message frequency)

## Running the Service

1. Ensure infrastructure is running (Redis, ClickHouse, NATS, Kafka/Redpanda)
2. Configure `cmd/aggregator/config.yaml` with correct connection strings
3. Verify JWT keys exist at configured paths
4. Run: `go run cmd/aggregator/main.go`
5. Check health: `curl http://localhost:8080/healthz`
6. Monitor metrics: `curl http://localhost:9091/metrics`
