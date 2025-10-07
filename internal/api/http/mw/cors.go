package mw

import (
	"net/http"
)

type CORSMiddleware struct {
	Enabled bool
	Origins []string
	Methods []string
	Headers []string
}

func NewCORSConfig(cfg CORSMiddleware) *CORSMiddleware {
	return &cfg
}

func (c *CORSMiddleware) Handler() func(http.Handler) http.Handler {
	if c == nil || !c.Enabled {
		return func(h http.Handler) http.Handler { return h }
	}

	origins := joinOrDefault(c.Origins, "*")
	methods := joinOrDefault(c.Methods, "GET, POST, OPTIONS")
	headers := joinOrDefault(c.Headers, "Authorization, Content-Type")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origins)
			w.Header().Set("Access-Control-Allow-Methods", methods)
			w.Header().Set("Access-Control-Allow-Headers", headers)
			w.Header().Set("Vary", "Origin")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func joinOrDefault(v []string, def string) string {
	if len(v) == 0 {
		return def
	}

	s := v[0]
	for i := 1; i < len(v); i++ {
		if v[i] != "" {
			s += "," + v[i]
		}
	}
	return s
}
