package http

import (
	"context"
	"dexcelerate/internal/stores/clickhouse"
	"net/http"

	"dexcelerate/internal/config"
	"dexcelerate/internal/security"
	rds "dexcelerate/internal/stores/redis"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nats-io/nats.go"
	"gitlab.com/nevasik7/alerting/logger"
)

type Server struct {
	log   logger.Logger
	cfg   *config.Config
	srv   *http.Server
	jwt   *security.Verifier
	redis *rds.Client
	ch    *ch.Conn
	nc    *nats.Conn
	js    nats.JetStreamContext
}

func NewServer(
	log logger.Logger,
	cfg *config.Config,
	jwt *security.Verifier,
	redis *rds.Client,
	ch *clickhouse.Conn,
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

func (s Server) Start() error {
	//TODO implement me
	panic("implement me")
}

func (s Server) Shutdown(ctx context.Context) error {
	//TODO implement me
	panic("implement me")
}
