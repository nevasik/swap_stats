package mw

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

type GzipMiddleware struct {
	Level int // gzip.NoCompression ... gzip.BestCompression
}

func NewGzip(level int) *GzipMiddleware {
	if level == 0 {
		level = gzip.BestSpeed
	}
	return &GzipMiddleware{Level: level}
}

func (m *GzipMiddleware) Handler(next http.Handler) http.Handler {
	var pool = sync.Pool{
		New: func() any {
			w, err := gzip.NewWriterLevel(io.Discard, m.Level)
			if err != nil {
				w, err = gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
			}
			return w
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Пропускаем сжатие для /metrics
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

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
		defer gzw.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")

		grw := &gzipResponseWriter{ResponseWriter: w, Writer: gzw}
		next.ServeHTTP(grw, r)
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
