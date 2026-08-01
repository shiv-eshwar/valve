package llm

import "errors"

// ErrTooLarge is returned when a request exceeds configured size limits.
var ErrTooLarge = errors.New("valve/llm: request too large")

// Gate rejects oversized requests before Check.
type Gate struct {
	MaxInputTokens  int64
	MaxRequestBytes int64
}

// CheckBytes enforces MaxRequestBytes.
func (g Gate) CheckBytes(n int) error {
	if g.MaxRequestBytes > 0 && int64(n) > g.MaxRequestBytes {
		return ErrTooLarge
	}
	return nil
}

// CheckInput enforces MaxInputTokens against estimated input (not including output reserve).
func (g Gate) CheckInput(inputTokens int64) error {
	if g.MaxInputTokens > 0 && inputTokens > g.MaxInputTokens {
		return ErrTooLarge
	}
	return nil
}
