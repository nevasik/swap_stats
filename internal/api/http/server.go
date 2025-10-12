package http

import (
	"context"
	"dexcelerate/internal/api/http/handlers"
	"dexcelerate/internal/api/http/mw"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type ServerDeps struct {
	Addr      string
	API       *handlers.API
	JWT       *mw.JWTMiddleware
	Gzip      *mw.GzipMiddleware
	Logging   *mw.LoggingMiddleware
	RateLimit *mw.RateLimitMiddleware
	CORS      *mw.CORSMiddleware

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type Server struct {
	srv    *http.Server
	Router chi.Router
}

func NewServer(d ServerDeps) *Server {
	if d.API == nil {
		panic("API handlers cannot be nil")
	}
	router := BuildRouter(d.API, d.Logging, d.Gzip, d.RateLimit, d.JWT, d.CORS)

	s := &http.Server{
		Addr:         d.Addr,
		Handler:      router,
		ReadTimeout:  d.ReadTimeout,
		WriteTimeout: d.WriteTimeout,
		IdleTimeout:  d.IdleTimeout,
	}

	return &Server{srv: s, Router: router}
}

func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
