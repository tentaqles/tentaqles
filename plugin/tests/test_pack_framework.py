# plugin/tests/test_pack_framework.py
"""Pack framework, plugin side (spec C §2.8, §2.12, §6): the build gate freezes the pack
config at approval and records `tq pack check --json` results in the definition of done,
accepts any `*-reviewer` that ships an agent file, and the on-ramps carry the generic pack
step. A fake `tq` on PATH prints fixture JSON written from the `tq.packs/v1` contract."""

from __future__ import annotations

import copy
import json
import os
import re
import subprocess
import sys
from pathlib import Path

import pytest

from tentaqles.workflow import gate
from tentaqles.workflow.project import load_project_config

PLUGIN = Path(__file__).resolve().parents[1]
REPO = PLUGIN.parent
SKILLS = PLUGIN / "skills"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'
PY = sys.executable.replace("\\", "/")
PASS_CMD = f'"{PY}" -c "import sys; sys.exit(0)"'
HASH = "9f2c" + "0" * 60
OTHER_HASH = "1d4e" + "0" * 60

FAKE_TQ = r'''
import json, os, sys, time
argv = sys.argv[1:]
with open(os.environ["FAKE_TQ_LOG"], "a", encoding="utf-8") as f:
    f.write(json.dumps(argv) + "\n")
mode = os.environ.get("FAKE_TQ_MODE", "")
if argv[:2] == ["pack", "list"]:
    if mode == "old":
        sys.stderr.write('tq: unknown command "pack" for "tq"\n'); sys.exit(1)
    if mode == "list-config-error":
        sys.stderr.write("tq-packs.yaml:3: unknown key packs.data.colour\n"); sys.exit(2)
    if mode == "list-garbage":
        print("not json"); sys.exit(0)
    print(open(os.environ["FAKE_TQ_LIST"], encoding="utf-8").read()); sys.exit(0)
if argv[:2] == ["pack", "check"]:
    if mode == "crash":
        sys.stderr.write("panic: runtime error: index out of range\n"); sys.exit(3)
    if mode == "garbage":
        print("{not json"); sys.exit(0)
    if mode == "sleep":
        time.sleep(30); sys.exit(0)
    print(open(os.environ["FAKE_TQ_CHECK"], encoding="utf-8").read())
    sys.exit(int(os.environ.get("FAKE_TQ_CHECK_EXIT", "0")))
sys.stderr.write("fake tq: unsupported " + " ".join(argv) + "\n"); sys.exit(1)
'''


def finding(i: int, level: str = "error") -> dict:
    return {"file": f"models/m{i}.sql", "line": i, "level": level, "rule": "LT01", "message": f"finding {i}"}


LIST_OUT = {
    "schema": "tq.packs/v1",
    "workspace": "acme",
    "config_hash": HASH,
    "packs": [
        {"name": "data", "enabled": True, "checks": [{"id": "data/contract-breaking", "kind": "builtin", "required": True}]},
        {"name": "ml", "enabled": True, "checks": []},
    ],
}

CHECK_OUT = {
    "schema": "tq.packs/v1",
    "workspace": "acme",
    "base": "HEAD",
    "config_hash": HASH,
    "results": [
        {"id": "data/contract-breaking", "pack": "data", "status": "ok", "required": True,
         "detail": "2 contracts, no breaking change", "findings": []},
        {"id": "data/sqlfluff", "pack": "data", "status": "fail", "required": True, "detail": "6 errors",
         "findings": [finding(i) for i in range(1, 7)],
         "tool": {"name": "sqlfluff", "version": "3.2.0", "path": "/usr/bin/sqlfluff", "exit_code": 1,
                  "duration_ms": 40, "timed_out": False}},
        {"id": "ml/ruff", "pack": "ml", "status": "fail", "required": False,
         "detail": "ruff is installed but not declared in packs.ml.tools (advisory)", "findings": [finding(9)]},
        {"id": "data/dbt-parse", "pack": "data", "status": "skip", "required": False,
         "detail": "dbt not installed (not declared)", "findings": []},
    ],
    "summary": {"ok": 1, "fail": 2, "skip": 1, "required_fail": 1},
}


def green_check() -> dict:
    out = copy.deepcopy(CHECK_OUT)
    out["results"] = [r for r in out["results"] if r["id"] != "data/sqlfluff"]
    out["summary"] = {"ok": 1, "fail": 1, "skip": 1, "required_fail": 0}
    return out


# ---------------------------------------------------------------------------
# fixtures
# ---------------------------------------------------------------------------


@pytest.fixture(autouse=True)
def isolated(tmp_path, monkeypatch):
    home = tmp_path / "home"
    home.mkdir()
    for k in ("TQ_HOME", "HOME", "USERPROFILE"):
        monkeypatch.setenv(k, str(home))
    for k in ("TQ_BIN", "CLAUDE_PLUGIN_ROOT", "FAKE_TQ_MODE", "FAKE_TQ_CHECK_EXIT"):
        monkeypatch.delenv(k, raising=False)
    monkeypatch.setattr(gate, "find_tq", lambda: None)  # no tq unless a test installs the fake
    return home


class FakeTq:
    def __init__(self, tmp_path: Path, monkeypatch):
        self.dir = tmp_path / "fakebin"
        self.dir.mkdir()
        script = self.dir / "fake_tq.py"
        script.write_text(FAKE_TQ, encoding="utf-8")
        if sys.platform == "win32":
            exe = self.dir / "tq.cmd"
            exe.write_text(f'@echo off\r\n"{sys.executable}" "{script}" %*\r\n', encoding="utf-8")
        else:
            exe = self.dir / "tq"
            exe.write_text(f'#!/bin/sh\nexec "{sys.executable}" "{script}" "$@"\n', encoding="utf-8")
            exe.chmod(0o755)
        self.exe = str(exe)
        self.log = tmp_path / "tq-calls.jsonl"
        self.list_file = tmp_path / "list.json"
        self.check_file = tmp_path / "check.json"
        self.mp = monkeypatch
        monkeypatch.setenv("PATH", str(self.dir) + os.pathsep + os.environ.get("PATH", ""))
        monkeypatch.setenv("FAKE_TQ_LOG", str(self.log))
        monkeypatch.setenv("FAKE_TQ_LIST", str(self.list_file))
        monkeypatch.setenv("FAKE_TQ_CHECK", str(self.check_file))
        monkeypatch.setattr(gate, "find_tq", lambda: self.exe)
        self.lists(LIST_OUT)
        self.checks(CHECK_OUT, exit_code=1)

    def lists(self, doc) -> None:
        self.list_file.write_text(json.dumps(doc), encoding="utf-8")

    def checks(self, doc, exit_code: int = 0) -> None:
        self.check_file.write_text(json.dumps(doc), encoding="utf-8")
        self.mp.setenv("FAKE_TQ_CHECK_EXIT", str(exit_code))

    def mode(self, m: str) -> None:
        self.mp.setenv("FAKE_TQ_MODE", m)

    def calls(self) -> list[list[str]]:
        if not self.log.exists():
            return []
        return [json.loads(ln) for ln in self.log.read_text(encoding="utf-8").splitlines() if ln.strip()]


@pytest.fixture
def fake_tq(tmp_path, monkeypatch) -> FakeTq:
    return FakeTq(tmp_path, monkeypatch)


def _git(root, *args):
    subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)


def _cp(i: int) -> dict:
    g = {"status": "passed", "attempts": 0, "findings": []}
    return {"id": i, "title": f"step {i}", "status": "passed", "verify": [PASS_CMD],
            "gates": {"behavior": dict(g), "review": dict(g)}}


@pytest.fixture
def repo(tmp_path):
    root = tmp_path / "proj"
    (root / "docs" / "checkpoints").mkdir(parents=True)
    _git(root, "init", "-q", "-b", "work")
    _git(root, "config", "user.email", "tester@acme.test")
    _git(root, "config", "user.name", "tester")
    (root / "app.py").write_text("x = 1\n", encoding="utf-8")
    project_block(root)
    plan = {"feature": "Demo", "slug": "demo", "workflow": "feature", "spec": "docs/specs/demo.md",
            "checkpoints": [_cp(1)]}
    (root / "docs" / "checkpoints" / "demo.json").write_text(json.dumps(plan, indent=2), encoding="utf-8")
    _git(root, "add", "app.py", "CLAUDE.md", "docs/checkpoints/demo.json")
    _git(root, "commit", "-q", "-m", "init")
    (root / ".claude" / "build").mkdir(parents=True)
    return root


def project_block(root: Path, **extra) -> None:
    cfg = {"commands": {"test": PASS_CMD}, **extra}
    (root / "CLAUDE.md").write_text("# Demo\n\n```json tq-project\n" + json.dumps(cfg) + "\n```\n", encoding="utf-8")


def dod_items(root: Path) -> dict[str, dict]:
    rec = gate.read_json(root / ".claude" / "build" / "demo" / "dod.json")
    assert gate.verified(rec)
    return {c["id"]: c for c in rec["checks"]}


def frozen(root: Path) -> dict:
    return (gate.load_approved(root, "demo") or {}).get("packs")


# ---------------------------------------------------------------------------
# freeze at approval
# ---------------------------------------------------------------------------


def test_approve_freezes_enabled_packs_and_config_hash(repo, fake_tq):
    out = gate.cmd_approve(repo, "demo")
    assert frozen(repo) == {"enabled": ["data", "ml"], "config_hash": HASH}
    assert ["pack", "list", "--enabled", "--json"] in fake_tq.calls()
    assert "packs frozen: data, ml" in out


def test_approve_accepts_a_top_level_enabled_list(repo, fake_tq):
    fake_tq.lists({"schema": "tq.packs/v1", "config_hash": HASH, "enabled": ["db"], "packs": {"db": {"enabled": True}}})
    gate.cmd_approve(repo, "demo")
    assert frozen(repo) == {"enabled": ["db"], "config_hash": HASH}


def test_approve_without_tq_freezes_tq_missing(repo):
    gate.cmd_approve(repo, "demo")
    assert frozen(repo) == {"enabled": [], "tq": "missing"}


def test_approve_with_a_tq_that_has_no_pack_command_freezes_no_packs(repo, fake_tq):
    fake_tq.mode("old")
    gate.cmd_approve(repo, "demo")
    assert frozen(repo) == {"enabled": [], "tq": "no-pack-command"}


@pytest.mark.parametrize("mode,frag", [("list-config-error", "unknown key"), ("list-garbage", "invalid JSON")])
def test_approve_refuses_when_the_pack_config_cannot_be_read(repo, fake_tq, mode, frag):
    # an unreadable repo layer must not freeze as "no packs": that would switch the pack checks off
    fake_tq.mode(mode)
    with pytest.raises(SystemExit) as e:
        gate.cmd_approve(repo, "demo")
    assert "tq pack list" in str(e.value) and frag in str(e.value)
    assert gate.load_approved(repo, "demo") is None


def test_approve_refuses_a_list_without_config_hash(repo, fake_tq):
    fake_tq.lists({"schema": "tq.packs/v1", "packs": []})
    with pytest.raises(SystemExit) as e:
        gate.cmd_approve(repo, "demo")
    assert "config_hash" in str(e.value)


# ---------------------------------------------------------------------------
# definition of done
# ---------------------------------------------------------------------------


def test_dod_records_one_item_per_result(repo, fake_tq):
    gate.cmd_approve(repo, "demo")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING pack:data/sqlfluff" in out
    items = dod_items(repo)
    assert items["pack:config"]["ok"] is True
    assert items["pack:run"]["ok"] is True
    assert items["pack:data/contract-breaking"]["ok"] is True
    assert items["pack:data/contract-breaking"]["detail"].startswith("2 contracts, no breaking change")
    assert items["pack:data/sqlfluff"]["ok"] is False  # required fail
    assert items["pack:ml/ruff"]["ok"] is None  # advisory fail: an undeclared tool
    assert items["pack:data/dbt-parse"]["ok"] is None  # skip
    detail = items["pack:data/sqlfluff"]["detail"]
    assert "models/m1.sql:1 error LT01 finding 1" in detail and "models/m5.sql:5" in detail
    assert "models/m6.sql" not in detail and "1 more" in detail  # the first 5 findings only
    check = next(c for c in fake_tq.calls() if c[:2] == ["pack", "check"])
    base = gate.load_approved(repo, "demo")["base"]
    assert check == ["pack", "check", "--json", "--base", base, "--budget", "600s"]


def test_dod_passes_with_only_advisory_fails_and_skips(repo, fake_tq):
    gate.cmd_approve(repo, "demo")
    fake_tq.checks(green_check(), exit_code=0)
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 0 and out.startswith("DoD demo: PASS"), out
    assert "skip pack:ml/ruff" in out


def test_dod_pack_budget_comes_from_the_frozen_project_block(repo, fake_tq, monkeypatch):
    project_block(repo, pack_budget=120)
    gate.cmd_approve(repo, "demo")
    fake_tq.checks(green_check(), exit_code=0)
    seen = []
    real = gate.run

    def spy(cmd, **kw):
        seen.append((list(cmd), kw.get("timeout")))
        return real(cmd, **kw)

    monkeypatch.setattr(gate, "run", spy)
    gate.cmd_dod(repo, "demo")
    pack = [(c, t) for c, t in seen if c[1:3] == ["pack", "check"]]
    assert pack and pack[0][0][-2:] == ["--budget", "120s"] and pack[0][1] == 150


def test_dod_fails_pack_config_when_the_hash_changed(repo, fake_tq):
    gate.cmd_approve(repo, "demo")
    doc = green_check()
    doc["config_hash"] = OTHER_HASH
    fake_tq.checks(doc, exit_code=0)
    code, _ = gate.cmd_dod(repo, "demo")
    items = dod_items(repo)
    assert code == 1 and items["pack:config"]["ok"] is False
    assert "pack config changed since approval; re-approve the plan" in items["pack:config"]["detail"]


@pytest.mark.parametrize("mode,frag", [("crash", "exit 3"), ("garbage", "invalid JSON")])
def test_dod_pack_run_fails_closed_on_a_crash_or_bad_json(repo, fake_tq, mode, frag):
    gate.cmd_approve(repo, "demo")
    fake_tq.mode(mode)
    code, _ = gate.cmd_dod(repo, "demo")
    items = dod_items(repo)
    assert code == 1 and items["pack:run"]["ok"] is False and frag in items["pack:run"]["detail"]


def test_dod_pack_run_fails_on_a_wrong_schema_or_shape(repo, fake_tq):
    gate.cmd_approve(repo, "demo")
    for doc in ({**green_check(), "schema": "tq.packs/v0"},
                {**green_check(), "results": {"id": "x"}},
                {**green_check(), "results": [{"id": "data/x", "status": "great", "required": True}]},
                {**green_check(), "results": [{"id": "../x", "status": "ok", "required": True}]}):
        fake_tq.checks(doc, exit_code=0)
        code, _ = gate.cmd_dod(repo, "demo")
        assert code == 1 and dod_items(repo)["pack:run"]["ok"] is False, doc


def test_dod_pack_run_fails_when_the_exit_code_contradicts_the_results(repo, fake_tq):
    gate.cmd_approve(repo, "demo")
    fake_tq.checks(green_check(), exit_code=1)  # exit 1 says a required check failed; none did
    code, _ = gate.cmd_dod(repo, "demo")
    assert code == 1 and dod_items(repo)["pack:run"]["ok"] is False


def test_dod_pack_run_fails_on_a_config_error_exit(repo, fake_tq):
    gate.cmd_approve(repo, "demo")
    fake_tq.checks(green_check(), exit_code=2)
    code, _ = gate.cmd_dod(repo, "demo")
    assert code == 1 and "exit 2" in dod_items(repo)["pack:run"]["detail"]


def test_dod_pack_run_fails_on_timeout(repo, fake_tq, monkeypatch):
    project_block(repo, pack_budget=1)
    gate.cmd_approve(repo, "demo")
    monkeypatch.setattr(gate, "PACK_DOD_GRACE", 1)
    fake_tq.mode("sleep")
    code, _ = gate.cmd_dod(repo, "demo")
    item = dod_items(repo)["pack:run"]
    assert code == 1 and item["ok"] is False and "timed out" in item["detail"]


def test_dod_fails_closed_when_tq_disappears_after_packs_were_frozen(repo, fake_tq, monkeypatch):
    gate.cmd_approve(repo, "demo")
    monkeypatch.setattr(gate, "find_tq", lambda: None)
    code, _ = gate.cmd_dod(repo, "demo")
    item = dod_items(repo)["pack:run"]
    assert code == 1 and item["ok"] is False and "tq" in item["detail"] and "data, ml" in item["detail"]


def test_dod_skips_packs_when_none_were_frozen_and_tq_is_missing(repo):
    gate.cmd_approve(repo, "demo")
    code, out = gate.cmd_dod(repo, "demo")
    items = dod_items(repo)
    assert code == 0, out
    assert items["pack"] == {"id": "pack", "ok": None, "detail": "skip: no packs frozen at approval"}
    assert "pack:run" not in items


def test_dod_skips_packs_for_an_approval_that_predates_the_freeze(repo, fake_tq):
    gate.cmd_approve(repo, "demo")
    approved = gate.load_approved(repo, "demo")
    approved.pop("packs")
    gate.save_approved(repo, "demo", approved)
    _, out = gate.cmd_dod(repo, "demo")
    assert dod_items(repo)["pack"]["ok"] is None, out
    assert not [c for c in fake_tq.calls() if c[:2] == ["pack", "check"]]


def test_dod_runs_the_check_when_tq_was_frozen_with_no_packs(repo, fake_tq):
    # a repo layer that enables a pack after approval changes the hash: re-approve
    fake_tq.lists({**LIST_OUT, "packs": []})
    gate.cmd_approve(repo, "demo")
    assert frozen(repo) == {"enabled": [], "config_hash": HASH}
    doc = green_check()
    doc["config_hash"] = OTHER_HASH
    fake_tq.checks(doc, exit_code=0)
    code, _ = gate.cmd_dod(repo, "demo")
    assert code == 1 and dod_items(repo)["pack:config"]["ok"] is False


def test_pack_budget_is_validated_in_the_project_block(tmp_path):
    project_block(tmp_path, pack_budget=300)
    assert load_project_config(tmp_path)[0]["pack_budget"] == 300
    for bad in (0, 1801, "600", True, 2.5):
        project_block(tmp_path, pack_budget=bad)
        cfg, warn = load_project_config(tmp_path)
        assert "pack_budget" not in cfg and "pack_budget" in warn, bad


# ---------------------------------------------------------------------------
# reviewer names
# ---------------------------------------------------------------------------


@pytest.fixture
def agents_dir(tmp_path, monkeypatch) -> Path:
    plug = tmp_path / "plugin"
    (plug / "agents").mkdir(parents=True)
    (plug / "agents" / "pipeline-reviewer.md").write_text("---\nname: pipeline-reviewer\n---\n", encoding="utf-8")
    (plug / "agents" / "pipeline-critic.md").write_text("---\nname: pipeline-critic\n---\n", encoding="utf-8")
    monkeypatch.setattr(gate, "plugin_root", lambda: plug)
    return plug / "agents"


def test_reviewer_names_accept_pack_reviewers_with_an_agent_file(agents_dir):
    assert gate.reviewer_ok("build-critic") and gate.reviewer_ok("breaker")
    assert gate.reviewer_ok("pipeline-reviewer")
    assert not gate.reviewer_ok("ml-reviewer")  # no agent file
    assert not gate.reviewer_ok("pipeline-critic")  # an agent file, but not a *-reviewer name
    for bad in ("Pipeline-reviewer", "../pipeline-reviewer", "pipeline-reviewer/../x-reviewer", "-reviewer", "9-reviewer", ""):
        assert not gate.reviewer_ok(bad), bad


def test_record_review_accepts_a_pack_reviewer(repo, agents_dir):
    gate.cmd_approve(repo, "demo")
    out = gate.cmd_record_review(repo, "demo", 1, "pipeline-reviewer", "PASS", {})
    assert "pipeline-reviewer: PASS recorded" in out
    with pytest.raises(SystemExit) as e:
        gate.cmd_record_review(repo, "demo", 1, "ml-reviewer", "PASS", {})
    assert "unknown reviewer 'ml-reviewer'" in str(e.value) and "agents/ml-reviewer.md" in str(e.value)


def test_cli_accepts_a_pack_reviewer_and_refuses_an_unknown_one(repo, agents_dir, capsys):
    gate.cmd_approve(repo, "demo")
    assert gate.main(["--root", str(repo), "record-review", "demo", "1", "--reviewer", "pipeline-reviewer", "--result", "PASS"]) == 0
    with pytest.raises(SystemExit):
        gate.main(["--root", str(repo), "record-review", "demo", "1", "--reviewer", "ml-reviewer", "--result", "PASS"])
    assert "ml-reviewer" in capsys.readouterr().err


# ---------------------------------------------------------------------------
# skills, docs, CI
# ---------------------------------------------------------------------------

ONRAMPS = ("build", "feature", "fix", "kickoff", "dod", "ship")


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def _bash_blocks(body: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", body, re.DOTALL)


@pytest.mark.parametrize("name", ONRAMPS)
def test_onramps_carry_the_generic_pack_step(name):
    body = _body(name)
    flat = " ".join(body.split())
    assert "## Packs" in body, name
    for token in ("tq pack list --enabled --json", "skills/pack/references/onramps/<pack>-*.md",
                  "tq pack crew --flags", f"`## {name}`"):
        assert token in flat, (name, token)


def test_pack_skill_covers_the_framework():
    body = _body("pack")
    fm = body.split("\n---\n", 1)[0]
    assert "name: pack" in fm and "Use when" in fm
    flat = " ".join(body.split())
    for token in ("tq pack check", "tq pack doctor", "tq pack crew", "tq pack list --enabled --json",
                  "--builtin-only", "pack:config", "pack:run", "pack:<pack>/<check>", "advisory",
                  "declared in packs.<p>.tools but not installed", "tq-packs.yaml", "tq pack baseline",
                  "references/onramps/", "build-gate dod", "re-approve", "never", "tq dotenv run"):
        assert token in flat, token
    blocks = _bash_blocks(body)
    assert blocks
    for block in blocks:
        assert block.lstrip().startswith(SNIPPET), block[:80]
        assert "python " not in block and "python3 " not in block
    assert "git add -A" not in body


def test_onramps_reference_dir_names_the_convention():
    d = SKILLS / "pack" / "references" / "onramps"
    readme = (d / "README.md").read_text(encoding="utf-8")
    flat = " ".join(readme.split())
    assert "<pack>-<topic>.md" in flat
    for name in ONRAMPS:
        assert f"## {name}" in flat, name
    for path in d.glob("*.md"):
        if path.name != "README.md":
            assert re.fullmatch(r"(data|ml|cloud|db)-[a-z0-9-]+\.md", path.name), path.name


def test_packs_doc_is_an_index_of_the_pack_docs():
    text = (REPO / "docs" / "PACKS.md").read_text(encoding="utf-8")
    for token in ("packs:", "tq-packs.yaml", "tq pack check", "tq pack list", "tq pack crew", "tq pack doctor",
                  "tq pack approvals", "tq pack baseline", "tq.packs/v1", "pack:config", "pack:run",
                  "config_hash", "packs/", "## Index"):
        assert token in text, token
    for link in re.findall(r"\]\((packs/[^)]+)\)", text):
        assert re.fullmatch(r"packs/(data|ml|cloud|db)-[a-z0-9-]+\.md", link), link


def test_ci_templates_job_loops_over_every_template():
    text = (REPO / ".github" / "workflows" / "test.yml").read_text(encoding="utf-8")
    job = text.split("\n  templates:", 1)[1].split("\n  desktop:", 1)[0]
    assert "templates/*/" in job and "pyproject.toml" in job and "tests" in job
    for old in ("templates/rag-starter/tests", "templates/pandas-jev-labeller/tests", "templates/n8n-jev-decision/tests"):
        assert old not in job, old  # the loop covers them; a hard-coded list misses new templates
