"""Demo workspace generator for Tentaqles.

Creates two mock client workspaces with sample code, docs, and manifests
to showcase workspace detection, identity isolation, and preflight checks.
"""
from __future__ import annotations

from pathlib import Path
from typing import Union

# ---------------------------------------------------------------------------
# All demo file contents keyed by relative path inside the demo root.
# ---------------------------------------------------------------------------

DEMO_FILES: dict[str, str] = {
    # ======================================================================
    # CLIENT ALPHA — Acme Corp  (Azure + PostgreSQL + GitHub)
    # ======================================================================
    "acme-corp/.tentaqles.yaml": """\
schema: tentaqles-client-v1
client: acme-corp
display_name: "Acme Corp"
language: en

cloud:
  provider: azure
  subscription_name: "Acme Dev"
  subscription_id: "demo-sub-001"
  preflight: "az account show --query name -o tsv"
  expected: "Acme Dev"

database:
  provider: postgresql
  dialect: postgresql
  host: azure
  access: mcp
  mcp_server: postgres

git:
  provider: github
  email: "dev@acmecorp.io"
  user: acme-dev
  host: github
  preflight: "gh auth status"
  expected_user: acme-dev

project_management:
  provider: asana
  workspace: acme

stack: [python, flask, postgresql, pandas]
""",
    # -- webapp ---------------------------------------------------------------
    "acme-corp/webapp/app.py": '''\
"""Acme Corp — Flask application factory.

Centralises extension initialisation so tests can create isolated app
instances without leaking state between runs.
"""
from flask import Flask

from .config import Config
from .routes import bp as main_bp


def create_app(config_class: type = Config) -> Flask:
    """Build and configure the Flask application."""
    app = Flask(__name__)
    app.config.from_object(config_class)

    # Register blueprints
    app.register_blueprint(main_bp)

    # Deferred import keeps the models module from importing before the app
    # context exists — avoids circular-import issues with SQLAlchemy.
    with app.app_context():
        from . import models  # noqa: F401 — triggers table creation

    return app


if __name__ == "__main__":
    create_app().run(debug=True)
''',
    "acme-corp/webapp/config.py": '''\
"""Acme Corp — Application configuration.

Environment variables are the single source of truth for secrets; the
defaults here are safe for local development only.
"""
import os


class Config:
    """Base configuration shared across all environments."""

    SECRET_KEY = os.environ.get("SECRET_KEY", "dev-only-change-me")
    SQLALCHEMY_DATABASE_URI = os.environ.get(
        "DATABASE_URL",
        "postgresql://localhost:5432/acme_dev",
    )
    SQLALCHEMY_TRACK_MODIFICATIONS = False
    AUTH_TOKEN_TTL_SECONDS = int(os.environ.get("AUTH_TOKEN_TTL", "3600"))


class TestConfig(Config):
    """Overrides for the test suite — uses an in-memory SQLite DB."""

    TESTING = True
    SQLALCHEMY_DATABASE_URI = "sqlite:///:memory:"
''',
    "acme-corp/webapp/models.py": '''\
"""Acme Corp — SQLAlchemy models.

Each model owns its own validation logic so that business rules stay
close to the data they protect.
"""
from datetime import datetime, timezone
from dataclasses import dataclass, field


@dataclass
class User:
    """Application user with hashed credentials."""

    id: int = 0
    username: str = ""
    email: str = ""
    password_hash: str = ""
    created_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    is_active: bool = True

    def check_password(self, raw: str) -> bool:
        """Verify *raw* against the stored hash (placeholder)."""
        # Real implementation would use bcrypt / argon2.
        return self.password_hash == raw

    def __repr__(self) -> str:
        return f"<User {self.username!r}>"


@dataclass
class AuditLog:
    """Immutable record of a security-relevant event."""

    id: int = 0
    user_id: int = 0
    action: str = ""
    detail: str = ""
    timestamp: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
''',
    "acme-corp/webapp/auth.py": '''\
"""Acme Corp — Authentication helpers.

Kept separate from routes so the auth logic can be unit-tested without
spinning up a full request context.
"""
from __future__ import annotations

import hashlib
import hmac
import secrets
from datetime import datetime, timezone

from .models import User, AuditLog

_TOKEN_STORE: dict[str, dict] = {}  # In-memory store for demo purposes


def hash_password(raw: str) -> str:
    """Return a salted SHA-256 hash (demo only — use bcrypt in prod)."""
    salt = secrets.token_hex(8)
    digest = hashlib.sha256(f"{salt}${raw}".encode()).hexdigest()
    return f"{salt}${digest}"


def verify_password(raw: str, stored: str) -> bool:
    """Constant-time comparison to prevent timing attacks."""
    salt, expected = stored.split("$", 1)
    candidate = hashlib.sha256(f"{salt}${raw}".encode()).hexdigest()
    return hmac.compare_digest(candidate, expected)


def issue_token(user: User) -> str:
    """Create a bearer token and store it in the in-memory registry."""
    token = secrets.token_urlsafe(32)
    _TOKEN_STORE[token] = {
        "user_id": user.id,
        "issued_at": datetime.now(timezone.utc).isoformat(),
    }
    return token


def validate_token(token: str) -> dict | None:
    """Return the token payload or None if expired / missing."""
    return _TOKEN_STORE.get(token)


def log_event(user: User, action: str, detail: str = "") -> AuditLog:
    """Create an audit trail entry for a security event."""
    return AuditLog(user_id=user.id, action=action, detail=detail)
''',
    "acme-corp/webapp/routes.py": '''\
"""Acme Corp — HTTP route definitions.

All routes live in a single Blueprint so the app factory can register
them without importing the entire module tree eagerly.
"""
from flask import Blueprint, jsonify, request

from .auth import issue_token, validate_token, verify_password, hash_password
from .models import User

bp = Blueprint("main", __name__)

# Demo user registry — replaced by a real DB in production.
_USERS: dict[str, User] = {}


@bp.route("/health")
def health():
    """Liveness probe for the container orchestrator."""
    return jsonify({"status": "ok"})


@bp.route("/register", methods=["POST"])
def register():
    """Create a new user account."""
    data = request.get_json(force=True)
    username = data.get("username", "")
    if username in _USERS:
        return jsonify({"error": "username taken"}), 409
    user = User(
        id=len(_USERS) + 1,
        username=username,
        email=data.get("email", ""),
        password_hash=hash_password(data.get("password", "")),
    )
    _USERS[username] = user
    return jsonify({"id": user.id, "username": user.username}), 201


@bp.route("/login", methods=["POST"])
def login():
    """Authenticate and return a bearer token."""
    data = request.get_json(force=True)
    user = _USERS.get(data.get("username", ""))
    if not user or not verify_password(data.get("password", ""), user.password_hash):
        return jsonify({"error": "invalid credentials"}), 401
    token = issue_token(user)
    return jsonify({"token": token})
''',
    "acme-corp/webapp/tests/__init__.py": "",
    "acme-corp/webapp/tests/test_auth.py": '''\
"""Unit tests for the authentication module.

Uses the TestConfig so no real database connection is required.
"""
import unittest

from ..auth import hash_password, verify_password, issue_token, validate_token
from ..models import User


class TestPasswordHashing(unittest.TestCase):
    """Verify salted hashing round-trips correctly."""

    def test_hash_and_verify(self):
        raw = "s3cur3-p@ss"
        hashed = hash_password(raw)
        self.assertTrue(verify_password(raw, hashed))

    def test_wrong_password_fails(self):
        hashed = hash_password("correct")
        self.assertFalse(verify_password("wrong", hashed))


class TestTokenLifecycle(unittest.TestCase):
    """Verify token issuance and validation."""

    def test_issue_and_validate(self):
        user = User(id=42, username="tester", email="t@acme.io")
        token = issue_token(user)
        payload = validate_token(token)
        self.assertIsNotNone(payload)
        self.assertEqual(payload["user_id"], 42)

    def test_invalid_token_returns_none(self):
        self.assertIsNone(validate_token("bogus-token"))


if __name__ == "__main__":
    unittest.main()
''',
    "acme-corp/webapp/requirements.txt": """\
flask>=3.0
gunicorn>=21.2
psycopg2-binary>=2.9
python-dotenv>=1.0
""",
    "acme-corp/webapp/README.md": """\
# Acme Corp Web Application

Flask-based web application with PostgreSQL backend, deployed on Azure.

## Quick start

```bash
pip install -r requirements.txt
flask run
```

## Architecture

- **app.py** — Application factory pattern
- **config.py** — Environment-based configuration
- **models.py** — Data models (User, AuditLog)
- **auth.py** — Authentication helpers (password hashing, token management)
- **routes.py** — HTTP endpoints (health, register, login)
""",
    # -- data-pipeline --------------------------------------------------------
    "acme-corp/data-pipeline/pipeline.py": '''\
"""Acme Corp — ETL pipeline orchestrator.

Reads configuration, applies transforms in declared order, and writes
results to the configured sink.  Each stage is idempotent so a partial
failure can be safely retried.
"""
from __future__ import annotations

import json
from pathlib import Path

from .transforms import clean_nulls, normalise_dates, deduplicate


def load_config(path: str | Path = "config.yaml") -> dict:
    """Load pipeline configuration from YAML (stubbed as JSON for demo)."""
    # Real code would use pyyaml; keeping deps minimal here.
    p = Path(path)
    if p.suffix in (".yaml", ".yml"):
        return {"source": "azure_blob", "sink": "postgresql", "stages": ["clean_nulls", "normalise_dates", "deduplicate"]}
    with open(p) as f:
        return json.load(f)


STAGE_REGISTRY = {
    "clean_nulls": clean_nulls,
    "normalise_dates": normalise_dates,
    "deduplicate": deduplicate,
}


def run_pipeline(config: dict, data: list[dict]) -> list[dict]:
    """Execute the declared transform stages sequentially."""
    for stage_name in config.get("stages", []):
        fn = STAGE_REGISTRY.get(stage_name)
        if fn is None:
            raise ValueError(f"Unknown stage: {stage_name}")
        data = fn(data)
    return data


if __name__ == "__main__":
    cfg = load_config()
    sample = [{"id": 1, "date": "2024/01/15", "value": None}]
    result = run_pipeline(cfg, sample)
    print(json.dumps(result, indent=2, default=str))
''',
    "acme-corp/data-pipeline/transforms.py": '''\
"""Acme Corp — Reusable data transforms.

Each function takes a list of row dicts and returns a new list.
Pure functions make testing and composition straightforward.
"""
from __future__ import annotations

from datetime import datetime


def clean_nulls(rows: list[dict]) -> list[dict]:
    """Replace None values with sensible defaults per column type."""
    cleaned = []
    for row in rows:
        cleaned.append({k: (v if v is not None else "") for k, v in row.items()})
    return cleaned


def normalise_dates(rows: list[dict], fmt: str = "%Y-%m-%d") -> list[dict]:
    """Coerce date-like strings into ISO-8601 format."""
    out = []
    for row in rows:
        new = dict(row)
        for key, val in row.items():
            if "date" in key.lower() and isinstance(val, str) and val:
                for candidate in ("%Y/%m/%d", "%d-%m-%Y", "%m/%d/%Y"):
                    try:
                        new[key] = datetime.strptime(val, candidate).strftime(fmt)
                        break
                    except ValueError:
                        continue
        out.append(new)
    return out


def deduplicate(rows: list[dict], key: str = "id") -> list[dict]:
    """Remove duplicate rows based on a key column."""
    seen: set = set()
    unique = []
    for row in rows:
        k = row.get(key)
        if k not in seen:
            seen.add(k)
            unique.append(row)
    return unique
''',
    "acme-corp/data-pipeline/__init__.py": "",
    "acme-corp/data-pipeline/config.yaml": """\
# Acme Corp ETL pipeline configuration
source:
  type: azure_blob
  container: raw-data
  path: "ingress/"

sink:
  type: postgresql
  table: processed_events
  mode: upsert

stages:
  - clean_nulls
  - normalise_dates
  - deduplicate

schedule:
  cron: "0 2 * * *"  # daily at 02:00 UTC
""",
    "acme-corp/data-pipeline/README.md": """\
# Acme Corp Data Pipeline

ETL pipeline that reads from Azure Blob Storage, applies transforms, and writes to PostgreSQL.

## Stages

1. **clean_nulls** — Replace None values with sensible defaults
2. **normalise_dates** — Coerce date strings to ISO-8601
3. **deduplicate** — Remove rows with duplicate IDs

## Configuration

See `config.yaml` for source, sink, and stage ordering.
""",
    # -- docs -----------------------------------------------------------------
    "acme-corp/docs/architecture.md": """\
# Acme Corp — Architecture Overview

## System Context

The Acme platform consists of two main subsystems:

1. **Web Application** — Flask-based API serving the customer portal
2. **Data Pipeline** — Nightly ETL feeding analytics dashboards

Both share a PostgreSQL database hosted on Azure Database for PostgreSQL.

## Deployment

- Azure App Service (webapp)
- Azure Container Instances (pipeline)
- Azure Blob Storage (raw data landing zone)
- GitHub Actions for CI/CD

## Security

- All secrets in Azure Key Vault
- JWT-based authentication for the API
- Row-level security in PostgreSQL for multi-tenant isolation
""",
    "acme-corp/docs/decisions.md": """\
# Acme Corp — Architecture Decision Records

## ADR-001: Use Flask over Django

**Status:** Accepted
**Date:** 2024-01-10

**Context:** We need a lightweight web framework for a small API surface.
**Decision:** Use Flask with Blueprints for modularity.
**Consequences:** We manage our own ORM setup and auth layer, but gain simplicity and faster startup.

---

## ADR-002: PostgreSQL as the single data store

**Status:** Accepted
**Date:** 2024-01-12

**Context:** Analytics and transactional data currently live in separate stores.
**Decision:** Consolidate into PostgreSQL with schema separation (public / analytics).
**Consequences:** Simplified ops, but requires careful query tuning for analytical workloads.

---

## ADR-003: Nightly batch ETL over streaming

**Status:** Accepted
**Date:** 2024-02-01

**Context:** Data freshness requirements are T+1; streaming adds operational complexity.
**Decision:** Run a nightly batch pipeline via Azure Container Instances.
**Consequences:** Lower cost and simpler monitoring, but no real-time dashboards.
""",
    # ======================================================================
    # CLIENT BETA — Globex Inc  (AWS + Snowflake + GitLab)
    # ======================================================================
    "globex-inc/.tentaqles.yaml": """\
schema: tentaqles-client-v1
client: globex-inc
display_name: "Globex Inc"
language: en

cloud:
  provider: aws
  account_alias: "globex-dev"
  account_id: "demo-acct-002"
  region: us-east-1
  preflight: "aws sts get-caller-identity --query Account --output text"
  expected: "demo-acct-002"

database:
  provider: snowflake
  dialect: snowflake
  host: aws
  access: mcp
  mcp_server: snowflake

git:
  provider: gitlab
  email: "eng@globexinc.com"
  user: globex-eng
  host: gitlab
  preflight: "glab auth status"
  expected_user: globex-eng

project_management:
  provider: jira
  workspace: globex

stack: [python, fastapi, snowflake, aws-lambda]
""",
    # -- api-service ----------------------------------------------------------
    "globex-inc/api-service/main.py": '''\
"""Globex Inc — FastAPI application entry point.

Wires up routers, middleware, and startup/shutdown hooks.
The app is designed to run behind an AWS ALB with Lambda adapter.
"""
from fastapi import FastAPI

from .endpoints import router
from .database import init_pool, close_pool

app = FastAPI(
    title="Globex API",
    version="0.2.0",
    docs_url="/docs",
)

app.include_router(router, prefix="/api/v1")


@app.on_event("startup")
async def on_startup():
    """Warm the connection pool on first request in Lambda."""
    await init_pool()


@app.on_event("shutdown")
async def on_shutdown():
    await close_pool()


@app.get("/health")
async def health():
    """Shallow health check for the load balancer."""
    return {"status": "ok"}
''',
    "globex-inc/api-service/endpoints.py": '''\
"""Globex Inc — API route definitions.

Each endpoint validates input via Pydantic schemas, delegates to a
service layer, and returns structured responses.
"""
from __future__ import annotations

from fastapi import APIRouter, HTTPException

from .schemas import ProjectCreate, ProjectResponse, QueryRequest, QueryResponse
from .database import execute_query

router = APIRouter(tags=["projects"])

# In-memory store for demo purposes.
_PROJECTS: dict[int, dict] = {}
_NEXT_ID = 1


@router.post("/projects", response_model=ProjectResponse, status_code=201)
async def create_project(body: ProjectCreate):
    """Create a new analytics project."""
    global _NEXT_ID
    project = {"id": _NEXT_ID, "name": body.name, "owner": body.owner, "status": "active"}
    _PROJECTS[_NEXT_ID] = project
    _NEXT_ID += 1
    return project


@router.get("/projects/{project_id}", response_model=ProjectResponse)
async def get_project(project_id: int):
    """Retrieve a project by ID."""
    proj = _PROJECTS.get(project_id)
    if not proj:
        raise HTTPException(status_code=404, detail="Project not found")
    return proj


@router.post("/query", response_model=QueryResponse)
async def run_query(body: QueryRequest):
    """Execute a read-only SQL query against Snowflake."""
    rows = await execute_query(body.sql)
    return QueryResponse(columns=list(rows[0].keys()) if rows else [], rows=rows)
''',
    "globex-inc/api-service/schemas.py": '''\
"""Globex Inc — Pydantic request / response models.

Keeping schemas in a dedicated module lets endpoints, tests, and
documentation generators share a single source of truth for the API
contract.
"""
from __future__ import annotations

from pydantic import BaseModel, Field


class ProjectCreate(BaseModel):
    """Payload for creating a new project."""

    name: str = Field(..., min_length=1, max_length=120)
    owner: str = Field(..., min_length=1, max_length=80)


class ProjectResponse(BaseModel):
    """Serialised representation of a project."""

    id: int
    name: str
    owner: str
    status: str


class QueryRequest(BaseModel):
    """Payload for executing a read-only SQL query."""

    sql: str = Field(..., min_length=1, description="Read-only SQL statement")


class QueryResponse(BaseModel):
    """Result set from a SQL query execution."""

    columns: list[str]
    rows: list[dict]
''',
    "globex-inc/api-service/database.py": '''\
"""Globex Inc — Database connection pool.

Abstracts Snowflake connectivity behind async helpers so endpoints
never import driver-specific code directly.
"""
from __future__ import annotations

import asyncio
from typing import Any

# Connection pool placeholder — real impl uses snowflake-connector-python.
_pool: dict[str, Any] | None = None


async def init_pool() -> None:
    """Initialise the Snowflake connection pool."""
    global _pool
    # In production this would create a snowflake.connector pool.
    _pool = {"status": "connected", "warehouse": "COMPUTE_WH"}


async def close_pool() -> None:
    """Gracefully drain and close all connections."""
    global _pool
    _pool = None


async def execute_query(sql: str) -> list[dict]:
    """Run a read-only query and return rows as dicts.

    In the demo this returns stub data; the real implementation
    would bind to Snowflake and enforce a read-only role.
    """
    if _pool is None:
        await init_pool()
    # Stub response for demo purposes.
    await asyncio.sleep(0)  # yield control to simulate I/O
    return [{"demo_col": "demo_value", "query": sql[:50]}]
''',
    "globex-inc/api-service/auth.py": '''\
"""Globex Inc — JWT authentication middleware.

Verifies tokens issued by the corporate IdP (AWS Cognito) and exposes
the current user to downstream handlers via request state.
"""
from __future__ import annotations

import hashlib
import hmac
import secrets
from datetime import datetime, timezone
from typing import Any

# Simulated JWKS cache — real implementation fetches from Cognito.
_JWKS_CACHE: dict[str, Any] = {}
_TOKEN_STORE: dict[str, dict] = {}


def create_token(user_id: str, roles: list[str] | None = None) -> str:
    """Issue a demo bearer token tied to *user_id*."""
    token = secrets.token_urlsafe(32)
    _TOKEN_STORE[token] = {
        "sub": user_id,
        "roles": roles or ["viewer"],
        "iat": datetime.now(timezone.utc).isoformat(),
    }
    return token


def decode_token(token: str) -> dict | None:
    """Return the token claims or None if invalid / expired."""
    return _TOKEN_STORE.get(token)


def require_role(claims: dict, role: str) -> bool:
    """Check whether the authenticated user holds *role*."""
    return role in claims.get("roles", [])
''',
    "globex-inc/api-service/__init__.py": "",
    "globex-inc/api-service/requirements.txt": """\
fastapi>=0.110
uvicorn[standard]>=0.27
pydantic>=2.6
snowflake-connector-python>=3.6
mangum>=0.17  # AWS Lambda adapter
""",
    "globex-inc/api-service/README.md": """\
# Globex Inc API Service

FastAPI-based analytics API backed by Snowflake, deployed on AWS Lambda via Mangum.

## Quick start

```bash
pip install -r requirements.txt
uvicorn main:app --reload
```

## Endpoints

- `POST /api/v1/projects` — Create a project
- `GET  /api/v1/projects/{id}` — Get a project
- `POST /api/v1/query` — Execute a read-only SQL query
- `GET  /health` — Load balancer health check
""",
    # -- docs -----------------------------------------------------------------
    "globex-inc/docs/api-spec.md": """\
# Globex Inc — API Specification

## Base URL

`https://api.globexinc.com/api/v1`

## Authentication

All endpoints require a Bearer token issued by AWS Cognito.

## Endpoints

### POST /projects
Create a new analytics project.

**Request body:**
```json
{ "name": "Q1 Dashboard", "owner": "data-team" }
```

**Response (201):**
```json
{ "id": 1, "name": "Q1 Dashboard", "owner": "data-team", "status": "active" }
```

### GET /projects/{id}
Retrieve a project by its numeric ID.

### POST /query
Execute a read-only SQL query against the Snowflake warehouse.

**Request body:**
```json
{ "sql": "SELECT COUNT(*) FROM events WHERE date >= '2024-01-01'" }
```
""",
    "globex-inc/docs/migration-plan.md": """\
# Globex Inc — Snowflake Migration Plan

## Objective

Migrate analytics workloads from Redshift to Snowflake by Q3 2024.

## Phases

### Phase 1 — Schema mapping (Weeks 1-2)
- Catalogue all Redshift tables and views
- Map data types to Snowflake equivalents
- Identify UDFs that need rewriting

### Phase 2 — Data transfer (Weeks 3-4)
- Set up Snowpipe for incremental loads
- Bulk-copy historical data via S3 staging
- Validate row counts and checksums

### Phase 3 — Application cutover (Weeks 5-6)
- Update connection strings in API service
- Run parallel reads against both warehouses
- Switch primary reads to Snowflake

### Phase 4 — Decommission Redshift (Week 7)
- Redirect remaining dashboards
- Archive Redshift snapshots to S3 Glacier
- Terminate Redshift cluster

## Rollback

At any phase, we can revert by restoring the Redshift cluster from
the latest snapshot and switching connection strings back.
""",
}


def create_demo(base_path: str | Path) -> str:
    """Create demo workspaces under *base_path*/tentaqles-demo/.

    Returns a human-readable summary of what was created.
    """
    root = Path(base_path).resolve() / "tentaqles-demo"
    root.mkdir(parents=True, exist_ok=True)

    created: list[str] = []
    for rel_path, content in DEMO_FILES.items():
        target = root / rel_path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
        created.append(rel_path)

    summary_lines = [
        f"Tentaqles demo created at: {root}",
        f"  Total files: {len(created)}",
        "",
        "  Acme Corp  (Azure / PostgreSQL / GitHub)",
        "    - webapp/        Flask app with auth, routes, models, tests",
        "    - data-pipeline/ ETL pipeline with transforms",
        "    - docs/          Architecture overview + 3 ADRs",
        "",
        "  Globex Inc (AWS / Snowflake / GitLab)",
        "    - api-service/   FastAPI app with endpoints, schemas, auth",
        "    - docs/          API spec + migration plan",
        "",
        "Next steps:",
        "  cd into a client directory and run /tentaqles:workspace-status",
    ]
    return "\n".join(summary_lines)
