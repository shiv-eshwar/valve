package api

import "context"

// Limiter is the public dual-bucket rate limiter contract.
type Limiter interface {
	Check(ctx context.Context, key Key, limits Limits, cost Cost) (Decision, error)
	Settle(ctx context.Context, reservationID string, actualTokens int64) (Decision, error)
	Refund(ctx context.Context, reservationID string) error
}
