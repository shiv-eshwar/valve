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
	expiresAt time.Time
	limits    api.Limits
}

// Reservation is an in-process pending reservation (fast path).
type Reservation struct {
	Key          api.Key
	Limits       api.Limits
	RPMCost      int64
	TPMReserved  int64
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
		// stale — discard without auto-return here (Close handles return)
		delete(p.lease, k)
		return nil
	}
	return b
}

// Has reports whether the lease can cover cost.
func (p *Pool) Has(key api.Key, cost api.Cost) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.getLeaseLocked(key, p.now())
	if b == nil {
		return false
	}
	return b.rpm >= cost.Requests && b.tpm >= cost.Tokens
}

// CreditLease adds borrowed credits and refreshes TTL.
func (p *Pool) CreditLease(key api.Key, limits api.Limits, rpm, tpm int64) {
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
	if b == nil || b.rpm < cost.Requests || b.tpm < cost.Tokens {
		p.miss++
		return api.Decision{}, false
	}
	b.rpm -= cost.Requests
	b.tpm -= cost.Tokens
	b.expiresAt = now.Add(p.cfg.LeaseTTL)
	p.hits++
	p.res[reservationID] = &Reservation{
		Key:         key,
		Limits:      limits,
		RPMCost:     cost.Requests,
		TPMReserved: cost.Tokens,
		Status:      "pending",
		ExpiresAt:   now.Add(store.ReservationTTL),
	}
	return api.Decision{
		Allowed:       true,
		RemainingRPM:  b.rpm,
		RemainingTPM:  b.tpm,
		LimitRPM:      limits.RequestsPerMinute,
		LimitTPM:      limits.TokensPerMinute,
		ReservationID: reservationID,
	}, true
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

// SettleLocal adjusts lease for actual token usage.
func (p *Pool) SettleLocal(id string, actualTokens int64) (api.Decision, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.purgeResLocked(now)
	r, ok := p.res[id]
	if !ok {
		return api.Decision{}, store.ErrReservationNotFound
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

// DeficitAfterSettle returns TPM still needed from Redis after a settle with overshoot.
// Call after SettleLocal when OvershootTPM > 0 — we already zeroed local lease.
func (p *Pool) PeekOvershoot(id string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.res[id]
	if !ok {
		return 0
	}
	return r.LastDecision.OvershootTPM
}

// ApplyBorrowedDeficit adds borrowed TPM then consumes overshoot against it.
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

// RefundLocal restores RPM+TPM to the lease.
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
	b.tpm += r.TPMReserved
	b.expiresAt = now.Add(p.cfg.LeaseTTL)
	r.Status = "refunded"
	r.ExpiresAt = now.Add(store.ReservationTTL)
	p.deny.Clear(r.Key)
	return nil
}

// SnapshotLeases returns a copy of remaining lease credits for Close/Return.
func (p *Pool) SnapshotLeases() []struct {
	Key    api.Key
	Limits api.Limits
	RPM    int64
	TPM    int64
} {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	var out []struct {
		Key    api.Key
		Limits api.Limits
		RPM    int64
		TPM    int64
	}
	for k, b := range p.lease {
		if !now.Before(b.expiresAt) {
			continue
		}
		if b.rpm <= 0 && b.tpm <= 0 {
			continue
		}
		// decode key
		parts := splitKey(k)
		out = append(out, struct {
			Key    api.Key
			Limits api.Limits
			RPM    int64
			TPM    int64
		}{
			Key:    api.Key{Subject: parts[0], Model: parts[1]},
			Limits: b.limits,
			RPM:    b.rpm,
			TPM:    b.tpm,
		})
		b.rpm = 0
		b.tpm = 0
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
