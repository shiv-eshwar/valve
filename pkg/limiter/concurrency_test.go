package limiter_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
	redisstore "github.com/shiv-eshwar/valve/pkg/store/redis"
)

func runConcurrency(t *testing.T, lim api.Limiter, rpm int64) {
	t.Helper()
	ctx := context.Background()
	key := api.Key{Subject: "hot", Model: "m"}
	limits := api.Limits{RequestsPerMinute: rpm, TokensPerMinute: 1_000_000}

	var allowed atomic.Int64
	var wg sync.WaitGroup
	workers := 100
	perWorker := 10
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
				if err != nil {
					t.Errorf("check err: %v", err)
					return
				}
				if d.Allowed {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	got := allowed.Load()
	if got > rpm {
		t.Fatalf("allowed=%d exceeds rpm capacity=%d (double-spend)", got, rpm)
	}
	if got != rpm {
		t.Fatalf("allowed=%d want exactly %d with no refill", got, rpm)
	}
}

func TestConcurrencyMemory(t *testing.T) {
	runConcurrency(t, limiter.New(memory.New()), 50)
}

func TestConcurrencyRedis(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	runConcurrency(t, limiter.New(redisstore.New(rdb)), 50)
}
