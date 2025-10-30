package handlers

import (
	"context"
	"dexcelerate/internal/service"
	"dexcelerate/pkg/httputil"
	"net/http"
	"time"

	"gitlab.com/nevasik7/alerting/logger"
)

//type Deps struct {
//	// ---- external services/clients ----
//	Redis           *redis.Client      // rate-limit, snapshots...
//	ClickHouse      *clickhouse.Conn   // native driver row ping
//	ClickHouseBatch *clickhouse.Writer // batcher to ClickHouse(main entry)
//	NATS            *nats.Client       // cluster fan-out
//	// ---- external services/clients ----
//
//	// ---- security ----
//	Signer *security.RS256Signer // JWT token signer (only for dev/testing)
//	// ---- security ----
//
//	//---- internal service ----
//	//Windows window.WindowEngine // hot window 5m/1h/24h for HTTP and WS
//	//Dedupe dedupe.Deduper // redis SetNX
//	//Snapshot snapshot.Store // snapshot window
//	//Offset store.Offset // offset consumer
//	// ---- internal service ----
//}

type Handler struct {
	Log        logger.Logger
	AggService *service.AggregatorService
}

func NewHandler(log logger.Logger, aggService *service.AggregatorService) *Handler {
	if aggService == nil {
		panic("aggregate service cannot be nil")
	}

	return &Handler{Log: log, AggService: aggService}
}

func (a *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	if err := httputil.JSON(w, http.StatusOK, map[string]any{}, nil); err != nil {
		a.Log.Errorf("Healthz handler error: %s", err.Error())
	}
	a.Log.Info("Healthz handler success")
}

// Check health external services/clients
func (a *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := a.AggService.CheckDependency(ctx); err != nil {
		err = httputil.Error(w, r, http.StatusServiceUnavailable, "dependencies_unhealthy", "dependencies check failed", map[string]any{
			"error": err.Error(),
		})
		if err != nil {
			a.Log.Errorf("Readiness handler error: %s", err.Error())
		}
		return
	}

	if err := httputil.JSON(w, http.StatusOK, map[string]string{"dependencies": "healthy"}, nil); err != nil {
		a.Log.Errorf("Readiness handler error: %s", err.Error())
	}

	a.Log.Info("Readiness handler success")
}
