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

//go:embed lua/check_io.lua
var checkIOLua string

//go:embed lua/settle.lua
var settleLua string

//go:embed lua/settle_io.lua
var settleIOLua string

//go:embed lua/refund.lua
var refundLua string

//go:embed lua/refund_io.lua
var refundIOLua string

//go:embed lua/borrow.lua
var borrowLua string

//go:embed lua/borrow_io.lua
var borrowIOLua string

//go:embed lua/return.lua
var returnLua string

//go:embed lua/return_io.lua
var returnIOLua string

// Store is a Redis/Valkey-backed dual/triple-bucket store.
type Store struct {
	rdb      redis.Cmdable
	check    *redis.Script
	checkIO  *redis.Script
	settle   *redis.Script
	settleIO *redis.Script
	refund   *redis.Script
	refundIO *redis.Script
	borrow   *redis.Script
	borrowIO *redis.Script
	ret      *redis.Script
	retIO    *redis.Script
}

// New wraps a go-redis client/cluster client.
func New(rdb redis.Cmdable) *Store {
	s := &Store{
		rdb:      rdb,
		check:    redis.NewScript(checkLua),
		checkIO:  redis.NewScript(checkIOLua),
		settle:   redis.NewScript(settleLua),
		settleIO: redis.NewScript(settleIOLua),
		refund:   redis.NewScript(refundLua),
		refundIO: redis.NewScript(refundIOLua),
		borrow:   redis.NewScript(borrowLua),
		borrowIO: redis.NewScript(borrowIOLua),
		ret:      redis.NewScript(returnLua),
		retIO:    redis.NewScript(returnIOLua),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, sc := range []*redis.Script{s.check, s.checkIO, s.settle, s.settleIO, s.refund, s.refundIO, s.borrow, s.borrowIO, s.ret, s.retIO} {
		_ = sc.Load(ctx, rdb)
	}
	return s
}

func modelOrDash(model string) string {
	if model == "" {
		return "-"
	}
	return model
}

func bucketKeys(key api.Key) (rpmKey, tpmKey string) {
	m := modelOrDash(key.Model)
	base := fmt.Sprintf("rl:{{{%s}}}:%s", key.Subject, m)
	return base + ":rpm", base + ":tpm"
}

func bucketKeysIO(key api.Key) (rpmKey, itpmKey, otpmKey string) {
	m := modelOrDash(key.Model)
	base := fmt.Sprintf("rl:{{{%s}}}:%s", key.Subject, m)
	return base + ":rpm", base + ":itpm", base + ":otpm"
}

func reservationKey(id string) string {
	return "rl:res:" + id
}

func (s *Store) reservationMode(ctx context.Context, reservationID string) (string, api.Key, error) {
	resKey := reservationKey(reservationID)
	vals, err := s.rdb.HMGet(ctx, resKey, "subject", "model", "mode").Result()
	if err != nil {
		return "", api.Key{}, err
	}
	if vals[0] == nil {
		return "", api.Key{}, store.ErrReservationNotFound
	}
	subject, _ := vals[0].(string)
	model, _ := vals[1].(string)
	mode, _ := vals[2].(string)
	if mode == "" {
		mode = "classic"
	}
	return mode, api.Key{Subject: subject, Model: model}, nil
}

// Check implements store.Store.
func (s *Store) Check(ctx context.Context, key api.Key, limits api.Limits, cost api.Cost, reservationID string) (api.Decision, error) {
	if err := limits.Validate(); err != nil {
		return api.Decision{}, err
	}
	if key.Subject == "" {
		return api.Decision{}, errors.New("valve: subject required")
	}
	if reservationID == "" {
		return api.Decision{}, errors.New("valve: reservation id required")
	}
	if limits.Split() {
		rpmKey, itpmKey, otpmKey := bucketKeysIO(key)
		resKey := reservationKey(reservationID)
		raw, err := s.checkIO.Run(ctx, s.rdb,
			[]string{rpmKey, itpmKey, otpmKey, resKey},
			limits.RequestsPerMinute,
			limits.InputTokensPerMinute,
			limits.OutputTokensPerMinute,
			cost.Requests,
			cost.InputTokens,
			cost.OutputTokens,
			int(store.ReservationTTL.Seconds()),
			key.Subject,
			modelOrDash(key.Model),
		).Result()
		if err != nil {
			return api.Decision{}, err
		}
		return parseDecisionIO(raw, limits, reservationID)
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
	return parseDecision(raw, limits, reservationID)
}

// Settle implements store.Store.
func (s *Store) Settle(ctx context.Context, reservationID string, actualTokens int64) (api.Decision, error) {
	mode, key, err := s.reservationMode(ctx, reservationID)
	if err != nil {
		return api.Decision{}, mapResErr(err)
	}
	if mode == "split" {
		return api.Decision{}, store.ErrWrongSettleMode
	}
	resKey := reservationKey(reservationID)
	rpmKey, tpmKey := bucketKeys(key)
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

// SettleIO implements store.Store.
func (s *Store) SettleIO(ctx context.Context, reservationID string, actualInput, actualOutput int64) (api.Decision, error) {
	mode, key, err := s.reservationMode(ctx, reservationID)
	if err != nil {
		return api.Decision{}, mapResErr(err)
	}
	if mode != "split" {
		return api.Decision{}, store.ErrWrongSettleMode
	}
	resKey := reservationKey(reservationID)
	rpmKey, itpmKey, otpmKey := bucketKeysIO(key)
	raw, err := s.settleIO.Run(ctx, s.rdb,
		[]string{resKey, rpmKey, itpmKey, otpmKey},
		actualInput,
		actualOutput,
		int(store.ReservationTTL.Seconds()),
	).Result()
	if err != nil {
		return api.Decision{}, mapResErr(err)
	}
	return parseSettleIO(raw, reservationID)
}

// Refund implements store.Store.
func (s *Store) Refund(ctx context.Context, reservationID string) error {
	mode, key, err := s.reservationMode(ctx, reservationID)
	if err != nil {
		return mapResErr(err)
	}
	resKey := reservationKey(reservationID)
	if mode == "split" {
		rpmKey, itpmKey, otpmKey := bucketKeysIO(key)
		_, err = s.refundIO.Run(ctx, s.rdb,
			[]string{resKey, rpmKey, itpmKey, otpmKey},
			int(store.ReservationTTL.Seconds()),
		).Result()
		return mapResErr(err)
	}
	rpmKey, tpmKey := bucketKeys(key)
	_, err = s.refund.Run(ctx, s.rdb,
		[]string{resKey, rpmKey, tpmKey},
		int(store.ReservationTTL.Seconds()),
	).Result()
	return mapResErr(err)
}

// Borrow implements store.Store.
func (s *Store) Borrow(ctx context.Context, key api.Key, limits api.Limits, spec store.BorrowSpec) (store.BorrowResult, error) {
	if err := limits.Validate(); err != nil {
		return store.BorrowResult{}, err
	}
	if key.Subject == "" {
		return store.BorrowResult{}, errors.New("valve: subject required")
	}
	if limits.Split() {
		rpmKey, itpmKey, otpmKey := bucketKeysIO(key)
		raw, err := s.borrowIO.Run(ctx, s.rdb,
			[]string{rpmKey, itpmKey, otpmKey},
			limits.RequestsPerMinute,
			limits.InputTokensPerMinute,
			limits.OutputTokensPerMinute,
			spec.MinRPM, spec.MinITPM, spec.MinOTPM,
			spec.ChunkRPM, spec.ChunkITPM, spec.ChunkOTPM,
		).Result()
		if err != nil {
			return store.BorrowResult{}, err
		}
		arr, ok := raw.([]any)
		if !ok || len(arr) < 9 {
			return store.BorrowResult{}, fmt.Errorf("valve: unexpected borrow_io response %#v", raw)
		}
		return store.BorrowResult{
			Allowed:       toInt64(arr[0]) == 1,
			LimitType:     api.LimitType(toString(arr[1])),
			GotRPM:        toInt64(arr[2]),
			GotITPM:       toInt64(arr[3]),
			GotOTPM:       toInt64(arr[4]),
			RemainingRPM:  toInt64(arr[5]),
			RemainingITPM: toInt64(arr[6]),
			RemainingOTPM: toInt64(arr[7]),
			RemainingTPM:  toInt64(arr[7]),
			RetryAfter:    time.Duration(toInt64(arr[8])) * time.Millisecond,
			LimitRPM:      limits.RequestsPerMinute,
			LimitITPM:     limits.InputTokensPerMinute,
			LimitOTPM:     limits.OutputTokensPerMinute,
			LimitTPM:      limits.OutputTokensPerMinute,
		}, nil
	}

	rpmKey, tpmKey := bucketKeys(key)
	raw, err := s.borrow.Run(ctx, s.rdb,
		[]string{rpmKey, tpmKey},
		limits.RequestsPerMinute,
		limits.TokensPerMinute,
		spec.MinRPM, spec.MinTPM, spec.ChunkRPM, spec.ChunkTPM,
	).Result()
	if err != nil {
		return store.BorrowResult{}, err
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) < 7 {
		return store.BorrowResult{}, fmt.Errorf("valve: unexpected borrow response %#v", raw)
	}
	return store.BorrowResult{
		Allowed:      toInt64(arr[0]) == 1,
		LimitType:    api.LimitType(toString(arr[1])),
		GotRPM:       toInt64(arr[2]),
		GotTPM:       toInt64(arr[3]),
		RemainingRPM: toInt64(arr[4]),
		RemainingTPM: toInt64(arr[5]),
		RetryAfter:   time.Duration(toInt64(arr[6])) * time.Millisecond,
		LimitRPM:     limits.RequestsPerMinute,
		LimitTPM:     limits.TokensPerMinute,
	}, nil
}

// Return implements store.Store.
func (s *Store) Return(ctx context.Context, key api.Key, limits api.Limits, rpm, tpm, itpm, otpm int64) error {
	if key.Subject == "" {
		return errors.New("valve: subject required")
	}
	if rpm <= 0 && tpm <= 0 && itpm <= 0 && otpm <= 0 {
		return nil
	}
	if limits.Split() {
		rpmKey, itpmKey, otpmKey := bucketKeysIO(key)
		_, err := s.retIO.Run(ctx, s.rdb,
			[]string{rpmKey, itpmKey, otpmKey},
			limits.RequestsPerMinute,
			limits.InputTokensPerMinute,
			limits.OutputTokensPerMinute,
			rpm, itpm, otpm,
		).Result()
		return err
	}
	rpmKey, tpmKey := bucketKeys(key)
	_, err := s.ret.Run(ctx, s.rdb,
		[]string{rpmKey, tpmKey},
		limits.RequestsPerMinute,
		limits.TokensPerMinute,
		rpm, tpm,
	).Result()
	return err
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
	case strings.Contains(msg, "wrong settle mode"):
		return store.ErrWrongSettleMode
	default:
		return err
	}
}

func parseDecision(raw any, limits api.Limits, reservationID string) (api.Decision, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) < 7 {
		return api.Decision{}, fmt.Errorf("valve: unexpected check response %#v", raw)
	}
	allowed := toInt64(arr[0]) == 1
	dec := api.Decision{
		Allowed:      allowed,
		LimitType:    api.LimitType(toString(arr[1])),
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
	return dec, nil
}

func parseDecisionIO(raw any, limits api.Limits, reservationID string) (api.Decision, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) < 9 {
		return api.Decision{}, fmt.Errorf("valve: unexpected check_io response %#v", raw)
	}
	allowed := toInt64(arr[0]) == 1
	dec := api.Decision{
		Allowed:       allowed,
		LimitType:     api.LimitType(toString(arr[1])),
		RemainingRPM:  toInt64(arr[2]),
		RemainingITPM: toInt64(arr[3]),
		RemainingOTPM: toInt64(arr[4]),
		RemainingTPM:  toInt64(arr[4]),
		RetryAfter:    time.Duration(toInt64(arr[5])) * time.Millisecond,
		ResetRPM:      time.UnixMilli(toInt64(arr[6])),
		ResetITPM:     time.UnixMilli(toInt64(arr[7])),
		ResetOTPM:     time.UnixMilli(toInt64(arr[8])),
		ResetTPM:      time.UnixMilli(toInt64(arr[8])),
		LimitRPM:      limits.RequestsPerMinute,
		LimitITPM:     limits.InputTokensPerMinute,
		LimitOTPM:     limits.OutputTokensPerMinute,
		LimitTPM:      limits.OutputTokensPerMinute,
	}
	if allowed {
		dec.ReservationID = reservationID
		dec.LimitType = api.LimitTypeNone
	}
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

func parseSettleIO(raw any, reservationID string) (api.Decision, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) < 11 {
		return api.Decision{}, fmt.Errorf("valve: unexpected settle_io response %#v", raw)
	}
	dec := api.Decision{
		Allowed:       toInt64(arr[0]) == 1,
		LimitType:     api.LimitType(toString(arr[1])),
		RemainingRPM:  toInt64(arr[2]),
		RemainingITPM: toInt64(arr[3]),
		RemainingOTPM: toInt64(arr[4]),
		RemainingTPM:  toInt64(arr[4]),
		RetryAfter:    time.Duration(toInt64(arr[5])) * time.Millisecond,
		ResetRPM:      time.UnixMilli(toInt64(arr[6])),
		ResetITPM:     time.UnixMilli(toInt64(arr[7])),
		ResetOTPM:     time.UnixMilli(toInt64(arr[8])),
		ResetTPM:      time.UnixMilli(toInt64(arr[8])),
		OvershootITPM: toInt64(arr[9]),
		OvershootOTPM: toInt64(arr[10]),
		ReservationID: reservationID,
	}
	if len(arr) >= 14 {
		dec.LimitRPM = toInt64(arr[11])
		dec.LimitITPM = toInt64(arr[12])
		dec.LimitOTPM = toInt64(arr[13])
		dec.LimitTPM = dec.LimitOTPM
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
