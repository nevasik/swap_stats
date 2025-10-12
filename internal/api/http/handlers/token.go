package handlers

import (
	"dexcelerate/pkg/httputil"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (a *API) Overview(w http.ResponseWriter, r *http.Request) {
	// TODO not ready
	resp := map[string]any{
		"top_tokens": []any{},
	}
	if err := httputil.JSON(w, http.StatusOK, resp, nil); err != nil {
		a.dependency.Log.Errorf("Overview tokens handler error: %s", err.Error())
	}

	a.dependency.Log.Infof("Overview tokens handler success")
}

func (a *API) TokenStats(w http.ResponseWriter, r *http.Request) {
	// TODO not ready
	id := chi.URLParam(r, "id")

	err := httputil.JSON(w, http.StatusOK, map[string]any{
		"token": id,
		"w5m":   map[string]any{},
		"w1h":   map[string]any{},
		"w24h":  map[string]any{},
	}, nil)
	if err != nil {
		a.dependency.Log.Errorf("TokenStats handler error: %s", err.Error())
	}

	a.dependency.Log.Infof("TokenStats handler success")
}

// Generate JWT токен for test/dev
func (a *API) MintToken(w http.ResponseWriter, r *http.Request) {
	// check signer
	if a.dependency.Signer == nil {
		err := httputil.Error(w, r, http.StatusServiceUnavailable, "signer_not_available", "JWT signer is not configured", nil)
		if err != nil {
			a.dependency.Log.Errorf("MintToken handler error: %s", err.Error())
		}
		return
	}

	type MintRequest struct {
		Subject string         `json:"subject"` // required
		TTL     time.Duration  `json:"ttl"`     // optional, default 1h
		ID      string         `json:"id"`      // optional (jti)
		Extra   map[string]any `json:"extra"`   // optional custom claims
	}

	var req MintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err = httputil.Error(w, r, http.StatusBadRequest, "invalid_request", "Invalid JSON body", map[string]any{
			"error": err.Error(),
		}); err != nil {
			a.dependency.Log.Errorf("MintToken decode error: %s", err.Error())
		}
		return
	}

	if req.Subject == "" {
		if err := httputil.Error(w, r, http.StatusBadRequest, "missing_subject", "Subject (sub) is required", nil); err != nil {
			a.dependency.Log.Errorf("MintToken validation error: %s", err.Error())
		}
		return
	}

	if req.TTL == 0 {
		req.TTL = 1 * time.Hour
	}

	// generate token
	token, err := a.dependency.Signer.Mint(req.Subject, req.TTL, req.ID, time.Time{}, req.Extra)
	if err != nil {
		if err = httputil.Error(w, r, http.StatusInternalServerError, "token_generation_failed", "Failed to generate JWT token", map[string]any{
			"error": err.Error(),
		}); err != nil {
			a.dependency.Log.Errorf("MintToken generation error: %s", err.Error())
		}
		return
	}

	resp := map[string]any{
		"token":      token,
		"subject":    req.Subject,
		"ttl":        req.TTL.String(),
		"expires_at": time.Now().Add(req.TTL).Unix(),
	}

	if err := httputil.JSON(w, http.StatusOK, resp, nil); err != nil {
		a.dependency.Log.Errorf("MintToken response error: %s", err.Error())
		return
	}

	a.dependency.Log.Infof("MintToken handler success: subject=%s, ttl=%s", req.Subject, req.TTL)
}
