"""unittest + http.server mock for ValveClient (stdlib only)."""

from __future__ import annotations

import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

from valve_client import Decision, ValveClient, ValveError


class _Handler(BaseHTTPRequestHandler):
    def log_message(self, format: str, *args) -> None:  # noqa: A003
        return

    def do_POST(self) -> None:  # noqa: N802
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)
        body = json.loads(raw.decode("utf-8")) if raw else {}

        if self.path == "/v1/check":
            allowed = body.get("cost", {}).get("tokens", 0) < 10_000
            payload = {
                "allowed": allowed,
                "limit_type": "" if allowed else "tokens",
                "remaining_rpm": 59,
                "remaining_tpm": 1000,
                "limit_rpm": 60,
                "limit_tpm": 90000,
                "retry_after_ms": 0 if allowed else 1500,
                "reservation_id": "res-1" if allowed else "",
                "overshoot_tpm": 0,
                "reset_rpm": "2026-08-01T00:00:00Z",
                "reset_tpm": "2026-08-01T00:00:00Z",
            }
            self._json(200, payload)
            return

        if self.path == "/v1/settle":
            if "actual_input_tokens" in body or "actual_output_tokens" in body:
                self._json(
                    200,
                    {
                        "allowed": True,
                        "limit_type": "",
                        "remaining_rpm": 59,
                        "remaining_tpm": 400,
                        "remaining_itpm": 900,
                        "remaining_otpm": 400,
                        "limit_rpm": 60,
                        "limit_tpm": 500,
                        "limit_itpm": 1000,
                        "limit_otpm": 500,
                        "retry_after_ms": 0,
                        "reservation_id": body.get("reservation_id", ""),
                        "overshoot_tpm": 0,
                        "reset_rpm": "2026-08-01T00:00:00Z",
                        "reset_tpm": "2026-08-01T00:00:00Z",
                    },
                )
                return
            self._json(
                200,
                {
                    "allowed": True,
                    "limit_type": "",
                    "remaining_rpm": 59,
                    "remaining_tpm": 920,
                    "limit_rpm": 60,
                    "limit_tpm": 90000,
                    "retry_after_ms": 0,
                    "reservation_id": body.get("reservation_id", ""),
                    "overshoot_tpm": 0,
                    "reset_rpm": "2026-08-01T00:00:00Z",
                    "reset_tpm": "2026-08-01T00:00:00Z",
                },
            )
            return

        if self.path == "/v1/refund":
            self._json(200, {})
            return

        self._json(404, {"error": "not found"})

    def _json(self, code: int, payload: dict) -> None:
        data = json.dumps(payload).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


class ValveClientTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.server = HTTPServer(("127.0.0.1", 0), _Handler)
        cls.port = cls.server.server_address[1]
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.client = ValveClient(f"http://127.0.0.1:{cls.port}")

    @classmethod
    def tearDownClass(cls) -> None:
        cls.server.shutdown()

    def test_check_settle(self) -> None:
        d = self.client.check(
            "org",
            "m",
            requests_per_minute=60,
            tokens_per_minute=90_000,
            tokens=100,
        )
        self.assertIsInstance(d, Decision)
        self.assertTrue(d.allowed)
        self.assertEqual(d.reservation_id, "res-1")
        s = self.client.settle(d.reservation_id, 80)
        self.assertEqual(s.remaining_tpm, 920)

    def test_check_deny(self) -> None:
        d = self.client.check(
            "org",
            "m",
            requests_per_minute=60,
            tokens_per_minute=90_000,
            tokens=50_000,
        )
        self.assertFalse(d.allowed)
        self.assertEqual(d.limit_type, "tokens")
        self.assertEqual(d.retry_after_ms, 1500)

    def test_refund(self) -> None:
        self.client.refund("res-1")  # no exception

    def test_settle_io(self) -> None:
        s = self.client.settle_io("res-1", 80, 40)
        self.assertEqual(s.remaining_itpm, 900)
        self.assertEqual(s.remaining_otpm, 400)

    def test_http_error(self) -> None:
        with self.assertRaises(ValveError) as ctx:
            self.client._post("/v1/missing", {})
        self.assertEqual(ctx.exception.status, 404)


if __name__ == "__main__":
    unittest.main()
