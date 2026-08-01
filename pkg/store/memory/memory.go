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
	subject      string
	model        string
	split        bool
	rpmCost      int64
	tpmReserved  int64
	itpmReserved int64
	otpmReserved int64
	limitRPM     int64
	limitTPM     int64
	limitITPM    int64
	limitOTPM    int64
	status       string // pending | settled | refunded
	createdMs    int64
	expiresAt    time.Time
	lastDecision api.Decision
}

// Store is an in-process dual/triple-bucket store with Redis-matching semantics.
type Store struct {
	mu   sync.Mutex
	rpm  map[string]bucketState
	tpm  map[string]bucketState
	itpm map[string]bucketState
	otpm map[string]bucketState
	res  map[string]reservation
	now  func() time.Time
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
		rpm:  make(map[string]bucketState),
		tpm:  make(map[string]bucketState),
		itpm: make(map[string]bucketState),
		otpm: make(map[string]bucketState),
		res:  make(map[string]reservation),
		now:  time.Now,
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

func clampCost(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

// Check implements store.Store.
func (s *Store) Check(_ context.Context, key api.Key, limits api.Limits, cost api.Cost, reservationID string) (api.Decision, error) {
	if err := limits.Validate(); err != nil {
		return api.Decision{}, err
	}
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
	cost.Requests = clampCost(cost.Requests)
	cost.Tokens = clampCost(cost.Tokens)
	cost.InputTokens = clampCost(cost.InputTokens)
	cost.OutputTokens = clampCost(cost.OutputTokens)

	if limits.Split() {
		return s.checkSplitLocked(key, limits, cost, reservationID, now, nowMs)
	}
	return s.checkClassicLocked(key, limits, cost, reservationID, now, nowMs)
}

func (s *Store) checkClassicLocked(key api.Key, limits api.Limits, cost api.Cost, reservationID string, now time.Time, nowMs int64) (api.Decision, error) {
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
		split:       false,
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

func (s *Store) checkSplitLocked(key api.Key, limits api.Limits, cost api.Cost, reservationID string, now time.Time, nowMs int64) (api.Decision, error) {
	bk := bucketKey(key)
	rpm := loadBucket(s.rpm, bk, limits.RequestsPerMinute, nowMs)
	itpm := loadBucket(s.itpm, bk, limits.InputTokensPerMinute, nowMs)
	otpm := loadBucket(s.otpm, bk, limits.OutputTokensPerMinute, nowMs)

	dec := api.Decision{
		LimitRPM:  limits.RequestsPerMinute,
		LimitITPM: limits.InputTokensPerMinute,
		LimitOTPM: limits.OutputTokensPerMinute,
		LimitTPM:  limits.OutputTokensPerMinute, // OpenAI header compat = output
	}

	rpm2, rpmRes := bucket.TryConsume(rpm, nowMs, cost.Requests)
	if !rpmRes.Allowed {
		dec.Allowed = false
		dec.LimitType = api.LimitTypeRequests
		dec.RemainingRPM = rpmRes.Remaining
		dec.RemainingITPM = bucket.RemainingInt(itpm.Tokens)
		dec.RemainingOTPM = bucket.RemainingInt(otpm.Tokens)
		dec.RemainingTPM = dec.RemainingOTPM
		dec.ResetRPM = rpmRes.ResetAt
		dec.ResetITPM = bucket.ResetAt(itpm, nowMs)
		dec.ResetOTPM = bucket.ResetAt(otpm, nowMs)
		dec.ResetTPM = dec.ResetOTPM
		dec.RetryAfter = rpmRes.RetryAfter
		return dec, nil
	}

	itpm2, itpmRes := bucket.TryConsume(itpm, nowMs, cost.InputTokens)
	if !itpmRes.Allowed {
		dec.Allowed = false
		dec.LimitType = api.LimitTypeInputTokens
		dec.RemainingRPM = bucket.RemainingInt(rpm.Tokens)
		dec.RemainingITPM = itpmRes.Remaining
		dec.RemainingOTPM = bucket.RemainingInt(otpm.Tokens)
		dec.RemainingTPM = dec.RemainingOTPM
		dec.ResetRPM = bucket.ResetAt(rpm, nowMs)
		dec.ResetITPM = itpmRes.ResetAt
		dec.ResetOTPM = bucket.ResetAt(otpm, nowMs)
		dec.ResetTPM = dec.ResetOTPM
		dec.RetryAfter = itpmRes.RetryAfter
		return dec, nil
	}

	otpm2, otpmRes := bucket.TryConsume(otpm, nowMs, cost.OutputTokens)
	if !otpmRes.Allowed {
		dec.Allowed = false
		dec.LimitType = api.LimitTypeOutputTokens
		dec.RemainingRPM = bucket.RemainingInt(rpm.Tokens)
		dec.RemainingITPM = bucket.RemainingInt(itpm.Tokens)
		dec.RemainingOTPM = otpmRes.Remaining
		dec.RemainingTPM = dec.RemainingOTPM
		dec.ResetRPM = bucket.ResetAt(rpm, nowMs)
		dec.ResetITPM = bucket.ResetAt(itpm, nowMs)
		dec.ResetOTPM = otpmRes.ResetAt
		dec.ResetTPM = dec.ResetOTPM
		dec.RetryAfter = otpmRes.RetryAfter
		return dec, nil
	}

	saveBucket(s.rpm, bk, rpm2)
	saveBucket(s.itpm, bk, itpm2)
	saveBucket(s.otpm, bk, otpm2)

	s.res[reservationID] = reservation{
		subject:      key.Subject,
		model:        key.Model,
		split:        true,
		rpmCost:      cost.Requests,
		itpmReserved: cost.InputTokens,
		otpmReserved: cost.OutputTokens,
		limitRPM:     limits.RequestsPerMinute,
		limitITPM:    limits.InputTokensPerMinute,
		limitOTPM:    limits.OutputTokensPerMinute,
		status:       "pending",
		createdMs:    nowMs,
		expiresAt:    now.Add(store.ReservationTTL),
	}

	dec.Allowed = true
	dec.RemainingRPM = rpmRes.Remaining
	dec.RemainingITPM = itpmRes.Remaining
	dec.RemainingOTPM = otpmRes.Remaining
	dec.RemainingTPM = otpmRes.Remaining
	dec.ResetRPM = rpmRes.ResetAt
	dec.ResetITPM = itpmRes.ResetAt
	dec.ResetOTPM = otpmRes.ResetAt
	dec.ResetTPM = otpmRes.ResetAt
	dec.ReservationID = reservationID
	return dec, nil
}

// Settle implements store.Store (classic TPM).
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
	if r.split {
		return api.Decision{}, store.ErrWrongSettleMode
	}
	if r.status == "settled" {
		return r.lastDecision, nil
	}
	if r.status == "refunded" {
		return api.Decision{}, store.ErrReservationRefunded
	}
	actualTokens = clampCost(actualTokens)

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

// SettleIO implements store.Store (split ITPM/OTPM).
func (s *Store) SettleIO(_ context.Context, reservationID string, actualInput, actualOutput int64) (api.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	nowMs := now.UnixMilli()
	s.purgeExpired(now)

	r, ok := s.res[reservationID]
	if !ok {
		return api.Decision{}, store.ErrReservationNotFound
	}
	if !r.split {
		return api.Decision{}, store.ErrWrongSettleMode
	}
	if r.status == "settled" {
		return r.lastDecision, nil
	}
	if r.status == "refunded" {
		return api.Decision{}, store.ErrReservationRefunded
	}
	actualInput = clampCost(actualInput)
	actualOutput = clampCost(actualOutput)

	key := api.Key{Subject: r.subject, Model: r.model}
	bk := bucketKey(key)
	rpm := loadBucket(s.rpm, bk, r.limitRPM, nowMs)
	itpm := loadBucket(s.itpm, bk, r.limitITPM, nowMs)
	otpm := loadBucket(s.otpm, bk, r.limitOTPM, nowMs)

	var overIn, overOut int64
	itpm, overIn = settleAxis(itpm, nowMs, r.itpmReserved, actualInput)
	otpm, overOut = settleAxis(otpm, nowMs, r.otpmReserved, actualOutput)
	saveBucket(s.itpm, bk, itpm)
	saveBucket(s.otpm, bk, otpm)

	dec := api.Decision{
		Allowed:       true,
		RemainingRPM:  bucket.RemainingInt(rpm.Tokens),
		RemainingITPM: bucket.RemainingInt(itpm.Tokens),
		RemainingOTPM: bucket.RemainingInt(otpm.Tokens),
		RemainingTPM:  bucket.RemainingInt(otpm.Tokens),
		LimitRPM:      r.limitRPM,
		LimitITPM:     r.limitITPM,
		LimitOTPM:     r.limitOTPM,
		LimitTPM:      r.limitOTPM,
		ResetRPM:      bucket.ResetAt(rpm, nowMs),
		ResetITPM:     bucket.ResetAt(itpm, nowMs),
		ResetOTPM:     bucket.ResetAt(otpm, nowMs),
		ResetTPM:      bucket.ResetAt(otpm, nowMs),
		ReservationID: reservationID,
		OvershootITPM: overIn,
		OvershootOTPM: overOut,
	}
	r.status = "settled"
	r.lastDecision = dec
	r.expiresAt = now.Add(store.ReservationTTL)
	s.res[reservationID] = r
	return dec, nil
}

func settleAxis(st bucket.State, nowMs, reserved, actual int64) (bucket.State, int64) {
	var overshoot int64
	delta := reserved - actual
	switch {
	case delta > 0:
		st = bucket.Credit(st, nowMs, delta)
	case delta < 0:
		var debited int64
		st, debited, overshoot = bucket.DebitFloor(st, nowMs, -delta)
		_ = debited
	}
	return st, overshoot
}

// Borrow implements store.Store.
func (s *Store) Borrow(_ context.Context, key api.Key, limits api.Limits, spec store.BorrowSpec) (store.BorrowResult, error) {
	if err := limits.Validate(); err != nil {
		return store.BorrowResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	nowMs := s.now().UnixMilli()
	if key.Subject == "" {
		return store.BorrowResult{}, errInvalid("subject required")
	}
	spec.MinRPM = clampCost(spec.MinRPM)
	spec.MinTPM = clampCost(spec.MinTPM)
	spec.MinITPM = clampCost(spec.MinITPM)
	spec.MinOTPM = clampCost(spec.MinOTPM)
	spec.ChunkRPM = clampCost(spec.ChunkRPM)
	spec.ChunkTPM = clampCost(spec.ChunkTPM)
	spec.ChunkITPM = clampCost(spec.ChunkITPM)
	spec.ChunkOTPM = clampCost(spec.ChunkOTPM)

	if limits.Split() {
		return s.borrowSplitLocked(key, limits, spec, nowMs)
	}
	return s.borrowClassicLocked(key, limits, spec, nowMs)
}

func wantGot(avail, min, chunk int64) int64 {
	want := min
	if chunk > want {
		want = chunk
	}
	if avail < want {
		return avail
	}
	return want
}

func (s *Store) borrowClassicLocked(key api.Key, limits api.Limits, spec store.BorrowSpec, nowMs int64) (store.BorrowResult, error) {
	bk := bucketKey(key)
	rpm := loadBucket(s.rpm, bk, limits.RequestsPerMinute, nowMs)
	tpm := loadBucket(s.tpm, bk, limits.TokensPerMinute, nowMs)
	availRPM := bucket.RemainingInt(rpm.Tokens)
	availTPM := bucket.RemainingInt(tpm.Tokens)

	out := store.BorrowResult{
		LimitRPM:     limits.RequestsPerMinute,
		LimitTPM:     limits.TokensPerMinute,
		RemainingRPM: availRPM,
		RemainingTPM: availTPM,
	}

	if availRPM < spec.MinRPM {
		_, res := bucket.TryConsume(rpm, nowMs, spec.MinRPM)
		out.Allowed = false
		out.LimitType = api.LimitTypeRequests
		out.RetryAfter = res.RetryAfter
		return out, nil
	}
	if availTPM < spec.MinTPM {
		_, res := bucket.TryConsume(tpm, nowMs, spec.MinTPM)
		out.Allowed = false
		out.LimitType = api.LimitTypeTokens
		out.RetryAfter = res.RetryAfter
		return out, nil
	}

	gotRPM := wantGot(availRPM, spec.MinRPM, spec.ChunkRPM)
	gotTPM := wantGot(availTPM, spec.MinTPM, spec.ChunkTPM)
	rpm2, _ := bucket.TryConsume(rpm, nowMs, gotRPM)
	tpm2, _ := bucket.TryConsume(tpm, nowMs, gotTPM)
	saveBucket(s.rpm, bk, rpm2)
	saveBucket(s.tpm, bk, tpm2)

	out.Allowed = true
	out.GotRPM = gotRPM
	out.GotTPM = gotTPM
	out.RemainingRPM = bucket.RemainingInt(rpm2.Tokens)
	out.RemainingTPM = bucket.RemainingInt(tpm2.Tokens)
	return out, nil
}

func (s *Store) borrowSplitLocked(key api.Key, limits api.Limits, spec store.BorrowSpec, nowMs int64) (store.BorrowResult, error) {
	bk := bucketKey(key)
	rpm := loadBucket(s.rpm, bk, limits.RequestsPerMinute, nowMs)
	itpm := loadBucket(s.itpm, bk, limits.InputTokensPerMinute, nowMs)
	otpm := loadBucket(s.otpm, bk, limits.OutputTokensPerMinute, nowMs)
	availRPM := bucket.RemainingInt(rpm.Tokens)
	availITPM := bucket.RemainingInt(itpm.Tokens)
	availOTPM := bucket.RemainingInt(otpm.Tokens)

	out := store.BorrowResult{
		LimitRPM:      limits.RequestsPerMinute,
		LimitITPM:     limits.InputTokensPerMinute,
		LimitOTPM:     limits.OutputTokensPerMinute,
		LimitTPM:      limits.OutputTokensPerMinute,
		RemainingRPM:  availRPM,
		RemainingITPM: availITPM,
		RemainingOTPM: availOTPM,
		RemainingTPM:  availOTPM,
	}

	if availRPM < spec.MinRPM {
		_, res := bucket.TryConsume(rpm, nowMs, spec.MinRPM)
		out.Allowed = false
		out.LimitType = api.LimitTypeRequests
		out.RetryAfter = res.RetryAfter
		return out, nil
	}
	if availITPM < spec.MinITPM {
		_, res := bucket.TryConsume(itpm, nowMs, spec.MinITPM)
		out.Allowed = false
		out.LimitType = api.LimitTypeInputTokens
		out.RetryAfter = res.RetryAfter
		return out, nil
	}
	if availOTPM < spec.MinOTPM {
		_, res := bucket.TryConsume(otpm, nowMs, spec.MinOTPM)
		out.Allowed = false
		out.LimitType = api.LimitTypeOutputTokens
		out.RetryAfter = res.RetryAfter
		return out, nil
	}

	gotRPM := wantGot(availRPM, spec.MinRPM, spec.ChunkRPM)
	gotITPM := wantGot(availITPM, spec.MinITPM, spec.ChunkITPM)
	gotOTPM := wantGot(availOTPM, spec.MinOTPM, spec.ChunkOTPM)
	rpm2, _ := bucket.TryConsume(rpm, nowMs, gotRPM)
	itpm2, _ := bucket.TryConsume(itpm, nowMs, gotITPM)
	otpm2, _ := bucket.TryConsume(otpm, nowMs, gotOTPM)
	saveBucket(s.rpm, bk, rpm2)
	saveBucket(s.itpm, bk, itpm2)
	saveBucket(s.otpm, bk, otpm2)

	out.Allowed = true
	out.GotRPM = gotRPM
	out.GotITPM = gotITPM
	out.GotOTPM = gotOTPM
	out.RemainingRPM = bucket.RemainingInt(rpm2.Tokens)
	out.RemainingITPM = bucket.RemainingInt(itpm2.Tokens)
	out.RemainingOTPM = bucket.RemainingInt(otpm2.Tokens)
	out.RemainingTPM = out.RemainingOTPM
	return out, nil
}

// Return implements store.Store.
func (s *Store) Return(_ context.Context, key api.Key, limits api.Limits, rpmAdd, tpmAdd, itpmAdd, otpmAdd int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if key.Subject == "" {
		return errInvalid("subject required")
	}
	if rpmAdd <= 0 && tpmAdd <= 0 && itpmAdd <= 0 && otpmAdd <= 0 {
		return nil
	}
	nowMs := s.now().UnixMilli()
	bk := bucketKey(key)
	if limits.Split() {
		rpm := loadBucket(s.rpm, bk, limits.RequestsPerMinute, nowMs)
		itpm := loadBucket(s.itpm, bk, limits.InputTokensPerMinute, nowMs)
		otpm := loadBucket(s.otpm, bk, limits.OutputTokensPerMinute, nowMs)
		rpm = bucket.Credit(rpm, nowMs, rpmAdd)
		itpm = bucket.Credit(itpm, nowMs, itpmAdd)
		otpm = bucket.Credit(otpm, nowMs, otpmAdd)
		saveBucket(s.rpm, bk, rpm)
		saveBucket(s.itpm, bk, itpm)
		saveBucket(s.otpm, bk, otpm)
		return nil
	}
	rpm := loadBucket(s.rpm, bk, limits.RequestsPerMinute, nowMs)
	tpm := loadBucket(s.tpm, bk, limits.TokensPerMinute, nowMs)
	rpm = bucket.Credit(rpm, nowMs, rpmAdd)
	tpm = bucket.Credit(tpm, nowMs, tpmAdd)
	saveBucket(s.rpm, bk, rpm)
	saveBucket(s.tpm, bk, tpm)
	return nil
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
	rpm = bucket.Credit(rpm, nowMs, r.rpmCost)
	saveBucket(s.rpm, bk, rpm)

	if r.split {
		itpm := loadBucket(s.itpm, bk, r.limitITPM, nowMs)
		otpm := loadBucket(s.otpm, bk, r.limitOTPM, nowMs)
		itpm = bucket.Credit(itpm, nowMs, r.itpmReserved)
		otpm = bucket.Credit(otpm, nowMs, r.otpmReserved)
		saveBucket(s.itpm, bk, itpm)
		saveBucket(s.otpm, bk, otpm)
	} else {
		tpm := loadBucket(s.tpm, bk, r.limitTPM, nowMs)
		tpm = bucket.Credit(tpm, nowMs, r.tpmReserved)
		saveBucket(s.tpm, bk, tpm)
	}

	r.status = "refunded"
	r.expiresAt = now.Add(store.ReservationTTL)
	s.res[reservationID] = r
	return nil
}

type invalidError string

func (e invalidError) Error() string { return "valve: " + string(e) }

func errInvalid(msg string) error { return invalidError(msg) }
