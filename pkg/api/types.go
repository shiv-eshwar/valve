package api

import (
	"fmt"
	"time"
)

// LimitType names which dimension caused a deny (or backend failure).
type LimitType string

const (
	LimitTypeNone         LimitType = ""
	LimitTypeRequests     LimitType = "requests"
	LimitTypeTokens       LimitType = "tokens"
	LimitTypeInputTokens  LimitType = "input_tokens"
	LimitTypeOutputTokens LimitType = "output_tokens"
	LimitTypeBackend      LimitType = "backend"
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

// Limits are per-minute budgets (capacity = budget, refill = budget/60s).
// Classic OpenAI-shaped: RequestsPerMinute + TokensPerMinute.
// Split (Anthropic-shaped): RequestsPerMinute + InputTokensPerMinute + OutputTokensPerMinute.
type Limits struct {
	RequestsPerMinute     int64
	TokensPerMinute       int64 // classic; ignored when Split()
	InputTokensPerMinute  int64
	OutputTokensPerMinute int64
}

// Split reports Anthropic-shaped ITPM+OTPM mode (both must be set).
func (l Limits) Split() bool {
	return l.InputTokensPerMinute > 0 && l.OutputTokensPerMinute > 0
}

// Validate rejects partial split config.
func (l Limits) Validate() error {
	in := l.InputTokensPerMinute > 0
	out := l.OutputTokensPerMinute > 0
	if in != out {
		return fmt.Errorf("valve: InputTokensPerMinute and OutputTokensPerMinute must both be set (or neither)")
	}
	return nil
}

// Cost is what a Check attempts to reserve.
type Cost struct {
	Requests     int64 // usually 1
	Tokens       int64 // classic TPM cost
	InputTokens  int64 // split ITPM
	OutputTokens int64 // split OTPM
}

// Decision is the result of Check or Settle.
type Decision struct {
	Allowed       bool
	LimitType     LimitType
	RemainingRPM  int64
	RemainingTPM  int64
	RemainingITPM int64
	RemainingOTPM int64
	LimitRPM      int64
	LimitTPM      int64
	LimitITPM     int64
	LimitOTPM     int64
	ResetRPM      time.Time
	ResetTPM      time.Time
	ResetITPM     time.Time
	ResetOTPM     time.Time
	RetryAfter    time.Duration
	ReservationID string
	// OvershootTPM is set on classic Settle when actual exceeded reserved.
	OvershootTPM int64
	// OvershootITPM / OvershootOTPM set on SettleIO.
	OvershootITPM int64
	OvershootOTPM int64
}
