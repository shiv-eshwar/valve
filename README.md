# valve

[![CI](https://github.com/shiv-eshwar/valve/actions/workflows/ci.yml/badge.svg)](https://github.com/shiv-eshwar/valve/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/shiv-eshwar/valve.svg)](https://pkg.go.dev/github.com/shiv-eshwar/valve)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

**Rate-limit any LLM or SLM API by requests and tokens** — not just OpenAI.

valve sits in front of hosted providers, open-source servers (vLLM, Ollama, TGI, …), or your own model HTTP API. It meters **RPM + token budgets** with reserve → settle → refund, so variable prompt/completion cost does not break fair tenancy.

Most rate limiters count requests. Model APIs burn **tokens**. valve meters both.

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

## Motto

> Fair, fast **request + token** limits for **any** model API you front — cloud or self-hosted, large or small.

OpenAI-style `x-ratelimit-*` headers and Anthropic-style input/output token splits are **optional shapes**, not a vendor lock-in.

## Who this helps

| You… | valve helps by… |
| --- | --- |
| Run a gateway in front of vLLM / Ollama / TGI / custom models | Per-tenant RPM + TPM so one user cannot starve others |
| Proxy a commercial API (OpenAI, Anthropic, …) | Same Check / Settle contract; map provider `usage` on settle |
| Need unknown output tokens accounted for | Reserve estimate → settle actual (or refund on hard fail) |
| Want polyglot enforcement | Call `valved` over HTTP/gRPC from any language |

## Features

- **RPM + TPM** dual token-bucket, all-or-nothing Check (provider-agnostic)
- **ITPM + OTPM** split mode when you need separate input/output budgets (`SettleIO`)
- **Reserve → Settle → Refund** for unknown generation length
- **Fast path**: in-process deny cache + local lease chunks (opt-in)
- Familiar **`x-ratelimit-*` headers** (widely supported by clients/SDKs)
- **Library + sidecar** (`valved` HTTP/gRPC)
- **Prometheus** metrics, structured deny logs (hashed subject)
- Redis/Valkey Lua + in-memory store for tests

## When to use / when not

**Use valve when** you proxy or serve model APIs (LLM or SLM) and need per-tenant **request and token** budgets — self-hosted or commercial.

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

Share this repo: **https://github.com/shiv-eshwar/valve**

## Install

```bash
go get github.com/shiv-eshwar/valve@v0.2.1
```

Requires Go 1.24+.

## Quick example

Works the same whether `model` is a cloud id or a self-hosted checkpoint name:

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

	// Chat-completions-shaped JSON is common (OpenAI-compatible servers, vLLM, etc.).
	body := []byte(`{"model":"my-model","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`)
	est, err := llm.EstimateChatRequest(body, nil)
	if err != nil {
		panic(err)
	}

	key := api.Key{Subject: "org_123", Model: "my-model"}
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

	actual := int64(42) // from your upstream usage / tokenizer
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
  -d '{"key":{"subject":"demo","model":"my-model"},"limits":{"requests_per_minute":60,"tokens_per_minute":90000},"cost":{"requests":1,"tokens":100}}'
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

## Examples (any upstream)

| Example | Use |
| --- | --- |
| [`examples/openai-proxy`](./examples/openai-proxy/) | Reverse proxy for **any** OpenAI-compatible HTTP API (set `OPENAI_BASE_URL`) |
| [`examples/vllm-proxy`](./examples/vllm-proxy/) | Same proxy pointed at local vLLM |
| [`examples/python-client`](./examples/python-client/) | Stdlib client for `valved` |

```bash
# Self-hosted (vLLM / Ollama / etc.)
cd examples/openai-proxy
OPENAI_BASE_URL=http://127.0.0.1:8000/v1 go run .

# Or any other chat-completions-compatible base URL
OPENAI_BASE_URL=https://api.example.com/v1 go run .
```

## Adopter kits

| Kit | Path |
| --- | --- |
| Python HTTP client | [`examples/python-client/`](./examples/python-client/) |
| Gin + `httpmw` demo | [`examples/gin-ratelimit/`](./examples/gin-ratelimit/) |
| Echo + `httpmw` demo | [`examples/echo-ratelimit/`](./examples/echo-ratelimit/) |
| vLLM / OpenAI-compatible proxy | [`examples/vllm-proxy/`](./examples/vllm-proxy/) |
| Helm chart | [`deploy/helm/valve/`](./deploy/helm/valve/) |
| Grafana dashboard | [`deploy/grafana/valve-dashboard.json`](./deploy/grafana/valve-dashboard.json) |
| Sticky routing notes | [`docs/STICKY_ROUTING.md`](./docs/STICKY_ROUTING.md) |

## Production checklist

1. Authenticate subjects **before** Check; valve trusts the `subject` you pass.
2. Prefer **fail-closed** (default) for abuse/cost control.
3. With fast path, sticky-route hot orgs when possible ([docs/STICKY_ROUTING.md](./docs/STICKY_ROUTING.md)).
4. Call `Limiter.Close` (or restart pods cleanly) so unused lease credits return.
5. Settle with real usage from your upstream when generation finishes; Refund if the request never left the gateway.

## Develop

```bash
go test ./... -race
go test ./pkg/limiter -bench='Benchmark(Naive|Valve)' -benchmem
```

See [docs/BENCHMARKS.md](./docs/BENCHMARKS.md) for sample numbers.

## License

MIT — see [LICENSE](./LICENSE).
