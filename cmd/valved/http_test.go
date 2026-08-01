package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/logx"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func TestHTTPCheckSettle(t *testing.T) {
	lim := limiter.New(memory.New())
	apiH := &httpAPI{lim: lim, log: logx.New(&bytes.Buffer{})}
	h := apiH.routes()

	raw := []byte(`{"key":{"subject":"s","model":"m"},"limits":{"requests_per_minute":10,"tokens_per_minute":1000},"cost":{"requests":1,"tokens":5}}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/check", bytes.NewReader(raw)))
	if rr.Code != 200 {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out["allowed"] != true {
		t.Fatalf("%v", out)
	}
	resID, _ := out["reservation_id"].(string)
	settleRaw, _ := json.Marshal(settleBody{ReservationID: resID, ActualTokens: 3})
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/v1/settle", bytes.NewReader(settleRaw)))
	if rr2.Code != 200 {
		t.Fatalf("settle=%d %s", rr2.Code, rr2.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	apiH := &httpAPI{lim: limiter.New(memory.New()), log: logx.New(&bytes.Buffer{})}
	rr := httptest.NewRecorder()
	apiH.routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
}
