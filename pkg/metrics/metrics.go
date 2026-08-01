package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/shiv-eshwar/valve/pkg/api"
)

// HitRatioProvider optionally reports lease hit ratio (fast path).
type HitRatioProvider interface {
	HitRatio() float64
}

// ObservedLimiter wraps an api.Limiter with Prometheus instrumentation.
type ObservedLimiter struct {
	inner     api.Limiter
	hits      HitRatioProvider
	reg       prometheus.Registerer
	checks    *prometheus.CounterVec
	settles   *prometheus.CounterVec
	refunds   prometheus.Counter
	dur       prometheus.Histogram
	lease     prometheus.GaugeFunc
	overshoot prometheus.Counter
	storeErrs prometheus.Counter
}

// Option configures ObservedLimiter.
type Option func(*ObservedLimiter)

// WithHitRatio binds a lease pool (or anything with HitRatio).
func WithHitRatio(p HitRatioProvider) Option {
	return func(o *ObservedLimiter) { o.hits = p }
}

// New wraps inner with metrics registered on reg (default prometheus.DefaultRegisterer).
func New(inner api.Limiter, reg prometheus.Registerer, opts ...Option) *ObservedLimiter {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	factory := promauto.With(reg)
	o := &ObservedLimiter{
		inner: inner,
		reg:   reg,
		checks: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "valve_checks_total",
			Help: "Rate limit Check outcomes",
		}, []string{"result", "limit_type"}),
		settles: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "valve_settles_total",
			Help: "Settle outcomes",
		}, []string{"result"}),
		refunds: factory.NewCounter(prometheus.CounterOpts{
			Name: "valve_refunds_total",
			Help: "Refund calls",
		}),
		dur: factory.NewHistogram(prometheus.HistogramOpts{
			Name:    "valve_check_duration_seconds",
			Help:    "Check latency",
			Buckets: prometheus.DefBuckets,
		}),
		overshoot: factory.NewCounter(prometheus.CounterOpts{
			Name: "valve_overshoot_tokens_total",
			Help: "TPM overshoot observed on Settle",
		}),
		storeErrs: factory.NewCounter(prometheus.CounterOpts{
			Name: "valve_store_errors_total",
			Help: "Store/backend errors returned to callers",
		}),
	}
	for _, opt := range opts {
		opt(o)
	}
	o.lease = factory.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "valve_lease_hit_ratio",
		Help: "Local lease hit ratio (0 if fast path off)",
	}, func() float64 {
		if o.hits == nil {
			return 0
		}
		return o.hits.HitRatio()
	})
	return o
}

// Check implements api.Limiter.
func (o *ObservedLimiter) Check(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost) (api.Decision, error) {
	start := time.Now()
	d, err := o.inner.Check(ctx, key, limits, cost)
	o.dur.Observe(time.Since(start).Seconds())
	if err != nil {
		o.storeErrs.Inc()
		return d, err
	}
	result := "deny"
	if d.Allowed {
		result = "allow"
	}
	lt := string(d.LimitType)
	if lt == "" {
		lt = "none"
	}
	o.checks.WithLabelValues(result, lt).Inc()
	return d, nil
}

// Settle implements api.Limiter.
func (o *ObservedLimiter) Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error) {
	d, err := o.inner.Settle(ctx, reservationID, actualTokens)
	if err != nil {
		o.storeErrs.Inc()
		o.settles.WithLabelValues("error").Inc()
		return d, err
	}
	o.settles.WithLabelValues("ok").Inc()
	if d.OvershootTPM > 0 {
		o.overshoot.Add(float64(d.OvershootTPM))
	}
	return d, nil
}

// SettleIO implements api.Limiter.
func (o *ObservedLimiter) SettleIO(ctx context.Context, reservationID string, actualInput, actualOutput int64) (api.Decision, error) {
	d, err := o.inner.SettleIO(ctx, reservationID, actualInput, actualOutput)
	if err != nil {
		o.storeErrs.Inc()
		o.settles.WithLabelValues("error").Inc()
		return d, err
	}
	o.settles.WithLabelValues("ok").Inc()
	if d.OvershootITPM > 0 {
		o.overshoot.Add(float64(d.OvershootITPM))
	}
	if d.OvershootOTPM > 0 {
		o.overshoot.Add(float64(d.OvershootOTPM))
	}
	return d, nil
}

// Refund implements api.Limiter.
func (o *ObservedLimiter) Refund(ctx context.Context, reservationID string) error {
	err := o.inner.Refund(ctx, reservationID)
	o.refunds.Inc()
	if err != nil {
		o.storeErrs.Inc()
	}
	return err
}

var _ api.Limiter = (*ObservedLimiter)(nil)
