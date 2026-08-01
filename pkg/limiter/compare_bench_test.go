package limiter_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/lease"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

// naiveFixedWindow is a minimal mutex map + per-minute counter (not dual-bucket).
// Used only as a comparative baseline in benches — not production code.
type naiveFixedWindow struct {
	mu      sync.Mutex
	windows map[string]*naiveWindow
}

type naiveWindow struct {
	minute  int64
	count   int64
	limit   int64
}

func newNaiveFixedWindow() *naiveFixedWindow {
	return &naiveFixedWindow{windows: make(map[string]*naiveWindow)}
}

func (n *naiveFixedWindow) allow(subject string, limitPerMinute int64) bool {
	nowMin := time.Now().Unix() / 60
	n.mu.Lock()
	defer n.mu.Unlock()
	w, ok := n.windows[subject]
	if !ok || w.minute != nowMin {
		w = &naiveWindow{minute: nowMin, limit: limitPerMinute}
		n.windows[subject] = w
	}
	if w.count >= w.limit {
		return false
	}
	w.count++
	return true
}

func BenchmarkNaiveFixedWindow(b *testing.B) {
	n := newNaiveFixedWindow()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !n.allow("bench", 1_000_000_000) {
			b.Fatal("denied")
		}
	}
}

func BenchmarkValveDirect(b *testing.B) {
	lim := limiter.New(memory.New())
	ctx := context.Background()
	key := api.Key{Subject: "bench", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 1_000_000_000, TokensPerMinute: 1_000_000_000}
	cost := api.Cost{Requests: 1, Tokens: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := lim.Check(ctx, key, limits, cost)
		if err != nil {
			b.Fatal(err)
		}
		if !d.Allowed {
			b.Fatal("denied")
		}
	}
}

func BenchmarkValveFastPath(b *testing.B) {
	lim := limiter.New(memory.New(), limiter.WithFastPath(lease.Config{
		RPMChunk: 64,
		TPMChunk: 64,
		LeaseTTL: time.Minute,
	}))
	ctx := context.Background()
	key := api.Key{Subject: "bench", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 1_000_000_000, TokensPerMinute: 1_000_000_000}
	cost := api.Cost{Requests: 1, Tokens: 1}
	_, _ = lim.Check(ctx, key, limits, cost)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := lim.Check(ctx, key, limits, cost)
		if err != nil {
			b.Fatal(err)
		}
		if !d.Allowed {
			b.Fatal("denied")
		}
	}
}
