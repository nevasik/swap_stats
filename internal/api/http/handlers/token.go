package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (a *API) Overview(w http.ResponseWriter, r *http.Request) {
	// TODO not ready
	resp := map[string]any{
		"status": "ok",
		"data": map[string]any{
			"top_tokens": []string{"USDC", "ETH", "WBTC"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *API) TokenStats(w http.ResponseWriter, r *http.Request) {
	// TODO not ready
	id := chi.URLParam(r, "id")

	resp := map[string]any{
		"token": id,
		"windows": map[string]any{
			"5m":  map[string]any{"vol_usd": 123.45, "trades": 10},
			"1h":  map[string]any{"vol_usd": 2345.67, "trades": 123},
			"24h": map[string]any{"vol_usd": 34567.89, "trades": 2345},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
