package memory_test

import (
	"context"
	"testing"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/store"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func TestSplitCheckSettleIO(t *testing.T) {
	s := memory.New()
	ctx := context.Background()
	key := api.Key{Subject: "org", Model: "m"}
	limits := api.Limits{
		RequestsPerMinute:     10,
		InputTokensPerMinute:  1000,
		OutputTokensPerMinute: 500,
	}
	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 200, OutputTokens: 100}, "r1")
	if err != nil || !d.Allowed {
		t.Fatalf("check: %+v %v", d, err)
	}
	if d.RemainingITPM != 800 || d.RemainingOTPM != 400 {
		t.Fatalf("remaining itpm/otpm: %+v", d)
	}
	if _, err := s.Settle(ctx, "r1", 50); err != store.ErrWrongSettleMode {
		t.Fatalf("classic settle on split: %v", err)
	}
	sd, err := s.SettleIO(ctx, "r1", 150, 80)
	if err != nil {
		t.Fatal(err)
	}
	// unused 50 input + 20 output credited back
	d2, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 850, OutputTokens: 420}, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if !d2.Allowed {
		t.Fatalf("expected allow after settle credit, got %+v settle=%+v", d2, sd)
	}
}

func TestSplitDenyDimensions(t *testing.T) {
	s := memory.New()
	ctx := context.Background()
	key := api.Key{Subject: "org", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 5, InputTokensPerMinute: 100, OutputTokensPerMinute: 100}

	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 101, OutputTokens: 1}, "a")
	if err != nil || d.Allowed || d.LimitType != api.LimitTypeInputTokens {
		t.Fatalf("want input deny: %+v %v", d, err)
	}
	d, err = s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 1, OutputTokens: 101}, "b")
	if err != nil || d.Allowed || d.LimitType != api.LimitTypeOutputTokens {
		t.Fatalf("want output deny: %+v %v", d, err)
	}
}

func TestPartialSplitRejected(t *testing.T) {
	s := memory.New()
	_, err := s.Check(context.Background(), api.Key{Subject: "a", Model: "m"},
		api.Limits{RequestsPerMinute: 1, InputTokensPerMinute: 10}, api.Cost{Requests: 1, InputTokens: 1}, "x")
	if err == nil {
		t.Fatal("expected validate error")
	}
}

func TestSplitRefund(t *testing.T) {
	s := memory.New()
	ctx := context.Background()
	key := api.Key{Subject: "org", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 2, InputTokensPerMinute: 100, OutputTokensPerMinute: 100}
	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 90, OutputTokens: 90}, "r")
	if err != nil || !d.Allowed {
		t.Fatal(d, err)
	}
	if err := s.Refund(ctx, "r"); err != nil {
		t.Fatal(err)
	}
	d2, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, InputTokens: 90, OutputTokens: 90}, "r2")
	if err != nil || !d2.Allowed {
		t.Fatalf("after refund: %+v %v", d2, err)
	}
}
