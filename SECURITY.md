# Security Policy

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security bugs.

Prefer:

1. GitHub **Security Advisories** on [shiv-eshwar/valve](https://github.com/shiv-eshwar/valve) (private report), or
2. Email the maintainers listed on the GitHub org/repo (if configured).

Include: valve version/tag, deployment shape (library vs `valved`), Redis/fail-mode config, and a minimal repro.

We aim to acknowledge reports within **72 hours**.

## Operational guidance

### Fail-closed vs fail-open

- **Default: fail-closed** — store errors deny with `limit_type=backend`. Prefer this for abuse and cost control.
- **Fail-open** — with fast path, allows only if a **local lease** still has credit; otherwise denies. Never invent unlimited traffic when Redis is down unless you explicitly accept that risk.

### Secrets and PII

- Deny logs hash subjects (`pkg/logx`); do not log raw API keys or `Authorization` headers.
- Treat rate-limit keys (org IDs, API key hashes) as sensitive identifiers in your own logging.

### Trust boundaries

- valve trusts the **subject** and **limits** supplied by the caller (gateway/auth). Always authenticate before Check.
- The sidecar HTTP/gRPC API should sit on a private network; it is not an auth product.

### Dependencies

Keep Go and module dependencies updated. Report supply-chain issues the same way as code vulns.
