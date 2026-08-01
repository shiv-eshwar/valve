# valve HTTP API (`valved`)

Polyglot gateway contract. Responses mirror the Go `api.Decision` fields.

Base URL default: `http://localhost:8080`

## Health

- `GET /healthz` → `200 ok`
- `GET /readyz` → `200 ready` (pings Redis when `REDIS_ADDR` is set)
- `GET /metrics` → Prometheus text

## POST `/v1/check`

```json
{
  "key": { "subject": "org_123", "model": "gpt-4o" },
  "limits": { "requests_per_minute": 60, "tokens_per_minute": 90000 },
  "cost": { "requests": 1, "tokens": 1200 }
}
```

Response:

```json
{
  "allowed": true,
  "limit_type": "",
  "remaining_rpm": 59,
  "remaining_tpm": 88800,
  "limit_rpm": 60,
  "limit_tpm": 90000,
  "retry_after_ms": 0,
  "reservation_id": "…",
  "overshoot_tpm": 0,
  "reset_rpm": "…",
  "reset_tpm": "…"
}
```

When `allowed` is false, `limit_type` is `requests`, `tokens`, or `backend`. Sidecar also emits a structured JSON deny log line (hashed subject only).

## POST `/v1/settle`

```json
{ "reservation_id": "…", "actual_tokens": 980 }
```

Returns a `Decision` object (same shape as check).

## POST `/v1/refund`

```json
{ "reservation_id": "…" }
```

Returns `{}` on success.

## gRPC

Service `valve.v1.RateLimit` on `GRPC_LISTEN` (default `:9090`). See [`proto/valve/v1/ratelimit.proto`](../proto/valve/v1/ratelimit.proto).

## Gateway integration notes

- Call **Check** before forwarding to the model; call **Settle** with provider `usage` totals (or **Refund** on hard failure before upstream).
- Map deny → HTTP `429` and copy remaining/limit fields into `x-ratelimit-*` headers (see `pkg/headers`).
- Envoy/Kong can use this HTTP API via ext_authz/filter WASM/custom; a native Envoy rl_service adapter is intentionally out of scope.
