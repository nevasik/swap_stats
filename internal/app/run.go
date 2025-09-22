package app

import (
	"context"
	"dexcelerate/internal/config"
	"os/signal"
	"syscall"
	"time"
)

// Run We assemble the container, start it, wait for the signal and stop
func Run(cfg *config.Config) error {
	ctxBuild, cancelBuild := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelBuild()

	container, cleanup, err := Build(ctxBuild, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	if err = container.Start(); err != nil {
		return err
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return container.Stop(shutdownCtx)
}
