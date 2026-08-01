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
	if err := limits.Validate(); err != nil {
		return api.Decision{}, err
	}
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
	if cost.InputTokens < 0 {
		cost.InputTokens = 0
	}
	if cost.OutputTokens < 0 {
		cost.OutputTokens = 0
	}

	if lt, retry, ok := l.pool.Deny().Get(key); ok {
		return denyDecision(limits, lt, retry), nil
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
	spec := store.BorrowSpec{
		MinRPM:    cost.Requests,
		ChunkRPM:  cfg.RPMChunk,
		MinTPM:    cost.Tokens,
		ChunkTPM:  cfg.TPMChunk,
		MinITPM:   cost.InputTokens,
		ChunkITPM: cfg.ITPMChunk,
		MinOTPM:   cost.OutputTokens,
		ChunkOTPM: cfg.OTPMChunk,
	}
	br, err := l.store.Borrow(ctx, key, limits, spec)
	if err != nil {
		return l.onStoreErrFast(key, limits, cost, err)
	}
	if !br.Allowed {
		l.pool.Deny().Set(key, br.LimitType, br.RetryAfter)
		return borrowDenyDecision(limits, br), nil
	}

	l.pool.CreditLease(key, limits, br.GotRPM, br.GotTPM, br.GotITPM, br.GotOTPM)
	d, ok := l.pool.TryDebit(key, limits, cost, id)
	if !ok {
		l.pool.Deny().Set(key, api.LimitTypeBackend, time.Second)
		return denyDecision(limits, api.LimitTypeBackend, time.Second), nil
	}
	return d, nil
}

func denyDecision(limits api.Limits, lt api.LimitType, retry time.Duration) api.Decision {
	d := api.Decision{
		Allowed:    false,
		LimitType:  lt,
		RetryAfter: retry,
		LimitRPM:   limits.RequestsPerMinute,
	}
	if limits.Split() {
		d.LimitITPM = limits.InputTokensPerMinute
		d.LimitOTPM = limits.OutputTokensPerMinute
		d.LimitTPM = limits.OutputTokensPerMinute
	} else {
		d.LimitTPM = limits.TokensPerMinute
	}
	return d
}

func borrowDenyDecision(limits api.Limits, br store.BorrowResult) api.Decision {
	d := api.Decision{
		Allowed:       false,
		LimitType:     br.LimitType,
		RemainingRPM:  br.RemainingRPM,
		RemainingTPM:  br.RemainingTPM,
		RemainingITPM: br.RemainingITPM,
		RemainingOTPM: br.RemainingOTPM,
		LimitRPM:      limits.RequestsPerMinute,
		RetryAfter:    br.RetryAfter,
	}
	if limits.Split() {
		d.LimitITPM = limits.InputTokensPerMinute
		d.LimitOTPM = limits.OutputTokensPerMinute
		d.LimitTPM = limits.OutputTokensPerMinute
		d.RemainingTPM = br.RemainingOTPM
	} else {
		d.LimitTPM = limits.TokensPerMinute
	}
	return d
}

func (l *Limiter) onStoreErr(limits api.Limits, _ error) (api.Decision, error) {
	switch l.failMode {
	case api.FailOpen:
		d := api.Decision{
			Allowed:      true,
			LimitType:    api.LimitTypeNone,
			RemainingRPM: limits.RequestsPerMinute,
			LimitRPM:     limits.RequestsPerMinute,
		}
		if limits.Split() {
			d.RemainingITPM = limits.InputTokensPerMinute
			d.RemainingOTPM = limits.OutputTokensPerMinute
			d.RemainingTPM = limits.OutputTokensPerMinute
			d.LimitITPM = limits.InputTokensPerMinute
			d.LimitOTPM = limits.OutputTokensPerMinute
			d.LimitTPM = limits.OutputTokensPerMinute
		} else {
			d.RemainingTPM = limits.TokensPerMinute
			d.LimitTPM = limits.TokensPerMinute
		}
		return d, nil
	default:
		return denyDecision(limits, api.LimitTypeBackend, time.Second), nil
	}
}

func (l *Limiter) onStoreErrFast(key api.Key, limits api.Limits, cost api.Cost, _ error) (api.Decision, error) {
	if l.failMode == api.FailOpen && l.pool.Has(key, cost, limits) {
		id, err := l.newID()
		if err != nil {
			return denyDecision(limits, api.LimitTypeBackend, time.Second), nil
		}
		if d, ok := l.pool.TryDebit(key, limits, cost, id); ok {
			return d, nil
		}
	}
	return denyDecision(limits, api.LimitTypeBackend, time.Second), nil
}

// Settle implements api.Limiter.
func (l *Limiter) Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error) {
	if l.pool != nil {
		if r, ok := l.pool.GetReservation(reservationID); ok {
			if r.Limits.Split() {
				return api.Decision{}, store.ErrWrongSettleMode
			}
			dec, err := l.pool.SettleLocal(reservationID, actualTokens)
			if err != nil {
				return api.Decision{}, err
			}
			if dec.OvershootTPM > 0 {
				need := dec.OvershootTPM
				l.pool.NoteBorrow()
				br, berr := l.store.Borrow(ctx, r.Key, r.Limits, store.BorrowSpec{
					MinTPM: need, ChunkTPM: need,
				})
				if berr == nil && br.Allowed {
					l.pool.CreditLease(r.Key, r.Limits, br.GotRPM, br.GotTPM, 0, 0)
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

// SettleIO implements api.Limiter.
func (l *Limiter) SettleIO(ctx context.Context, reservationID string, actualInput, actualOutput int64) (api.Decision, error) {
	if l.pool != nil {
		if r, ok := l.pool.GetReservation(reservationID); ok {
			if !r.Limits.Split() {
				return api.Decision{}, store.ErrWrongSettleMode
			}
			dec, err := l.pool.SettleLocalIO(reservationID, actualInput, actualOutput)
			if err != nil {
				return api.Decision{}, err
			}
			if dec.OvershootITPM > 0 || dec.OvershootOTPM > 0 {
				l.pool.NoteBorrow()
				br, berr := l.store.Borrow(ctx, r.Key, r.Limits, store.BorrowSpec{
					MinITPM: dec.OvershootITPM, ChunkITPM: dec.OvershootITPM,
					MinOTPM: dec.OvershootOTPM, ChunkOTPM: dec.OvershootOTPM,
				})
				if berr == nil && br.Allowed {
					l.pool.CreditLease(r.Key, r.Limits, br.GotRPM, 0, br.GotITPM, br.GotOTPM)
					l.pool.ApplyBorrowedDeficitIO(reservationID, br.GotITPM, br.GotOTPM)
					if updated, ok := l.pool.GetReservation(reservationID); ok {
						return updated.LastDecision, nil
					}
				}
			}
			return dec, nil
		}
	}
	return l.store.SettleIO(ctx, reservationID, actualInput, actualOutput)
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
		if err := l.store.Return(ctx, snap.Key, snap.Limits, snap.RPM, snap.TPM, snap.ITPM, snap.OTPM); err != nil && first == nil {
			first = err
		}
	}
	return first
}

var _ api.Limiter = (*Limiter)(nil)
