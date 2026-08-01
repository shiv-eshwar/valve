package memory

import (
	"context"
	"sync"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/bucket"
	"github.com/shiv-eshwar/valve/pkg/store"
)

type bucketState struct {
	tokens float64
	lastMs int64
}

type reservation struct {
	subject     string
	model       string
	rpmCost     int64
	tpmReserved int64
	limitRPM    int64
	limitTPM    int64
	status      string // pending | settled | refunded
	createdMs   int64
	expiresAt   time.Time
	// snapshot from last settle for idempotent Settle
	lastDecision api.Decision
}

// Store is an in-process dual-bucket store with the same semantics as Redis Lua.
type Store struct {
	mu  sync.Mutex
	rpm map[string]bucketState
	tpm map[string]bucketState
	res map[string]reservation
	now func() time.Time
}

// Option configures Store.
type Option func(*Store)

// WithClock overrides the time source (tests).
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// New returns an empty memory store.
func New(opts ...Option) *Store {
	s := &Store{
		rpm: make(map[string]bucketState),
		tpm: make(map[string]bucketState),
		res: make(map[string]reservation),
		now: time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func bucketKey(key api.Key) string {
	model := key.Model
	if model == "" {
		model = "-"
	}
	return key.Subject + "\x00" + model
}

func (s *Store) nowMs() int64 {
	return s.now().UnixMilli()
}

func (s *Store) purgeExpired(now time.Time) {
	for id, r := range s.res {
		if now.After(r.expiresAt) {
			delete(s.res, id)
		}
	}
}

func loadBucket(m map[string]bucketState, k string, capacity int64, nowMs int64) bucket.State {
	b, ok := m[k]
	st := bucket.State{Capacity: capacity, Tokens: b.tokens, LastMs: b.lastMs}
	if !ok || b.lastMs == 0 {
		st.Tokens = 0
		st.LastMs = 0
	}
	st = bucket.Refill(st, nowMs)
	return st
}

func saveBucket(m map[string]bucketState, k string, st bucket.State) {
	m[k] = bucketState{tokens: st.Tokens, lastMs: st.LastMs}
}

// Check implements store.Store.
func (s *Store) Check(_ context.Context, key api.Key, limits api.Limits, cost api.Cost, reservationID string) (api.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	nowMs := now.UnixMilli()
	s.purgeExpired(now)

	if key.Subject == "" {
		return api.Decision{}, errInvalid("subject required")
	}
	if reservationID == "" {
		return api.Decision{}, errInvalid("reservation id required")
	}
	if cost.Requests < 0 {
		cost.Requests = 0
	}
	if cost.Tokens < 0 {
		cost.Tokens = 0
	}

	bk := bucketKey(key)
	rpm := loadBucket(s.rpm, bk, limits.RequestsPerMinute, nowMs)
	tpm := loadBucket(s.tpm, bk, limits.TokensPerMinute, nowMs)

	dec := api.Decision{
		LimitRPM: limits.RequestsPerMinute,
		LimitTPM: limits.TokensPerMinute,
	}

	rpm2, rpmRes := bucket.TryConsume(rpm, nowMs, cost.Requests)
	if !rpmRes.Allowed {
		dec.Allowed = false
		dec.LimitType = api.LimitTypeRequests
		dec.RemainingRPM = rpmRes.Remaining
		dec.RemainingTPM = bucket.RemainingInt(tpm.Tokens)
		dec.ResetRPM = rpmRes.ResetAt
		dec.ResetTPM = bucket.ResetAt(tpm, nowMs)
		dec.RetryAfter = rpmRes.RetryAfter
		return dec, nil
	}

	tpm2, tpmRes := bucket.TryConsume(tpm, nowMs, cost.Tokens)
	if !tpmRes.Allowed {
		// all-or-nothing: do not persist RPM debit
		dec.Allowed = false
		dec.LimitType = api.LimitTypeTokens
		dec.RemainingRPM = bucket.RemainingInt(rpm.Tokens)
		dec.RemainingTPM = tpmRes.Remaining
		dec.ResetRPM = bucket.ResetAt(rpm, nowMs)
		dec.ResetTPM = tpmRes.ResetAt
		dec.RetryAfter = tpmRes.RetryAfter
		return dec, nil
	}

	saveBucket(s.rpm, bk, rpm2)
	saveBucket(s.tpm, bk, tpm2)

	s.res[reservationID] = reservation{
		subject:     key.Subject,
		model:       key.Model,
		rpmCost:     cost.Requests,
		tpmReserved: cost.Tokens,
		limitRPM:    limits.RequestsPerMinute,
		limitTPM:    limits.TokensPerMinute,
		status:      "pending",
		createdMs:   nowMs,
		expiresAt:   now.Add(store.ReservationTTL),
	}

	dec.Allowed = true
	dec.RemainingRPM = rpmRes.Remaining
	dec.RemainingTPM = tpmRes.Remaining
	dec.ResetRPM = rpmRes.ResetAt
	dec.ResetTPM = tpmRes.ResetAt
	dec.ReservationID = reservationID
	return dec, nil
}

// Settle implements store.Store.
func (s *Store) Settle(_ context.Context, reservationID string, actualTokens int64) (api.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	nowMs := now.UnixMilli()
	s.purgeExpired(now)

	r, ok := s.res[reservationID]
	if !ok {
		return api.Decision{}, store.ErrReservationNotFound
	}
	if r.status == "settled" {
		return r.lastDecision, nil
	}
	if r.status == "refunded" {
		return api.Decision{}, store.ErrReservationRefunded
	}
	if actualTokens < 0 {
		actualTokens = 0
	}

	key := api.Key{Subject: r.subject, Model: r.model}
	bk := bucketKey(key)
	tpm := loadBucket(s.tpm, bk, r.limitTPM, nowMs)
	rpm := loadBucket(s.rpm, bk, r.limitRPM, nowMs)

	var overshoot int64
	delta := r.tpmReserved - actualTokens
	switch {
	case delta > 0:
		tpm = bucket.Credit(tpm, nowMs, delta)
	case delta < 0:
		var debited int64
		tpm, debited, overshoot = bucket.DebitFloor(tpm, nowMs, -delta)
		_ = debited
	}

	saveBucket(s.tpm, bk, tpm)

	dec := api.Decision{
		Allowed:       true,
		RemainingRPM:  bucket.RemainingInt(rpm.Tokens),
		RemainingTPM:  bucket.RemainingInt(tpm.Tokens),
		LimitRPM:      r.limitRPM,
		LimitTPM:      r.limitTPM,
		ResetRPM:      bucket.ResetAt(rpm, nowMs),
		ResetTPM:      bucket.ResetAt(tpm, nowMs),
		ReservationID: reservationID,
		OvershootTPM:  overshoot,
	}
	r.status = "settled"
	r.lastDecision = dec
	r.expiresAt = now.Add(store.ReservationTTL)
	s.res[reservationID] = r
	return dec, nil
}

// Refund implements store.Store.
func (s *Store) Refund(_ context.Context, reservationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	nowMs := now.UnixMilli()
	s.purgeExpired(now)

	r, ok := s.res[reservationID]
	if !ok {
		return store.ErrReservationNotFound
	}
	if r.status == "refunded" {
		return nil
	}
	if r.status == "settled" {
		return store.ErrReservationSettled
	}

	key := api.Key{Subject: r.subject, Model: r.model}
	bk := bucketKey(key)
	rpm := loadBucket(s.rpm, bk, r.limitRPM, nowMs)
	tpm := loadBucket(s.tpm, bk, r.limitTPM, nowMs)
	rpm = bucket.Credit(rpm, nowMs, r.rpmCost)
	tpm = bucket.Credit(tpm, nowMs, r.tpmReserved)
	saveBucket(s.rpm, bk, rpm)
	saveBucket(s.tpm, bk, tpm)

	r.status = "refunded"
	r.expiresAt = now.Add(store.ReservationTTL)
	s.res[reservationID] = r
	return nil
}

type invalidError string

func (e invalidError) Error() string { return "valve: " + string(e) }

func errInvalid(msg string) error { return invalidError(msg) }
