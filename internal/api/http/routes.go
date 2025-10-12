package http

import (
	"dexcelerate/internal/api/http/handlers"
	"dexcelerate/internal/api/http/mw"
	"dexcelerate/internal/metrics"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func BuildRouter(
	api *handlers.API,
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

	// public tech
	r.Get("/healthz", api.Healthz)
	r.Get("/readiness", api.Readiness)
	r.Mount("/metrics", metrics.Handler())

	// auth (dev/testing only)
	r.Post("/auth/mint-token", api.MintToken)

	// protected (JWT + RL)
	r.Group(func(protected chi.Router) {
		if rateLimitMW != nil {
			protected.Use(rateLimitMW.Handler)
		}
		if jwtMW != nil {
			protected.Use(jwtMW.Handler)
		}

		protected.Route("/api", func(apiR chi.Router) {
			apiR.Get("/overview", api.Overview)
			apiR.Route("/tokens", func(tt chi.Router) {
				tt.Get("/{id}/stats", api.TokenStats)
			})
		})
	})
	return r
}
