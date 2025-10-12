package httputil

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

type Envelope map[string]any

type APIError struct {
	Code    string `json:"code"` // example "bad_request", "not_found"
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

func JSON(w http.ResponseWriter, status int, body any, headers map[string]string) error {
	// No body -> 204
	if body == nil && status == http.StatusNoContent {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		return nil
	}

	var payload any
	switch body.(type) {
	case *APIError, APIError:
		payload = Envelope{
			"status": "error",
			"error":  body,
		}
	default:
		payload = Envelope{
			"status": "ok",
			"data":   body,
		}
	}

	// Заголовки до записи
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	return enc.Encode(payload)
}

func Error(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) error {
	traceID := middleware.GetReqID(r.Context())
	return JSON(w, status, APIError{
		Code:    code,
		Message: message,
		Details: details,
		TraceID: traceID,
	}, map[string]string{
		"Cache-Control": "no-store",
	})
}
