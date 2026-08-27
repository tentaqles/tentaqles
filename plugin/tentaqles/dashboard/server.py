"""HTTP + SSE server for the Tentaqles dashboard.

Stdlib-only. Uses ``ThreadingHTTPServer`` so snapshot requests and long-lived
SSE streams can be served concurrently. Works on Windows.
"""

from __future__ import annotations

import json
import queue
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from tentaqles.dashboard.html import DASHBOARD_HTML
from tentaqles.dashboard.snapshot import get_dashboard_snapshot
from tentaqles.dashboard.sse import get_broker

# Workspace roots used by /api/snapshot. The CLI entry point overwrites this
# before calling run_server().
_WORKSPACE_ROOTS: list[str] = []
_WORKSPACE_LOCK = threading.Lock()


def set_workspace_roots(roots: list[str]) -> None:
    global _WORKSPACE_ROOTS
    with _WORKSPACE_LOCK:
        _WORKSPACE_ROOTS = list(roots or [])


def get_workspace_roots() -> list[str]:
    with _WORKSPACE_LOCK:
        return list(_WORKSPACE_ROOTS)


def _json_bytes(obj: Any) -> bytes:
    return json.dumps(obj, ensure_ascii=False, default=str).encode("utf-8")


class _DashboardHandler(BaseHTTPRequestHandler):
    # Silence the default stderr access log
    def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
        return

    def _send(self, status: int, content_type: str, body: bytes,
              extra_headers: dict[str, str] | None = None) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        if extra_headers:
            for k, v in extra_headers.items():
                self.send_header(k, v)
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass

    # --- GET routing ---

    def do_GET(self) -> None:  # noqa: N802
        path = (self.path or "/").split("?", 1)[0]
        if path == "/" or path == "/index.html":
            self._serve_index()
        elif path == "/api/health":
            self._serve_health()
        elif path == "/api/snapshot":
            self._serve_snapshot()
        elif path == "/api/events":
            self._serve_events()
        else:
            self._send(404, "application/json",
                       _json_bytes({"error": "not found", "path": path}))

    # --- POST routing ---

    def do_POST(self) -> None:  # noqa: N802
        path = (self.path or "/").split("?", 1)[0]
        if path == "/api/capture":
            self._serve_capture()
        else:
            self._send(404, "application/json",
                       _json_bytes({"error": "not found", "path": path}))

    # --- handlers ---

    def _serve_index(self) -> None:
        self._send(200, "text/html; charset=utf-8", DASHBOARD_HTML.encode("utf-8"))

    def _serve_health(self) -> None:
        self._send(200, "application/json",
                   _json_bytes({"status": "ok", "version": "0.2"}))

    def _serve_snapshot(self) -> None:
        try:
            snap = get_dashboard_snapshot(get_workspace_roots())
        except Exception as exc:
            self._send(500, "application/json",
                       _json_bytes({"error": "snapshot failed", "detail": str(exc)}))
            return
        self._send(200, "application/json", _json_bytes(snap))

    def _serve_capture(self) -> None:
        length = int(self.headers.get("Content-Length") or 0)
        raw = b""
        if length > 0:
            try:
                raw = self.rfile.read(length)
            except Exception:
                raw = b""
        try:
            payload = json.loads(raw.decode("utf-8")) if raw else {}
        except Exception:
            payload = {}
        # Stub: acknowledge and publish as an event so live clients see it.
        try:
            get_broker().publish({"type": "capture", "payload": payload})
        except Exception:
            pass
        self._send(200, "application/json",
                   _json_bytes({"status": "ok", "received": bool(payload)}))

    def _serve_events(self) -> None:
        # Manually write SSE headers — we stream the body.
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.send_header("X-Accel-Buffering", "no")
        self.end_headers()

        broker = get_broker()
        q = broker.subscribe()
        last_heartbeat = time.monotonic()

        try:
            # Send an initial snapshot so new clients render immediately.
            try:
                snap = get_dashboard_snapshot(get_workspace_roots())
                self.wfile.write(
                    b"data: " + _json_bytes(snap) + b"\n\n"
                )
                self.wfile.flush()
            except Exception:
                pass

            while True:
                try:
                    event = q.get(timeout=5.0)
                except queue.Empty:
                    event = None

                now = time.monotonic()
                if event is not None:
                    try:
                        self.wfile.write(b"data: " + _json_bytes(event) + b"\n\n")
                        self.wfile.flush()
                    except (BrokenPipeError, ConnectionResetError, OSError):
                        break
                    if isinstance(event, dict) and event.get("type") == "shutdown":
                        break

                if now - last_heartbeat >= 30.0:
                    try:
                        self.wfile.write(b": heartbeat\n\n")
                        self.wfile.flush()
                    except (BrokenPipeError, ConnectionResetError, OSError):
                        break
                    last_heartbeat = now
        finally:
            broker.unsubscribe(q)


def _try_bind(host: str, port: int) -> ThreadingHTTPServer | None:
    try:
        return ThreadingHTTPServer((host, port), _DashboardHandler)
    except OSError:
        return None


def run_server(
    host: str = "127.0.0.1",
    port: int = 8765,
    port_fallbacks: tuple[int, ...] = (8766, 8767, 8768, 8769, 8770),
) -> None:
    """Start the dashboard server and block until interrupted.

    Tries ``port`` first, then each of ``port_fallbacks``. Emits a shutdown
    event to all SSE clients on KeyboardInterrupt.
    """
    httpd: ThreadingHTTPServer | None = _try_bind(host, port)
    chosen = port
    if httpd is None:
        for fb in port_fallbacks:
            httpd = _try_bind(host, fb)
            if httpd is not None:
                chosen = fb
                break
    if httpd is None:
        raise OSError(
            f"could not bind to any port in {(port,) + tuple(port_fallbacks)}"
        )

    print(f"Tentaqles dashboard running at http://{host}:{chosen}", flush=True)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down dashboard...", flush=True)
    finally:
        try:
            get_broker().publish({"type": "shutdown"})
        except Exception:
            pass
        try:
            httpd.shutdown()
        except Exception:
            pass
        try:
            httpd.server_close()
        except Exception:
            pass
