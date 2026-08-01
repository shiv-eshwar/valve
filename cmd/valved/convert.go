package main

import (
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	valvepb "github.com/shiv-eshwar/valve/pkg/gen/valve/v1"
)

func decisionToPB(d api.Decision) *valvepb.Decision {
	return &valvepb.Decision{
		Allowed:       d.Allowed,
		LimitType:     string(d.LimitType),
		RemainingRpm:  d.RemainingRPM,
		RemainingTpm:  d.RemainingTPM,
		LimitRpm:      d.LimitRPM,
		LimitTpm:      d.LimitTPM,
		RetryAfterMs:  d.RetryAfter.Milliseconds(),
		ReservationId: d.ReservationID,
		OvershootTpm:  d.OvershootTPM,
	}
}

func decisionFromHTTP(d api.Decision) map[string]any {
	return map[string]any{
		"allowed":        d.Allowed,
		"limit_type":     string(d.LimitType),
		"remaining_rpm":  d.RemainingRPM,
		"remaining_tpm":  d.RemainingTPM,
		"limit_rpm":      d.LimitRPM,
		"limit_tpm":      d.LimitTPM,
		"retry_after_ms": d.RetryAfter.Milliseconds(),
		"reservation_id": d.ReservationID,
		"overshoot_tpm":  d.OvershootTPM,
		"reset_rpm":      d.ResetRPM.UTC().Format(time.RFC3339Nano),
		"reset_tpm":      d.ResetTPM.UTC().Format(time.RFC3339Nano),
	}
}

func keyFromPB(k *valvepb.Key) api.Key {
	if k == nil {
		return api.Key{}
	}
	return api.Key{Subject: k.Subject, Model: k.Model}
}

func limitsFromPB(l *valvepb.Limits) api.Limits {
	if l == nil {
		return api.Limits{}
	}
	return api.Limits{RequestsPerMinute: l.RequestsPerMinute, TokensPerMinute: l.TokensPerMinute}
}

func costFromPB(c *valvepb.Cost) api.Cost {
	if c == nil {
		return api.Cost{}
	}
	return api.Cost{Requests: c.Requests, Tokens: c.Tokens}
}
