package app

import (
	"compress/gzip"
	"context"
	"dexcelerate/internal/api/http"
	"dexcelerate/internal/api/http/handlers"
	"dexcelerate/internal/api/http/mw"
	"dexcelerate/internal/config"
	deduper "dexcelerate/internal/dedupe/redis"
	"dexcelerate/internal/metrics"
	"dexcelerate/internal/pubsub/nats"
	"dexcelerate/internal/security"
	"dexcelerate/internal/stores/clickhouse"
	"dexcelerate/internal/stores/redis"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/pyroscope-go"
	loggerCfg "gitlab.com/nevasik7/alerting/config"
	"gitlab.com/nevasik7/alerting/logger"
)

type Container struct {
	app *App

	// infra
	redis *redis.Client
	ch    *clickhouse.Conn
	nc    *nats.Client

	// deduper
	// in-memory dedupe
	rdbDedupe *deduper.RedisDedupe

	// services
	// consumer, windowEngine, broadcaster etc...

	cleanupF func()

	// servers
	httpSrv *http.Server

	// metrics
	profiler *pyroscope.Profiler
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
func Build(ctx context.Context, cfg *config.Config) (*Container, func()) {
	lg := logger.New(loggerCfg.LoggerCfg{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
	})

	profiler, err := metrics.InitPProf(&metrics.PProfConfig{
		AppInstanceID: cfg.App.InstanceID,
		AppName:       cfg.Metrics.Pyroscope.AppName,
		ServerAddr:    cfg.Metrics.Pyroscope.ServerAddr,
		AuthToken:     cfg.Metrics.Pyroscope.AuthToken,
		Tags:          cfg.Metrics.Pyroscope.Tags,
	})
	if err != nil {
		lg.Panicf("Pyroscope initialize failed: %v", err)
	}
	lg.Infof("Pyroscope initialize to %s as %s", cfg.Metrics.Pyroscope.ServerAddr, cfg.Metrics.Pyroscope.AppName)

	rdb, err := redis.New(ctx, lg, &cfg.Stores.Redis)
	if err != nil {
		lg.Panicf("Failed to initialize redis client: %v", err)
	}

	// deduper
	// for dev inMemoryDedupe := dedupe.NewInMemoryDedupe(lg, cfg.Dedupe.TTL, cfg.Dedupe.JanitorEvery)
	bloom, err := deduper.NewBloom(lg, &cfg.Dedupe.Bloom, rdb)
	if err != nil {
		lg.Panicf("Failed to initialize bloom: %v", err)
	}
	lg.Infof("Bloom initialize by key=%s, cap=%d, errRate=%f", bloom.Key, bloom.Capacity, bloom.ErrRate)

	rdbDedupe, err := deduper.NewRedisDeduper(lg, &cfg.Dedupe, rdb, bloom)
	if err != nil {
		lg.Panicf("Failed to initialize redis deduper: %v", err)
	}
	lg.Infof("Deduper redis client initialize by prefix %s", cfg.Dedupe.Prefix)

	ch, err := clickhouse.New(ctx, &cfg.Stores.ClickHouse)
	if err != nil {
		lg.Panicf("Failed to initialize clickhouse client: %v", err)
	}
	url := strings.Split(cfg.Stores.ClickHouse.DSN, "?")
	lg.Infof("Successfully initialize clickhouse client, url=%s", url[0])

	chWriter, err := clickhouse.NewWriter(lg, &cfg.Stores.ClickHouse, ch)
	if err != nil {
		lg.Panicf("Failed to initialize clickhouse writer")
	}
	lg.Info("Successfully initialize clickhouse writer")

	natsCl, err := nats.New(lg, &cfg.PubSub.NATS)
	if err != nil || natsCl == nil {
		lg.Panicf("Failed to initialize nats client: %v", err)
	}
	lg.Infof("Successfully initialize nats client, url=%s", cfg.PubSub.NATS.URL)

	var verifier *security.RS256Verifier
	var signer *security.RS256Signer
	if cfg.Security.JWT.Enabled {
		cfgJWT := &cfg.Security.JWT
		if verifier, err = security.NewRS256Verifier(cfgJWT); err != nil || verifier == nil { //
			lg.Panicf("Failed to initialize verifier: %v", err)
		}
		if signer, err = security.NewRS256Signer(cfgJWT); err != nil || signer == nil {
			lg.Errorf("Failed to initialize signer: %v", err)
			// signer is not required for us -> continue
		}
	}
	lg.Info("Successfully initialize JWT-Verifier")

	gzipMW := mw.NewGzip(gzip.NoCompression)
	logMW := mw.NewLogging(lg)
	rlMW := mw.NewRateLimit(&cfg.RateLimit, rdb, verifier)

	var jwtMW *mw.JWTMiddleware
	if verifier != nil && cfg.Security.JWT.Enabled {
		if jwtMW, err = mw.NewJWTMiddleware(verifier); err != nil {
			lg.Panicf("Failed to initialize jwt middleware: %v", err)
		}
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
		Signer:          signer, // maybe nil if jwt.enabled=false
	})
	if api == nil {
		lg.Panicf("Failed to initialize API handler is nil")
	}
	lg.Info("Successfully initialize API")

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
		lg.Panicf("Failed to initialize HTTP server is nil")
	}
	lg.Info("Successfully initialize HTTP server")

	app := New(lg, httpSrv)
	if app == nil {
		lg.Panicf("Failed to initialize app is nil")
	}
	lg.Info("Successfully initialize app")

	c := &Container{
		app:       app,
		redis:     rdb,
		ch:        ch,
		nc:        natsCl,
		rdbDedupe: rdbDedupe,
		httpSrv:   httpSrv,
		profiler:  profiler,
	}

	cleanupF := func() {
		ctxClean, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if c.profiler != nil {
			if err = c.profiler.Stop(); err != nil {
				lg.Errorf("Failed to stop profiler: %v", err)
			}
		}

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

	lg.Info("Wiring successfully initialize")
	return c, cleanupF
}
