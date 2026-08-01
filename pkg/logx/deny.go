package logx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
)

// Logger writes structured JSON deny events.
type Logger struct {
	w io.Writer
}

// New returns a logger writing to w (default stdout).
func New(w io.Writer) *Logger {
	if w == nil {
		w = os.Stdout
	}
	return &Logger{w: w}
}

// HashSubject returns a short SHA-256 prefix (never log raw keys).
func HashSubject(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:8])
}

// DenyEvent is emitted when a Check denies.
type DenyEvent struct {
	Time        string `json:"time"`
	Event       string `json:"event"`
	SubjectHash string `json:"subject_hash"`
	Model       string `json:"model"`
	LimitType   string `json:"limit_type"`
	RetryAfter  string `json:"retry_after"`
}

// Deny logs a deny decision with hashed subject.
func (l *Logger) Deny(key api.Key, d api.Decision) {
	ev := DenyEvent{
		Time:        time.Now().UTC().Format(time.RFC3339Nano),
		Event:       "rate_limit_deny",
		SubjectHash: HashSubject(key.Subject),
		Model:       key.Model,
		LimitType:   string(d.LimitType),
		RetryAfter:  d.RetryAfter.String(),
	}
	b, _ := json.Marshal(ev)
	_, _ = l.w.Write(append(b, '\n'))
}
