package headers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/headers"
)

func TestWrite(t *testing.T) {
	h := make(http.Header)
	d := api.Decision{
		LimitRPM:     60,
		RemainingRPM: 59,
		LimitTPM:     90000,
		RemainingTPM: 88000,
		ResetRPM:     time.Now().Add(30 * time.Second),
		ResetTPM:     time.Now().Add(45 * time.Second),
		RetryAfter:   2 * time.Second,
	}
	headers.Write(h, d)
	if h.Get(headers.LimitRequests) != "60" {
		t.Fatal(h.Get(headers.LimitRequests))
	}
	if h.Get(headers.RemainingTokens) != "88000" {
		t.Fatal(h.Get(headers.RemainingTokens))
	}
	if h.Get(headers.RetryAfter) != "2" {
		t.Fatal(h.Get(headers.RetryAfter))
	}
	if h.Get(headers.ResetRequests) == "" || h.Get(headers.ResetTokens) == "" {
		t.Fatal("missing reset headers")
	}
}
