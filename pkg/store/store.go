package store

import (
	"context"
	"errors"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
)

// Sentinel errors for reservation lifecycle.
var (
	ErrReservationNotFound = errors.New("valve: reservation not found")
	ErrReservationSettled  = errors.New("valve: reservation already settled")
	ErrReservationRefunded = errors.New("valve: reservation already refunded")
)

// ReservationTTL is how long a pending reservation is retained.
const ReservationTTL = 15 * time.Minute

// BorrowResult is the outcome of borrowing a lease chunk from shared budgets.
type BorrowResult struct {
	Allowed      bool
	LimitType    api.LimitType
	GotRPM       int64
	GotTPM       int64
	RemainingRPM int64
	RemainingTPM int64
	RetryAfter   time.Duration
	LimitRPM     int64
	LimitTPM     int64
}

// Store persists dual-bucket state and reservations.
type Store interface {
	Check(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost, reservationID string) (api.Decision, error)
	Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error)
	Refund(ctx context.Context, reservationID string) error
	// Borrow debits a chunk (at least min) for local leasing; no reservation row.
	Borrow(ctx context.Context, key api.Key, limits api.Limits, minRPM, minTPM, chunkRPM, chunkTPM int64) (BorrowResult, error)
	// Return credits unused lease tokens back to shared budgets.
	Return(ctx context.Context, key api.Key, limits api.Limits, rpm, tpm int64) error
}
