"""Thin stdlib client for the valved HTTP API (docs/HTTP_API.md)."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Optional


@dataclass
class Decision:
    allowed: bool
    limit_type: str
    remaining_rpm: int
    remaining_tpm: int
    limit_rpm: int
    limit_tpm: int
    retry_after_ms: int
    reservation_id: str
    overshoot_tpm: int
    reset_rpm: str
    reset_tpm: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Decision":
        return cls(
            allowed=bool(data.get("allowed", False)),
            limit_type=str(data.get("limit_type") or ""),
            remaining_rpm=int(data.get("remaining_rpm") or 0),
            remaining_tpm=int(data.get("remaining_tpm") or 0),
            limit_rpm=int(data.get("limit_rpm") or 0),
            limit_tpm=int(data.get("limit_tpm") or 0),
            retry_after_ms=int(data.get("retry_after_ms") or 0),
            reservation_id=str(data.get("reservation_id") or ""),
            overshoot_tpm=int(data.get("overshoot_tpm") or 0),
            reset_rpm=str(data.get("reset_rpm") or ""),
            reset_tpm=str(data.get("reset_tpm") or ""),
        )


class ValveError(Exception):
    """Non-2xx or transport failure talking to valved."""

    def __init__(self, message: str, status: Optional[int] = None, body: str = ""):
        super().__init__(message)
        self.status = status
        self.body = body


class ValveClient:
    def __init__(self, base_url: str = "http://localhost:8080", timeout: float = 5.0):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout

    def _post(self, path: str, payload: dict[str, Any]) -> Any:
        data = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(
            f"{self.base_url}{path}",
            data=data,
            headers={"Content-Type": "application/json", "Accept": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read().decode("utf-8")
                if not raw:
                    return {}
                return json.loads(raw)
        except urllib.error.HTTPError as e:
            body = e.read().decode("utf-8", errors="replace")
            raise ValveError(f"HTTP {e.code} for {path}", status=e.code, body=body) from e
        except urllib.error.URLError as e:
            raise ValveError(f"request failed for {path}: {e}") from e

    def check(
        self,
        subject: str,
        model: str,
        *,
        requests_per_minute: int,
        tokens_per_minute: int,
        requests: int = 1,
        tokens: int,
    ) -> Decision:
        body = {
            "key": {"subject": subject, "model": model},
            "limits": {
                "requests_per_minute": requests_per_minute,
                "tokens_per_minute": tokens_per_minute,
            },
            "cost": {"requests": requests, "tokens": tokens},
        }
        return Decision.from_dict(self._post("/v1/check", body))

    def settle(self, reservation_id: str, actual_tokens: int) -> Decision:
        return Decision.from_dict(
            self._post(
                "/v1/settle",
                {"reservation_id": reservation_id, "actual_tokens": actual_tokens},
            )
        )

    def refund(self, reservation_id: str) -> None:
        self._post("/v1/refund", {"reservation_id": reservation_id})


def main() -> None:
    import os
    import sys

    base = os.environ.get("VALVE_URL", "http://127.0.0.1:8080")
    client = ValveClient(base)
    d = client.check(
        "demo",
        "gpt-4o",
        requests_per_minute=60,
        tokens_per_minute=90_000,
        tokens=100,
    )
    if not d.allowed:
        # Gateway pattern: map deny → HTTP 429; valved itself returns 200 + allowed=false.
        print(
            f"denied limit_type={d.limit_type} retry_after_ms={d.retry_after_ms}",
            file=sys.stderr,
        )
        sys.exit(1)
    print(f"allowed reservation_id={d.reservation_id} remaining_tpm={d.remaining_tpm}")
    settled = client.settle(d.reservation_id, actual_tokens=80)
    print(f"settled remaining_tpm={settled.remaining_tpm}")


if __name__ == "__main__":
    main()
