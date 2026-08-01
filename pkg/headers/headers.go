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
	LimitInputTokens     = "x-ratelimit-limit-input-tokens"
	RemainingInputTokens = "x-ratelimit-remaining-input-tokens"
	ResetInputTokens     = "x-ratelimit-reset-input-tokens"
	LimitOutputTokens     = "x-ratelimit-limit-output-tokens"
	RemainingOutputTokens = "x-ratelimit-remaining-output-tokens"
	ResetOutputTokens     = "x-ratelimit-reset-output-tokens"
	RetryAfter = "Retry-After"
)

// Write sets OpenAI-compatible rate limit headers from a Decision.
// When ITPM/OTPM fields are set, also writes input/output token headers.
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
	if d.LimitITPM > 0 || d.RemainingITPM > 0 || !d.ResetITPM.IsZero() {
		h.Set(LimitInputTokens, strconv.FormatInt(d.LimitITPM, 10))
		h.Set(RemainingInputTokens, strconv.FormatInt(d.RemainingITPM, 10))
		if !d.ResetITPM.IsZero() {
			h.Set(ResetInputTokens, formatReset(d.ResetITPM))
		}
	}
	if d.LimitOTPM > 0 || d.RemainingOTPM > 0 || !d.ResetOTPM.IsZero() {
		h.Set(LimitOutputTokens, strconv.FormatInt(d.LimitOTPM, 10))
		h.Set(RemainingOutputTokens, strconv.FormatInt(d.RemainingOTPM, 10))
		if !d.ResetOTPM.IsZero() {
			h.Set(ResetOutputTokens, formatReset(d.ResetOTPM))
		}
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
	d := time.Until(t)
	if d < 0 {
		d = 0
	}
	secs := int(d.Round(time.Second) / time.Second)
	return strconv.Itoa(secs) + "s"
}
