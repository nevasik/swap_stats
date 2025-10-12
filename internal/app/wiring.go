package app

import (
	"compress/gzip"
	"context"
	"dexcelerate/internal/api/http"
	"dexcelerate/internal/api/http/handlers"
	"dexcelerate/internal/api/http/mw"
	"dexcelerate/internal/config"
	"dexcelerate/internal/pubsub/nats"
	"dexcelerate/internal/security"
	"dexcelerate/internal/stores/clickhouse"
	"dexcelerate/internal/stores/redis"
	"errors"
	"fmt"
	"strings"
	"time"

	loggerCfg "gitlab.com/nevasik7/alerting/config"
	"gitlab.com/nevasik7/alerting/logger"
)

type Container struct {
	app *App

	// infra
	redis *redis.Client
	ch    *clickhouse.Conn
	nc    *nats.Client
	// timeseries repo will wrap clickhouse.Writer when add readers

	// services
	// consumer, windowEngine, broadcaster etc...

	cleanupF func()

	// servers
	httpSrv *http.Server
}

func (c *Container) Start() error {
	return c.app.Start()
}

func (c *Container) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.app.Shutdown(ctx); err != nil {
		return fmt.Errorf("app shutdown is failed, error=%w", err)
	}

	if c.cleanupF != nil {
		c.cleanupF()
	}
	return nil
}

// Construct image app
func Build(ctx context.Context, cfg *config.Config) (*Container, func(), error) {
	lg := logger.New(loggerCfg.LoggerCfg{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
	})

	rdb, err := redis.New(ctx, lg, &cfg.Stores.Redis)
	if err != nil || rdb == nil {
		lg.Panicf("Failed to initialize redis client: %v", err)
	}

	ch, err := clickhouse.New(ctx, &cfg.Stores.ClickHouse)
	if err != nil {
		lg.Panicf("Failed to initialize clickhouse client: %v", err)
	}

	url := strings.Split(cfg.Stores.ClickHouse.DSN, "?")
	lg.Infof("Successfully initialized clickhouse client, url=%s", url[0])

	chWriter := clickhouse.NewWriter(lg, ch, &cfg.Stores.ClickHouse)
	if chWriter == nil {
		lg.Panicf("Failed to initialize clickhouse writer")
	}
	lg.Info("Successfully initialized clickhouse writer")

	natsCl, err := nats.New(lg, &cfg.PubSub.NATS)
	if err != nil || natsCl == nil {
		lg.Panicf("Failed to initialize nats client: %v", err)
		return nil, nil, err
	}
	lg.Infof("Successfully initialized nats client, url=%s", cfg.PubSub.NATS.URL)

	var verifier *security.RS256Verifier
	var signer *security.RS256Signer
	if cfg.Security.JWT.Enabled {
		cfgJWT := &cfg.Security.JWT
		if verifier, err = security.NewRS256Verifier(cfgJWT); err != nil || verifier == nil { //
			lg.Errorf("Failed to initialize verifier: %v", err)
			return nil, nil, err
		}
		if signer, err = security.NewRS256Signer(cfgJWT); err != nil || signer == nil {
			lg.Errorf("Failed to initialize signer: %v", err)
			// signer is not required for us -> continue
		}
	}
	lg.Info("Successfully initialized JWT-Verifier")

	gzipMW := mw.NewGzip(gzip.NoCompression)
	logMW := mw.NewLogging(lg)
	rlMW := mw.NewRateLimit(&cfg.RateLimit, rdb, verifier)

	var jwtMW *mw.JWTMiddleware
	if verifier != nil && cfg.Security.JWT.Enabled {
		jwtMW = mw.NewJWTMiddleware(verifier)
		lg.Info("Successfully added JWT Middleware")
	}

	var corsMW *mw.CORSMiddleware
	if cfg.API.HTTP.CORS.Enabled {
		corsMW = mw.NewCORSConfig(&cfg.API.HTTP.CORS)
		lg.Info("Successfully added CORS Middleware")
	}

	// handlers dependency
	api := handlers.NewAPI(&handlers.Deps{
		Log:             lg,
		Redis:           rdb,
		ClickHouse:      ch,
		ClickHouseBatch: chWriter,
		NATS:            natsCl,
	})
	if api == nil {
		return nil, nil, errors.New("api struct from NewAPI is nil")
	}
	lg.Info("Successfully initialized API")

	// http server
	httpSrv := http.NewServer(http.ServerDeps{
		Addr:         cfg.API.HTTP.Addr,
		API:          api,
		JWT:          jwtMW,
		Gzip:         gzipMW,
		Logging:      logMW,
		RateLimit:    rlMW,
		CORS:         corsMW,
		ReadTimeout:  cfg.API.HTTP.ReadTimeout,
		WriteTimeout: cfg.API.HTTP.WriteTimeout,
		IdleTimeout:  cfg.API.HTTP.IdleTimeout,
	})
	if httpSrv == nil {
		return nil, nil, errors.New("http server is nil")
	}
	lg.Info("Successfully initialized HTTP server")

	app := New(lg, httpSrv)
	if app == nil {
		return nil, nil, errors.New("init app is failed, app struct is nil")
	}
	lg.Info("Successfully initialized app")

	c := &Container{
		app:     app,
		redis:   rdb,
		ch:      ch,
		nc:      natsCl,
		httpSrv: httpSrv,
	}

	cleanupF := func() {
		ctxClean, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err = httpSrv.Shutdown(ctxClean); err != nil {
			lg.Errorf("Failed to shutdown by cleanupF HTTP server: %v", err)
		}

		if err = ch.Close(); err != nil {
			lg.Errorf("Failed to close by cleanupF clickhouse client: %v", err)
		}

		if err = chWriter.Close(ctxClean); err != nil {
			lg.Errorf("Failed to close by cleanupF clickhouse writer: %v", err)
		}

		if err = rdb.Close(); err != nil {
			lg.Errorf("Failed to close by cleanupF redis client: %v", err)
		}

		lg.Info("Successfully cleaned up dependency")
	}

	lg.Info("Wiring successfully initialized")
	return c, cleanupF, nil
}
