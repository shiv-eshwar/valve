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

	"github.com/shiv-eshwar/valve/pkg/api"
	"github.com/shiv-eshwar/valve/pkg/limiter"
	"github.com/shiv-eshwar/valve/pkg/store/memory"
)

func main() {
	ctx := context.Background()
	lim := limiter.New(memory.New())

	key := api.Key{Subject: "org_123", Model: "gpt-4o"}
	limits := api.Limits{RequestsPerMinute: 60, TokensPerMinute: 90_000}
	cost := api.Cost{Requests: 1, Tokens: 1_200}

	d, err := lim.Check(ctx, key, limits, cost)
	if err != nil {
		panic(err)
	}
	if !d.Allowed {
		fmt.Println("denied:", d.LimitType, "retry after", d.RetryAfter)
		return
	}

	// ... call your model, then settle with real usage ...
	actual := int64(980)
	_, err = lim.Settle(ctx, d.ReservationID, actual)
	if err != nil {
		panic(err)
	}
	fmt.Println("ok; remaining TPM", d.RemainingTPM)
}
```

## Phase 1 status

Correct core: `Check` / `Settle` / `Refund`, in-memory + Redis/Lua stores, fail-closed/open. Local leases and sidecar land in later phases — see [engineering.md](./engineering.md).

## License

MIT
