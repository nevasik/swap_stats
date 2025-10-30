package http

import (
	"compress/gzip"
	"context"
	"dexcelerate/internal/api/http/handlers"
	"dexcelerate/internal/api/http/mw"
	"dexcelerate/internal/config"
	"dexcelerate/internal/security"
	"dexcelerate/internal/service"
	"dexcelerate/internal/stores/redis"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gitlab.com/nevasik7/alerting/logger"
)

type ServerDeps struct {
	Logger     logger.Logger
	Cfg        *config.Config
	RdbClient  *redis.Client
	AggService *service.AggregatorService
}

type Server struct {
	srv    *http.Server
	Router chi.Router
}

func NewServer(d *ServerDeps) *Server {
	lg := d.Logger

	var verifier *security.RS256Verifier
	if d.Cfg.Security.JWT.Enabled {
		cfgJWT := &d.Cfg.Security.JWT
		if verifier, err := security.NewRS256Verifier(cfgJWT); err != nil || verifier == nil { //
			lg.Panicf("Failed to initialize verifier: %v", err)
		}
	}
	lg.Info("Successfully initialize JWT-Verifier")

	gzipMW := mw.NewGzip(gzip.NoCompression)
	logMW := mw.NewLogging(lg)
	rlMW := mw.NewRateLimit(&d.Cfg.RateLimit, d.RdbClient, verifier)

	var jwtMW *mw.JWTMiddleware
	if d.Cfg.Security.JWT.Enabled {
		var err error
		if jwtMW, err = mw.NewJWTMiddleware(verifier); err != nil {
			lg.Panicf("Failed to initialize jwt middleware: %v", err)
		}
		lg.Info("Successfully added JWT Middleware")
	}

	var corsMW *mw.CORSMiddleware
	if d.Cfg.API.HTTP.CORS.Enabled {
		corsMW = mw.NewCORSConfig(&d.Cfg.API.HTTP.CORS)
		lg.Info("Successfully added CORS Middleware")
	}

	h := handlers.NewHandler(lg, d.AggService)

	router := BuildRouter(h, logMW, gzipMW, rlMW, jwtMW, corsMW)

	s := &http.Server{
		Addr:         d.Cfg.API.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  d.Cfg.API.HTTP.ReadTimeout,
		WriteTimeout: d.Cfg.API.HTTP.WriteTimeout,
		IdleTimeout:  d.Cfg.API.HTTP.IdleTimeout,
	}

	return &Server{srv: s, Router: router}
}

func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
