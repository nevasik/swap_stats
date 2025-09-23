package app

import (
	"context"
	"errors"
	"net/http"

	"gitlab.com/nevasik7/alerting"
)

type HTTPServer interface {
	Start() error
	Shutdown(ctx context.Context) error
}

type App struct {
	alert   alerting.Alerting
	httpSrv HTTPServer
}

func New(lg alerting.Alerting, httpSrv HTTPServer) *App {
	return &App{alert: lg, httpSrv: httpSrv}
}

func (a *App) Start() error {
	a.alert.Debug("App started begin...")

	go func() {
		if err := a.httpSrv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.alert.Fatalf("Start HTTP server is error=%v", err)
		}
	}()

	a.alert.Info("App started")
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	a.alert.Debug("App stopped begin...")

	if err := a.httpSrv.Shutdown(ctx); err != nil {
		return err
	}

	a.alert.Info("App stopped")
	return nil
}
