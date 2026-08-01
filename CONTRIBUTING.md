# Contributing to valve

Thanks for helping make dual-dimension rate limiting better.

By participating, you agree to follow our [Code of Conduct](./CODE_OF_CONDUCT.md).

## Prerequisites

- Go **1.24+**
- Optional: Docker Compose v2 (for `make compose-smoke`)
- Optional: `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc` (only if regenerating stubs)

## Setup

```bash
git clone https://github.com/shiv-eshwar/valve.git
cd valve
go test ./... -race
```

## Workflow

1. Open an issue or pick a **good first issue** below.
2. Branch from `main`.
3. Keep changes focused; match existing package layout and naming.
4. Add/adjust tests for behavior changes.
5. Run:

```bash
go test ./... -race
gofmt -w $(git ls-files '*.go' | grep -v pkg/gen)
```

6. Open a PR with a short “why” and test notes.

### Proto regeneration

Generated code lives in [`pkg/gen/valve/v1`](./pkg/gen/valve/v1) and is **checked in** so CI does not need `protoc`.

```bash
protoc --go_out=pkg/gen --go_opt=module=github.com/shiv-eshwar/valve/pkg/gen \
  --go-grpc_out=pkg/gen --go-grpc_opt=module=github.com/shiv-eshwar/valve/pkg/gen \
  proto/valve/v1/ratelimit.proto
```

### Compose smoke

```bash
make compose-smoke
```

## Design source of truth

- Product/behavior: [`WHAT_THIS_IS.md`](./WHAT_THIS_IS.md)
- Tracker: [`engineering.md`](./engineering.md)
- Do not invent new deny semantics without updating the behavior matrix.

## Good first issues

| Idea | Area |
| --- | --- |
| Thin **Python** client for `docs/HTTP_API.md` | examples |
| **Gin** / Echo middleware wrapper around `pkg/middleware/http` | middleware |
| Split **ITPM / OTPM** (Anthropic-shaped) buckets | core |
| Grafana dashboard JSON scraping `valve_*` metrics | ops |
| Documented sticky-routing notes for hot org keys | docs |
| Example against **vLLM** OpenAI-compatible server | examples |

## Code of conduct

Be respectful. No harassment. Assume good intent in reviews.

## License

By contributing, you agree your contributions are licensed under the MIT License.
