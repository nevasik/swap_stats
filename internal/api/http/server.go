package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"

	"dexcelerate/internal/config"
	"dexcelerate/internal/security"
	rds "dexcelerate/internal/stores/redis"
	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gitlab.com/nevasik7/alerting/logger"
)

type Server struct {
	log   logger.Logger
	cfg   *config.Config
	srv   *http.Server
	jwt   *security.Verifier
	redis *rds.Client
	ch    *sql.DB
	nc    *nats.Conn
	js    nats.JetStreamContext
}

func NewServer(
	log logger.Logger,
	cfg *config.Config,
	jwt *security.Verifier,
	redis *rds.Client,
	ch *sql.DB,
	nc *nats.Conn,
	js nats.JetStreamContext,
) *Server {
	return &Server{
		log:   log,
		cfg:   cfg,
		jwt:   jwt,
		redis: redis,
		ch:    ch,
		nc:    nc,
		js:    js,
	}
}
