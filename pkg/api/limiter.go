package api

import "context"

// Limiter is the public rate limiter contract (classic TPM and optional ITPM/OTPM).
type Limiter interface {
	Check(ctx context.Context, key Key, limits Limits, cost Cost) (Decision, error)
	Settle(ctx context.Context, reservationID string, actualTokens int64) (Decision, error)
	// SettleIO reconciles a split-mode reservation (ITPM + OTPM).
	SettleIO(ctx context.Context, reservationID string, actualInput, actualOutput int64) (Decision, error)
	Refund(ctx context.Context, reservationID string) error
}
