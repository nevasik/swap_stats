package app

import (
	"context"
	"dexcelerate/internal/api/http"
	"dexcelerate/internal/config"
	dedupe "dexcelerate/internal/dedupe/redis"
	"dexcelerate/internal/metrics"
	"dexcelerate/internal/pubsub/nats"
	"dexcelerate/internal/security"
	"dexcelerate/internal/service"
	"dexcelerate/internal/stores/clickhouse"
	"dexcelerate/internal/stores/redis"
	"dexcelerate/internal/window"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/pyroscope-go"
	lgcfg "gitlab.com/nevasik7/alerting/config"
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
	rdbDedupe *dedupe.RedisDedupe

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
	lg := logger.New(lgcfg.LoggerCfg{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
	})
	lg.Info("Successfully initialize logger")

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
	lg.Infof("Successfully initialize Pyroscope to %s as %s", cfg.Metrics.Pyroscope.ServerAddr, cfg.Metrics.Pyroscope.AppName)

	// Redis client
	rdb, err := redis.New(ctx, lg, &cfg.Stores.Redis)
	if err != nil {
		lg.Panicf("Failed to initialize redis client: %v", err)
	}

	// Bloom
	bloom, err := dedupe.NewBloom(&cfg.Dedupe.Bloom, rdb)
	if err != nil {
		lg.Panicf("Failed to initialize bloom: %v", err)
	}
	lg.Infof("Successfully initialize Bloom by key=%s, cap=%d, errRate=%f", bloom.Key, bloom.Capacity, bloom.ErrRate)

	// Dedupe
	deduper, err := dedupe.NewRedisDeduper(lg, &cfg.Dedupe, rdb, bloom)
	if err != nil {
		lg.Panicf("Failed to initialize redis deduper: %v", err)
	}
	lg.Infof("Successfully initialize Deduper redis_client by prefix %s", cfg.Dedupe.Prefix)

	// Windows Engine
	windowEngine, err := window.NewWindowEngine(lg, &cfg.Window)
	if err != nil {
		lg.Panicf("Failed to initialize window engine: %v", err)
	}
	lg.Infof("Successfully initialize Window Engine")

	// NATS Broadcaster
	natsCl, err := nats.New(lg, &cfg.PubSub.NATS)
	if err != nil || natsCl == nil {
		lg.Panicf("Failed to initialize nats client: %v", err)
	}
	lg.Infof("Successfully initialize nats client, url=%s", cfg.PubSub.NATS.URL)

	// ClickHouse Client
	ch, err := clickhouse.New(ctx, &cfg.Stores.ClickHouse)
	if err != nil {
		lg.Panicf("Failed to initialize clickhouse client: %v", err)
	}
	url := strings.Split(cfg.Stores.ClickHouse.DSN, "?")
	lg.Infof("Successfully initialize clickhouse client, url=%s", url[0])

	// ClickHouse Writer
	chWriter, err := clickhouse.NewWriter(lg, &cfg.Stores.ClickHouse, ch)
	if err != nil {
		lg.Panicf("Failed to initialize clickhouse writer")
	}
	lg.Info("Successfully initialize clickhouse writer")

	// Service Layer
	aggregatorService := service.NewAggregatorService(lg, windowEngine, natsCl, chWriter, deduper)

	// TODO: initialize consumer

	var signer *security.RS256Signer
	if cfg.Security.JWT.Enabled {
		if signer, err = security.NewRS256Signer(&cfg.Security.JWT); err != nil || signer == nil {
			lg.Errorf("Failed to initialize signer: %v", err) // signer is not required for us -> continue
		}
		lg.Info("Successfully initialize JWT-Verifier")
	}

	// HTTP Server
	httpSrv := http.NewServer(&http.ServerDeps{
		Logger:     lg,
		Cfg:        cfg,
		RdbClient:  rdb,
		AggService: aggregatorService,
	})
	lg.Info("Successfully initialize HTTP server")

	c := &Container{
		app:       NewApp(lg, httpSrv),
		redis:     rdb,
		ch:        ch,
		nc:        natsCl,
		rdbDedupe: deduper,
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

	lg.Info("Successfully initialize Wiring")
	return c, cleanupF
}
