package httpmw

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/headers"
	"github.com/shiv-eshwar/valve/pkg/logx"
)

// KeyFunc extracts the rate-limit key from the request.
type KeyFunc func(*http.Request) api.Key

// LimitsFunc returns limits for the request.
type LimitsFunc func(*http.Request) api.Limits

// CostFunc returns the cost to reserve (may read a cached body).
type CostFunc func(*http.Request) api.Cost

// Config for Middleware.
type Config struct {
	Limiter api.Limiter
	Key     KeyFunc
	Limits  LimitsFunc
	Cost    CostFunc
	Log     *logx.Logger
}

// Middleware enforces Check before calling next; on deny returns 429 + headers.
// Successful Checks attach ReservationID on the request context via ReservationIDHeader
// is not used — callers should Settle in their handler using a context value.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	if cfg.Log == nil {
		cfg.Log = logx.New(nil)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := cfg.Key(r)
			limits := cfg.Limits(r)
			cost := cfg.Cost(r)
			d, err := cfg.Limiter.Check(r.Context(), key, limits, cost)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			headers.Write(w.Header(), d)
			if !d.Allowed {
				cfg.Log.Deny(key, d)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]string{
						"type":       "rate_limit",
						"limit_type": string(d.LimitType),
					},
					"reservation_id": d.ReservationID,
				})
				return
			}
			ctx := withReservation(r.Context(), d.ReservationID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type ctxKey int

const reservationKey ctxKey = 1

func withReservation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reservationKey, id)
}

// ReservationIDFromContext returns the ID from a successful middleware Check.
func ReservationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(reservationKey).(string)
	return v
}
