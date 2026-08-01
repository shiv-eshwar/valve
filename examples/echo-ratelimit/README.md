# Echo + valve middleware

Minimal Echo server wrapping [`pkg/middleware/http`](../../pkg/middleware/http). Echo stays out of the core module.

## Run

```bash
cd examples/echo-ratelimit
go run .
```

```bash
curl -i -H 'X-Subject: alice' http://127.0.0.1:8089/hello
```

Demo limits: **5 RPM**, **1000 TPM**, cost **100 tokens**/request.
