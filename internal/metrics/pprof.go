package metrics

import (
	"github.com/grafana/pyroscope-go"
)

type PProfConfig struct {
	AppName    string
	ServerAddr string
}

func InitPProf(cfg PProfConfig) (*pyroscope.Profiler, error) {
	return pyroscope.Start(pyroscope.Config{
		ApplicationName: cfg.AppName,
		ServerAddress:   cfg.ServerAddr, // TODO write address pyroscope
		Logger:          pyroscope.StandardLogger,
		Tags:            map[string]string{"hostname": "localhost"},

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
