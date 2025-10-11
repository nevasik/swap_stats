package main

import (
	"dexcelerate/internal/app"
	"dexcelerate/internal/config"
	"log"
	"os"
)

func main() {
	cfgPath := os.Getenv("CONFIG")
	if cfgPath == "" {
		cfgPath = "cmd/aggregator/config.yaml"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed load config, error=%v", err)
	}

	if err = app.Run(cfg); err != nil {
		log.Fatalf("App run is failed, error=%v", err)
	}
}
