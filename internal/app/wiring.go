package app

import (
	"context"
	apihttp "dexcelerate/internal/api/http"
	"dexcelerate/internal/config"
	natspub "dexcelerate/internal/pubsub/nats"
	"dexcelerate/internal/security"
	chstore "dexcelerate/internal/stores/clickhouse"
	redisstore "dexcelerate/internal/stores/redis"

	"gitlab.com/nevasik7/alerting"
	tgalert "gitlab.com/nevasik7/alerting/alerters"
	alertingcfg "gitlab.com/nevasik7/alerting/config"
	"gitlab.com/nevasik7/alerting/logger"
)

type Container struct {
	app *App

	redis *redisstore.Client
	ch    *chstore.Conn
	nc    natspub.NatsConn
}

type NatsConn interface {
	Drain() error
}

func Build(ctx context.Context, cfg *config.Config) (*Container, func(), error) {
	log := logger.New(alertingcfg.LoggerCfg{
		Level:  cfg.Logger.Level,
		Format: cfg.Logger.Format,
	})
	log.Info("Logger initialized success")

	tgAlert := tgalert.NewTelegramAlerter(&alertingcfg.TelegramCfg{
		BotToken: cfg.Alerting.Token,
		ChatID:   cfg.Alerting.ChatID,
		AppName:  cfg.Alerting.AppName,
	}, log)
	log.Info("Telegram alert initialized success")

	// general alert manager
	alert := alerting.NewAlerting(log, tgAlert)

	redisCli, err := redisstore.New(ctx, cfg.Stores.Redis)
	if err != nil {
		alert.Errorf("Failed create redis store, error=%v", err)
		return nil, func() {}, err
	}

	chConn, err := chstore.New(ctx, cfg.Stores.ClickHouse)
	if err != nil {
		alert.Errorf("Failed create clickhouse store, error=%v", err)
		_ = redisCli.Close()
		return nil, func() {}, err
	}

	writer := chstore.NewWriter(alert, chConn.Native, cfg.Stores.ClickHouse.Writer)

	nc, js, err := natspub.New(ctx, cfg.PubSub.Nats)
	if err != nil {
		_ = chConn.Close()
		_ = redisCli.Close()
		return nil, func() {}, err
	}

	jwtVerifier, err := security.NewRS256Verifier(cfg.Security.JWT)
	if err != nil {
		_ = nc.Drain()
		_ = chConn.Close()
		_ = redisCli.Close()
		return nil, func() {}, err
	}

	httpSrv := apihttp.NewServer(log, cfg, jwtVerifier, redisCli, chConn.DB, nc, js)

	a := New(log, httpSrv)

	c := &Container{
		app:   a,
		redis: redisCli,
		ch:    chConn,
		nc:    nc,
	}

	cleanup := func() {
		// HTTP закрывается в Stop(); здесь — только клиенты
		_ = c.ch.Close()
		_ = c.redis.Close()
		_ = c.nc.Drain()
	}

	return c, cleanup, nil
}

func (c *Container) Start() error {
	return c.app.Start()
}

func (c *Container) Stop(ctx context.Context) error {
	return c.app.Stop(ctx)
}
