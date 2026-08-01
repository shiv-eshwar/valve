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

## Fast path (Phase 2)

Opt in to L0 deny cache + L1 local lease borrowing so most Checks never hit Redis:

```go
import "github.com/shiv-eshwar/valve/pkg/lease"

lim := limiter.New(store, limiter.WithFastPath(lease.DefaultConfig()))
defer lim.Close(ctx) // best-effort return unused lease credits
```

Defaults: RPM chunk `5`, TPM chunk `500`, lease TTL `2s`.

**Overshoot:** Borrow debits the shared store first, so total allows cannot exceed the global budget (without refill). Unused credits held in-process across pods are bounded by:

```text
unused_in_flight ≤ num_pods × chunk_size
```

Call `Close` on shutdown to return leftovers. See [WHAT_THIS_IS.md](./WHAT_THIS_IS.md) and [engineering.md](./engineering.md).

## Status

- Phase 1: Correct core (`Check` / `Settle` / `Refund`, memory + Redis/Lua)
- Phase 2: Fast path (`WithFastPath`, deny cache, lease borrow/return, benches)
- Next: Phase 3 — LLM ergonomics (estimator, OpenAI headers, proxy example)

## License

MIT
