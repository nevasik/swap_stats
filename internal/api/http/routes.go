package http

import (
	"dexcelerate/internal/api/http/mw"
	"dexcelerate/internal/metrics"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func BuildRouter(
	api *API,
	logMW *mw.LoggingMiddleware,
	gzipMW *mw.GzipMiddleware,
	rateLimitMW *mw.RateLimitMiddleware,
	jwtMW *mw.JWTMiddleware,
	corsMW *mw.CORSMiddleware,
) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	if logMW != nil {
		r.Use(logMW.Handler)
	}
	if gzipMW != nil {
		r.Use(gzipMW.Handler)
	}
	if corsMW != nil {
		r.Use(corsMW.Handler())
	}

	// tech endpoint not auth
	r.Get("/healthz", api.Healthz)
	r.Get("/readiness", api.Readiness)
	r.Mount("/metrics", metrics.Handler())

	// tech endpoint with rate limit and jwt
	protected := chi.NewRouter()
	if rateLimitMW != nil {
		r.Use(rateLimitMW.Handler)
	}
	if jwtMW != nil {
		r.Use(jwtMW.Handler)
	}

	protected.Route("/api", func(apiR chi.Router) {
		apiR.Get("/overview", api.Overview)
		apiR.Route("/tokens", func(tt chi.Router) {
			tt.Get("/{id}/stats", api.TokenStats)
		})
	})

	r.Mount("/", protected)
	return r
}
