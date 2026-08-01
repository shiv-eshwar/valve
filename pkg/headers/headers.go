package headers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
)

const (
	LimitRequests     = "x-ratelimit-limit-requests"
	RemainingRequests = "x-ratelimit-remaining-requests"
	ResetRequests     = "x-ratelimit-reset-requests"
	LimitTokens       = "x-ratelimit-limit-tokens"
	RemainingTokens   = "x-ratelimit-remaining-tokens"
	ResetTokens       = "x-ratelimit-reset-tokens"
	RetryAfter        = "Retry-After"
)

// Write sets OpenAI-compatible rate limit headers from a Decision.
func Write(h http.Header, d api.Decision) {
	h.Set(LimitRequests, strconv.FormatInt(d.LimitRPM, 10))
	h.Set(RemainingRequests, strconv.FormatInt(d.RemainingRPM, 10))
	h.Set(LimitTokens, strconv.FormatInt(d.LimitTPM, 10))
	h.Set(RemainingTokens, strconv.FormatInt(d.RemainingTPM, 10))
	if !d.ResetRPM.IsZero() {
		h.Set(ResetRequests, formatReset(d.ResetRPM))
	}
	if !d.ResetTPM.IsZero() {
		h.Set(ResetTokens, formatReset(d.ResetTPM))
	}
	if d.RetryAfter > 0 {
		secs := int(d.RetryAfter.Round(time.Second) / time.Second)
		if secs < 1 {
			secs = 1
		}
		h.Set(RetryAfter, strconv.Itoa(secs))
	}
}

func formatReset(t time.Time) string {
	// OpenAI often uses relative duration strings; seconds-until is widely parseable.
	d := time.Until(t)
	if d < 0 {
		d = 0
	}
	secs := int(d.Round(time.Second) / time.Second)
	return strconv.Itoa(secs) + "s"
}
