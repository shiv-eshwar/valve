# valve

Dual-dimension (RPM + TPM) distributed rate limiter for LLM and API gateways.

Most rate limiters count requests. LLM APIs burn **tokens**. valve enforces both with an atomic dual token-bucket, reserve → settle → refund accounting, and OpenAI-compatible decision fields.

Design: [WHAT_THIS_IS.md](./WHAT_THIS_IS.md) · Progress: [engineering.md](./engineering.md)

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

	// ... call model, parse usage ...
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

## Fast path (Phase 2)

```go
import "github.com/shiv-eshwar/valve/pkg/lease"

lim := limiter.New(store, limiter.WithFastPath(lease.DefaultConfig()))
defer lim.Close(ctx)
```

Defaults: RPM chunk `5`, TPM chunk `500`, lease TTL `2s`.

```text
unused_in_flight ≤ num_pods × chunk_size
```

## LLM proxy example (Phase 3)

```bash
cd examples/openai-proxy && go run .
# optional: REDIS_ADDR=localhost:6379 RPM=60 TPM=90000
```

See [examples/openai-proxy/README.md](./examples/openai-proxy/README.md).

## Status

- Phase 1: Correct core (`Check` / `Settle` / `Refund`, memory + Redis/Lua)
- Phase 2: Fast path (`WithFastPath`, deny cache, lease borrow/return)
- Phase 3: LLM ergonomics (`pkg/llm`, `pkg/headers`, openai-proxy, CI)
- Next: Phase 4 — Ops (`valved` sidecar, Compose, Prometheus)

## License

MIT
