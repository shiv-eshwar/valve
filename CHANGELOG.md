# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.1.0]: https://github.com/shiv-eshwar/valve/releases/tag/v0.1.0
