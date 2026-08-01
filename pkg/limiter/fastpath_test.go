package limiter_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/lease"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
	redisstore "github.com/shiv-eshwar/valve/pkg/store/redis"
)

func TestFastPathHitRatio(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	lim := limiter.New(s, limiter.WithFastPath(lease.Config{
		RPMChunk: 5,
		TPMChunk: 500,
		LeaseTTL: time.Minute,
	}))
	key := api.Key{Subject: "hit", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 10_000, TokensPerMinute: 1_000_000}
	cost := api.Cost{Requests: 1, Tokens: 10}

	const n = 1000
	for i := 0; i < n; i++ {
		d, err := lim.Check(ctx, key, limits, cost)
		if err != nil || !d.Allowed {
			t.Fatalf("i=%d allowed=%v err=%v", i, d.Allowed, err)
		}
	}
	ratio := lim.Pool().HitRatio()
	borrows := lim.Pool().BorrowCount()
	if ratio < 0.80 {
		t.Fatalf("hit ratio=%v want >= 0.80 (borrows=%d)", ratio, borrows)
	}
	// chunk=5 ⇒ about n/5 borrows; allow exact equality.
	if borrows > n/5 {
		t.Fatalf("borrows=%d too high for chunk=5 over %d checks", borrows, n)
	}
}

func TestFastPathDenyCacheAndInvalidateOnRefund(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	lim := limiter.New(s, limiter.WithFastPath(lease.Config{
		RPMChunk: 1,
		TPMChunk: 1,
		LeaseTTL: time.Minute,
	}))
	key := api.Key{Subject: "deny", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 2, TokensPerMinute: 100}

	var lastID string
	for i := 0; i < 2; i++ {
		d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
		if err != nil || !d.Allowed {
			t.Fatalf("setup %d: %+v %v", i, d, err)
		}
		lastID = d.ReservationID
	}
	d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
	if err != nil || d.Allowed || d.LimitType != api.LimitTypeRequests {
		t.Fatalf("expected rpm deny: %+v err=%v", d, err)
	}
	// Second deny should hit L0 cache (still deny).
	d2, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
	if err != nil || d2.Allowed {
		t.Fatalf("cached deny: %+v %v", d2, err)
	}

	if err := lim.Refund(ctx, lastID); err != nil {
		t.Fatal(err)
	}
	d3, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
	if err != nil || !d3.Allowed {
		t.Fatalf("expected allow after refund cleared deny: %+v %v", d3, err)
	}
}

func TestFastPathSettleRefundLocal(t *testing.T) {
	ctx := context.Background()
	lim := limiter.New(memory.New(), limiter.WithFastPath(lease.DefaultConfig()))
	key := api.Key{Subject: "settle", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 100, TokensPerMinute: 10_000}
	d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 200})
	if err != nil || !d.Allowed {
		t.Fatal(err)
	}
	sd, err := lim.Settle(ctx, d.ReservationID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if sd.RemainingTPM < 150 {
		// after debit 200 from lease (chunk 500), remaining lease tpm = 300; settle credits 150 => 450
		t.Fatalf("tpm=%d", sd.RemainingTPM)
	}
}

func TestFastPathOvershootBound(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	st := redisstore.New(rdb)

	const (
		pods  = 3
		rpm   = 20
		chunk = 5
	)
	cfg := lease.Config{RPMChunk: chunk, TPMChunk: 100, LeaseTTL: time.Minute}
	var lims []*limiter.Limiter
	for i := 0; i < pods; i++ {
		lims = append(lims, limiter.New(st, limiter.WithFastPath(cfg)))
	}

	key := api.Key{Subject: "over", Model: "m"}
	limits := api.Limits{RequestsPerMinute: rpm, TokensPerMinute: 100_000}
	ctx := context.Background()

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for _, lim := range lims {
		wg.Add(1)
		go func(lim *limiter.Limiter) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
				if err != nil {
					t.Errorf("err %v", err)
					return
				}
				if d.Allowed {
					allowed.Add(1)
				}
			}
		}(lim)
	}
	wg.Wait()

	got := allowed.Load()
	// Borrow debits Redis first, so without refill total allows cannot exceed RPM.
	// Documented overshoot bound (unused in-flight lease credits) is ≤ N×chunk;
	// those credits are already taken from the global budget, not additive to it.
	if got != int64(rpm) {
		t.Fatalf("allowed=%d want exactly rpm=%d (no double-spend); overshoot ceiling unused lease ≤ %d",
			got, rpm, pods*chunk)
	}
}

func TestFastPathCloseReturnsLease(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	lim := limiter.New(s, limiter.WithFastPath(lease.Config{
		RPMChunk: 10,
		TPMChunk: 100,
		LeaseTTL: time.Minute,
	}))
	key := api.Key{Subject: "close", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 100, TokensPerMinute: 1000}
	d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
	if err != nil || !d.Allowed {
		t.Fatal(err)
	}
	if err := lim.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// After return, a new limiter without leftover lease should still see restored budget.
	lim2 := limiter.New(s) // direct path
	var allows int
	for i := 0; i < 100; i++ {
		d, err := lim2.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
		if err != nil {
			t.Fatal(err)
		}
		if d.Allowed {
			allows++
		}
	}
	// One consumed by first check; 99 should remain if Close returned the rest of the chunk.
	if allows < 90 {
		t.Fatalf("allows=%d after close return; lease likely not returned", allows)
	}
}

func TestFastPathFailOpenUsesLeaseOnly(t *testing.T) {
	lim := limiter.New(errStore{}, limiter.WithFailMode(api.FailOpen), limiter.WithFastPath(lease.DefaultConfig()))
	d, err := lim.Check(context.Background(), api.Key{Subject: "a", Model: "m"},
		api.Limits{RequestsPerMinute: 10, TokensPerMinute: 100}, api.Cost{Requests: 1, Tokens: 1})
	if err != nil {
		t.Fatal(err)
	}
	// No lease yet and store broken → deny backend (stricter fail-open).
	if d.Allowed || d.LimitType != api.LimitTypeBackend {
		t.Fatalf("%+v", d)
	}
}
