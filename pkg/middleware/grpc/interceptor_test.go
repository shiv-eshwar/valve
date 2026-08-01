package grpcmw_test

import (
	"context"
	"testing"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	grpcmw "github.com/shiv-eshwar/valve/pkg/middleware/grpc"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnaryInterceptorDeny(t *testing.T) {
	lim := limiter.New(memory.New())
	cfg := grpcmw.Config{
		Limiter: lim,
		Key:     func(context.Context, any) api.Key { return api.Key{Subject: "g", Model: "m"} },
		Limits:  func(context.Context, any) api.Limits { return api.Limits{RequestsPerMinute: 1, TokensPerMinute: 100} },
		Cost:    func(context.Context, any) api.Cost { return api.Cost{Requests: 1, Tokens: 1} },
	}
	interceptor := grpcmw.UnaryInterceptor(cfg)
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, handler)
	if err != nil {
		t.Fatal(err)
	}
	_, err = interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, handler)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("err=%v", err)
	}
}
