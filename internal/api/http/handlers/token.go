package handlers

import (
	"dexcelerate/pkg/httputil"
	"net/http"

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
