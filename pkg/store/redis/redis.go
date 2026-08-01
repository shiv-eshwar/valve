package redisstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "embed"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/store"

	"github.com/redis/go-redis/v9"
)

//go:embed lua/check.lua
var checkLua string

//go:embed lua/settle.lua
var settleLua string

//go:embed lua/refund.lua
var refundLua string

// Store is a Redis/Valkey-backed dual-bucket store.
type Store struct {
	rdb    redis.Cmdable
	check  *redis.Script
	settle *redis.Script
	refund *redis.Script
}

// New wraps a go-redis client/cluster client.
func New(rdb redis.Cmdable) *Store {
	return &Store{
		rdb:    rdb,
		check:  redis.NewScript(checkLua),
		settle: redis.NewScript(settleLua),
		refund: redis.NewScript(refundLua),
	}
}

func modelOrDash(model string) string {
	if model == "" {
		return "-"
	}
	return model
}

// bucketKeys returns cluster-safe rpm/tpm keys with hash tag on subject.
func bucketKeys(key api.Key) (rpmKey, tpmKey string) {
	m := modelOrDash(key.Model)
	base := fmt.Sprintf("rl:{{{%s}}}:%s", key.Subject, m)
	return base + ":rpm", base + ":tpm"
}

func reservationKey(id string) string {
	return "rl:res:" + id
}

func keysForReservation(rdb redis.Cmdable, ctx context.Context, reservationID string) (api.Key, string, string, string, error) {
	resKey := reservationKey(reservationID)
	vals, err := rdb.HMGet(ctx, resKey, "subject", "model").Result()
	if err != nil {
		return api.Key{}, "", "", "", err
	}
	if vals[0] == nil {
		return api.Key{}, "", "", "", store.ErrReservationNotFound
	}
	subject, _ := vals[0].(string)
	model, _ := vals[1].(string)
	key := api.Key{Subject: subject, Model: model}
	rpm, tpm := bucketKeys(key)
	return key, resKey, rpm, tpm, nil
}

// Check implements store.Store.
func (s *Store) Check(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost, reservationID string) (api.Decision, error) {
	if key.Subject == "" {
		return api.Decision{}, errors.New("valve: subject required")
	}
	if reservationID == "" {
		return api.Decision{}, errors.New("valve: reservation id required")
	}
	rpmKey, tpmKey := bucketKeys(key)
	resKey := reservationKey(reservationID)

	raw, err := s.check.Run(ctx, s.rdb,
		[]string{rpmKey, tpmKey, resKey},
		limits.RequestsPerMinute,
		limits.TokensPerMinute,
		cost.Requests,
		cost.Tokens,
		int(store.ReservationTTL.Seconds()),
		key.Subject,
		modelOrDash(key.Model),
	).Result()
	if err != nil {
		return api.Decision{}, err
	}
	return parseDecision(raw, limits, reservationID, true)
}

// Settle implements store.Store.
func (s *Store) Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error) {
	_, resKey, rpmKey, tpmKey, err := keysForReservation(s.rdb, ctx, reservationID)
	if err != nil {
		return api.Decision{}, mapResErr(err)
	}
	raw, err := s.settle.Run(ctx, s.rdb,
		[]string{resKey, rpmKey, tpmKey},
		actualTokens,
		int(store.ReservationTTL.Seconds()),
	).Result()
	if err != nil {
		return api.Decision{}, mapResErr(err)
	}
	return parseSettle(raw, reservationID)
}

// Refund implements store.Store.
func (s *Store) Refund(ctx context.Context, reservationID string) error {
	_, resKey, rpmKey, tpmKey, err := keysForReservation(s.rdb, ctx, reservationID)
	if err != nil {
		return mapResErr(err)
	}
	_, err = s.refund.Run(ctx, s.rdb,
		[]string{resKey, rpmKey, tpmKey},
		int(store.ReservationTTL.Seconds()),
	).Result()
	return mapResErr(err)
}

func mapResErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "reservation not found"):
		return store.ErrReservationNotFound
	case strings.Contains(msg, "reservation already settled"):
		return store.ErrReservationSettled
	case strings.Contains(msg, "reservation already refunded"):
		return store.ErrReservationRefunded
	default:
		return err
	}
}

func parseDecision(raw any, limits api.Limits, reservationID string, fillLimits bool) (api.Decision, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) < 7 {
		return api.Decision{}, fmt.Errorf("valve: unexpected check response %#v", raw)
	}
	allowed := toInt64(arr[0]) == 1
	limitType := api.LimitType(toString(arr[1]))
	dec := api.Decision{
		Allowed:      allowed,
		LimitType:    limitType,
		RemainingRPM: toInt64(arr[2]),
		RemainingTPM: toInt64(arr[3]),
		RetryAfter:   time.Duration(toInt64(arr[4])) * time.Millisecond,
		ResetRPM:     time.UnixMilli(toInt64(arr[5])),
		ResetTPM:     time.UnixMilli(toInt64(arr[6])),
		LimitRPM:     limits.RequestsPerMinute,
		LimitTPM:     limits.TokensPerMinute,
	}
	if allowed {
		dec.ReservationID = reservationID
		dec.LimitType = api.LimitTypeNone
	}
	_ = fillLimits
	return dec, nil
}

func parseSettle(raw any, reservationID string) (api.Decision, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) < 8 {
		return api.Decision{}, fmt.Errorf("valve: unexpected settle response %#v", raw)
	}
	dec := api.Decision{
		Allowed:       toInt64(arr[0]) == 1,
		LimitType:     api.LimitType(toString(arr[1])),
		RemainingRPM:  toInt64(arr[2]),
		RemainingTPM:  toInt64(arr[3]),
		RetryAfter:    time.Duration(toInt64(arr[4])) * time.Millisecond,
		ResetRPM:      time.UnixMilli(toInt64(arr[5])),
		ResetTPM:      time.UnixMilli(toInt64(arr[6])),
		OvershootTPM:  toInt64(arr[7]),
		ReservationID: reservationID,
	}
	if len(arr) >= 10 {
		dec.LimitRPM = toInt64(arr[8])
		dec.LimitTPM = toInt64(arr[9])
	}
	return dec, nil
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	case []byte:
		n, _ := strconv.ParseInt(string(x), 10, 64)
		return n
	default:
		return 0
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
