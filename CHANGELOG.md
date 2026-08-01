# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.1] — 2026-08-01

### Changed

- Docs: provider-agnostic positioning (any LLM/SLM — self-hosted or commercial); README motto, who-this-helps, production checklist

## [0.2.0] — 2026-08-01

### Added

- Anthropic-shaped **ITPM / OTPM** split mode (`Limits.InputTokensPerMinute` + `OutputTokensPerMinute`, `SettleIO`)
- Redis/memory triple-bucket Lua/path; fast-path lease chunks for ITPM/OTPM
- Input/output `x-ratelimit-*` headers; HTTP/gRPC optional settle IO fields
- Echo middleware example, vLLM proxy README, Helm chart (`deploy/helm/valve`)

### Changed

- `api.Limiter` gains `SettleIO`; store `Borrow`/`Return` take split-aware specs

## [0.1.1] — 2026-08-01

### Added

- Python stdlib HTTP client for `valved` (`examples/python-client`)
- Gin + `httpmw` adapter demo (`examples/gin-ratelimit`, separate `go.mod`)
- Grafana dashboard JSON (`deploy/grafana/valve-dashboard.json`)
- Sticky-routing notes for hot org keys (`docs/STICKY_ROUTING.md`)

## [0.1.0] — 2026-08-01

First public release of **valve**: dual-dimension (RPM + TPM) rate limiting for LLM/API gateways.

### Added

- Dual token-bucket core with atomic Redis/Valkey Lua and in-memory store
- `Check` / `Settle` / `Refund` reservation lifecycle
- Fast path: deny cache + local lease borrow/return (`WithFastPath`)
- LLM helpers: token estimate, size gate, usage/SSE parse, mismatch counters
- OpenAI-compatible `x-ratelimit-*` header helper
- Example OpenAI-compatible reverse proxy
- `valved` sidecar (HTTP + gRPC), Prometheus metrics, structured deny logs
- net/http middleware and gRPC unary interceptor
- Docker Compose (redis + valved + prometheus) and Kubernetes manifests
- Design docs (`WHAT_THIS_IS.md`), HTTP API docs, CI (race + compose smoke)
- Community: CONTRIBUTING, SECURITY, CODE_OF_CONDUCT, issue/PR templates

### Security

- Fail-closed by default; fail-open documented and lease-scoped on the fast path
- Deny logs use hashed subjects only

[0.2.1]: https://github.com/shiv-eshwar/valve/releases/tag/v0.2.1
[0.2.0]: https://github.com/shiv-eshwar/valve/releases/tag/v0.2.0
[0.1.1]: https://github.com/shiv-eshwar/valve/releases/tag/v0.1.1
[0.1.0]: https://github.com/shiv-eshwar/valve/releases/tag/v0.1.0
