package limiter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/lease"
	"github.com/shiv-eshwar/valve/pkg/store"
)

// Limiter wires a Store with fail-mode policy and optional fast path.
type Limiter struct {
	store    store.Store
	failMode api.FailMode
	newID    func() (string, error)
	pool     *lease.Pool
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

// WithFastPath enables L0 deny cache + L1 local lease borrowing.
func WithFastPath(cfg lease.Config) Option {
	return func(l *Limiter) {
		l.pool = lease.NewPool(cfg)
	}
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

// Pool exposes lease stats when fast path is enabled.
func (l *Limiter) Pool() *lease.Pool { return l.pool }

// Check implements api.Limiter.
func (l *Limiter) Check(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost) (api.Decision, error) {
	if l.pool != nil {
		return l.checkFast(ctx, key, limits, cost)
	}
	return l.checkDirect(ctx, key, limits, cost)
}

func (l *Limiter) checkDirect(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost) (api.Decision, error) {
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

func (l *Limiter) checkFast(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost) (api.Decision, error) {
	if cost.Requests < 0 {
		cost.Requests = 0
	}
	if cost.Tokens < 0 {
		cost.Tokens = 0
	}

	if lt, retry, ok := l.pool.Deny().Get(key); ok {
		return api.Decision{
			Allowed:      false,
			LimitType:    lt,
			RetryAfter:   retry,
			LimitRPM:     limits.RequestsPerMinute,
			LimitTPM:     limits.TokensPerMinute,
			RemainingRPM: 0,
			RemainingTPM: 0,
		}, nil
	}

	id, err := l.newID()
	if err != nil {
		return l.onStoreErrFast(key, limits, cost, err)
	}

	if d, ok := l.pool.TryDebit(key, limits, cost, id); ok {
		return d, nil
	}

	cfg := l.pool.Config()
	l.pool.NoteBorrow()
	br, err := l.store.Borrow(ctx, key, limits, cost.Requests, cost.Tokens, cfg.RPMChunk, cfg.TPMChunk)
	if err != nil {
		return l.onStoreErrFast(key, limits, cost, err)
	}
	if !br.Allowed {
		l.pool.Deny().Set(key, br.LimitType, br.RetryAfter)
		return api.Decision{
			Allowed:      false,
			LimitType:    br.LimitType,
			RemainingRPM: br.RemainingRPM,
			RemainingTPM: br.RemainingTPM,
			LimitRPM:     limits.RequestsPerMinute,
			LimitTPM:     limits.TokensPerMinute,
			RetryAfter:   br.RetryAfter,
		}, nil
	}

	l.pool.CreditLease(key, limits, br.GotRPM, br.GotTPM)
	d, ok := l.pool.TryDebit(key, limits, cost, id)
	if !ok {
		// Should not happen after successful borrow of at least cost.
		l.pool.Deny().Set(key, api.LimitTypeBackend, time.Second)
		return api.Decision{
			Allowed:    false,
			LimitType:  api.LimitTypeBackend,
			LimitRPM:   limits.RequestsPerMinute,
			LimitTPM:   limits.TokensPerMinute,
			RetryAfter: time.Second,
		}, nil
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

// onStoreErrFast: fail-open only if local lease can cover cost; else backend deny.
func (l *Limiter) onStoreErrFast(key api.Key, limits api.Limits, cost api.Cost, _ error) (api.Decision, error) {
	if l.failMode == api.FailOpen && l.pool.Has(key, cost) {
		id, err := l.newID()
		if err != nil {
			return api.Decision{
				Allowed:    false,
				LimitType:  api.LimitTypeBackend,
				LimitRPM:   limits.RequestsPerMinute,
				LimitTPM:   limits.TokensPerMinute,
				RetryAfter: time.Second,
			}, nil
		}
		if d, ok := l.pool.TryDebit(key, limits, cost, id); ok {
			return d, nil
		}
	}
	return api.Decision{
		Allowed:    false,
		LimitType:  api.LimitTypeBackend,
		LimitRPM:   limits.RequestsPerMinute,
		LimitTPM:   limits.TokensPerMinute,
		RetryAfter: time.Second,
	}, nil
}

// Settle implements api.Limiter.
func (l *Limiter) Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error) {
	if l.pool != nil {
		if _, ok := l.pool.GetReservation(reservationID); ok {
			dec, err := l.pool.SettleLocal(reservationID, actualTokens)
			if err != nil {
				return api.Decision{}, err
			}
			if dec.OvershootTPM > 0 {
				r, _ := l.pool.GetReservation(reservationID)
				need := dec.OvershootTPM
				l.pool.NoteBorrow()
				br, berr := l.store.Borrow(ctx, r.Key, r.Limits, 0, need, 0, need)
				if berr == nil && br.Allowed {
					l.pool.CreditLease(r.Key, r.Limits, br.GotRPM, br.GotTPM)
					l.pool.ApplyBorrowedDeficit(reservationID, br.GotTPM)
					if updated, ok := l.pool.GetReservation(reservationID); ok {
						return updated.LastDecision, nil
					}
				}
			}
			return dec, nil
		}
	}
	return l.store.Settle(ctx, reservationID, actualTokens)
}

// Refund implements api.Limiter.
func (l *Limiter) Refund(ctx context.Context, reservationID string) error {
	if l.pool != nil {
		if _, ok := l.pool.GetReservation(reservationID); ok {
			return l.pool.RefundLocal(reservationID)
		}
	}
	return l.store.Refund(ctx, reservationID)
}

// Close best-effort returns unused lease credits to the store.
func (l *Limiter) Close(ctx context.Context) error {
	if l.pool == nil {
		return nil
	}
	var first error
	for _, snap := range l.pool.SnapshotLeases() {
		if err := l.store.Return(ctx, snap.Key, snap.Limits, snap.RPM, snap.TPM); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Ensure interface compliance.
var _ api.Limiter = (*Limiter)(nil)
