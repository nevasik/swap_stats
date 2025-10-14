package app

import (
	"context"
	"dexcelerate/internal/config"
	"errors"
	"os/signal"
	"syscall"
	"time"
)

// We assemble the container, start it, wait for the signal and stop
func Run(cfg *config.Config) error {
	ctxBuild, cancelBuild := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelBuild()

	container, cleanup := Build(ctxBuild, cfg)

	if cleanup == nil {
		return errors.New("cleanup is nil")
	}
	defer cleanup()

	if err := container.Start(); err != nil {
		return err
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	<-sigCtx.Done()

	return container.Stop()
}
