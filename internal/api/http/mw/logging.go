package mw

import (
	"net/http"
	"time"
)

type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

type LoggingMiddleware struct {
	Log Logger
}

func NewLogging(log Logger) *LoggingMiddleware {
	return &LoggingMiddleware{Log: log}
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

		m.Log.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lrw.status,
			"size", lrw.size,
			"dur_ms", dur.Milliseconds(),
			"ip", remoteIP,
			"ua", r.UserAgent(),
		)
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
