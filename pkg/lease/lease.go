package lease

import (
	"sync"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/store"
)

// localBucket holds borrowed credits for one subject.
type localBucket struct {
	rpm       int64
	tpm       int64
	itpm      int64
	otpm      int64
	expiresAt time.Time
	limits    api.Limits
}

// Reservation is an in-process pending reservation (fast path).
type Reservation struct {
	Key          api.Key
	Limits       api.Limits
	RPMCost      int64
	TPMReserved  int64
	ITPMReserved int64
	OTPMReserved int64
	Status       string // pending | settled | refunded
	LastDecision api.Decision
	ExpiresAt    time.Time
}

// Stats exposes lease hit counters.
type Stats struct {
	Hits    int64
	Misses  int64
	Borrows int64
}

// Pool manages per-key leases, local reservations, and deny cache.
type Pool struct {
	mu      sync.Mutex
	cfg     Config
	now     func() time.Time
	deny    *DenyCache
	lease   map[string]*localBucket
	res     map[string]*Reservation
	hits    int64
	miss    int64
	borrows int64
}

// NewPool creates a lease pool.
func NewPool(cfg Config) *Pool {
	cfg = cfg.normalized()
	return &Pool{
		cfg:   cfg,
		now:   time.Now,
		deny:  NewDenyCache(),
		lease: make(map[string]*localBucket),
		res:   make(map[string]*Reservation),
	}
}

// Config returns the pool config.
func (p *Pool) Config() Config { return p.cfg }

// Deny returns the deny cache.
func (p *Pool) Deny() *DenyCache { return p.deny }

// Stats returns counters.
func (p *Pool) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Stats{Hits: p.hits, Misses: p.miss, Borrows: p.borrows}
}

// BorrowCount returns how many store Borrow calls were made.
func (p *Pool) BorrowCount() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.borrows
}

func (p *Pool) keyStr(key api.Key) string { return cacheKey(key) }

func (p *Pool) purgeResLocked(now time.Time) {
	for id, r := range p.res {
		if now.After(r.ExpiresAt) {
			delete(p.res, id)
		}
	}
}

func (p *Pool) getLeaseLocked(key api.Key, now time.Time) *localBucket {
	k := p.keyStr(key)
	b, ok := p.lease[k]
	if !ok {
		return nil
	}
	if !now.Before(b.expiresAt) {
		delete(p.lease, k)
		return nil
	}
	return b
}

func covers(b *localBucket, cost api.Cost, split bool) bool {
	if b == nil {
		return false
	}
	if b.rpm < cost.Requests {
		return false
	}
	if split {
		return b.itpm >= cost.InputTokens && b.otpm >= cost.OutputTokens
	}
	return b.tpm >= cost.Tokens
}

// Has reports whether the lease can cover cost.
func (p *Pool) Has(key api.Key, cost api.Cost, limits api.Limits) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return covers(p.getLeaseLocked(key, p.now()), cost, limits.Split())
}

// CreditLease adds borrowed credits and refreshes TTL.
func (p *Pool) CreditLease(key api.Key, limits api.Limits, rpm, tpm, itpm, otpm int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	k := p.keyStr(key)
	b := p.lease[k]
	if b == nil || !now.Before(b.expiresAt) {
		b = &localBucket{}
		p.lease[k] = b
	}
	b.rpm += rpm
	b.tpm += tpm
	b.itpm += itpm
	b.otpm += otpm
	b.limits = limits
	b.expiresAt = now.Add(p.cfg.LeaseTTL)
}

// TryDebit debits the lease and records a local reservation. Ok=false if insufficient.
func (p *Pool) TryDebit(key api.Key, limits api.Limits, cost api.Cost, reservationID string) (api.Decision, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.purgeResLocked(now)
	b := p.getLeaseLocked(key, now)
	split := limits.Split()
	if !covers(b, cost, split) {
		p.miss++
		return api.Decision{}, false
	}
	b.rpm -= cost.Requests
	if split {
		b.itpm -= cost.InputTokens
		b.otpm -= cost.OutputTokens
	} else {
		b.tpm -= cost.Tokens
	}
	b.expiresAt = now.Add(p.cfg.LeaseTTL)
	p.hits++
	p.res[reservationID] = &Reservation{
		Key:          key,
		Limits:       limits,
		RPMCost:      cost.Requests,
		TPMReserved:  cost.Tokens,
		ITPMReserved: cost.InputTokens,
		OTPMReserved: cost.OutputTokens,
		Status:       "pending",
		ExpiresAt:    now.Add(store.ReservationTTL),
	}
	dec := api.Decision{
		Allowed:       true,
		RemainingRPM:  b.rpm,
		LimitRPM:      limits.RequestsPerMinute,
		ReservationID: reservationID,
	}
	if split {
		dec.RemainingITPM = b.itpm
		dec.RemainingOTPM = b.otpm
		dec.RemainingTPM = b.otpm
		dec.LimitITPM = limits.InputTokensPerMinute
		dec.LimitOTPM = limits.OutputTokensPerMinute
		dec.LimitTPM = limits.OutputTokensPerMinute
	} else {
		dec.RemainingTPM = b.tpm
		dec.LimitTPM = limits.TokensPerMinute
	}
	return dec, true
}

// NoteBorrow increments borrow counter.
func (p *Pool) NoteBorrow() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.borrows++
}

// GetReservation returns a local reservation.
func (p *Pool) GetReservation(id string) (*Reservation, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.purgeResLocked(p.now())
	r, ok := p.res[id]
	return r, ok
}

// SettleLocal adjusts lease for classic TPM actual usage.
func (p *Pool) SettleLocal(id string, actualTokens int64) (api.Decision, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.purgeResLocked(now)
	r, ok := p.res[id]
	if !ok {
		return api.Decision{}, store.ErrReservationNotFound
	}
	if r.Limits.Split() {
		return api.Decision{}, store.ErrWrongSettleMode
	}
	if r.Status == "settled" {
		return r.LastDecision, nil
	}
	if r.Status == "refunded" {
		return api.Decision{}, store.ErrReservationRefunded
	}
	if actualTokens < 0 {
		actualTokens = 0
	}

	b := p.getLeaseLocked(r.Key, now)
	if b == nil {
		b = &localBucket{limits: r.Limits, expiresAt: now.Add(p.cfg.LeaseTTL)}
		p.lease[p.keyStr(r.Key)] = b
	}

	var overshoot int64
	delta := r.TPMReserved - actualTokens
	restored := false
	switch {
	case delta > 0:
		b.tpm += delta
		restored = true
	case delta < 0:
		need := -delta
		if b.tpm >= need {
			b.tpm -= need
		} else {
			overshoot = need - b.tpm
			b.tpm = 0
		}
	}
	b.expiresAt = now.Add(p.cfg.LeaseTTL)

	dec := api.Decision{
		Allowed:       true,
		RemainingRPM:  b.rpm,
		RemainingTPM:  b.tpm,
		LimitRPM:      r.Limits.RequestsPerMinute,
		LimitTPM:      r.Limits.TokensPerMinute,
		ReservationID: id,
		OvershootTPM:  overshoot,
	}
	r.Status = "settled"
	r.LastDecision = dec
	r.ExpiresAt = now.Add(store.ReservationTTL)
	if restored {
		p.deny.Clear(r.Key)
	}
	return dec, nil
}

// SettleLocalIO adjusts lease for split ITPM/OTPM actual usage.
func (p *Pool) SettleLocalIO(id string, actualInput, actualOutput int64) (api.Decision, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.purgeResLocked(now)
	r, ok := p.res[id]
	if !ok {
		return api.Decision{}, store.ErrReservationNotFound
	}
	if !r.Limits.Split() {
		return api.Decision{}, store.ErrWrongSettleMode
	}
	if r.Status == "settled" {
		return r.LastDecision, nil
	}
	if r.Status == "refunded" {
		return api.Decision{}, store.ErrReservationRefunded
	}
	if actualInput < 0 {
		actualInput = 0
	}
	if actualOutput < 0 {
		actualOutput = 0
	}

	b := p.getLeaseLocked(r.Key, now)
	if b == nil {
		b = &localBucket{limits: r.Limits, expiresAt: now.Add(p.cfg.LeaseTTL)}
		p.lease[p.keyStr(r.Key)] = b
	}

	overIn, restIn := applyLocalDelta(&b.itpm, r.ITPMReserved, actualInput)
	overOut, restOut := applyLocalDelta(&b.otpm, r.OTPMReserved, actualOutput)
	b.expiresAt = now.Add(p.cfg.LeaseTTL)

	dec := api.Decision{
		Allowed:       true,
		RemainingRPM:  b.rpm,
		RemainingITPM: b.itpm,
		RemainingOTPM: b.otpm,
		RemainingTPM:  b.otpm,
		LimitRPM:      r.Limits.RequestsPerMinute,
		LimitITPM:     r.Limits.InputTokensPerMinute,
		LimitOTPM:     r.Limits.OutputTokensPerMinute,
		LimitTPM:      r.Limits.OutputTokensPerMinute,
		ReservationID: id,
		OvershootITPM: overIn,
		OvershootOTPM: overOut,
	}
	r.Status = "settled"
	r.LastDecision = dec
	r.ExpiresAt = now.Add(store.ReservationTTL)
	if restIn || restOut {
		p.deny.Clear(r.Key)
	}
	return dec, nil
}

func applyLocalDelta(bal *int64, reserved, actual int64) (overshoot int64, restored bool) {
	delta := reserved - actual
	switch {
	case delta > 0:
		*bal += delta
		return 0, true
	case delta < 0:
		need := -delta
		if *bal >= need {
			*bal -= need
		} else {
			overshoot = need - *bal
			*bal = 0
		}
	}
	return overshoot, false
}

// ApplyBorrowedDeficit adds borrowed TPM then consumes classic overshoot.
func (p *Pool) ApplyBorrowedDeficit(id string, gotTPM int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.res[id]
	if !ok {
		return
	}
	b := p.getLeaseLocked(r.Key, p.now())
	if b == nil {
		return
	}
	over := r.LastDecision.OvershootTPM
	b.tpm += gotTPM
	if over > 0 {
		if b.tpm >= over {
			b.tpm -= over
			r.LastDecision.OvershootTPM = 0
		} else {
			r.LastDecision.OvershootTPM = over - b.tpm
			b.tpm = 0
		}
	}
	r.LastDecision.RemainingTPM = b.tpm
	r.LastDecision.RemainingRPM = b.rpm
}

// ApplyBorrowedDeficitIO consumes split overshoot against borrowed ITPM/OTPM.
func (p *Pool) ApplyBorrowedDeficitIO(id string, gotITPM, gotOTPM int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.res[id]
	if !ok {
		return
	}
	b := p.getLeaseLocked(r.Key, p.now())
	if b == nil {
		return
	}
	b.itpm += gotITPM
	b.otpm += gotOTPM
	if over := r.LastDecision.OvershootITPM; over > 0 {
		if b.itpm >= over {
			b.itpm -= over
			r.LastDecision.OvershootITPM = 0
		} else {
			r.LastDecision.OvershootITPM = over - b.itpm
			b.itpm = 0
		}
	}
	if over := r.LastDecision.OvershootOTPM; over > 0 {
		if b.otpm >= over {
			b.otpm -= over
			r.LastDecision.OvershootOTPM = 0
		} else {
			r.LastDecision.OvershootOTPM = over - b.otpm
			b.otpm = 0
		}
	}
	r.LastDecision.RemainingITPM = b.itpm
	r.LastDecision.RemainingOTPM = b.otpm
	r.LastDecision.RemainingTPM = b.otpm
	r.LastDecision.RemainingRPM = b.rpm
}

// RefundLocal restores credits to the lease.
func (p *Pool) RefundLocal(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.purgeResLocked(now)
	r, ok := p.res[id]
	if !ok {
		return store.ErrReservationNotFound
	}
	if r.Status == "refunded" {
		return nil
	}
	if r.Status == "settled" {
		return store.ErrReservationSettled
	}
	b := p.getLeaseLocked(r.Key, now)
	if b == nil {
		b = &localBucket{limits: r.Limits, expiresAt: now.Add(p.cfg.LeaseTTL)}
		p.lease[p.keyStr(r.Key)] = b
	}
	b.rpm += r.RPMCost
	if r.Limits.Split() {
		b.itpm += r.ITPMReserved
		b.otpm += r.OTPMReserved
	} else {
		b.tpm += r.TPMReserved
	}
	b.expiresAt = now.Add(p.cfg.LeaseTTL)
	r.Status = "refunded"
	r.ExpiresAt = now.Add(store.ReservationTTL)
	p.deny.Clear(r.Key)
	return nil
}

// LeaseSnap is a Close/Return snapshot.
type LeaseSnap struct {
	Key    api.Key
	Limits api.Limits
	RPM    int64
	TPM    int64
	ITPM   int64
	OTPM   int64
}

// SnapshotLeases returns a copy of remaining lease credits for Close/Return.
func (p *Pool) SnapshotLeases() []LeaseSnap {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	var out []LeaseSnap
	for k, b := range p.lease {
		if !now.Before(b.expiresAt) {
			continue
		}
		if b.rpm <= 0 && b.tpm <= 0 && b.itpm <= 0 && b.otpm <= 0 {
			continue
		}
		parts := splitKey(k)
		out = append(out, LeaseSnap{
			Key:    api.Key{Subject: parts[0], Model: parts[1]},
			Limits: b.limits,
			RPM:    b.rpm,
			TPM:    b.tpm,
			ITPM:   b.itpm,
			OTPM:   b.otpm,
		})
		b.rpm = 0
		b.tpm = 0
		b.itpm = 0
		b.otpm = 0
	}
	return out
}

func splitKey(k string) [2]string {
	for i := 0; i < len(k); i++ {
		if k[i] == 0 {
			return [2]string{k[:i], k[i+1:]}
		}
	}
	return [2]string{k, "-"}
}

// HitRatio returns hits/(hits+misses), or 0 if no samples.
func (p *Pool) HitRatio() float64 {
	s := p.Stats()
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}
