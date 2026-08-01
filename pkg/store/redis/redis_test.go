package redisstore_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/store"
	redisstore "github.com/shiv-eshwar/valve/pkg/store/redis"
)

func newTestStore(t *testing.T) (*redisstore.Store, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return redisstore.New(rdb), mr
}

func TestRedisCheckDenyTPMNoPartial(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	key := api.Key{Subject: "org", Model: "gpt"}
	limits := api.Limits{RequestsPerMinute: 100, TokensPerMinute: 50}

	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 51}, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.LimitType != api.LimitTypeTokens {
		t.Fatalf("%+v", d)
	}
	d, err = s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 10}, "r2")
	if err != nil || !d.Allowed {
		t.Fatalf("%+v %v", d, err)
	}
	if d.RemainingRPM != 99 {
		t.Fatalf("rpm=%d", d.RemainingRPM)
	}
}

func TestRedisSettleAndRefund(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	key := api.Key{Subject: "org2", Model: "gpt"}
	limits := api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000}

	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 200}, "res-a")
	if err != nil || !d.Allowed {
		t.Fatalf("%+v %v", d, err)
	}
	sd, err := s.Settle(ctx, "res-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if sd.RemainingTPM != 900 { // 800 after check + 100 refund = 900
		t.Fatalf("tpm=%d want 900", sd.RemainingTPM)
	}

	d, err = s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 50}, "res-b")
	if err != nil || !d.Allowed {
		t.Fatal(err)
	}
	if err := s.Refund(ctx, "res-b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Refund(ctx, "res-b"); err != nil {
		t.Fatalf("idempotent: %v", err)
	}
}

func TestRedisRefundAfterSettle(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	key := api.Key{Subject: "org3", Model: "-"}
	limits := api.Limits{RequestsPerMinute: 5, TokensPerMinute: 100}
	_, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 10}, "x")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Settle(ctx, "x", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refund(ctx, "x"); err != store.ErrReservationSettled {
		t.Fatalf("err=%v", err)
	}
}

func TestRedisHashTagKeys(t *testing.T) {
	s, mr := newTestStore(t)
	ctx := context.Background()
	key := api.Key{Subject: "abc", Model: "m1"}
	limits := api.Limits{RequestsPerMinute: 10, TokensPerMinute: 100}
	_, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1}, "k1")
	if err != nil {
		t.Fatal(err)
	}
	keys := mr.Keys()
	foundRPM := false
	for _, k := range keys {
		if k == "rl:{{{abc}}}:m1:rpm" {
			foundRPM = true
		}
	}
	if !foundRPM {
		t.Fatalf("keys=%v missing hash-tagged rpm", keys)
	}
}
