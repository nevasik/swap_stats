package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	EventsIngested = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "swapstats",
			Subsystem: "ingest",
			Name:      "events_ingested_total",
			Help:      "Number of events ingested from broker",
		},
		[]string{"topic"},
	)

	HTTPRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "swapstats",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests",
		},
		[]string{"path", "method", "code"},
	)

	HTTPDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "swapstats",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP latency seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"path", "method"},
	)
)

var (
	once    sync.Once
	reg     *prometheus.Registry
	handler http.Handler
)

func Init() {
	once.Do(func() {
		reg = prometheus.NewRegistry()
		reg.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
		handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		})
	})
}

func Handler() http.Handler {
	Init()
	return handler
}

func Register(cs ...prometheus.Collector) {
	Init()
	for _, c := range cs {
		reg.MustRegister(c)
	}
}

func ExposeRegistry() *prometheus.Registry {
	Init()
	return reg
}
