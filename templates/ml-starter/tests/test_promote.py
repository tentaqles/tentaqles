"""scripts/promote.py against a fake MLflow tracking server (http.server, no network)."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

import pytest

SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "promote.py"


class FakeMlflow:
    def __init__(self, versions: dict[tuple[str, str], str] | None = None, delay: float = 0.0,
                 fail_alias: bool = False) -> None:
        self.versions = versions if versions is not None else {("churn", "3"): "READY"}
        self.aliases: dict[tuple[str, str], set[str]] = {}
        self.delay = delay
        self.fail_alias = fail_alias
        self.requests: list[dict] = []
        fake = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args: object) -> None:  # keep test output quiet
                pass

            def _reply(self, code: int, body: dict) -> None:
                raw = json.dumps(body).encode()
                self.send_response(code)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

            def do_GET(self) -> None:
                time.sleep(fake.delay)
                url = urlparse(self.path)
                fake.requests.append({"method": "GET", "path": url.path, "auth": self.headers.get("Authorization")})
                if url.path != "/api/2.0/mlflow/model-versions/get":
                    return self._reply(404, {"error_code": "ENDPOINT_NOT_FOUND"})
                q = parse_qs(url.query)
                key = (q.get("name", [""])[0], q.get("version", [""])[0])
                if key not in fake.versions:
                    return self._reply(404, {"error_code": "RESOURCE_DOES_NOT_EXIST",
                                             "message": "secret-detail-should-not-print"})
                return self._reply(200, {"model_version": {
                    "name": key[0], "version": key[1], "status": fake.versions[key],
                    "aliases": sorted(fake.aliases.get(key, set()))}})

            def do_POST(self) -> None:
                time.sleep(fake.delay)
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
                fake.requests.append({"method": "POST", "path": self.path, "body": body,
                                      "auth": self.headers.get("Authorization"),
                                      "ctype": self.headers.get("Content-Type")})
                if self.path != "/api/2.0/mlflow/registered-models/alias":
                    return self._reply(404, {"error_code": "ENDPOINT_NOT_FOUND"})
                if fake.fail_alias:
                    return self._reply(500, {"error_code": "INTERNAL_ERROR"})
                key = (body["name"], body["version"])
                for k in fake.aliases:  # an alias points at one version
                    fake.aliases[k].discard(body["alias"])
                fake.aliases.setdefault(key, set()).add(body["alias"])
                return self._reply(200, {})

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> "FakeMlflow":
        self.thread.start()
        return self

    def __exit__(self, *exc: object) -> None:
        self.server.shutdown()
        self.server.server_close()


def _run(args: list[str], **env: str) -> subprocess.CompletedProcess[str]:
    full = {k: v for k, v in os.environ.items() if not k.startswith("MLFLOW_")}
    full.update(env)
    return subprocess.run([sys.executable, str(SCRIPT), *args], capture_output=True, text=True,
                          env=full, timeout=60)


def test_sets_the_alias_and_verifies_it() -> None:
    with FakeMlflow() as fake:
        r = _run(["churn", "3", "champion"], MLFLOW_TRACKING_URI=fake.url)
    assert r.returncode == 0, r.stderr
    assert "champion" in r.stdout and "churn" in r.stdout
    assert fake.aliases[("churn", "3")] == {"champion"}
    post = [q for q in fake.requests if q["method"] == "POST"]
    assert post == [{"method": "POST", "path": "/api/2.0/mlflow/registered-models/alias",
                     "body": {"name": "churn", "alias": "champion", "version": "3"}, "auth": None,
                     "ctype": "application/json"}]
    assert [q["method"] for q in fake.requests] == ["GET", "POST", "GET"]


def test_token_is_sent_and_never_printed() -> None:
    token = "tok-" + "x" * 12  # built at runtime: the commit guard scans for secret shapes
    with FakeMlflow() as fake:
        r = _run(["churn", "3", "production"], MLFLOW_TRACKING_URI=fake.url, MLFLOW_TRACKING_TOKEN=token)
    assert r.returncode == 0, r.stderr
    assert all(q["auth"] == f"Bearer {token}" for q in fake.requests)
    assert token not in r.stdout + r.stderr


def test_a_missing_version_is_refused_without_echoing_the_server_message() -> None:
    with FakeMlflow() as fake:
        r = _run(["churn", "9", "champion"], MLFLOW_TRACKING_URI=fake.url)
    assert r.returncode == 3
    assert "RESOURCE_DOES_NOT_EXIST" in r.stderr
    assert "secret-detail-should-not-print" not in r.stdout + r.stderr
    assert not any(q["method"] == "POST" for q in fake.requests)


def test_a_version_that_is_not_ready_is_refused() -> None:
    with FakeMlflow(versions={("churn", "3"): "PENDING_REGISTRATION"}) as fake:
        r = _run(["churn", "3", "champion"], MLFLOW_TRACKING_URI=fake.url)
    assert r.returncode == 3 and "PENDING_REGISTRATION" in r.stderr
    assert not any(q["method"] == "POST" for q in fake.requests)


def test_a_registry_error_on_the_alias_call_fails() -> None:
    with FakeMlflow(fail_alias=True) as fake:
        r = _run(["churn", "3", "champion"], MLFLOW_TRACKING_URI=fake.url)
    assert r.returncode == 3 and "INTERNAL_ERROR" in r.stderr


@pytest.mark.parametrize("args", [
    ["churn;rm", "3", "champion"],
    ["churn", "3a", "champion"],
    ["churn", "3", "champ ion"],
    ["churn", "3", "latest"],
    ["churn", "3", "v4"],
    ["churn", "3"],
])
def test_bad_arguments_are_refused_before_any_call(args: list[str]) -> None:
    with FakeMlflow() as fake:
        r = _run(args, MLFLOW_TRACKING_URI=fake.url)
    assert r.returncode == 2, (args, r.stdout, r.stderr)
    assert fake.requests == []


def test_tracking_uri_is_required_and_must_be_http() -> None:
    assert _run(["churn", "3", "champion"]).returncode == 2
    r = _run(["churn", "3", "champion"], MLFLOW_TRACKING_URI="file:///tmp/mlruns")
    assert r.returncode == 2 and "http" in r.stderr


def test_a_slow_registry_times_out() -> None:
    with FakeMlflow(delay=3.0) as fake:
        start = time.monotonic()
        r = _run(["churn", "3", "champion"], MLFLOW_TRACKING_URI=fake.url, MLFLOW_HTTP_REQUEST_TIMEOUT="1")
        assert time.monotonic() - start < 20
    assert r.returncode == 3 and "timed out" in r.stderr
