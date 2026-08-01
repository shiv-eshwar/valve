package limiter_test

import (
	"context"
	"testing"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/lease"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func BenchmarkCheck_Direct(b *testing.B) {
	lim := limiter.New(memory.New())
	ctx := context.Background()
	key := api.Key{Subject: "bench", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 1_000_000_000, TokensPerMinute: 1_000_000_000}
	cost := api.Cost{Requests: 1, Tokens: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := lim.Check(ctx, key, limits, cost)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheck_FastPath(b *testing.B) {
	lim := limiter.New(memory.New(), limiter.WithFastPath(lease.Config{
		RPMChunk: 64,
		TPMChunk: 64,
		LeaseTTL: time.Minute,
	}))
	ctx := context.Background()
	key := api.Key{Subject: "bench", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 1_000_000_000, TokensPerMinute: 1_000_000_000}
	cost := api.Cost{Requests: 1, Tokens: 1}
	// Warm lease.
	_, _ = lim.Check(ctx, key, limits, cost)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := lim.Check(ctx, key, limits, cost)
		if err != nil {
			b.Fatal(err)
		}
	}
}
