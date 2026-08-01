package limiter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/store"
)

// Limiter wires a Store with fail-mode policy.
type Limiter struct {
	store    store.Store
	failMode api.FailMode
	newID    func() (string, error)
}

// Option configures Limiter.
type Option func(*Limiter)

// WithFailMode sets store-error behavior for Check.
func WithFailMode(m api.FailMode) Option {
	return func(l *Limiter) { l.failMode = m }
}

// WithIDGenerator overrides reservation ID creation (tests).
func WithIDGenerator(fn func() (string, error)) Option {
	return func(l *Limiter) { l.newID = fn }
}

// New returns a Limiter. Default fail mode is FailClosed.
func New(s store.Store, opts ...Option) *Limiter {
	l := &Limiter{
		store:    s,
		failMode: api.FailClosed,
		newID:    randomID,
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Check implements api.Limiter.
func (l *Limiter) Check(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost) (api.Decision, error) {
	id, err := l.newID()
	if err != nil {
		return l.onStoreErr(limits, err)
	}
	d, err := l.store.Check(ctx, key, limits, cost, id)
	if err != nil {
		return l.onStoreErr(limits, err)
	}
	return d, nil
}

func (l *Limiter) onStoreErr(limits api.Limits, err error) (api.Decision, error) {
	switch l.failMode {
	case api.FailOpen:
		return api.Decision{
			Allowed:      true,
			LimitType:    api.LimitTypeNone,
			RemainingRPM: limits.RequestsPerMinute,
			RemainingTPM: limits.TokensPerMinute,
			LimitRPM:     limits.RequestsPerMinute,
			LimitTPM:     limits.TokensPerMinute,
		}, nil
	default:
		return api.Decision{
			Allowed:    false,
			LimitType:  api.LimitTypeBackend,
			LimitRPM:   limits.RequestsPerMinute,
			LimitTPM:   limits.TokensPerMinute,
			RetryAfter: time.Second,
		}, nil
	}
}

// Settle implements api.Limiter.
func (l *Limiter) Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error) {
	return l.store.Settle(ctx, reservationID, actualTokens)
}

// Refund implements api.Limiter.
func (l *Limiter) Refund(ctx context.Context, reservationID string) error {
	return l.store.Refund(ctx, reservationID)
}

// Ensure interface compliance.
var _ api.Limiter = (*Limiter)(nil)
