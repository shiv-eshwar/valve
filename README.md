# valve

[![CI](https://github.com/shiv-eshwar/valve/actions/workflows/ci.yml/badge.svg)](https://github.com/shiv-eshwar/valve/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/shiv-eshwar/valve.svg)](https://pkg.go.dev/github.com/shiv-eshwar/valve)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

**Dual-dimension (RPM + TPM) rate limiting for LLM and API gateways** — atomic token buckets, reserve → settle → refund, local lease fast path, OpenAI-compatible headers, Go library + `valved` sidecar.

Most rate limiters count requests. LLM APIs burn **tokens**. valve meters both.

| Doc | |
| --- | --- |
| Design | [WHAT_THIS_IS.md](./WHAT_THIS_IS.md) |
| Progress | [engineering.md](./engineering.md) |
| HTTP API | [docs/HTTP_API.md](./docs/HTTP_API.md) |
| Benchmarks | [docs/BENCHMARKS.md](./docs/BENCHMARKS.md) |
| Sticky routing | [docs/STICKY_ROUTING.md](./docs/STICKY_ROUTING.md) |
| Grafana | [deploy/grafana/](./deploy/grafana/) |
| Contributing | [CONTRIBUTING.md](./CONTRIBUTING.md) |
| Code of Conduct | [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md) |
| Security | [SECURITY.md](./SECURITY.md) |
| Changelog | [CHANGELOG.md](./CHANGELOG.md) |
| Releases | [GitHub Releases](https://github.com/shiv-eshwar/valve/releases) |

## Features

- **RPM + TPM** dual token-bucket, all-or-nothing Check
- **ITPM + OTPM** split mode (Anthropic-shaped) via `SettleIO`
- **Reserve → Settle → Refund** for unknown LLM output tokens
- **Fast path**: in-process deny cache + local lease chunks (opt-in)
- **OpenAI-compatible** `x-ratelimit-*` headers
- **Library + sidecar** (`valved` HTTP/gRPC) for any language
- **Prometheus** metrics, structured deny logs (hashed subject)
- Redis/Valkey Lua + in-memory store for tests

## When to use / when not

**Use valve when** you proxy or serve LLM APIs and need per-tenant RPM and TPM, or you want OpenAI-shaped limits in your own gateway.

**Do not use valve as** a full API gateway, billing ledger, tokenizer product, or strongly consistent global multi-region quota system. See [What this is not](./WHAT_THIS_IS.md#what-this-is-not).

## Latency targets (SLOs)

| Path | Target |
| --- | --- |
| L0/L1 local allow/deny (fast path) | p99 &lt; 50 µs (lab) |
| L2 Redis/Lua Check | p99 &lt; 1–2 ms (same AZ) |
| Overshoot (unused lease credits) | ≤ `num_pods × chunk_size` |

## Cold start (anyone)

```bash
git clone https://github.com/shiv-eshwar/valve.git
cd valve
go test ./... -race          # library + sidecar tests
docker compose up -d --build # redis + valved + prometheus (optional)
curl -sf http://127.0.0.1:8080/healthz
```

## Install

```bash
go get github.com/shiv-eshwar/valve@v0.2.0
```

Requires Go 1.24+.

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
# Prometheus: http://127.0.0.1:9091
```

```bash
go run ./cmd/valved
REDIS_ADDR=localhost:6379 go run ./cmd/valved
```

HTTP `:8080` · gRPC `:9090`

## Fast path

```go
import "github.com/shiv-eshwar/valve/pkg/lease"

lim := limiter.New(store, limiter.WithFastPath(lease.DefaultConfig()))
defer lim.Close(ctx)
```

Defaults: RPM chunk `5`, TPM chunk `500`, lease TTL `2s`.

## LLM proxy example

```bash
cd examples/openai-proxy && go run .
```

## Adopter kits

| Kit | Path |
| --- | --- |
| Python HTTP client | [`examples/python-client/`](./examples/python-client/) |
| Gin + `httpmw` demo | [`examples/gin-ratelimit/`](./examples/gin-ratelimit/) |
| Echo + `httpmw` demo | [`examples/echo-ratelimit/`](./examples/echo-ratelimit/) |
| vLLM via openai-proxy | [`examples/vllm-proxy/`](./examples/vllm-proxy/) |
| Helm chart | [`deploy/helm/valve/`](./deploy/helm/valve/) |
| Grafana dashboard | [`deploy/grafana/valve-dashboard.json`](./deploy/grafana/valve-dashboard.json) |
| Sticky routing notes | [`docs/STICKY_ROUTING.md`](./docs/STICKY_ROUTING.md) |

## Develop

```bash
go test ./... -race
go test ./pkg/limiter -bench='Benchmark(Naive|Valve)' -benchmem
```

See [docs/BENCHMARKS.md](./docs/BENCHMARKS.md) for sample numbers.

## License

MIT — see [LICENSE](./LICENSE).
