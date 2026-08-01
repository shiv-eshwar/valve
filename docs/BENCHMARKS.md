# Benchmarks

Comparative microbenchmarks (in-process memory store). Not a substitute for Redis same-AZ latency.

## How to run

```bash
go test ./pkg/limiter -bench='Benchmark(Naive|Valve)' -benchmem
```

Also available: `BenchmarkCheck_Direct` / `BenchmarkCheck_FastPath` (same valve paths, older names).

## What is compared

| Bench | Meaning |
| --- | --- |
| `BenchmarkNaiveFixedWindow` | Mutex map + per-minute request counter (RPM only; no TPM, no reservations) |
| `BenchmarkValveDirect` | valve dual-bucket Check on memory store (no fast path) |
| `BenchmarkValveFastPath` | valve Check with local lease warm (`WithFastPath`) |

Naive is a **lower-bound baseline** for single-dimension counting. valve pays for dual RPM+TPM, reservation IDs, and settle semantics — the right comparison for gateways that need those features.

## Sample results

Captured on **2026-08-01**, `darwin/arm64`, Apple M5, `go test` `-count=1`:

```text
BenchmarkNaiveFixedWindow-10    	34203828	        34.09 ns/op	       0 B/op	       0 allocs/op
BenchmarkValveDirect-10         	   23948	    124333 ns/op	     368 B/op	       3 allocs/op
BenchmarkValveFastPath-10       	   40897	    120096 ns/op	     357 B/op	       2 allocs/op
```

Re-run on your hardware before citing numbers. Redis/Lua Check targets are in the README SLOs (same-AZ p99), not these CPU benches.
