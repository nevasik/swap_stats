package metrics

import (
	"github.com/grafana/pyroscope-go"
)

type PProfConfig struct {
	Enabled       bool   `yaml:"enabled"`
	AppInstanceID string `yaml:""`
	AppName       string
	ServerAddr    string
	AuthToken     string
	Tags          map[string]string
}

func InitPProf(cfg *PProfConfig) (*pyroscope.Profiler, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	pTags := map[string]string{
		"env":      "dev",
		"instance": cfg.AppInstanceID,
	}
	for k, v := range cfg.Tags {
		pTags[k] = v
	}

	return pyroscope.Start(pyroscope.Config{
		ApplicationName: cfg.AppName,
		ServerAddress:   cfg.ServerAddr,
		AuthToken:       cfg.AuthToken,
		Logger:          pyroscope.StandardLogger,
		Tags:            pTags,
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,

			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,

			pyroscope.ProfileGoroutines,
			pyroscope.ProfileMutexCount,
			pyroscope.ProfileMutexDuration,
			pyroscope.ProfileBlockCount,
			pyroscope.ProfileBlockDuration,
		},
	})
}
