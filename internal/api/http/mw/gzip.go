package mw

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"gitlab.com/nevasik7/alerting/logger"
)

type GzipMiddleware struct {
	Level  int // gzip.NoCompression ... gzip.BestCompression
	Logger logger.Logger
}

func NewGzip(level int, log logger.Logger) *GzipMiddleware {
	if level == 0 {
		level = gzip.BestSpeed
	}
	return &GzipMiddleware{Level: level, Logger: log}
}

func (m *GzipMiddleware) Handler(next http.Handler) http.Handler {
	var pool = sync.Pool{
		New: func() any {
			w, _ := gzip.NewWriterLevel(io.Discard, m.Level)
			return w
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// client not support gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// if gzip or it's event-stream - continue
		if enc := w.Header().Get("Content-Encoding"); enc != "" {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(r.Header.Get("Accept"), "text/event-stream") {
			next.ServeHTTP(w, r)
			return
		}

		gzw := pool.Get().(*gzip.Writer)
		defer pool.Put(gzw)

		gzw.Reset(w)
		defer func(gzw *gzip.Writer) {
			if err := gzw.Close(); err != nil {
				m.Logger.Errorf("failed to close gzip writer: %v", err)
			}
		}(gzw)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	io.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	// if not send status let it 200 - OK
	if _, ok := w.ResponseWriter.(interface{ WriteHeader(int) }); !ok {
		// write header auto call by first Write
	}
	return w.Writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if f, ok := w.Writer.(interface{ Flush() error }); ok {
		_ = f.Flush()
	}

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
