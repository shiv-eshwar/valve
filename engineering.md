# Engineering

Living progress tracker for the dual-dimension rate limiter.  
Source of product truth: [`WHAT_THIS_IS.md`](./WHAT_THIS_IS.md).

Update this file whenever work lands. Do not delete remaining work — refine it.

---

## Current status

| Field | Value |
| --- | --- |
| Phase | **4 — Ops (complete)** |
| Status | valved HTTP+gRPC, Prometheus, middleware, Compose/k8s, HTTP_API docs, CI compose smoke |
| Last updated | 2026-08-01 |
| Next up | Phase 5 — OSS polish (CONTRIBUTING, changelog, v0.1.0) |

---

## How to update this file

1. When a task finishes: set `- [ ]` → `- [x]` and note the date inline if useful.
2. Bump **Current status** (phase, one-line status, `Last updated`).
3. Add a row to **Decision log** when a design choice changes.
4. Never erase unfinished tasks; rewrite them if the approach improves.
5. If a phase slips, update **Estimates** — keep history visible in the decision log.

---

## North star

Pulled from `WHAT_THIS_IS.md`:

| Goal | Target |
| --- | --- |
| Dual enforcement | RPM + TPM atomically (all-or-nothing) |
| Local path latency | p99 &lt; 50 µs (L0/L1) |
| Redis path latency | p99 &lt; 1–2 ms (L2) |
| LLM accounting | Reserve → settle → refund |
| Client contract | OpenAI-compatible `x-ratelimit-*` headers |
| Store failure default | Fail-closed (configurable fail-open) |
| Language / store | Go + Redis/Valkey Lua |

---

## Locked defaults

| Decision | Choice |
| --- | --- |
| Language | Go |
| Store | Redis / Valkey + Lua (`EVALSHA`) |
| Algorithm | Dual token bucket |
| Redis failure | Fail-closed by default |
| Headers | OpenAI-compatible |
| Denied requests | Do not consume budget |

---

## Phase breakdown

### Phase 0 — Spec / docs

**Goal:** Anyone can understand the system before code exists.

- [x] `WHAT_THIS_IS.md` — problem, algorithms, architecture, scaling, behavior matrix, API contract
- [x] `engineering.md` — progress tracker, phases, decision log
- [x] README stub pointing at the two docs (2026-08-01)

**Exit criteria:** Design questions answerable from docs alone.

---

### Phase 1 — Correct core

**Goal:** Distributed correctness before performance tricks.

**Estimate:** ~2 weeks

- [x] Go module layout (`pkg/bucket`, `pkg/store/redis`, `pkg/api`) — module `github.com/shiv-eshwar/valve`
- [x] In-memory store for unit tests
- [x] Single token-bucket math (refill, allow, deny, retry_after)
- [x] Dual-bucket Check Lua script (all-or-nothing RPM + TPM)
- [x] Cold-key initialization (buckets start at capacity)
- [x] Use Redis `TIME` inside Lua
- [x] `Check(ctx, key, limits, cost) -> Decision`
- [x] `Settle(ctx, reservation_id, actual_tokens)`
- [x] `Refund(ctx, reservation_id)`
- [x] Reservation ID lifecycle + TTL cleanup (15m)
- [x] Concurrent stress test (many clients, one subject) — memory + miniredis
- [x] Fail-closed / fail-open config switch (behavior only; no lease yet)

**Exit criteria:** Under concurrency, no double-spend; settle/refund balances match tests; Redis integration test green.

**Better-approach notes (do before Phase 2):**

- Keep public API reservation-based even if v1 settle is “best effort” — avoids rewrite when streaming lands.
- Prefer one Lua file per operation with golden tests over string-concat scripts in Go.

---

### Phase 2 — Fast path

**Goal:** Limiter stays faster than the API under high QPS.

**Estimate:** ~2 weeks

- [x] L0 in-process deny cache (subject → not-before time) — `pkg/lease`
- [x] L1 local lease / chunk borrow for RPM + TPM — `Borrow`/`Return` + `WithFastPath`
- [x] Lease TTL + refresh path (default 2s; defaults RPM chunk 5, TPM chunk 500)
- [x] Best-effort lease return on shutdown — `Limiter.Close`
- [x] Document overshoot bound: unused in-flight lease ≤ `pods × chunk`; allows ≤ global (borrow debits Redis)
- [x] Benchmarks: `BenchmarkCheck_Direct` vs `BenchmarkCheck_FastPath`; hit-ratio test ≥ 80%
- [x] Redis Cluster hash tags `{subject}` — already in Phase 1 keys
- [x] `EVALSHA` via go-redis `Script.Run` + `Script.Load` warm on `redisstore.New`

**Exit criteria:** Bench proves ~majority local hits; p99 local &lt; 50 µs in lab; overshoot formula documented and tested at small N.

**Better-approach notes:**

- Tune chunk size per tier later; start with conservative global defaults.
- Deny cache must invalidate on settle refund that restores headroom (or short TTL only).

---

### Phase 3 — LLM ergonomics

**Goal:** Usable in front of real model APIs.

**Estimate:** ~2 weeks

- [x] Fast token estimator (chars/4) with documented error bound — `pkg/llm`
- [x] Optional exact tokenizer hook interface — `llm.Tokenizer`
- [x] Per-request max token/size gate — `llm.Gate` → HTTP 413 in proxy
- [x] OpenAI-compatible response headers helper — `pkg/headers`
- [x] Example: reverse proxy — `examples/openai-proxy`
- [x] Streaming settle path (SSE usage parse + settle timeout)
- [x] Estimate vs actual mismatch metrics — `llm.Metrics` atomics
- [x] Minimal GitHub Actions CI (`go test ./... -race`)

**Exit criteria:** Example proxy enforces RPM+TPM; headers match contract; streaming settle covered by tests.

**Better-approach notes:**

- Do not block Check on heavy tokenizer; estimate → settle is the product.
- Mirror provider header names exactly to maximize SDK reuse.

---

### Phase 4 — Ops and integration

**Goal:** Runnable in real clusters; observable; adoptable by other services.

**Estimate:** ~2 weeks

- [x] Prometheus metrics — `pkg/metrics` (`valve_checks_total`, settle/refund, check duration, lease hit ratio, overshoot, store errors)
- [x] Structured deny logs — `pkg/logx` (hashed subject)
- [x] `cmd/valved` HTTP + gRPC sidecar
- [x] net/http middleware — `pkg/middleware/http`
- [x] gRPC interceptor — `pkg/middleware/grpc`
- [x] Docker Compose — `docker-compose.yml` (redis + valved + prometheus)
- [x] Deploy manifests — `deploy/k8s/`
- [x] CI: race tests + compose smoke job
- [x] Gateway contract — `docs/HTTP_API.md` (Envoy native adapter skipped by design)

**Exit criteria:** `docker compose up` demo works; CI green; metrics visible in a local Prometheus scrape.

**Better-approach notes:**

- Sidecar API should match library Decision 1:1 to avoid dual semantics.
- Prefer Envoy-compatible proto only if it does not warp dual-bucket settle; otherwise ship clear HTTP first.

---

### Phase 5 — OSS polish

**Goal:** Others can adopt and contribute without reading chat history.

**Estimate:** ~1–2 weeks

- [ ] README with quickstart, SLOs, and link to `WHAT_THIS_IS.md`
- [ ] LICENSE (MIT or Apache-2.0)
- [ ] CONTRIBUTING.md + good first issues
- [ ] Comparative benchmarks vs naive INCR and single-bucket libs
- [ ] “When to use this” / “when not to” (pointer to What this is not)
- [ ] Versioned module tag `v0.1.0`
- [ ] Changelog

**Exit criteria:** Cold contributor can run tests and understand scope in one sitting.

---

## Suggested repo layout (Phase 1+)

```text
cmd/valved/           # sidecar
pkg/bucket/               # token bucket + dual check math
pkg/store/memory/
pkg/store/redis/          # Lua scripts, EVALSHA, borrow/return
pkg/lease/                # deny cache + local chunk lease (Phase 2)
pkg/limiter/              # facade + WithFastPath
pkg/llm/                  # estimate, gate, usage/SSE, mismatch metrics
pkg/headers/              # OpenAI-compatible response headers
examples/openai-proxy/    # runnable reverse proxy
.github/workflows/ci.yml
pkg/middleware/http/
pkg/middleware/grpc/
proto/                    # optional gRPC
scripts/bench/
examples/openai-proxy/
WHAT_THIS_IS.md
engineering.md
```

---

## Decision log

| Date | Decision | Rationale |
| --- | --- | --- |
| 2026-08-01 | Dual token bucket as core algorithm | Native weighted cost (tokens); burst + sustained rate; clean atomic debit |
| 2026-08-01 | Go + Redis/Valkey Lua | Gateway-native language; atomic multi-key updates; sub-ms store |
| 2026-08-01 | Fail-closed default | Abuse/cost control preferred over silent overage; fail-open configurable |
| 2026-08-01 | OpenAI-compatible headers | Matches client/SDK expectations; dual remaining/reset signals |
| 2026-08-01 | Docs before code | Empty repo; `WHAT_THIS_IS.md` + `engineering.md` as source of truth |
| 2026-08-01 | Regional Redis in v1 | Honest multi-region tradeoffs; no fake global strong consistency |
| 2026-08-01 | Brand rename `bucketrate` → `valve` | Short, memorable OSS name; path `github.com/shiv-eshwar/valve`; sidecar `valved` |
| 2026-08-01 | miniredis for Redis tests | No host Redis required; Lua + TIME covered in CI |
| 2026-08-01 | Hash tags in Phase 1 keys | `rl:{{{subject}}}:...` avoids key rewrite in Phase 2 |
| 2026-08-01 | Limits passed on Check | Capacity = per-minute budget; refill = budget/60 |
| 2026-08-01 | Fast path opt-in via `WithFastPath` | Keeps Phase 1 exact tests; gateway enables leases explicitly |
| 2026-08-01 | Borrow debits Redis before local spend | Global allows ≤ capacity; overshoot is unused lease credits ≤ N×chunk |
| 2026-08-01 | Fail-open + fast path = lease-only | No invented allows when store is down and lease empty |
| 2026-08-01 | Char/4 estimator + Tokenizer hook | Hot path stays fast; exact tiktoken optional |
| 2026-08-01 | Headers package mirrors OpenAI names | SDK-compatible `x-ratelimit-*` |
| 2026-08-01 | CI on Phase 3 | Strangers can verify green without tribal knowledge |
| 2026-08-01 | HTTP-first sidecar; skip Envoy rl_service | Decision 1:1 JSON + gRPC; document gateway HTTP contract |
| 2026-08-01 | ObservedLimiter wrapper | Keep core limiter pure; metrics opt-in |

---

## Risks and open follow-ups

| Risk | Impact | Mitigation / when |
| --- | --- | --- |
| Lease unused credits under many pods | Temporary budget held off Redis until Close/TTL | Chunk defaults small; `Close` returns; documented |
| Tokenizer cost on hot path | Limiter slower than API | Fast estimate only on Check; Phase 3 |
| Streaming without final usage | Stuck reservations | Timeout + refund/settle policy; Phase 3 |
| Redis hot keys (huge tenants) | Latency spikes | Finer keying / sticky routing; Phase 4 |
| Multi-region double budget | 2× usage if multi-home | Document; CRDT/active-active only as later phase |
| Deny cache vs refund race | False deny after refund | Cleared on Settle credit / Refund (Phase 2) |

---

## Progress snapshot

| Phase | Name | State |
| --- | --- | --- |
| 0 | Spec / docs | **Done** |
| 1 | Correct core | **Done** (2026-08-01) |
| 2 | Fast path | **Done** (2026-08-01) |
| 3 | LLM ergonomics | **Done** (2026-08-01) |
| 4 | Ops / integration | **Done** (2026-08-01) |
| 5 | OSS polish | Not started |

---

## Working agreement

- Correctness before leases; leases before OSS marketing.
- Every deny path must name `limit_type`.
- Benchmarks gate Phase 2 exit; concurrency tests gate Phase 1 exit.
- When stuck, re-read the behavior matrix in `WHAT_THIS_IS.md` before inventing new semantics.
