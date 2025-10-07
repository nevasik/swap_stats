package http

import (
	"database/sql"
	"dexcelerate/internal/stores/clickhouse"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"gitlab.com/nevasik7/alerting/logger"
)

type Deps struct {
	Log logger.Logger

	// ---- external services/clients ----
	Redis           *redis.Client      // rate-limit, snapshots...
	ClickHouse      *sql.DB            // raw conn(run ping)
	ClickHouseBatch *clickhouse.Writer // batcher to ClickHouse(main entry)
	NATS            *nats.Conn         // cluster fan-out
	// ---- external services/clients ----

	// ---- internal service ----
	//Windows *window.Engine // hot window 5m/1h/24h for HTTP and WS
	//Dedupe dedupe.Deduper // redis SetNX
	//Snapshot snapshot.Store // snapshot window
	//Offset store.Offset // offset consumer
	// ---- internal service ----
}

type API struct {
	dependency Deps
}

func NewAPI(d Deps) *API {
	return &API{dependency: d}
}

func (a *API) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Check health external services/clients
func (a *API) Readiness(w http.ResponseWriter, r *http.Request) {
	// TODO not ready
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

func (a *API) Overview(w http.ResponseWriter, r *http.Request) {
	// TODO not ready
	_ = json.NewEncoder(w).Encode(map[string]any{
		"top_tokens": []string{"USDC", "ETH"},
	})
}

func (a *API) TokenStats(w http.ResponseWriter, r *http.Request) {
	// TODO not ready
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":   "USDC",
		"w5m":  map[string]any{"vol_usd": 0, "trades": 0},
		"w1h":  map[string]any{"vol_usd": 0, "trades": 0},
		"w24h": map[string]any{"vol_usd": 0, "trades": 0},
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf("failed to encode response: %v", err)))
	}
}
