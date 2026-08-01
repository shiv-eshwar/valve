package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/headers"
	"github.com/shiv-eshwar/valve/pkg/lease"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/llm"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func TestProxyJSONSettleAndHeaders(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`))
	}))
	t.Cleanup(up.Close)
	u, _ := url.Parse(up.URL)

	lim := limiter.New(memory.New(), limiter.WithFastPath(lease.DefaultConfig()))
	h := NewHandler(ProxyConfig{
		Upstream: u,
		Limiter:  lim,
		Limits:   api.Limits{RequestsPerMinute: 10, TokensPerMinute: 10000},
		Gate:     llm.Gate{MaxRequestBytes: 1 << 20, MaxInputTokens: 10000},
	})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"abcd"}],"max_tokens":10}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-API-Key", "test-key")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get(headers.LimitRequests) != "10" {
		t.Fatalf("headers=%v", rr.Header())
	}
	if !strings.Contains(rr.Body.String(), "prompt_tokens") {
		t.Fatal(rr.Body.String())
	}
}

func TestProxyRateLimit429(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	t.Cleanup(up.Close)
	u, _ := url.Parse(up.URL)

	lim := limiter.New(memory.New())
	h := NewHandler(ProxyConfig{
		Upstream: u,
		Limiter:  lim,
		Limits:   api.Limits{RequestsPerMinute: 1, TokensPerMinute: 10000},
		Gate:     llm.Gate{MaxRequestBytes: 1 << 20},
	})

	body := `{"model":"m","messages":[{"content":"hi"}]}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("X-API-Key", "k")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if i == 0 && rr.Code != 200 {
			t.Fatalf("first=%d", rr.Code)
		}
		if i == 1 {
			if rr.Code != http.StatusTooManyRequests {
				t.Fatalf("second=%d body=%s", rr.Code, rr.Body.String())
			}
			var payload map[string]any
			_ = json.Unmarshal(rr.Body.Bytes(), &payload)
			if rr.Header().Get(headers.RetryAfter) == "" && rr.Header().Get(headers.LimitRequests) == "" {
				t.Fatal("missing rate limit headers on 429")
			}
		}
	}
}

func TestProxyStreamingSettle(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":4}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(up.Close)
	u, _ := url.Parse(up.URL)

	lim := limiter.New(memory.New(), limiter.WithFastPath(lease.Config{
		RPMChunk: 5, TPMChunk: 500, LeaseTTL: time.Minute,
	}))
	h := NewHandler(ProxyConfig{
		Upstream:   u,
		Limiter:    lim,
		Limits:     api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000},
		Gate:       llm.Gate{MaxRequestBytes: 1 << 20},
		SettleWait: 5 * time.Second,
	})

	body := `{"model":"m","stream":true,"messages":[{"content":"abcd"}],"max_tokens":10}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-API-Key", "stream-key")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("code=%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "[DONE]") {
		t.Fatal(rr.Body.String())
	}
	// Budgets settled — another request with tiny TPM should still work if we didn't leak reservation.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req2.Header.Set("X-API-Key", "stream-key")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != 200 {
		t.Fatalf("second stream code=%d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestProxyTooLarge(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(up.Close)
	u, _ := url.Parse(up.URL)
	h := NewHandler(ProxyConfig{
		Upstream: u,
		Limiter:  limiter.New(memory.New()),
		Limits:   api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000},
		Gate:     llm.Gate{MaxRequestBytes: 10},
	})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 50)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code=%d", rr.Code)
	}
}
