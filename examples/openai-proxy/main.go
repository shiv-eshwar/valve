package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/lease"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/llm"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
	redisstore "github.com/shiv-eshwar/valve/pkg/store/redis"
)

func main() {
	base := env("OPENAI_BASE_URL", "https://api.openai.com")
	listen := env("LISTEN", ":8080")
	upstream, err := url.Parse(base)
	if err != nil {
		log.Fatal(err)
	}

	var lim *limiter.Limiter
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		lim = limiter.New(redisstore.New(rdb), limiter.WithFastPath(lease.DefaultConfig()))
		log.Printf("using redis at %s with fast path", addr)
	} else {
		lim = limiter.New(memory.New(), limiter.WithFastPath(lease.DefaultConfig()))
		log.Printf("using in-memory store with fast path")
	}
	defer func() { _ = lim.Close(context.Background()) }()

	limits := api.Limits{
		RequestsPerMinute: envInt64("RPM", 60),
		TokensPerMinute:   envInt64("TPM", 90_000),
	}
	h := NewHandler(ProxyConfig{
		Upstream: upstream,
		Limiter:  lim,
		Limits:   limits,
		Gate: llm.Gate{
			MaxInputTokens:  envInt64("MAX_INPUT_TOKENS", 128_000),
			MaxRequestBytes: envInt64("MAX_REQUEST_BYTES", 2<<20),
		},
	})

	srv := &http.Server{Addr: listen, Handler: h}
	go func() {
		log.Printf("valve openai-proxy listening on %s -> %s (rpm=%d tpm=%d)", listen, base, limits.RequestsPerMinute, limits.TokensPerMinute)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
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
