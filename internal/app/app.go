package app

import (
	"context"
	"errors"
	"net/http"

	"gitlab.com/nevasik7/alerting/logger"
)

type HTTPServer interface {
	Start() error
	Shutdown(ctx context.Context) error
}

type App struct {
	log     logger.Logger
	httpSrv HTTPServer
}

func New(httpSrv HTTPServer, log logger.Logger) *App {
	return &App{httpSrv: httpSrv, log: log}
}

func (a *App) Start() error {
	a.log.Debug("App started begin...")

	go func() {
		if err := a.httpSrv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Fatalf("Start HTTP server is error=%v", err)
		}
	}()

	a.log.Info("App started")
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	a.log.Debug("App stopped begin...")

	if err := a.httpSrv.Shutdown(ctx); err != nil {
		return err
	}

	a.log.Info("App stopped")
	return nil
}
