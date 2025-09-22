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
	lg      logger.Logger
	httpSrv HTTPServer
}

func New(lg logger.Logger, httpSrv HTTPServer) *App {
	return &App{lg: lg, httpSrv: httpSrv}
}

func (a *App) Start() error {
	a.lg.Debug("App started begin...")

	go func() {
		if err := a.httpSrv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.lg.Fatalf("Start HTTP server is error=%v", err)
		}
	}()

	a.lg.Info("App started")
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.lg.Debug("App stopped begin...")

	if err := a.httpSrv.Shutdown(ctx); err != nil {
		return err
	}

	a.lg.Info("App stopped")
	return nil
}
