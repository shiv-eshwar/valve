package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/store"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func TestCheckDenyRPMNoDebit(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1_000_000)
	s := memory.New(memory.WithClock(func() time.Time { return now }))
	key := api.Key{Subject: "u1", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 2, TokensPerMinute: 1000}

	for i := 0; i < 2; i++ {
		d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 10}, "r"+string(rune('a'+i)))
		if err != nil || !d.Allowed {
			t.Fatalf("setup allow %d: %+v err=%v", i, d, err)
		}
	}
	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 10}, "r-deny")
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.LimitType != api.LimitTypeRequests {
		t.Fatalf("decision=%+v", d)
	}
	// refund one to prove RPM was only 2 consumed (not 3)
	if err := s.Refund(ctx, "ra"); err != nil {
		t.Fatal(err)
	}
	d, err = s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 10}, "r-after")
	if err != nil || !d.Allowed {
		t.Fatalf("expected allow after refund: %+v err=%v", d, err)
	}
}

func TestCheckDenyTPMNoPartialRPM(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1_000_000)
	s := memory.New(memory.WithClock(func() time.Time { return now }))
	key := api.Key{Subject: "u2", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 100, TokensPerMinute: 50}

	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 51}, "big")
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed || d.LimitType != api.LimitTypeTokens {
		t.Fatalf("decision=%+v", d)
	}
	// RPM should still be full
	d, err = s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 10}, "ok")
	if err != nil || !d.Allowed {
		t.Fatalf("expected allow: %+v err=%v", d, err)
	}
	if d.RemainingRPM != 99 {
		t.Fatalf("remaining rpm=%d want 99 (no partial debit)", d.RemainingRPM)
	}
}

func TestSettleRefundsSurplus(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1_000_000)
	s := memory.New(memory.WithClock(func() time.Time { return now }))
	key := api.Key{Subject: "u3", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000}

	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 200}, "res1")
	if err != nil || !d.Allowed {
		t.Fatalf("%+v %v", d, err)
	}
	if d.RemainingTPM != 800 {
		t.Fatalf("tpm=%d", d.RemainingTPM)
	}
	sd, err := s.Settle(ctx, "res1", 120)
	if err != nil {
		t.Fatal(err)
	}
	// 200 reserved, 120 actual => +80 credit => 800+80=880
	if sd.RemainingTPM != 880 {
		t.Fatalf("tpm after settle=%d want 880", sd.RemainingTPM)
	}
	// RPM stays consumed
	if sd.RemainingRPM != 9 {
		t.Fatalf("rpm=%d want 9", sd.RemainingRPM)
	}
}

func TestSettleDebitDeficit(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1_000_000)
	s := memory.New(memory.WithClock(func() time.Time { return now }))
	key := api.Key{Subject: "u4", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000}

	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 100}, "res2")
	if err != nil || !d.Allowed {
		t.Fatalf("%+v %v", d, err)
	}
	sd, err := s.Settle(ctx, "res2", 250)
	if err != nil {
		t.Fatal(err)
	}
	// reserved 100, actual 250 => debit 150 more => 900-150=750
	if sd.RemainingTPM != 750 {
		t.Fatalf("tpm=%d want 750", sd.RemainingTPM)
	}
}

func TestRefundRestoresBothIdempotent(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(1_000_000)
	s := memory.New(memory.WithClock(func() time.Time { return now }))
	key := api.Key{Subject: "u5", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000}

	d, err := s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 100}, "res3")
	if err != nil || !d.Allowed {
		t.Fatal(err)
	}
	if err := s.Refund(ctx, "res3"); err != nil {
		t.Fatal(err)
	}
	if err := s.Refund(ctx, "res3"); err != nil {
		t.Fatalf("idempotent refund: %v", err)
	}
	d, err = s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1}, "res4")
	if err != nil || !d.Allowed {
		t.Fatal(err)
	}
	if d.RemainingRPM != 9 || d.RemainingTPM != 999 {
		t.Fatalf("after full refund budgets should be nearly full: rpm=%d tpm=%d", d.RemainingRPM, d.RemainingTPM)
	}
}

func TestSettleIdempotent(t *testing.T) {
	s := memory.New()
	key := api.Key{Subject: "u6", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000}
	_, err := s.Check(context.Background(), key, limits, api.Cost{Requests: 1, Tokens: 50}, "res5")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Settle(context.Background(), "res5", 40)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Settle(context.Background(), "res5", 999)
	if err != nil {
		t.Fatal(err)
	}
	if a.RemainingTPM != b.RemainingTPM {
		t.Fatalf("idempotent settle mismatch %d vs %d", a.RemainingTPM, b.RemainingTPM)
	}
}

func TestRefundAfterSettleErrors(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	key := api.Key{Subject: "u7", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000}
	_, _ = s.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 10}, "res6")
	_, _ = s.Settle(ctx, "res6", 10)
	if err := s.Refund(ctx, "res6"); err != store.ErrReservationSettled {
		t.Fatalf("err=%v", err)
	}
}
