package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/shiv-eshwar/valve/pkg/api"
	valvepb "github.com/shiv-eshwar/valve/pkg/gen/valve/v1"
	"github.com/shiv-eshwar/valve/pkg/lease"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/logx"
	"github.com/shiv-eshwar/valve/pkg/metrics"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
	redisstore "github.com/shiv-eshwar/valve/pkg/store/redis"
	"google.golang.org/grpc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	listen := env("LISTEN", ":8080")
	grpcListen := env("GRPC_LISTEN", ":9090")
	logger := logx.New(os.Stdout)

	var (
		inner *limiter.Limiter
		rdb   *redis.Client
		ready func(context.Context) error
	)

	failMode := api.FailClosed
	if env("FAIL_MODE", "closed") == "open" {
		failMode = api.FailOpen
	}

	opts := []limiter.Option{limiter.WithFailMode(failMode)}
	if envBool("FAST_PATH", true) {
		opts = append(opts, limiter.WithFastPath(lease.Config{
			RPMChunk: envInt64("RPM_CHUNK", 5),
			TPMChunk: envInt64("TPM_CHUNK", 500),
			LeaseTTL: time.Duration(envInt64("LEASE_TTL_MS", 2000)) * time.Millisecond,
		}))
	}

	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: addr})
		inner = limiter.New(redisstore.New(rdb), opts...)
		ready = func(c context.Context) error { return rdb.Ping(c).Err() }
		log.Printf("valved: redis=%s", addr)
	} else {
		inner = limiter.New(memory.New(), opts...)
		ready = nil
		log.Printf("valved: memory store")
	}

	reg := prometheus.DefaultRegisterer
	var lim api.Limiter = inner
	if envBool("FAST_PATH", true) && inner.Pool() != nil {
		lim = metrics.New(inner, reg, metrics.WithHitRatio(inner.Pool()))
	} else {
		lim = metrics.New(inner, reg)
	}

	apiHTTP := &httpAPI{lim: lim, log: logger, ready: ready}
	httpSrv := &http.Server{Addr: listen, Handler: apiHTTP.routes()}

	go func() {
		log.Printf("valved http on %s", listen)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	lis, err := net.Listen("tcp", grpcListen)
	if err != nil {
		log.Fatal(err)
	}
	gs := grpc.NewServer()
	valvepb.RegisterRateLimitServer(gs, &grpcServer{lim: lim, log: logger})
	go func() {
		log.Printf("valved grpc on %s", grpcListen)
		if err := gs.Serve(lis); err != nil {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	gs.GracefulStop()
	_ = inner.Close(shutdownCtx)
	if rdb != nil {
		_ = rdb.Close()
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt64(k string, def int64) int64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
