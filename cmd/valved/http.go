package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/logx"
)

type httpAPI struct {
	lim   api.Limiter
	log   *logx.Logger
	ready func(context.Context) error
}

type checkBody struct {
	Key struct {
		Subject string `json:"subject"`
		Model   string `json:"model"`
	} `json:"key"`
	Limits struct {
		RequestsPerMinute     int64 `json:"requests_per_minute"`
		TokensPerMinute       int64 `json:"tokens_per_minute"`
		InputTokensPerMinute  int64 `json:"input_tokens_per_minute"`
		OutputTokensPerMinute int64 `json:"output_tokens_per_minute"`
	} `json:"limits"`
	Cost struct {
		Requests     int64 `json:"requests"`
		Tokens       int64 `json:"tokens"`
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"cost"`
}

func (b checkBody) toAPI() (api.Key, api.Limits, api.Cost) {
	return api.Key{Subject: b.Key.Subject, Model: b.Key.Model},
		api.Limits{
			RequestsPerMinute:     b.Limits.RequestsPerMinute,
			TokensPerMinute:       b.Limits.TokensPerMinute,
			InputTokensPerMinute:  b.Limits.InputTokensPerMinute,
			OutputTokensPerMinute: b.Limits.OutputTokensPerMinute,
		},
		api.Cost{
			Requests:     b.Cost.Requests,
			Tokens:       b.Cost.Tokens,
			InputTokens:  b.Cost.InputTokens,
			OutputTokens: b.Cost.OutputTokens,
		}
}

type settleBody struct {
	ReservationID      string `json:"reservation_id"`
	ActualTokens       *int64 `json:"actual_tokens"`
	ActualInputTokens  *int64 `json:"actual_input_tokens"`
	ActualOutputTokens *int64 `json:"actual_output_tokens"`
}

type refundBody struct {
	ReservationID string `json:"reservation_id"`
}

func (a *httpAPI) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if a.ready != nil {
			if err := a.ready(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("POST /v1/check", a.check)
	mux.HandleFunc("POST /v1/settle", a.settle)
	mux.HandleFunc("POST /v1/refund", a.refund)
	return mux
}

func (a *httpAPI) check(w http.ResponseWriter, r *http.Request) {
	var body checkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	key, limits, cost := body.toAPI()
	d, err := a.lim.Check(r.Context(), key, limits, cost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !d.Allowed {
		a.log.Deny(key, d)
	}
	writeJSON(w, http.StatusOK, decisionFromHTTP(d))
}

func (a *httpAPI) settle(w http.ResponseWriter, r *http.Request) {
	var body settleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var (
		d   api.Decision
		err error
	)
	if body.ActualInputTokens != nil || body.ActualOutputTokens != nil {
		var in, out int64
		if body.ActualInputTokens != nil {
			in = *body.ActualInputTokens
		}
		if body.ActualOutputTokens != nil {
			out = *body.ActualOutputTokens
		}
		d, err = a.lim.SettleIO(r.Context(), body.ReservationID, in, out)
	} else {
		var actual int64
		if body.ActualTokens != nil {
			actual = *body.ActualTokens
		}
		d, err = a.lim.Settle(r.Context(), body.ReservationID, actual)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, decisionFromHTTP(d))
}

func (a *httpAPI) refund(w http.ResponseWriter, r *http.Request) {
	var body refundBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.lim.Refund(r.Context(), body.ReservationID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
