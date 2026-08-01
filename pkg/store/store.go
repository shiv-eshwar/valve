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

// Store persists dual-bucket state and reservations.
type Store interface {
	Check(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost, reservationID string) (api.Decision, error)
	Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error)
	Refund(ctx context.Context, reservationID string) error
}
