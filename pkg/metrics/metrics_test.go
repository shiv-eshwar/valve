package metrics_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/metrics"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func TestObservedLimiterCounts(t *testing.T) {
	reg := prometheus.NewRegistry()
	inner := limiter.New(memory.New())
	lim := metrics.New(inner, reg)

	ctx := context.Background()
	key := api.Key{Subject: "m", Model: "x"}
	limits := api.Limits{RequestsPerMinute: 1, TokensPerMinute: 100}
	d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
	if err != nil || !d.Allowed {
		t.Fatalf("%+v %v", d, err)
	}
	d2, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: 1})
	if err != nil || d2.Allowed {
		t.Fatalf("expected deny %+v", d2)
	}
	_, _ = lim.Settle(ctx, d.ReservationID, 1)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var checks float64
	for _, mf := range mfs {
		if mf.GetName() == "valve_checks_total" {
			for _, m := range mf.GetMetric() {
				checks += m.GetCounter().GetValue()
			}
		}
	}
	if checks < 2 {
		t.Fatalf("checks=%v", checks)
	}
}
