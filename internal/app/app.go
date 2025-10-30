package app

import (
	"context"
	"errors"
	"net/http"
	"time"

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

func NewApp(log logger.Logger, httpSrv HTTPServer) *App {
	if log == nil {
		panic("logger cannot be nil")
	}
	if httpSrv == nil {
		panic("HTTP server cannot be nil")
	}
	return &App{
		log:     log,
		httpSrv: httpSrv,
	}
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

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// TODO: доделать текущие доработки со снапшотом при закрытии приложения
	// Создаем финальный snapshot перед остановкой
	//snapshot, err := app.windowEngine.Snapshot(ctx)
	//if err != nil {
	// Timeout — не успели сохранить snapshot
	//return fmt.Errorf("snapshot failed: %w", err)
	//}

	// Сохраняем в Redis с тем же context
	//if err := app.redis.Set(ctx, "snapshot:final", snapshot).Err(); err != nil {
	//	return err
	//}

	if err := a.httpSrv.Shutdown(ctx); err != nil {
		return err
	}

	a.log.Info("App stopped")
	return nil
}
