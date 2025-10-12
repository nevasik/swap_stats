package mw

import (
	"net/http"
	"time"

	"gitlab.com/nevasik7/alerting/logger"
)

type LoggingMiddleware struct {
	Logger logger.Logger
}

func NewLogging(logger logger.Logger) *LoggingMiddleware {
	if logger == nil {
		panic("logger cannot be nil")
	}
	return &LoggingMiddleware{Logger: logger}
}

func (m *LoggingMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := &loggingRW{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)

		dur := time.Since(start)

		remoteIP := r.Header.Get("X-Forwarded-For")
		if remoteIP == "" {
			remoteIP = r.RemoteAddr
		}

		m.Logger.Infof("%s %s -> %d (%d bytes, %dms) [IP: %s, UA: %s]",
			r.Method, r.URL.Path, lrw.status, lrw.size, dur.Milliseconds(), remoteIP, r.UserAgent())
	})
}

type loggingRW struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *loggingRW) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *loggingRW) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}
