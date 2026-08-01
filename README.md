# valve

Dual-dimension (RPM + TPM) distributed rate limiter for LLM and API gateways.

Most rate limiters count requests. LLM APIs burn **tokens**. valve enforces both with an atomic dual token-bucket, reserve → settle → refund accounting, and OpenAI-compatible decision fields.

Design: [WHAT_THIS_IS.md](./WHAT_THIS_IS.md) · Progress: [engineering.md](./engineering.md) · HTTP API: [docs/HTTP_API.md](./docs/HTTP_API.md)

## Install

```bash
go get github.com/shiv-eshwar/valve@latest
```

## Quick example

```go
package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/headers"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/llm"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func main() {
	ctx := context.Background()
	lim := limiter.New(memory.New())

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)
	est, err := llm.EstimateChatRequest(body, nil)
	if err != nil {
		panic(err)
	}

	key := api.Key{Subject: "org_123", Model: "gpt-4o"}
	limits := api.Limits{RequestsPerMinute: 60, TokensPerMinute: 90_000}
	d, err := lim.Check(ctx, key, limits, api.Cost{Requests: 1, Tokens: est.TotalTokens})
	if err != nil {
		panic(err)
	}

	h := make(http.Header)
	headers.Write(h, d)
	if !d.Allowed {
		fmt.Println("denied:", d.LimitType, h.Get("Retry-After"))
		return
	}

	actual := int64(42)
	sd, err := lim.Settle(ctx, d.ReservationID, actual)
	if err != nil {
		panic(err)
	}
	llm.RecordSettle(est.TotalTokens, actual)
	headers.Write(h, sd)
	fmt.Println("ok; remaining TPM", sd.RemainingTPM)
}
```

## Sidecar (`valved`) + Compose

```bash
docker compose up -d --build
curl -s http://127.0.0.1:8080/healthz
curl -s -X POST http://127.0.0.1:8080/v1/check \
  -H 'Content-Type: application/json' \
  -d '{"key":{"subject":"demo","model":"gpt-4o"},"limits":{"requests_per_minute":60,"tokens_per_minute":90000},"cost":{"requests":1,"tokens":100}}'
# Prometheus UI: http://127.0.0.1:9091  (scrapes valved /metrics)
```

Or run locally:

```bash
go run ./cmd/valved          # memory store
REDIS_ADDR=localhost:6379 go run ./cmd/valved
```

HTTP `:8080` · gRPC `:9090` · see [docs/HTTP_API.md](./docs/HTTP_API.md).

## Fast path

```go
import "github.com/shiv-eshwar/valve/pkg/lease"

lim := limiter.New(store, limiter.WithFastPath(lease.DefaultConfig()))
defer lim.Close(ctx)
```

## LLM proxy example

```bash
cd examples/openai-proxy && go run .
```

## Status

- Phase 1–3: core, fast path, LLM ergonomics
- Phase 4: `valved`, Prometheus, middleware, Compose/k8s
- Next: Phase 5 — OSS polish (`CONTRIBUTING`, changelog, `v0.1.0`)

## License

MIT
