"""Build gate (/tentaqles:build): the kit's checkpoint-gate cases, ported, plus the
fixes: Windows paths, PowerShell, Stop non-lockup, evidence + tree-hash reset,
locked tests, UTF-8, and a 127.0.0.1-only viewer."""

from __future__ import annotations

import copy
import json
import os
import subprocess
import sys
import threading
import urllib.error
import urllib.request
from pathlib import Path

import pytest

from tentaqles.workflow import gate

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "build-gate.py"
PY = sys.executable.replace("\\", "/")
PASS_CMD = f'"{PY}" -c "import sys; sys.exit(0)"'
FAIL_CMD = f'"{PY}" -c "import sys; sys.exit(3)"'


def _git(root, *args):
    subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)


def cp(i, status="pending", b="pending", r="pending", title=None, verify=None):
    g = lambda s: {"status": s, "attempts": 0, "findings": []}  # noqa: E731
    return {"id": i, "title": title or f"step {i}", "status": status,
            "verify": verify or [PASS_CMD],
            "gates": {"behavior": g(b), "review": g(r)}}


PLAN = {"feature": "Demo", "slug": "demo",
        "checkpoints": [cp(1, "passed", "passed", "passed"), cp(2), cp(3)]}


def with_gate(plan, cid, **gates):
    p = copy.deepcopy(plan)
    c = next(c for c in p["checkpoints"] if c["id"] == cid)
    for k, v in gates.items():
        if k == "status":
            c["status"] = v
        elif k == "attempts":
            c["gates"]["behavior"]["attempts"] = v
        else:
            c["gates"][k]["status"] = v
    return p


@pytest.fixture
def repo(tmp_path):
    root = tmp_path / "proj"
    (root / "docs" / "checkpoints").mkdir(parents=True)
    (root / "tests").mkdir()
    _git(root, "init", "-q", "-b", "work")
    _git(root, "config", "user.email", "dev@example.com")
    _git(root, "config", "user.name", "Dev")
    (root / "app.py").write_text("def f():\n    return 1\n", encoding="utf-8")
    (root / "tests" / "test_a.py").write_text("def test_a():\n    assert True\n", encoding="utf-8")
    save_plan(root, PLAN)
    _git(root, "add", "app.py", "tests/test_a.py", "docs/checkpoints/demo.json")
    _git(root, "commit", "-q", "-m", "init")
    (root / ".claude" / "build").mkdir(parents=True)
    return root


def save_plan(root, plan):
    (root / "docs" / "checkpoints" / "demo.json").write_text(
        json.dumps(plan, indent=2, ensure_ascii=False), encoding="utf-8")


def plan_file(root):
    return str(root / "docs" / "checkpoints" / "demo.json")


def hook(root, payload, session="s1"):
    payload = {"cwd": str(root), "session_id": session, **payload}
    return gate.hook(payload)


def agent(marker):
    return {"tool_name": "Agent", "tool_input": {"prompt": f"{marker}\nDo the work."}}


def write(path, plan):
    return {"tool_name": "Write", "tool_input": {"file_path": path, "content": json.dumps(plan)}}


def bash(cmd, tool="Bash"):
    return {"tool_name": tool, "tool_input": {"command": cmd}}


def approve_and_lock(root, cid=2, paths=("tests/test_a.py",)):
    gate.cmd_approve(root, "demo")
    gate.cmd_lock_tests(root, "demo", cid, [str(root / p) for p in paths], none=not paths)


# ---------------------------------------------------------------------------
# Agent markers (kit cases)
# ---------------------------------------------------------------------------


def test_no_marker_allowed(repo):
    assert hook(repo, agent("plain prompt"))[0] == 0


def test_marker_before_approval_blocked(repo):
    code, _, err = hook(repo, agent("[checkpoint demo#2 tests]"))
    assert code == 2 and "approve" in err


def test_tests_stage_after_approval_allowed(repo):
    gate.cmd_approve(repo, "demo")
    assert hook(repo, agent("[checkpoint demo#2 tests]"))[0] == 0


def test_implement_needs_locked_tests(repo):
    gate.cmd_approve(repo, "demo")
    code, _, err = hook(repo, agent("[checkpoint demo#2 implement]"))
    assert code == 2 and "lock-tests" in err
    gate.cmd_lock_tests(repo, "demo", 2, [], none=True)
    assert hook(repo, agent("[checkpoint demo#2 implement]"))[0] == 0


@pytest.mark.parametrize("marker,frag", [
    ("[checkpoint demo#3 implement]", "haven't passed"),
    ("[checkpoint demo#2 review]", "behavior"),
    ("[checkpoint demo#1 fix]", "already passed"),
    ("[checkpoint demo#9 tests]", "doesn't exist"),
    ("[checkpoint demo#2 deploy]", "unknown stage"),
])
def test_marker_blocks(repo, marker, frag):
    approve_and_lock(repo)
    code, _, err = hook(repo, agent(marker))
    assert code == 2 and frag in err


def test_marker_records_owner_session_and_base(repo):
    approve_and_lock(repo)
    hook(repo, agent("[checkpoint demo#2 implement]"), session="abc")
    sess = gate.load_session(repo, "demo")
    assert sess["session_id"] == "abc" and sess["stage"] == "implement" and sess["bases"]["2"]


def test_review_needs_fresh_evidence(repo):
    approve_and_lock(repo)
    assert gate.cmd_verify(repo, "demo", 2)[0] == 0
    save_plan(repo, with_gate(PLAN, 2, behavior="passed"))
    assert hook(repo, agent("[checkpoint demo#2 review]"))[0] == 0
    (repo / "app.py").write_text("def f():\n    return 2\n", encoding="utf-8")
    code, _, err = hook(repo, agent("[checkpoint demo#2 review]"))
    assert code == 2 and "gate reset" in err


# ---------------------------------------------------------------------------
# Plan writes (kit cases + evidence)
# ---------------------------------------------------------------------------


def test_plan_transition_rules(repo):
    approve_and_lock(repo)
    p = plan_file(repo)
    assert hook(repo, write(p, with_gate(PLAN, 2, behavior="failed")))[0] == 0
    assert hook(repo, write(p, with_gate(PLAN, 3, behavior="passed")))[0] == 2
    assert hook(repo, write(p, with_gate(PLAN, 2, review="passed")))[0] == 2
    assert hook(repo, write(p, with_gate(PLAN, 2, status="passed")))[0] == 2
    other = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "x.json"), "content": "{}"}}
    assert hook(repo, other)[0] == 0


def test_behavior_pass_requires_evidence(repo):
    approve_and_lock(repo)
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed")))
    assert code == 2 and "no verify evidence" in err


def test_failed_verify_blocks_pass(repo):
    plan = copy.deepcopy(PLAN)
    plan["checkpoints"][1]["verify"] = [FAIL_CMD]
    save_plan(repo, plan)
    approve_and_lock(repo)
    code, msg = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and "exit 3" in msg
    code, _, err = hook(repo, write(plan_file(repo), with_gate(plan, 2, behavior="passed")))
    assert code == 2 and "failed" in err


def test_diff_hash_gate_reset(repo):
    """Evidence is tied to the tree hash: any code change resets the gate."""
    approve_and_lock(repo)
    p = plan_file(repo)
    assert gate.cmd_verify(repo, "demo", 2)[0] == 0
    assert hook(repo, write(p, with_gate(PLAN, 2, behavior="passed")))[0] == 0
    # code changes after the passing run -> gate resets
    (repo / "app.py").write_text("def f():\n    return 42\n", encoding="utf-8")
    code, _, err = hook(repo, write(p, with_gate(PLAN, 2, behavior="passed")))
    assert code == 2 and "gate reset" in err
    # a new untracked file also counts
    assert gate.cmd_verify(repo, "demo", 2)[0] == 0
    (repo / "new_module.py").write_text("X = 1\n", encoding="utf-8")
    assert hook(repo, write(p, with_gate(PLAN, 2, behavior="passed")))[0] == 2
    # re-verify -> passable again
    assert gate.cmd_verify(repo, "demo", 2)[0] == 0
    assert hook(repo, write(p, with_gate(PLAN, 2, behavior="passed")))[0] == 0
    # committing the same content does not reset (hash is content, not commit)
    _git(repo, "add", "app.py", "new_module.py")
    _git(repo, "commit", "-q", "-m", "impl")
    assert hook(repo, write(p, with_gate(PLAN, 2, behavior="passed")))[0] == 0
    # bookkeeping (the plan, reviews) never resets the gate
    (repo / "docs" / "reviews").mkdir(parents=True)
    (repo / "docs" / "reviews" / "demo-2.md").write_text("notes", encoding="utf-8")
    full = with_gate(PLAN, 2, behavior="passed", review="passed", status="passed")
    assert hook(repo, write(p, full))[0] == 0


def test_full_pass_and_downgrade(repo):
    approve_and_lock(repo)
    p = plan_file(repo)
    gate.cmd_verify(repo, "demo", 2)
    full = with_gate(PLAN, 2, behavior="passed", review="passed", status="passed")
    assert hook(repo, write(p, full))[0] == 0
    # reopening (moving toward pending) is always allowed
    assert hook(repo, write(p, PLAN))[0] == 0


def test_edit_and_multiedit_paths(repo):
    approve_and_lock(repo)
    save_plan(repo, PLAN)
    text = Path(plan_file(repo)).read_text(encoding="utf-8")
    old = '"id": 3,\n      "title": "step 3",\n      "status": "pending"'
    assert old in text
    edit = {"tool_name": "Edit", "tool_input": {"file_path": plan_file(repo), "old_string": old,
                                                 "new_string": old.replace('"pending"', '"passed"')}}
    assert hook(repo, edit)[0] == 2
    multi = {"tool_name": "MultiEdit", "tool_input": {"file_path": plan_file(repo), "edits": [
        {"old_string": old, "new_string": old.replace('"pending"', '"passed"')}]}}
    assert hook(repo, multi)[0] == 2


def test_windows_backslash_paths_are_normalized(repo):
    """Kit bug: `docs/checkpoints/` never matched C:\\...\\docs\\checkpoints\\x.json."""
    approve_and_lock(repo)
    back = str(repo).replace("/", "\\") + "\\docs\\checkpoints\\demo.json"
    payload = write(back, with_gate(PLAN, 3, behavior="passed"))
    code, _, err = hook(repo, payload)
    assert code == 2 and "#3" in err
    locked = str(repo).replace("/", "\\") + "\\tests\\test_a.py"
    edit = {"tool_name": "Edit", "tool_input": {"file_path": locked, "old_string": "a", "new_string": "b"}}
    assert hook(repo, edit)[0] == 2
    state = str(repo).replace("/", "\\") + "\\.claude\\build\\demo\\evidence.jsonl"
    assert hook(repo, {"tool_name": "Write", "tool_input": {"file_path": state, "content": "{}"}})[0] == 2


# ---------------------------------------------------------------------------
# Shell guard (kit cases + PowerShell + locked tests)
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("cmd,tool,want", [
    ("cat > docs/checkpoints/demo.json <<EOF\n{}\nEOF", "Bash", 2),
    ("python3 -c \"open('docs/checkpoints/demo.json','w')\"", "Bash", 2),
    ("git add docs/checkpoints/demo.json", "Bash", 0),
    ("cat docs/checkpoints/demo.json", "Bash", 0),
    ("echo '{}' > .claude/build/demo/evidence.jsonl", "Bash", 2),
    ("cat .claude/build/demo/evidence.jsonl", "Bash", 0),
    ("Set-Content docs\\checkpoints\\demo.json '{}'", "PowerShell", 2),
    ("Get-Content docs\\checkpoints\\demo.json", "PowerShell", 0),
    ("Out-File -FilePath .claude\\build\\demo\\approved.json", "PowerShell", 2),
    ("rm tests/test_a.py", "Bash", 2),
    ("sed -i s/True/False/ tests/test_a.py", "Bash", 2),
    ("git checkout -- tests/test_a.py", "Bash", 2),
    ("Remove-Item tests\\test_a.py", "PowerShell", 2),
    ("python -m pytest tests/test_a.py -q", "Bash", 0),
    ("pytest tests/test_a.py 2>&1", "Bash", 0),
])
def test_shell_guard(repo, cmd, tool, want):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd, tool))[0] == want


def test_gate_cli_approve_asks_human(repo):
    code, out, _ = hook(repo, bash('bash "/x/scripts/tq_run.sh" build-gate.py approve demo'))
    assert code == 0
    decision = json.loads(out)["hookSpecificOutput"]
    assert decision["permissionDecision"] == "ask" and "approve" in decision["permissionDecisionReason"]
    code, out, _ = hook(repo, bash('bash tq_run.sh build-gate.py verify demo 2'))
    assert code == 0 and out == ""


def test_locked_test_edits_blocked_and_tamper_fails_verify(repo):
    approve_and_lock(repo)
    edit = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "tests" / "test_a.py"), "content": "x"}}
    code, _, err = hook(repo, edit)
    assert code == 2 and "locked test" in err
    # a write that slipped past the guard (e.g. an interpreter) is caught by verify
    (repo / "tests" / "test_a.py").write_text("def test_a():\n    pass\n", encoding="utf-8")
    code, msg = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and "locked tests changed" in msg


def test_settings_locked_while_build_active(repo):
    approve_and_lock(repo)
    payload = {"tool_name": "Write", "tool_input": {"file_path": str(repo / ".claude" / "settings.local.json"),
                                                    "content": "{}"}}
    assert hook(repo, payload)[0] == 2
    gate.cmd_finish(repo, "demo")
    assert hook(repo, payload)[0] == 0


# ---------------------------------------------------------------------------
# Stop hook: never a permanent lock-up
# ---------------------------------------------------------------------------


def stop(root, session="s1", active=False):
    return hook(root, {"hook_event_name": "Stop", "stop_hook_active": active}, session=session)


def own(root, session="s1"):
    approve_and_lock(root)
    hook(root, agent("[checkpoint demo#2 implement]"), session=session)


def test_stop_blocks_once_then_allows(repo):
    own(repo)
    save_plan(repo, with_gate(PLAN, 2, behavior="failed"))
    code, _, err = stop(repo)
    assert code == 2 and "#2" in err
    assert stop(repo)[0] == 0          # second stop goes through
    assert stop(repo)[0] == 2          # counter reset: a later stop is blocked once again


def test_stop_hook_active_is_honored(repo):
    own(repo)
    save_plan(repo, with_gate(PLAN, 2, behavior="failed"))
    assert stop(repo, active=True)[0] == 0


def test_stop_other_session_and_halted_and_done(repo):
    own(repo)
    mid = with_gate(PLAN, 2, behavior="failed")
    save_plan(repo, mid)
    assert stop(repo, session="someone-else")[0] == 0
    save_plan(repo, {**mid, "halted": {"reason": "circuit breaker", "checkpoint": 2}})
    assert stop(repo)[0] == 0
    done = with_gate(PLAN, 2, behavior="passed", review="passed", status="passed")
    done = with_gate(done, 3, behavior="passed", review="passed", status="passed")
    save_plan(repo, done)
    assert stop(repo)[0] == 0


def test_stop_with_invalid_plan_does_not_deadlock(repo):
    """Kit bug: a structurally invalid plan blocked Stop forever."""
    own(repo)
    bad = with_gate(PLAN, 3, behavior="passed")  # written behind the hooks' back
    save_plan(repo, bad)
    code, _, err = stop(repo)
    assert code == 2 and "inconsistent" in err
    assert stop(repo)[0] == 0
    # and the bad checkpoint can be reverted (a downgrade is always allowed)
    assert hook(repo, write(plan_file(repo), PLAN))[0] == 0


def test_stop_skipped_in_headless_child(repo, monkeypatch):
    own(repo)
    save_plan(repo, with_gate(PLAN, 2, behavior="failed"))
    monkeypatch.setenv("TENTAQLES_HEADLESS_CHILD", "1")
    assert stop(repo)[0] == 0


# ---------------------------------------------------------------------------
# Setup, tq rules, viewer, script entry point
# ---------------------------------------------------------------------------


def test_setup_is_idempotent_and_finish_removes(repo):
    gate.cmd_setup(repo)
    gate.cmd_setup(repo)
    settings = json.loads((repo / ".claude" / "settings.local.json").read_text(encoding="utf-8"))
    for event in ("PreToolUse", "Stop"):
        groups = settings["hooks"][event]
        cmds = [h["command"] for g in groups for h in g["hooks"]]
        assert sum(gate.SETTINGS_TAG in c for c in cmds) == 1
        assert all(h.get("timeout") for g in groups for h in g["hooks"])
    assert "PowerShell" in settings["hooks"]["PreToolUse"][0]["matcher"]
    exclude = subprocess.run(["git", "rev-parse", "--git-path", "info/exclude"], cwd=repo,
                             capture_output=True, text=True).stdout.strip()
    text = (repo / exclude).read_text(encoding="utf-8") if not os.path.isabs(exclude) else Path(exclude).read_text()
    assert "/.claude/build/" in text
    gate.cmd_approve(repo, "demo")
    gate.cmd_finish(repo, "demo")
    settings = json.loads((repo / ".claude" / "settings.local.json").read_text(encoding="utf-8"))
    assert not settings.get("hooks")


def test_ping_reports_hook_activity(repo):
    assert gate.cmd_ping(repo)[0] == 1
    hook(repo, bash("ls"))
    assert gate.cmd_ping(repo)[0] == 0


def test_tq_rules_generated_for_locked_tests(repo):
    yaml = pytest.importorskip("yaml")
    approve_and_lock(repo)
    data = yaml.safe_load((repo / ".claude" / "tq-rules.yaml").read_text(encoding="utf-8"))
    rule = data["rules"][0]
    assert rule["action"] == "ask" and rule["id"].startswith("tentaqles-build/")
    import re
    assert re.search(rule["path"], "C:/repo/tests/test_a.py")
    assert re.fullmatch(rule["tool"], "Edit")
    gate.cmd_finish(repo, "demo")
    assert not (repo / ".claude" / "tq-rules.yaml").exists()


def test_tq_rules_merge_keeps_project_rules(repo):
    yaml = pytest.importorskip("yaml")
    path = repo / ".claude" / "tq-rules.yaml"
    path.parent.mkdir(exist_ok=True)
    path.write_text("rules:\n  - id: app/no-console\n    action: ask\n    tool: Edit|Write\n"
                    "    content: 'console\\.log'\n    reason: use the logger\n", encoding="utf-8")
    approve_and_lock(repo)
    ids = [r["id"] for r in yaml.safe_load(path.read_text(encoding="utf-8"))["rules"]]
    assert ids == ["app/no-console", "tentaqles-build/locked-tests"]


def test_viewer_binds_localhost_only(repo):
    srv = gate.make_server(repo, "demo", 0)
    try:
        assert srv.server_address[0] == "127.0.0.1"
        t = threading.Thread(target=srv.serve_forever, daemon=True)
        t.start()
        port = srv.server_address[1]
        body = urllib.request.urlopen(f"http://127.0.0.1:{port}/", timeout=5).read().decode("utf-8")
        assert "step 2" in body
        with pytest.raises(urllib.error.HTTPError):
            urllib.request.urlopen(f"http://127.0.0.1:{port}/app.py", timeout=5)
    finally:
        srv.shutdown()
        srv.server_close()


def test_review_bundle_has_diff_and_evidence(repo):
    approve_and_lock(repo)
    hook(repo, agent("[checkpoint demo#2 implement]"))
    (repo / "app.py").write_text("def f():\n    return 7\n", encoding="utf-8")
    gate.cmd_verify(repo, "demo", 2)
    path, base = gate.cmd_review_bundle(repo, "demo", 2).splitlines()
    assert base.startswith("base=") and len(base) > 10
    out = Path(path)
    text = out.read_text(encoding="utf-8")
    assert "return 7" in text and "Latest verify evidence" in text


def test_script_hook_utf8_and_exit_codes(repo):
    """End to end through the script: UTF-8 plan text, Windows path, exit 2 + stderr."""
    plan = copy.deepcopy(PLAN)
    plan["checkpoints"][2]["title"] = "Ação → preço"
    save_plan(repo, plan)
    gate.cmd_approve(repo, "demo")
    back = str(repo).replace("/", "\\") + "\\docs\\checkpoints\\demo.json"
    payload = {"cwd": str(repo), "session_id": "s1",
               **write(back, with_gate(plan, 3, behavior="passed"))}
    env = {k: v for k, v in os.environ.items() if k not in ("PYTHONUTF8", "PYTHONIOENCODING")}
    proc = subprocess.run([sys.executable, str(SCRIPT), "hook"], input=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
                          capture_output=True, env=env, timeout=60)
    assert proc.returncode == 2
    assert "build-gate" in proc.stderr.decode("utf-8")
    proc = subprocess.run([sys.executable, str(SCRIPT), "--root", str(repo), "status", "demo"],
                          capture_output=True, env=env, timeout=60)
    assert proc.returncode == 0 and "Ação → preço" in proc.stdout.decode("utf-8")


def test_hook_fails_open_on_garbage(repo):
    proc = subprocess.run([sys.executable, str(SCRIPT), "hook"], input=b"not json", capture_output=True, timeout=60)
    assert proc.returncode == 0
