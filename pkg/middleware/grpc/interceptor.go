package grpcmw

import (
	"context"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// KeyFunc extracts a rate-limit key from the RPC context and request.
type KeyFunc func(ctx context.Context, req any) api.Key

// LimitsFunc returns limits for the RPC.
type LimitsFunc func(ctx context.Context, req any) api.Limits

// CostFunc returns cost for the RPC.
type CostFunc func(ctx context.Context, req any) api.Cost

// Config for UnaryInterceptor.
type Config struct {
	Limiter api.Limiter
	Key     KeyFunc
	Limits  LimitsFunc
	Cost    CostFunc
	Log     *logx.Logger
}

type ctxKey int

const reservationKey ctxKey = 1

// ReservationIDFromContext returns ID set after a successful Check in the interceptor.
func ReservationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(reservationKey).(string)
	return v
}

// UnaryInterceptor runs Check before the handler; denies with ResourceExhausted.
func UnaryInterceptor(cfg Config) grpc.UnaryServerInterceptor {
	if cfg.Log == nil {
		cfg.Log = logx.New(nil)
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		key := cfg.Key(ctx, req)
		limits := cfg.Limits(ctx, req)
		cost := cfg.Cost(ctx, req)
		d, err := cfg.Limiter.Check(ctx, key, limits, cost)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "valve: %v", err)
		}
		if !d.Allowed {
			cfg.Log.Deny(key, d)
			return nil, status.Errorf(codes.ResourceExhausted, "rate limited: %s", d.LimitType)
		}
		ctx = context.WithValue(ctx, reservationKey, d.ReservationID)
		return handler(ctx, req)
	}
}
