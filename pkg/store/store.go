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
	ErrWrongSettleMode     = errors.New("valve: wrong settle mode for reservation")
)

// ReservationTTL is how long a pending reservation is retained.
const ReservationTTL = 15 * time.Minute

// BorrowSpec describes minimum need and preferred chunk sizes for a lease borrow.
type BorrowSpec struct {
	MinRPM, MinTPM, MinITPM, MinOTPM       int64
	ChunkRPM, ChunkTPM, ChunkITPM, ChunkOTPM int64
}

// BorrowResult is the outcome of borrowing a lease chunk from shared budgets.
type BorrowResult struct {
	Allowed      bool
	LimitType    api.LimitType
	GotRPM       int64
	GotTPM       int64
	GotITPM      int64
	GotOTPM      int64
	RemainingRPM int64
	RemainingTPM int64
	RemainingITPM int64
	RemainingOTPM int64
	RetryAfter   time.Duration
	LimitRPM     int64
	LimitTPM     int64
	LimitITPM    int64
	LimitOTPM    int64
}

// Store persists bucket state and reservations.
type Store interface {
	Check(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost, reservationID string) (api.Decision, error)
	Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error)
	SettleIO(ctx context.Context, reservationID string, actualInput, actualOutput int64) (api.Decision, error)
	Refund(ctx context.Context, reservationID string) error
	Borrow(ctx context.Context, key api.Key, limits api.Limits, spec BorrowSpec) (BorrowResult, error)
	Return(ctx context.Context, key api.Key, limits api.Limits, rpm, tpm, itpm, otpm int64) error
}
