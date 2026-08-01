# Python HTTP client for `valved`

Stdlib-only client for the [HTTP API](../../docs/HTTP_API.md). No pip packages required.

## Against local `valved`

```bash
# terminal 1
go run ./cmd/valved

# terminal 2
cd examples/python-client
python3 valve_client.py
# or: VALVE_URL=http://127.0.0.1:8080 python3 valve_client.py
```

## Library usage

```python
from valve_client import ValveClient

client = ValveClient("http://127.0.0.1:8080")
d = client.check(
    "org_123",
    "gpt-4o",
    requests_per_minute=60,
    tokens_per_minute=90_000,
    tokens=1200,
)
if not d.allowed:
    # Map to HTTP 429 in your gateway; include Retry-After from retry_after_ms.
    raise SystemExit(f"rate limited: {d.limit_type}")

# After upstream usage is known:
client.settle(d.reservation_id, actual_tokens=980)
# Or on hard failure before upstream:
# client.refund(d.reservation_id)
```

Note: `valved` returns **HTTP 200** with `"allowed": false` on deny. Your edge/gateway should translate that to **429** for clients.

## Tests

```bash
cd examples/python-client
python3 -m unittest test_client.py -v
```
