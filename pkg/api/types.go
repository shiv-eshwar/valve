package api

import "time"

// LimitType names which dimension caused a deny (or backend failure).
type LimitType string

const (
	LimitTypeNone     LimitType = ""
	LimitTypeRequests LimitType = "requests"
	LimitTypeTokens   LimitType = "tokens"
	LimitTypeBackend  LimitType = "backend"
)

// FailMode controls Check behavior when the store errors.
type FailMode int

const (
	// FailClosed denies with LimitTypeBackend (default).
	FailClosed FailMode = iota
	// FailOpen allows without a reservation when the store is unavailable.
	FailOpen
)

// Key identifies the rate-limit subject.
type Key struct {
	Subject string // required
	Model   string // required; use "-" if unused
}

// Limits are OpenAI-shaped per-minute budgets (capacity = budget, refill = budget/60s).
type Limits struct {
	RequestsPerMinute int64
	TokensPerMinute   int64
}

// Cost is what a Check attempts to reserve.
type Cost struct {
	Requests int64 // usually 1
	Tokens   int64 // estimated TPM cost
}

// Decision is the result of Check or Settle.
type Decision struct {
	Allowed       bool
	LimitType     LimitType
	RemainingRPM  int64
	RemainingTPM  int64
	LimitRPM      int64
	LimitTPM      int64
	ResetRPM      time.Time
	ResetTPM      time.Time
	RetryAfter    time.Duration
	ReservationID string
	// OvershootTPM is set on Settle when actual exceeded reserved and the
	// bucket could not fully cover the deficit (floor at 0).
	OvershootTPM int64
}
