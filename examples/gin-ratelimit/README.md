# Gin + valve middleware

Minimal Gin server that wraps [`pkg/middleware/http`](../../pkg/middleware/http) so Gin stays out of the core module.

## Run

```bash
cd examples/gin-ratelimit
go run .
```

```bash
curl -i -H 'X-Subject: alice' http://127.0.0.1:8088/hello
# After 5 allows / minute for that subject, expect HTTP 429 + x-ratelimit-* headers.
```

Limits in the demo: **5 RPM**, **1000 TPM**, cost **100 tokens** per request.

## Adapter pattern

`valveGin` runs `httpmw.Middleware` as an `http.Handler`, then continues the Gin chain only when Check allows. On deny, middleware writes **429** and the adapter calls `c.Abort()`.

Successful checks expose `httpmw.ReservationIDFromContext(c.Request.Context())` for Settle/Refund in your handlers.
