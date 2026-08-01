package limiter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/store"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

type errStore struct{}

func (errStore) Check(context.Context, api.Key, api.Limits, api.Cost, string) (api.Decision, error) {
	return api.Decision{}, errors.New("boom")
}
func (errStore) Settle(context.Context, string, int64) (api.Decision, error) {
	return api.Decision{}, errors.New("boom")
}
func (errStore) Refund(context.Context, string) error { return errors.New("boom") }
func (errStore) Borrow(context.Context, api.Key, api.Limits, int64, int64, int64, int64) (store.BorrowResult, error) {
	return store.BorrowResult{}, errors.New("boom")
}
func (errStore) Return(context.Context, api.Key, api.Limits, int64, int64) error {
	return errors.New("boom")
}

func TestFailClosed(t *testing.T) {
	lim := limiter.New(errStore{}, limiter.WithFailMode(api.FailClosed))
	d, err := lim.Check(context.Background(), api.Key{Subject: "a", Model: "m"},
		api.Limits{RequestsPerMinute: 10, TokensPerMinute: 100}, api.Cost{Requests: 1, Tokens: 1})
	if err != nil {
		t.Fatalf("fail-closed should not return error, got %v", err)
	}
	if d.Allowed || d.LimitType != api.LimitTypeBackend {
		t.Fatalf("%+v", d)
	}
}

func TestFailOpen(t *testing.T) {
	lim := limiter.New(errStore{}, limiter.WithFailMode(api.FailOpen))
	d, err := lim.Check(context.Background(), api.Key{Subject: "a", Model: "m"},
		api.Limits{RequestsPerMinute: 10, TokensPerMinute: 100}, api.Cost{Requests: 1, Tokens: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Allowed || d.ReservationID != "" {
		t.Fatalf("%+v", d)
	}
}

func TestSettleRefundViaLimiter(t *testing.T) {
	lim := limiter.New(memory.New())
	ctx := context.Background()
	key := api.Key{Subject: "s", Model: "m"}
	limits := api.Limits{RequestsPerMinute: 10, TokensPerMinute: 1000}
	d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 100})
	if err != nil || !d.Allowed {
		t.Fatalf("%+v %v", d, err)
	}
	sd, err := lim.Settle(ctx, d.ReservationID, 40)
	if err != nil {
		t.Fatal(err)
	}
	if sd.RemainingTPM != 960 { // 900 remaining after reserve + 60 refund
		t.Fatalf("tpm=%d", sd.RemainingTPM)
	}
}
