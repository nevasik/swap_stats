package mw

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"gitlab.com/nevasik7/alerting/logger"
)

const (
	maxBodyLogSize = 4096
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

		var reqBody string
		if shouldLogBody(r) {
			reqBody = captureRequestBody(r)
		}

		lrw := &loggingRW{
			ResponseWriter: w,
			status:         http.StatusOK,
			body:           &bytes.Buffer{},
		}

		next.ServeHTTP(lrw, r)

		dur := time.Since(start)

		remoteIP := r.Header.Get("X-Forwarded-For")
		if remoteIP == "" {
			remoteIP = r.RemoteAddr
		}

		// generate log message
		logMsg := buildLogMessage(r, lrw, dur, remoteIP, reqBody)
		m.Logger.Infof(logMsg)
	})
}

// check need log body
func shouldLogBody(r *http.Request) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
		return false
	}

	ct := r.Header.Get("Content-Type")
	return strings.Contains(ct, "application/json")
}

// read and up request body
func captureRequestBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBodyLogSize))
	if err != nil {
		return "[error reading body]"
	}

	_ = r.Body.Close()

	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	body := string(bodyBytes)
	if len(bodyBytes) == maxBodyLogSize {
		body += "... [truncated]"
	}

	return body
}

// Generate final log message
func buildLogMessage(r *http.Request, lrw *loggingRW, dur time.Duration, remoteIP, reqBody string) string {
	var sb strings.Builder

	sb.WriteString(r.Method)
	sb.WriteString(" ")
	sb.WriteString(r.URL.Path)
	if r.URL.RawQuery != "" {
		sb.WriteString("?")
		sb.WriteString(r.URL.RawQuery)
	}
	sb.WriteString(" -> ")
	sb.WriteString(http.StatusText(lrw.status))
	sb.WriteString(" (")
	sb.WriteString(intToString(lrw.status))
	sb.WriteString(")")

	sb.WriteString(" | ")
	sb.WriteString(dur.String())
	sb.WriteString(" | ")
	sb.WriteString(intToString(lrw.size))
	sb.WriteString(" bytes")
	sb.WriteString(" | IP: ")
	sb.WriteString(remoteIP)

	if reqBody != "" {
		sb.WriteString(" | ReqBody: ")
		sb.WriteString(reqBody)
	}

	respBody := lrw.body.String()
	if respBody != "" && len(respBody) <= maxBodyLogSize {
		ct := lrw.Header().Get("Content-Type")
		if strings.Contains(ct, "application/json") {
			if len(respBody) > 200 {
				respBody = respBody[:200] + "... [truncated]"
			}
			sb.WriteString(" | RespBody: ")
			sb.WriteString(respBody)
		}
	}

	return sb.String()
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf) - 1
	neg := n < 0
	if neg {
		n = -n
	}

	for n > 0 {
		buf[i] = byte('0' + n%10)
		n /= 10
		i--
	}

	if neg {
		buf[i] = '-'
		i--
	}

	return string(buf[i+1:])
}

type loggingRW struct {
	http.ResponseWriter
	status int
	size   int
	body   *bytes.Buffer
}

func (w *loggingRW) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *loggingRW) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.size += n

	if w.body != nil && w.body.Len() < maxBodyLogSize {
		w.body.Write(b[:n])
	}

	return n, err
}
