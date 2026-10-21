"""The hub's task block and review records (spec A §13.1, §10, §13.4).

The gate accepts a top-level `task` object with exactly the hub's keys, refuses one the
environment did not set, reports the task the human approved, and exports only the records it
signed itself for the hub's review page. The skills carry the block from TQ_HUB_TASK to the plan
and end a headless Phase 0 with the relocate line.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

from tentaqles.workflow import gate

ROOT = Path(__file__).resolve().parents[1]
PY = sys.executable.replace("\\", "/")
PASS_CMD = f'"{PY}" -c "import sys; sys.exit(0)"'
TASK = {"hub": "c-abcdefghijkl", "workspace": "acme", "kind": "feature", "external": None}


def _git(root, *args):
    subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)


def _plan(task=TASK):
    cp = {"id": 1, "title": "Parse rows", "status": "pending", "verify": [PASS_CMD], "tests": ["tests/test_a.py"],
          "gates": {"behavior": {"status": "pending", "attempts": 0, "findings": []},
                    "review": {"status": "pending", "attempts": 0, "findings": []}}}
    plan = {"feature": "Demo", "slug": "demo", "workflow": "feature", "summary": "Export invoices.",
            "spec": "docs/specs/demo.md", "checkpoints": [cp]}
    if task is not None:
        plan["task"] = task
    return plan


def save(root, plan):
    (root / "docs" / "checkpoints" / "demo.json").write_text(json.dumps(plan, indent=2), encoding="utf-8")


@pytest.fixture(autouse=True)
def tq_home(tmp_path, monkeypatch):
    """The signing key lives under TQ_HOME; never touch the real one, never inherit a hub task."""
    monkeypatch.setenv("TQ_HOME", str(tmp_path / "tqhome"))
    for k in ("TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"):
        monkeypatch.delenv(k, raising=False)


@pytest.fixture
def repo(tmp_path):
    root = tmp_path / "proj"
    (root / "docs" / "checkpoints").mkdir(parents=True)
    (root / "tests").mkdir()
    _git(root, "init", "-q", "-b", "work")
    _git(root, "config", "user.email", "dev@example.com")
    _git(root, "config", "user.name", "Dev")
    (root / "tests" / "test_a.py").write_text("def test_a():\n    assert True\n", encoding="utf-8")
    save(root, _plan())
    _git(root, "add", "tests/test_a.py", "docs/checkpoints/demo.json")
    _git(root, "commit", "-q", "-m", "init")
    (root / ".claude" / "build").mkdir(parents=True)
    return root


def test_plan_problem_accepts_the_task_block():
    assert gate.plan_problem(_plan()) == ""
    assert gate.plan_problem(_plan(task=None)) == ""
    ext = {**TASK, "external": {"provider": "linear", "id": "ACME-7", "url": "https://linear.app/x"}}
    assert gate.plan_problem(_plan(task=ext)) == ""


@pytest.mark.parametrize("task,needle", [
    ("c-abcdefghijkl", "task must be an object"),
    ({**TASK, "hub_id": "t-42"}, "unknown key"),
    ({k: v for k, v in TASK.items() if k != "kind"}, "missing kind"),
    ({**TASK, "hub": "t-42"}, "task.hub"),
    ({**TASK, "workspace": ""}, "task.workspace"),
    ({**TASK, "kind": "epic"}, "task.kind"),
    ({**TASK, "external": {"provider": "linear"}}, "task.external"),
    ({**TASK, "external": "ACME-7"}, "task.external"),
])
def test_plan_problem_refuses_a_malformed_task(task, needle):
    assert needle in gate.plan_problem(_plan(task=task))


def test_hook_refuses_a_task_without_tq_hub_task(repo):
    with pytest.raises(gate.Block) as e:
        gate.check_plan(json.dumps(_plan()), json.dumps(_plan()), repo, "demo", gate.TreeHash(repo))
    assert "TQ_HUB_TASK" in str(e.value)


def test_hook_accepts_only_the_task_the_hub_set(repo, monkeypatch):
    monkeypatch.setenv("TQ_HUB_TASK", json.dumps(TASK))
    gate.check_plan(json.dumps(_plan()), json.dumps(_plan()), repo, "demo", gate.TreeHash(repo))
    other = {**TASK, "hub": "c-zzzzzzzzzzzz"}
    with pytest.raises(gate.Block) as e:
        gate.check_plan(json.dumps(_plan(task=other)), json.dumps(_plan()), repo, "demo", gate.TreeHash(repo))
    assert "verbatim" in str(e.value)


def test_hook_without_a_task_is_unchanged(repo):
    gate.check_plan(json.dumps(_plan(task=None)), json.dumps(_plan(task=None)), repo, "demo", gate.TreeHash(repo))


def test_state_reports_the_task_the_human_approved(repo):
    assert gate.cmd_state(repo, "demo")["task"] == TASK  # before approval: the plan's
    gate.cmd_approve(repo, "demo")
    save(repo, _plan(task={**TASK, "hub": "c-zzzzzzzzzzzz"}))  # edited behind the hook's back
    assert gate.cmd_state(repo, "demo")["task"] == TASK  # the signed approval wins


def test_state_records_export_only_signed_records(repo, capsys):
    gate.cmd_approve(repo, "demo")
    gate.cmd_verify(repo, "demo", 1)
    gate.cmd_record_review(repo, "demo", 1, "build-critic", "PASS", {"minors": 1})
    with open(gate.state_dir(repo, "demo") / "evidence.jsonl", "a", encoding="utf-8") as fh:
        fh.write(json.dumps({"checkpoint": 1, "ok": True, "results": [{"cmd": "forged", "exit": 0}]}) + "\n")
    capsys.readouterr()
    assert gate.main(["--root", str(repo), "state", "demo", "--json", "--records"]) == 0
    rec = json.loads(capsys.readouterr().out)["records"]
    assert [r["cmd"] for e in rec["evidence"] for r in e["results"]] == [PASS_CMD]
    assert rec["evidence"][0]["current"] is True and rec["evidence"][0]["ok"] is True
    assert rec["reviews"][0]["reviewer"] == "build-critic" and rec["reviews"][0]["minors"] == 1
    assert rec["plan"]["summary"] == "Export invoices." and rec["plan"]["spec"] == "docs/specs/demo.md"
    assert rec["plan"]["checkpoints"][0]["approved"] is True
    assert rec["plan"]["checkpoints"][0]["tests"] == [{"file": "tests/test_a.py", "layer": ""}]
    assert rec["dod"] is None
    gate.main(["--root", str(repo), "state", "demo", "--json"])
    assert "records" not in json.loads(capsys.readouterr().out)


def test_planner_and_on_ramps_document_the_task_block():
    planner = (ROOT / "agents" / "build-planner.md").read_text(encoding="utf-8")
    assert "TQ_HUB_TASK" in planner and '"task"' in planner and "verbatim" in planner
    for name in ("feature", "fix", "kickoff"):
        text = (ROOT / "skills" / name / "SKILL.md").read_text(encoding="utf-8")
        assert "TQ_HUB_TASK" in text and "task=" in text, name
        assert "reject the plan" in text, name
    build = (ROOT / "skills" / "build" / "SKILL.md").read_text(encoding="utf-8")
    assert '`{"hub", "workspace", "kind", "external"}`' in build


def test_build_skill_headless_hand_off_line():
    text = (ROOT / "skills" / "build" / "SKILL.md").read_text(encoding="utf-8")
    phase0 = text.split("## Phase 0", 1)[1].split("## Phase 1", 1)[0]
    assert "TQ_HUB_RELOCATE <absolute worktree path> <slug>" in phase0
    assert "TENTAQLES_HEADLESS=1" in phase0 and "only once per slug" in phase0
    assert "rev-parse --show-toplevel" in phase0  # a native path on Windows, not /c/...
