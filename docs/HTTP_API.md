# valve HTTP API (`valved`)

Polyglot gateway contract for **any** LLM/SLM upstream. Responses mirror the Go `api.Decision` fields.  
`model` is an opaque string (cloud model id or self-hosted name).

Base URL default: `http://localhost:8080`

## Health

- `GET /healthz` → `200 ok`
- `GET /readyz` → `200 ready` (pings Redis when `REDIS_ADDR` is set)
- `GET /metrics` → Prometheus text

## POST `/v1/check`

### Classic (OpenAI-shaped TPM)

```json
{
  "key": { "subject": "org_123", "model": "gpt-4o" },
  "limits": { "requests_per_minute": 60, "tokens_per_minute": 90000 },
  "cost": { "requests": 1, "tokens": 1200 }
}
```

### Split (Anthropic-shaped ITPM + OTPM)

Set **both** `input_tokens_per_minute` and `output_tokens_per_minute` (partial split is rejected). Classic `tokens_per_minute` is ignored in this mode.

```json
{
  "key": { "subject": "org_123", "model": "claude-sonnet" },
  "limits": {
    "requests_per_minute": 60,
    "input_tokens_per_minute": 40000,
    "output_tokens_per_minute": 8000
  },
  "cost": { "requests": 1, "input_tokens": 1200, "output_tokens": 512 }
}
```

Response (classic fields always present; ITPM/OTPM fields when split):

```json
{
  "allowed": true,
  "limit_type": "",
  "remaining_rpm": 59,
  "remaining_tpm": 88800,
  "remaining_itpm": 38800,
  "remaining_otpm": 7488,
  "limit_rpm": 60,
  "limit_tpm": 8000,
  "limit_itpm": 40000,
  "limit_otpm": 8000,
  "retry_after_ms": 0,
  "reservation_id": "…",
  "overshoot_tpm": 0,
  "reset_rpm": "…",
  "reset_tpm": "…"
}
```

In split mode, `remaining_tpm` / `limit_tpm` mirror **output** for OpenAI header compatibility.

When `allowed` is false, `limit_type` is `requests`, `tokens`, `input_tokens`, `output_tokens`, or `backend`.

## POST `/v1/settle`

Classic:

```json
{ "reservation_id": "…", "actual_tokens": 980 }
```

Split (presence of either IO field selects SettleIO — zeros allowed):

```json
{
  "reservation_id": "…",
  "actual_input_tokens": 1100,
  "actual_output_tokens": 420
}
```

Returns a `Decision` object (same shape as check). Wrong settle mode for the reservation returns HTTP 400.

## POST `/v1/refund`

```json
{ "reservation_id": "…" }
```

Returns `{}` on success.

## gRPC

Service `valve.v1.RateLimit` on `GRPC_LISTEN` (default `:9090`). See [`proto/valve/v1/ratelimit.proto`](../proto/valve/v1/ratelimit.proto). Optional settle fields select classic vs SettleIO.

## Gateway integration notes

- Call **Check** before forwarding to the model; call **Settle** / SettleIO with provider `usage` (or **Refund** on hard failure before upstream).
- Map deny → HTTP `429` and copy remaining/limit fields into `x-ratelimit-*` headers (see `pkg/headers`). Split mode also sets `x-ratelimit-*-input-tokens` / `*-output-tokens`.
- Envoy/Kong can use this HTTP API via ext_authz/filter WASM/custom; a native Envoy rl_service adapter is intentionally out of scope.
