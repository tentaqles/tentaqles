"""Point an MLflow registered-model alias at a version: the project's promotion command.

    python scripts/promote.py <model> <version> <target-alias>

This is packs.ml.promotion.command. It runs only from `tq pack ml promote apply`, which
the human runs after approving the request on the hub; the guard tq/model-promote denies
agents running it directly.

- Tracking server: MLFLOW_TRACKING_URI (http or https). Auth, when needed, comes from
  MLFLOW_TRACKING_TOKEN (bearer) or MLFLOW_TRACKING_USERNAME/MLFLOW_TRACKING_PASSWORD
  (basic). Credentials come from the environment only (`tq dotenv run`), are never
  printed, and this script never reads a .env file.
- Calls (MLflow REST API 2.0): GET model-versions/get (the version must be READY),
  POST registered-models/alias {name, alias, version}, then GET again to verify.
- Each request times out after MLFLOW_HTTP_REQUEST_TIMEOUT seconds (default 30, max 120).
- Exit codes: 0 done, 2 usage or configuration, 3 registry error or timeout.
"""

from __future__ import annotations

import base64
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

SAFE = re.compile(r"^[A-Za-z0-9._@-]{1,128}$")  # same charset tq fills placeholders with
ALIAS = re.compile(r"^[A-Za-z][A-Za-z0-9_-]{0,63}$")
RESERVED_ALIAS = re.compile(r"^(latest|v\d+)$", re.IGNORECASE)  # MLflow reserves these
API = "/api/2.0/mlflow"


class Usage(Exception):
    pass


class RegistryError(Exception):
    pass


def _timeout() -> float:
    raw = os.environ.get("MLFLOW_HTTP_REQUEST_TIMEOUT", "30")
    try:
        value = float(raw)
    except ValueError as exc:
        raise Usage("MLFLOW_HTTP_REQUEST_TIMEOUT must be a number of seconds") from exc
    return min(max(value, 1.0), 120.0)


def _headers() -> dict[str, str]:
    h = {"Accept": "application/json"}
    token = os.environ.get("MLFLOW_TRACKING_TOKEN")
    user, password = os.environ.get("MLFLOW_TRACKING_USERNAME"), os.environ.get("MLFLOW_TRACKING_PASSWORD")
    if token:
        h["Authorization"] = f"Bearer {token}"
    elif user and password:
        h["Authorization"] = "Basic " + base64.b64encode(f"{user}:{password}".encode()).decode()
    return h


def _base_url() -> str:
    uri = os.environ.get("MLFLOW_TRACKING_URI", "").strip().rstrip("/")
    if not uri:
        raise Usage("MLFLOW_TRACKING_URI is not set; run through `tq dotenv run -- ...`")
    if urllib.parse.urlparse(uri).scheme not in ("http", "https"):
        raise Usage("MLFLOW_TRACKING_URI must be an http or https tracking server URL")
    return uri


def _call(method: str, path: str, *, query: dict | None = None, body: dict | None = None) -> dict:
    url = _base_url() + API + path
    if query:
        url += "?" + urllib.parse.urlencode(query)
    data = None
    headers = _headers()
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=_timeout()) as resp:  # scheme checked: http/https only
            raw = resp.read(1 << 20)
    except urllib.error.HTTPError as exc:
        code = f"HTTP_{exc.code}"
        try:
            code = json.loads(exc.read(1 << 16) or b"{}").get("error_code") or code
        except ValueError:
            pass
        raise RegistryError(f"{method} {path}: {code}") from None  # never echo the server's message
    except TimeoutError:
        raise RegistryError(f"{method} {path}: timed out") from None
    except urllib.error.URLError as exc:
        if isinstance(exc.reason, TimeoutError):
            raise RegistryError(f"{method} {path}: timed out") from None
        raise RegistryError(f"{method} {path}: cannot reach the tracking server") from None
    try:
        return json.loads(raw or b"{}")
    except ValueError:
        raise RegistryError(f"{method} {path}: response is not JSON") from None


def _version(model: str, version: str) -> dict:
    return _call("GET", "/model-versions/get", query={"name": model, "version": version}).get("model_version", {})


def promote(model: str, version: str, target: str) -> None:
    mv = _version(model, version)
    status = mv.get("status", "")
    if status != "READY":
        raise RegistryError(f"{model} version {version} is {status or 'not ready'}, not READY")
    _call("POST", "/registered-models/alias", body={"name": model, "alias": target, "version": version})
    if target not in _version(model, version).get("aliases", []):
        raise RegistryError(f"alias {target} was not set on {model} version {version}")


def parse(argv: list[str]) -> tuple[str, str, str]:
    if len(argv) != 3:
        raise Usage("usage: promote.py <model> <version> <target-alias>")
    model, version, target = argv
    for name, value in (("model", model), ("version", version), ("target", target)):
        if not SAFE.match(value):
            raise Usage(f"{name} must match [A-Za-z0-9._@-]")
    if not version.isdigit():
        raise Usage("version must be a registry version number")
    if not ALIAS.match(target) or RESERVED_ALIAS.match(target):
        raise Usage("target must be an alias name (letters, digits, _ or -), not 'latest' or v<n>")
    return model, version, target


def main(argv: list[str] | None = None) -> int:
    try:
        model, version, target = parse(sys.argv[1:] if argv is None else argv)
        _base_url()
        _timeout()
        promote(model, version, target)
    except Usage as exc:
        print(f"promote: {exc}", file=sys.stderr)
        return 2
    except RegistryError as exc:
        print(f"promote: registry error: {exc}", file=sys.stderr)
        return 3
    print(f"promote: alias {target} -> {model} version {version}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
