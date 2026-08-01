package redisstore_test

import (
	"context"
	"testing"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/store"
)

func TestRedisSplitCheckSettleIO(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	key := api.Key{Subject: "org", Model: "claude"}
	limits := api.Limits{
		RequestsPerMinute:     20,
		InputTokensPerMinute:  1000,
		OutputTokensPerMinute: 500,
	}
	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 200, OutputTokens: 100}, "io1")
	if err != nil || !d.Allowed {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := s.Settle(ctx, "io1", 10); err != store.ErrWrongSettleMode {
		t.Fatalf("want wrong mode, got %v", err)
	}
	sd, err := s.SettleIO(ctx, "io1", 180, 90)
	if err != nil {
		t.Fatal(err)
	}
	if sd.RemainingITPM < 800 {
		t.Fatalf("expected input credit, got %+v", sd)
	}
	d2, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 820, OutputTokens: 410}, "io2")
	if err != nil || !d2.Allowed {
		t.Fatalf("after settle: %+v %v", d2, err)
	}
}

func TestRedisSplitDeny(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	key := api.Key{Subject: "org", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 10, InputTokensPerMinute: 50, OutputTokensPerMinute: 50}
	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 51, OutputTokens: 1}, "x")
	if err != nil || d.Allowed || d.LimitType != api.LimitTypeInputTokens {
		t.Fatalf("%+v %v", d, err)
	}
}
