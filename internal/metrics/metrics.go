package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func Handler() http.Handler {
	h := promhttp.Handler()
	return h
}
