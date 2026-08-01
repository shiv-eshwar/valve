# Sticky routing for hot org keys

valve keys RPM/TPM buckets with Redis Cluster hash tags:

```text
rl:{{{subject}}}:{model}:rpm
rl:{{{subject}}}:{model}:tpm
```

All keys for one subject hash to the **same slot**. A huge tenant (one org) therefore concentrates load on one Redis shard — that is expected, not a bug.

## When to sticky-route

With **fast path** (`WithFastPath`), each `valved` / library process borrows a local lease chunk from Redis, then serves many Checks in-process. Sticky ingress (same subject → same pod) improves:

- Lease hit ratio (`valve_lease_hit_ratio`)
- Redis Borrow/Return chatter for that subject

Without sticky routing, correctness still holds: every Borrow debits Redis before local spend. You pay more Redis RTT and may hold more unused lease credit across pods.

## Overshoot bound

Unused in-flight lease credits are bounded by:

```text
≤ num_pods × chunk_size
```

(RPM and TPM chunks separately.) See [README SLOs](../README.md#latency-targets-slos) and `WHAT_THIS_IS.md`. Call `Limiter.Close` on shutdown to return leftovers.

## Practical guidance

| Situation | Recommendation |
| --- | --- |
| Few huge orgs | Hash `subject` (or org ID) at the load balancer / service mesh to one `valved` replica |
| Many small tenants | Round-robin is fine; Redis slot skew is rare |
| No fast path | Sticky routing is less important; every Check already hits Redis/Lua |

This doc does **not** ship a router. Wire sticky affinity in your ingress (NGINX `hash`, Envoy consistent hash, K8s session affinity, etc.) using the same subject you pass to Check.
