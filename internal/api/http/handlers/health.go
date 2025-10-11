package handlers

import (
	"context"
	"dexcelerate/internal/pubsub/nats"
	"dexcelerate/internal/stores/clickhouse"
	"dexcelerate/internal/stores/redis"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"gitlab.com/nevasik7/alerting/logger"
)

type Deps struct {
	Log logger.Logger

	// ---- external services/clients ----
	Redis           *redis.Client      // rate-limit, snapshots...
	ClickHouse      *clickhouse.Conn   // native driver row ping
	ClickHouseBatch *clickhouse.Writer // batcher to ClickHouse(main entry)
	NATS            *nats.Client       // cluster fan-out
	// ---- external services/clients ----

	// ---- internal service ----
	//Windows *window.Engine // hot window 5m/1h/24h for HTTP and WS
	//Dedupe dedupe.Deduper // redis SetNX
	//Snapshot snapshot.Store // snapshot window
	//Offset store.Offset // offset consumer
	// ---- internal service ----
}

type API struct {
	dependency *Deps
}

func NewAPI(d *Deps) *API {
	return &API{dependency: d}
}

func (a *API) Healthz(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]any{
		"status": "ok",
		"data":   map[string]any{},
	}

	_ = writeResponseJson(w, http.StatusOK, resp)
}

// Check health external services/clients
func (a *API) Readiness(w http.ResponseWriter, _ *http.Request) {
	a.dependency.Log.Info("Checking dependencies API")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp := make(map[string]any)

	if err := a.checkDependencies(ctx); err != nil {
		resp["status"] = "error"
		resp["data"] = map[string]any{
			"error": err.Error(),
		}
	} else {
		resp["status"] = "ok"
		resp["data"] = map[string]any{
			"dependencies": "is healthy",
		}
	}

	_ = writeResponseJson(w, http.StatusOK, resp)
}

func (a *API) checkDependencies(ctx context.Context) error {
	errors := make([]string, 0, 4)

	if a.dependency.Redis != nil {
		if err := a.dependency.Redis.Ping(ctx).Err(); err != nil {
			errors = append(errors, fmt.Sprintf("Redis connection error: %v", err))
		} else {
			a.dependency.Log.Info("Redis: OK")
		}
	} else {
		errors = append(errors, "Redis: not initialized")
	}

	if a.dependency.ClickHouse != nil {
		if err := a.dependency.ClickHouse.Native.Ping(ctx); err != nil {
			errors = append(errors, fmt.Sprintf("ClickHouse connection error: %v", err))
		} else {
			a.dependency.Log.Info("ClickHouse: OK")
		}
	} else {
		errors = append(errors, "ClickHouse: not initialized")
	}

	if a.dependency.ClickHouseBatch != nil {
		if err := a.checkClickHouseBatch(ctx); err != nil {
			errors = append(errors, fmt.Sprintf("ClickHouseBatch connection error: %v", err))
		} else {
			a.dependency.Log.Info("ClickHouseBatch: OK")
		}
	} else {
		errors = append(errors, "ClickHouseBatch: not initialized")
	}

	if a.dependency.NATS != nil {
		if !a.dependency.NATS.Ready() {
			errors = append(errors, "NATS: not initialized")
		} else {
			if a.dependency.NATS.Status().String() != "CONNECTED" {
				errors = append(errors, "NATS: connection not ready")
			} else {
				a.dependency.Log.Info("NATS: OK")
			}
		}
	} else {
		errors = append(errors, "NATS: not initialized")
	}

	if len(errors) > 0 {
		return fmt.Errorf("dependency check failed: %v", strings.Join(errors, "; "))
	}
	return nil
}

// Helper function for checking ClickHouse batch writer
func (a *API) checkClickHouseBatch(_ context.Context) error {
	// TODO write test select query to clickhouse
	return nil
}

func writeResponseJson(w http.ResponseWriter, statusCode int, data any) error {
	buf, err := json.Marshal(data)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)

	log.Printf("Health response: %s", string(buf))

	if _, err = w.Write(buf); err != nil {
		return fmt.Errorf("could not write response: %v", err)
	}
	return nil
}
