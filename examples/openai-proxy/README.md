# openai-proxy example

Rate-limited reverse proxy in front of OpenAI-compatible APIs using **valve**.

## Run (memory store)

```bash
cd examples/openai-proxy
go run . 
# LISTEN=:8080 OPENAI_BASE_URL=https://api.openai.com
```

Point your client at `http://localhost:8080` and send the same paths as OpenAI (`/v1/chat/completions`, etc.).

Optional env:

| Env | Default | Meaning |
| --- | --- | --- |
| `LISTEN` | `:8080` | Bind address |
| `OPENAI_BASE_URL` | `https://api.openai.com` | Upstream |
| `REDIS_ADDR` | (empty = memory) | e.g. `localhost:6379` |
| `RPM` | `60` | Requests per minute |
| `TPM` | `90000` | Tokens per minute |
| `MAX_INPUT_TOKENS` | `128000` | Pre-Check size gate |
| `MAX_REQUEST_BYTES` | `2097152` | Body size gate |

Subject key: SHA-256 prefix of `X-API-Key` or `Authorization` (stub identity, not a full auth product).

## Behavior

1. Estimate tokens (char/4 + `max_tokens`)
2. `Check` RPM+TPM (fast path on)
3. Deny → `429` + `x-ratelimit-*` headers
4. Allow → forward; JSON or SSE tee; `Settle` on `usage` (or Refund on failure)

## Test

```bash
go test -race .
```
