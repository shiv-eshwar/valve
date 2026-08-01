package bucket

import (
	"math"
	"time"
)

// State is a single token-bucket snapshot.
type State struct {
	Tokens   float64
	LastMs   int64
	Capacity int64
}

// RefillPerSec returns capacity/60 (OpenAI-shaped per-minute budget).
func RefillPerSec(capacity int64) float64 {
	if capacity <= 0 {
		return 0
	}
	return float64(capacity) / 60.0
}

// Refill updates tokens for elapsed time since LastMs using nowMs.
func Refill(s State, nowMs int64) State {
	if s.Capacity <= 0 {
		s.Tokens = 0
		s.LastMs = nowMs
		return s
	}
	if s.LastMs == 0 {
		s.Tokens = float64(s.Capacity)
		s.LastMs = nowMs
		return s
	}
	if nowMs < s.LastMs {
		s.LastMs = nowMs
		return s
	}
	elapsed := float64(nowMs-s.LastMs) / 1000.0
	rate := RefillPerSec(s.Capacity)
	s.Tokens = math.Min(float64(s.Capacity), s.Tokens+elapsed*rate)
	s.LastMs = nowMs
	return s
}

// Result is the outcome of trying to consume cost tokens.
type Result struct {
	Allowed    bool
	Tokens     float64
	Remaining  int64
	RetryAfter time.Duration
	ResetAt    time.Time
}

// TryConsume refills then attempts to debit cost. On deny, state tokens are
// returned refilled but not debited.
func TryConsume(s State, nowMs int64, cost int64) (State, Result) {
	s = Refill(s, nowMs)
	remaining := int64(math.Floor(s.Tokens))
	resetAt := ResetAt(s, nowMs)

	if cost < 0 {
		cost = 0
	}
	if cost > s.Capacity {
		return s, Result{
			Allowed:    false,
			Tokens:     s.Tokens,
			Remaining:  remaining,
			RetryAfter: retryAfter(s, float64(cost)),
			ResetAt:    resetAt,
		}
	}
	if s.Tokens+1e-9 < float64(cost) {
		return s, Result{
			Allowed:    false,
			Tokens:     s.Tokens,
			Remaining:  remaining,
			RetryAfter: retryAfter(s, float64(cost)),
			ResetAt:    resetAt,
		}
	}
	s.Tokens -= float64(cost)
	rem := int64(math.Floor(s.Tokens))
	return s, Result{
		Allowed:    true,
		Tokens:     s.Tokens,
		Remaining:  rem,
		RetryAfter: 0,
		ResetAt:    ResetAt(s, nowMs),
	}
}

// Credit adds tokens capped at capacity (used by settle refund / full refund).
func Credit(s State, nowMs int64, amount int64) State {
	s = Refill(s, nowMs)
	if amount <= 0 {
		return s
	}
	s.Tokens = math.Min(float64(s.Capacity), s.Tokens+float64(amount))
	return s
}

// DebitFloor debits up to available tokens; returns new state, amount debited,
// and overshoot (requested - debited).
func DebitFloor(s State, nowMs int64, amount int64) (State, int64, int64) {
	s = Refill(s, nowMs)
	if amount <= 0 {
		return s, 0, 0
	}
	avail := int64(math.Floor(s.Tokens))
	if avail >= amount {
		s.Tokens -= float64(amount)
		return s, amount, 0
	}
	overshoot := amount - avail
	s.Tokens = s.Tokens - float64(avail)
	if s.Tokens < 0 {
		s.Tokens = 0
	}
	return s, avail, overshoot
}

func retryAfter(s State, need float64) time.Duration {
	rate := RefillPerSec(s.Capacity)
	if rate <= 0 {
		return time.Hour
	}
	deficit := need - s.Tokens
	if deficit <= 0 {
		return 0
	}
	secs := math.Ceil(deficit / rate)
	return time.Duration(secs) * time.Second
}

// ResetAt is the approximate time until the bucket is full again.
func ResetAt(s State, nowMs int64) time.Time {
	rate := RefillPerSec(s.Capacity)
	now := time.UnixMilli(nowMs)
	if rate <= 0 {
		return now
	}
	need := float64(s.Capacity) - s.Tokens
	if need <= 0 {
		return now
	}
	secs := need / rate
	return now.Add(time.Duration(secs * float64(time.Second)))
}

// RemainingInt floors tokens.
func RemainingInt(tokens float64) int64 {
	return int64(math.Floor(tokens))
}
