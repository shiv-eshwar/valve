package httpmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/headers"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	httpmw "github.com/shiv-eshwar/valve/pkg/middleware/http"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func TestMiddlewareDeny429(t *testing.T) {
	lim := limiter.New(memory.New())
	mw := httpmw.Middleware(httpmw.Config{
		Limiter: lim,
		Key:     func(*http.Request) api.Key { return api.Key{Subject: "u", Model: "m"} },
		Limits:  func(*http.Request) api.Limits { return api.Limits{RequestsPerMinute: 1, TokensPerMinute: 100} },
		Cost:    func(*http.Request) api.Cost { return api.Cost{Requests: 1, Tokens: 1} },
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != 200 {
		t.Fatalf("first=%d", rr.Code)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second=%d", rr2.Code)
	}
	if rr2.Header().Get(headers.LimitRequests) != "1" {
		t.Fatalf("headers=%v", rr2.Header())
	}
}
