package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/headers"
	"github.com/shiv-eshwar/valve/pkg/llm"
)

// ProxyConfig configures the rate-limited reverse proxy.
type ProxyConfig struct {
	Upstream   *url.URL
	Limiter    api.Limiter
	Limits     api.Limits
	Gate       llm.Gate
	Tokenizer  llm.Tokenizer
	Transport  http.RoundTripper
	SettleWait time.Duration // streaming settle timeout; default 5m
}

// Handler is an HTTP reverse proxy that enforces RPM+TPM via valve.
type Handler struct {
	cfg    ProxyConfig
	client *http.Client
}

// NewHandler builds the proxy handler.
func NewHandler(cfg ProxyConfig) *Handler {
	if cfg.SettleWait <= 0 {
		cfg.SettleWait = 5 * time.Minute
	}
	if cfg.Transport == nil {
		cfg.Transport = http.DefaultTransport
	}
	return &Handler{
		cfg: cfg,
		client: &http.Client{
			Transport: cfg.Transport,
			Timeout:   0,
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limitBytes := h.cfg.Gate.MaxRequestBytes
	if limitBytes <= 0 {
		limitBytes = 8 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limitBytes+1))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	if err := h.cfg.Gate.CheckBytes(len(body)); err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}

	est, err := llm.EstimateChatRequest(body, h.cfg.Tokenizer)
	if err != nil {
		n := int64(llm.EstimateTokens(string(body)))
		if n < 1 {
			n = 1
		}
		est = llm.ChatEstimate{InputTokens: n, TotalTokens: n}
	}
	if err := h.cfg.Gate.CheckInput(est.InputTokens); err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}

	subject := subjectFromRequest(r)
	key := api.Key{Subject: subject, Model: modelFromBody(body)}
	cost := api.Cost{Requests: 1, Tokens: est.TotalTokens}

	dec, err := h.cfg.Limiter.Check(ctx, key, h.cfg.Limits, cost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !dec.Allowed {
		headers.Write(w.Header(), dec)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"type":       "rate_limit",
				"limit_type": string(dec.LimitType),
			},
		})
		return
	}

	upURL := h.cfg.Upstream.ResolveReference(&url.URL{Path: r.URL.Path, RawQuery: r.URL.RawQuery})
	upReq, err := http.NewRequestWithContext(ctx, r.Method, upURL.String(), bytes.NewReader(body))
	if err != nil {
		_ = h.cfg.Limiter.Refund(ctx, dec.ReservationID)
		http.Error(w, "upstream request", http.StatusBadGateway)
		return
	}
	upReq.Header = r.Header.Clone()
	upReq.ContentLength = int64(len(body))

	upRes, err := h.client.Do(upReq)
	if err != nil {
		_ = h.cfg.Limiter.Refund(ctx, dec.ReservationID)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer upRes.Body.Close()

	for k, vv := range upRes.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	if isStream(body, r) {
		h.handleStream(ctx, w, upRes, dec, est.TotalTokens)
		return
	}
	h.handleJSON(ctx, w, upRes, dec, est.TotalTokens)
}

func (h *Handler) handleJSON(ctx context.Context, w http.ResponseWriter, upRes *http.Response, dec api.Decision, estimated int64) {
	body, err := io.ReadAll(upRes.Body)
	if err != nil {
		_ = h.cfg.Limiter.Refund(ctx, dec.ReservationID)
		http.Error(w, "upstream read", http.StatusBadGateway)
		return
	}
	actual := estimated
	if u, ok := llm.ParseUsageJSON(body); ok && u.Total() > 0 {
		actual = u.Total()
	}
	sd, err := h.cfg.Limiter.Settle(ctx, dec.ReservationID, actual)
	if err != nil {
		sd = dec
	} else {
		llm.RecordSettle(estimated, actual)
	}
	headers.Write(w.Header(), sd)
	w.WriteHeader(upRes.StatusCode)
	_, _ = w.Write(body)
}

func (h *Handler) handleStream(ctx context.Context, w http.ResponseWriter, upRes *http.Response, dec api.Decision, estimated int64) {
	// Emit Check-time headers before the body (settle updates budgets for later requests).
	headers.Write(w.Header(), dec)
	w.WriteHeader(upRes.StatusCode)

	pr, pw := io.Pipe()
	parseDone := make(chan struct{})
	var usage llm.Usage
	var usageOK bool
	go func() {
		defer close(parseDone)
		usage, usageOK = llm.ParseUsageSSE(pr)
	}()

	mw := io.MultiWriter(w, pw)
	_, copyErr := io.Copy(mw, upRes.Body)
	_ = pw.CloseWithError(copyErr)
	<-parseDone

	settleCtx, cancel := context.WithTimeout(context.Background(), h.cfg.SettleWait)
	defer cancel()
	actual := estimated
	if usageOK && usage.Total() > 0 {
		actual = usage.Total()
	}
	if copyErr != nil && !usageOK {
		_ = h.cfg.Limiter.Refund(settleCtx, dec.ReservationID)
		return
	}
	if _, err := h.cfg.Limiter.Settle(settleCtx, dec.ReservationID, actual); err != nil {
		return
	}
	llm.RecordSettle(estimated, actual)
}

func subjectFromRequest(r *http.Request) string {
	if k := r.Header.Get("X-API-Key"); k != "" {
		sum := sha256.Sum256([]byte(k))
		return hex.EncodeToString(sum[:8])
	}
	if a := r.Header.Get("Authorization"); a != "" {
		sum := sha256.Sum256([]byte(a))
		return hex.EncodeToString(sum[:8])
	}
	return "anonymous"
}

func modelFromBody(body []byte) string {
	var raw struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.Model == "" {
		return "-"
	}
	return raw.Model
}

func isStream(body []byte, r *http.Request) bool {
	var raw struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &raw)
	if raw.Stream {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}
