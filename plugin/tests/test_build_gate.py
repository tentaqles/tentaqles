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


@pytest.fixture(autouse=True)
def tq_home(tmp_path, monkeypatch):
    """The signing key lives under TQ_HOME; never touch the real one."""
    home = tmp_path / "tqhome"
    monkeypatch.setenv("TQ_HOME", str(home))
    return home


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
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    full = with_gate(PLAN, 2, behavior="passed", review="passed", status="passed")
    assert hook(repo, write(p, full))[0] == 0


def test_full_pass_and_downgrade(repo):
    approve_and_lock(repo)
    p = plan_file(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
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


@pytest.mark.parametrize("sub", ["approve demo", "unlock-tests demo tests/test_a.py", "finish demo"])
def test_human_only_subcommands_refused_to_the_agent(repo, sub):
    code, _, err = hook(repo, bash(f'bash "/x/scripts/tq_run.sh" build-gate.py {sub}'))
    assert code == 2 and "own terminal" in err
    code, out, _ = hook(repo, bash('bash tq_run.sh build-gate.py verify demo 2'))
    assert code == 0 and out == ""


def test_human_only_cli_needs_tty_and_code(repo):
    """Piped stdin is not a human: the CLI refuses even if the hook were bypassed."""
    proc = subprocess.run([sys.executable, str(SCRIPT), "--root", str(repo), "approve", "demo"],
                          input=b"yes\n", capture_output=True, timeout=60)
    assert proc.returncode == 2 and b"interactive terminal" in proc.stderr
    assert gate.load_approved(repo, "demo") is None

    class TTY:
        def __init__(self, answer):
            self.answer, self.out = answer, []

        def isatty(self):
            return True

        def readline(self):
            return self.answer

        def write(self, s):
            self.out.append(s)

        def flush(self):
            pass

    assert gate.human_confirm("x", stdin=TTY("nope\n"), stdout=TTY("")) is False
    out = TTY("")

    class Echo(TTY):
        def readline(self):
            text = "".join(out.out)
            return text.rsplit("Type ", 1)[1].split(" ", 1)[0] + "\n"

    assert gate.human_confirm("x", stdin=Echo(""), stdout=out) is True


# ---------------------------------------------------------------------------
# Bypass attempts (security review): the agent must not be able to produce or
# alter anything the gate trusts.
# ---------------------------------------------------------------------------


def test_agent_written_approval_keeps_gate_closed(repo):
    state = repo / ".claude" / "build" / "demo"
    state.mkdir(parents=True, exist_ok=True)
    fake = {"slug": "demo", "verify": {"1": [PASS_CMD], "2": [PASS_CMD], "3": [PASS_CMD]},
            "tests_locked": {"2": []}, "locked_tests": {}, "approval_id": "x", "sig": "0" * 64}
    (state / "approved.json").write_text(json.dumps(fake), encoding="utf-8")
    assert gate.load_approved(repo, "demo") is None
    code, _, err = hook(repo, agent("[checkpoint demo#2 implement]"))
    assert code == 2 and "approved" in err


def test_agent_forged_evidence_is_ignored(repo):
    approve_and_lock(repo)
    th = gate.tree_hash(repo)
    appr = gate.load_approved(repo, "demo")
    row = {"checkpoint": 2, "tree": th, "ok": True, "approval_id": appr["approval_id"], "results": []}
    with open(repo / ".claude" / "build" / "demo" / "evidence.jsonl", "a", encoding="utf-8") as f:
        f.write(json.dumps({**row, "sig": "f" * 64}) + "\n")
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed")))
    assert code == 2 and "no verify evidence" in err


def test_echo_pytest_passed_is_not_evidence(repo):
    approve_and_lock(repo)
    assert hook(repo, bash('echo "pytest passed: 12 passed in 0.1s"'))[0] == 0
    assert hook(repo, bash('python -m pytest tests/test_a.py -q'))[0] == 0
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed")))
    assert code == 2 and "no verify evidence" in err


def test_evidence_from_older_approval_does_not_carry_over(repo):
    approve_and_lock(repo)
    assert gate.cmd_verify(repo, "demo", 2)[0] == 0
    gate.cmd_approve(repo, "demo")  # a new approval (e.g. after a split)
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed")))
    assert code == 2 and "predates the current approval" in err


def test_plan_changed_after_approval_needs_reapproval(repo):
    approve_and_lock(repo)
    plan = copy.deepcopy(PLAN)
    plan["checkpoints"][1]["verify"] = ['echo "always green"']
    save_plan(repo, plan)
    code, _, err = hook(repo, agent("[checkpoint demo#2 implement]"))
    assert code == 2 and "changed" in err
    assert gate.cmd_verify(repo, "demo", 2)[0] == 1


def test_lock_tests_must_cover_the_approved_test_files(repo):
    plan = copy.deepcopy(PLAN)
    plan["checkpoints"][1]["tests"] = [{"file": "tests/test_a.py", "layer": "unit"}]
    save_plan(repo, plan)
    gate.cmd_approve(repo, "demo")
    with pytest.raises(SystemExit, match="must be locked"):
        gate.cmd_lock_tests(repo, "demo", 2, [], none=True)


def test_verify_fails_when_code_rewrites_files_while_running(repo):
    plan = copy.deepcopy(PLAN)
    sneaky = f'"{PY}" -c "open(\'app.py\',\'a\').write(\'#x\\n\')"'
    plan["checkpoints"][1]["verify"] = [sneaky]
    save_plan(repo, plan)
    approve_and_lock(repo)
    code, msg = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and "changed while" in msg


@pytest.mark.parametrize("cmd,tool", [
    # the old exemption: any segment naming build-gate.py skipped every check
    ('echo build-gate.py status > .claude/build/demo/approved.json', "Bash"),
    ('bash tq_run.sh build-gate.py status demo > .claude/build/demo/evidence.jsonl', "Bash"),
    # quoting, case, dot segments and separators
    ('echo x > ".claude/build/demo/approved.json"', "Bash"),
    ("echo x > '.claude/build/demo/approved.json'", "Bash"),
    ('echo x > .claude/./build//demo/approved.json', "Bash"),
    ('echo x > docs/../.claude/build/demo/approved.json', "Bash"),
    ('Set-Content -Path ".Claude\\Build\\demo\\approved.json" -Value x', "PowerShell"),
    ('cp /tmp/x tests/../tests/test_a.py', "Bash"),
    ('rm "tests/test_a.py"', "Bash"),
    ('true;rm tests/test_a.py', "Bash"),
    ('ls && (rm tests/test_a.py)', "Bash"),
    ('echo $(rm tests/test_a.py)', "Bash"),
    ('git status; git checkout -- "tests\\test_a.py"', "Bash"),
    ('python -c "open(\'docs/checkpoints/demo.json\',\'w\').write(\'{}\')"', "Bash"),
    ('echo x > "$STATE/approved.json"', "Bash"),
    ('rm tests/*.py', "Bash"),
    ('echo "unclosed > .claude/build/demo/approved.json', "Bash"),
    ('echo x >> .git/info/exclude', "Bash"),
    ('cat ~/.tentaqles/build-gate/hmac.key', "Bash"),
])
def test_shell_bypass_variants_blocked(repo, cmd, tool, monkeypatch):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd, tool))[0] == 2, cmd


def test_file_tool_variants_blocked(repo):
    approve_and_lock(repo)
    variants = [str(repo / "docs" / ".." / ".claude" / "build" / "demo" / "approved.json"),
                str(repo / ".git" / "info" / "exclude"),
                str(repo / ".claude" / "tq-rules.yaml")]
    for path in variants:
        payload = {"tool_name": "Write", "tool_input": {"file_path": path, "content": "{}"}}
        assert hook(repo, payload)[0] == 2, path
    key = {"tool_name": "Read", "tool_input": {"file_path": str(gate.key_path())}}
    assert hook(repo, key)[0] == 2


def test_tq_rules_protect_state_and_key(repo):
    yaml = pytest.importorskip("yaml")
    import re
    approve_and_lock(repo)
    rules = {r["id"]: r for r in yaml.safe_load((repo / ".claude" / "tq-rules.yaml").read_text(encoding="utf-8"))["rules"]}
    edit = rules["tentaqles-build/state-edit"]
    assert edit["action"] == "deny"
    for p in ("C:/r/.claude/build/demo/approved.json", "/r/.claude/tq-rules.yaml", "/r/.claude/settings.local.json"):
        assert re.search(edit["path"], p, re.IGNORECASE), p
    shell = rules["tentaqles-build/state-shell"]["command"]
    for c in ("echo x > .claude/build/demo/approved.json", "Set-Content .claude\\tq-rules.yaml x",
              "python -c \"open('.claude/build/x','w')\""):
        assert re.search(shell, c, re.IGNORECASE), c
    assert not re.search(shell, "cat .claude/build/demo/approved.json", re.IGNORECASE)
    assert re.search(rules["tentaqles-build/key-shell"]["command"], "type C:\\u\\.tentaqles\\build-gate\\hmac.key")


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
    rule = next(r for r in data["rules"] if r["id"] == "tentaqles-build/locked-tests")
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
    assert ids[0] == "app/no-console" and "tentaqles-build/locked-tests" in ids and "tentaqles-build/state-edit" in ids


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


def test_review_pass_needs_a_recorded_critic_pass(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed", review="passed")))
    assert code == 2 and "record-review" in err and "build-critic" in err


def test_review_pass_refused_while_any_reviewer_failed_or_blocked(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    gate.cmd_record_review(repo, "demo", 2, "spec-critic", "BLOCKED", {"unjudged": 1})
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed", review="passed")))
    assert code == 2 and "spec-critic" in err and "BLOCKED" in err
    # a human judgement settles it
    gate.cmd_judge(repo, "demo", 2, "spec-critic", "PASS", "checked the env by hand")
    assert hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed", review="passed")))[0] == 0


def test_review_records_are_tied_to_the_tree(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    (repo / "app.py").write_text("def f():\n    return 2\n", encoding="utf-8")
    gate.cmd_verify(repo, "demo", 2)  # fresh behaviour evidence, stale review
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed", review="passed")))
    assert code == 2 and "build-critic" in err


def test_record_review_rejects_unknown_reviewer_or_result(repo):
    approve_and_lock(repo)
    with pytest.raises(SystemExit):
        gate.cmd_record_review(repo, "demo", 2, "build-critc", "PASS", {})
    with pytest.raises(SystemExit):
        gate.cmd_record_review(repo, "demo", 2, "build-critic", "pass", {})
    with pytest.raises(SystemExit):
        gate.cmd_judge(repo, "demo", 2, "build-critic", "BLOCKED", "x")


def test_forged_review_record_is_ignored(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    path = repo / ".claude" / "build" / "demo" / "reviews.jsonl"
    tree = gate.tree_hash(repo)
    path.write_text(json.dumps({"checkpoint": 2, "reviewer": "build-critic", "result": "PASS", "tree": tree, "ts": "now"}) + "\n",
                    encoding="utf-8")
    assert gate.reviews_for(repo, "demo", 2, tree) == {}


def test_judge_and_record_review_are_human_only_for_judge(repo):
    code, _, err = hook(repo, bash("python build-gate.py judge demo 2 --reviewer spec-critic --result PASS --note ok"))
    assert code == 2 and "human" in err.lower()
    assert hook(repo, bash("python build-gate.py record-review demo 2 --reviewer build-critic --result PASS"))[0] == 0


def test_review_na_only_for_hotfix_plans(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    na = with_gate(PLAN, 2, behavior="passed", review="n/a", status="passed")
    code, _, err = hook(repo, write(plan_file(repo), na))
    assert code == 2 and "hotfix" in err
    save_plan(repo, {**PLAN, "workflow": "fix", "hotfix": True})
    gate.cmd_approve(repo, "demo")
    gate.cmd_verify(repo, "demo", 2)
    assert gate.load_approved(repo, "demo")["hotfix"] is True
    assert hook(repo, write(plan_file(repo), {**na, "workflow": "fix", "hotfix": True}))[0] == 0


def test_events_are_logged_aggregate_only(repo, tq_home):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "FAIL", {"blockers": 1, "majors": 0, "minors": 2})
    lines = [json.loads(l) for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]
    kinds = [l["kind"] for l in lines]
    assert kinds == ["verify", "review"]
    assert lines[0]["ok"] is True and lines[0]["slug"] == "demo" and lines[0]["checkpoint"] == 2
    assert lines[1]["reviewer"] == "build-critic" and lines[1]["result"] == "FAIL" and lines[1]["blockers"] == 1
    for l in lines:
        assert "app.py" not in json.dumps(l) and "tests/" not in json.dumps(l)


def test_finish_logs_a_finish_event(repo, tq_home):
    approve_and_lock(repo)
    gate.cmd_finish(repo, "demo")
    lines = [json.loads(l) for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]
    assert [l["kind"] for l in lines] == ["finish"] and lines[0]["slug"] == "demo"


def test_agent_cannot_flip_hotfix_without_reapproval(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    na = with_gate(PLAN, 2, behavior="passed", review="n/a", status="passed")
    code, _, err = hook(repo, write(plan_file(repo), {**na, "hotfix": True}))
    assert code == 2 and "hotfix" in err
    assert gate.load_approved(repo, "demo")["hotfix"] is False


def test_non_hotfix_na_review_does_not_block_stop(repo):
    own(repo)
    save_plan(repo, with_gate(PLAN, 2, behavior="failed", review="n/a"))
    code, _, err = stop(repo)
    assert code == 2 and "inconsistent" not in err  # blocked for the failed gate, not for the n/a


def test_event_workflow_is_sanitised(repo, tq_home):
    save_plan(repo, {**PLAN, "workflow": "../x"})
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    row = json.loads((tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()[0])
    assert row["workflow"] == "build"


# ---------------------------------------------------------------------------
# Definition of done
# ---------------------------------------------------------------------------

PROJECT_BLOCK = ('# Demo\n\n## Project config\n\n```json tq-project\n'
                 '{"commands": {"test": %s}, "architecture_paths": ["core/**"], "classified_paths": ["data/**"], "changelog": "CHANGELOG.md"}\n'
                 '```\n')


def _project(repo, test_cmd=PASS_CMD):
    (repo / "CLAUDE.md").write_text(PROJECT_BLOCK % json.dumps(test_cmd), encoding="utf-8")
    (repo / "CHANGELOG.md").write_text("# Changelog\n", encoding="utf-8")
    _git(repo, "add", "CLAUDE.md", "CHANGELOG.md")
    _git(repo, "commit", "-q", "-m", "project config")


def _all_passed(repo, workflow="feature"):
    plan = {**PLAN, "workflow": workflow, "spec": "docs/specs/demo.md"}
    for cid in (2, 3):
        plan = with_gate(plan, cid, behavior="passed", review="passed", status="passed")
    save_plan(repo, plan)
    return plan


def _touch_changelog(repo):
    (repo / "CHANGELOG.md").write_text("# Changelog\n- demo\n", encoding="utf-8")


def _dod_checks(repo):
    rec = gate.read_json(repo / ".claude" / "build" / "demo" / "dod.json")
    assert gate.verified(rec)
    return {c["id"]: c for c in rec["checks"]}, rec


def test_dod_passes_when_everything_is_recorded(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 0 and out.startswith("DoD demo: PASS")
    checks, rec = _dod_checks(repo)
    assert rec["ok"] and checks["tests"]["ok"] and checks["adr"]["ok"] and checks["changelog"]["ok"]
    assert checks["lint"]["detail"].startswith("skip")


def test_dod_fails_on_failing_test_command(repo):
    _project(repo, FAIL_CMD)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING tests" in out


def test_dod_requires_adr_for_architecture_paths(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    (repo / "core").mkdir()
    (repo / "core" / "x.py").write_text("x = 1\n", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING adr" in out and "core/x.py" not in out  # no file names in the summary line
    (repo / "docs" / "adr").mkdir(parents=True)
    (repo / "docs" / "adr" / "0002-core.md").write_text("# ADR-0002\n", encoding="utf-8")
    assert gate.cmd_dod(repo, "demo")[0] == 0


def test_dod_requires_changelog_and_privacy_and_migration_plan(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING changelog" in out
    _touch_changelog(repo)
    (repo / "data").mkdir()
    (repo / "data" / "customers.sql").write_text("select 1;\n", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert "MISSING privacy" in out
    (repo / "docs" / "privacy.md").write_text("# privacy\n", encoding="utf-8")
    (repo / "migrations").mkdir()
    (repo / "migrations" / "001.sql").write_text("alter table x add y int;\n", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert "MISSING migration" in out
    (repo / "docs" / "specs").mkdir(parents=True)
    (repo / "docs" / "specs" / "demo.md").write_text("# demo\n\n## Migration plan\nexpand/contract\n", encoding="utf-8")
    assert gate.cmd_dod(repo, "demo")[0] == 0


def test_dod_with_invalid_project_config_is_missing_item(repo):
    (repo / "CLAUDE.md").write_text("```json tq-project\n{bad json}\n```\n", encoding="utf-8")
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING project_config" in out and "MISSING tests" in out


def test_dod_uses_the_frozen_test_command(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    # the agent rewrites CLAUDE.md to a no-op test command after approval
    # different size on purpose: a same-size rewrite inside the index timestamp tick is invisible to git (racy-git)
    (repo / "CLAUDE.md").write_text(PROJECT_BLOCK % json.dumps(FAIL_CMD) + "<!-- edited -->\n", encoding="utf-8")
    assert gate.cmd_dod(repo, "demo")[0] == 0  # frozen PASS_CMD ran, not the edited one


def test_dod_override_requires_reason_and_is_logged(repo, tq_home):
    _project(repo, FAIL_CMD)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    with pytest.raises(SystemExit):
        gate.cmd_dod(repo, "demo", override="   ")
    code, out = gate.cmd_dod(repo, "demo", override="release train leaves now; tests fixed in #42")
    assert code == 0 and "OVERRIDDEN" in out
    learn = (repo / "docs" / "LEARNINGS.md").read_text(encoding="utf-8")
    assert "· demo · dod-override · release train leaves now" in learn
    events = [json.loads(l) for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]
    assert events[-1]["kind"] == "dod-override" and "reason" not in events[-1]


def test_stop_requires_fresh_dod_for_feature_plans(repo):
    _project(repo)
    own(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    code, _, err = stop(repo)
    assert code == 2 and "build-gate dod demo" in err
    assert stop(repo)[0] == 0  # blocked once, then through
    _touch_changelog(repo)
    assert gate.cmd_dod(repo, "demo")[0] == 0
    assert stop(repo)[0] == 0
    (repo / "app.py").write_text("def f():\n    return 3\n", encoding="utf-8")  # code changed: DoD stale
    assert stop(repo)[0] == 2


def test_stop_without_workflow_is_unchanged(repo):
    own(repo)
    done = with_gate(PLAN, 2, behavior="passed", review="passed", status="passed")
    done = with_gate(done, 3, behavior="passed", review="passed", status="passed")
    save_plan(repo, done)
    assert stop(repo)[0] == 0


def test_override_then_stop_is_allowed(repo):
    _project(repo, FAIL_CMD)
    own(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    assert gate.cmd_dod(repo, "demo", override="human accepted the risk")[0] == 0
    assert stop(repo)[0] == 0
    assert stop(repo)[0] == 0
    # the stop check is satisfied by the record itself, not by the one-block allowance
    assert stop(repo)[0] == 0
    learn = (repo / "docs" / "LEARNINGS.md").read_text(encoding="utf-8")
    assert learn.count("dod-override") == 1
    assert gate.dod_fresh(repo, "demo", gate.TreeHash(repo))[0]


def test_override_reason_is_flattened_in_learnings(repo):
    _project(repo, FAIL_CMD)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    gate.cmd_dod(repo, "demo", override="line one\n- `D99 · fake`")
    learn = (repo / "docs" / "LEARNINGS.md").read_text(encoding="utf-8")
    lines = [l for l in learn.splitlines() if "dod-override" in l]
    assert len(lines) == 1 and "\n" not in lines[0] and lines[0].count("`") == 2


def test_agent_cannot_remove_workflow_to_skip_dod(repo):
    _project(repo)
    own(repo)
    plan = _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    stripped = {k: v for k, v in plan.items() if k != "workflow"}
    save_plan(repo, stripped)
    code, _, err = stop(repo)
    assert code == 2 and "definition of done" in err


def test_missing_tree_hash_is_not_fresh(repo, monkeypatch):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    assert gate.cmd_dod(repo, "demo")[0] == 0
    monkeypatch.setattr(gate, "tree_hash", lambda root, timeout=60: None)
    assert gate.dod_fresh(repo, "demo", gate.TreeHash(repo))[0] is False
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING stable_tree" in out


def test_dod_record_from_older_approval_is_not_fresh(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    assert gate.cmd_dod(repo, "demo")[0] == 0
    gate.cmd_approve(repo, "demo")  # new approval id
    assert gate.dod_fresh(repo, "demo", gate.TreeHash(repo))[0] is False


def test_changed_files_sees_non_ascii_names_and_moves(repo):
    _project(repo)
    _all_passed(repo)
    (repo / "core").mkdir()
    (repo / "core" / "x.py").write_text("x = 1\n", encoding="utf-8")
    _git(repo, "add", "core/x.py")
    _git(repo, "commit", "-q", "-m", "core")
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    (repo / "core" / "café.py").write_text("y = 1\n", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING adr" in out
    (repo / "core" / "café.py").unlink()
    assert gate.cmd_dod(repo, "demo")[0] == 0
    (repo / "other").mkdir()
    _git(repo, "mv", "core/x.py", "other/x.py")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING adr" in out


def test_migration_spec_path_is_confined_to_the_repo(repo, tmp_path):
    _project(repo)
    outside = tmp_path / "evil.md"
    outside.write_text("## Migration plan\n", encoding="utf-8")
    plan = _all_passed(repo)
    save_plan(repo, {**plan, "spec": str(outside)})
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    (repo / "migrations").mkdir()
    (repo / "migrations" / "001.sql").write_text("select 1;\n", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING migration" in out
    save_plan(repo, {**plan, "spec": 7})
    assert "MISSING migration" in gate.cmd_dod(repo, "demo")[1]


def test_hook_refuses_agent_dod_override_but_allows_plain_dod(repo):
    code, _, err = hook(repo, bash("python build-gate.py dod demo --override x"))
    assert code == 2 and "human" in err
    assert hook(repo, bash("python build-gate.py dod demo"))[0] == 0


def test_agent_cannot_pad_plan_to_skip_dod(repo):
    _project(repo)
    own(repo)
    plan = _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    padded = copy.deepcopy(plan)
    padded["checkpoints"].append(cp(4))
    save_plan(repo, padded)
    code, _, err = stop(repo)
    assert code == 2 and "definition of done" in err


def test_tree_hash_sees_same_size_edit_in_same_second(repo):
    for i in range(3):
        a, b = f"v = {i}0\n", f"v = {i}1\n"
        (repo / "app.py").write_text(a, encoding="utf-8")
        _git(repo, "add", "app.py")  # refreshes the real index right now: the edit below is racy
        h1 = gate.tree_hash(repo)
        (repo / "app.py").write_text(b, encoding="utf-8")
        assert gate.tree_hash(repo) != h1


def test_hook_refuses_abbreviated_dod_override(repo):
    for flag in ("--over", "--o", "--overr=x"):
        code, _, err = hook(repo, bash(f"python build-gate.py dod demo {flag} x"))
        assert code == 2 and "human" in err


def _decisions(repo):
    path = repo / ".claude" / "build" / "demo" / "decisions.jsonl"
    return [json.loads(l) for l in path.read_text(encoding="utf-8").splitlines()] if path.exists() else []


def test_ask_creates_pending_and_is_idempotent(repo):
    out1 = gate.cmd_ask(repo, "demo", "plan-approval", "3 checkpoints, verify commands frozen on approve", ["approve", "edit", "stop"])
    out2 = gate.cmd_ask(repo, "demo", "plan-approval", "same question again", ["approve", "edit", "stop"])
    assert out1 == out2 and out1.startswith("decision ")
    pend = gate.pending_decisions(repo, "demo")
    assert len(pend) == 1 and pend[0]["kind"] == "plan-approval" and pend[0]["status"] == "pending"
    assert all(gate.verified(d) for d in _decisions(repo))


def test_answer_records_and_plan_approval_freezes_plan(repo):
    did = gate.cmd_ask(repo, "demo", "plan-approval", "approve?", ["approve", "edit", "stop"]).split()[1]
    gate.cmd_answer(repo, "demo", did, "approve", "looks right", actor="renato")
    assert gate.pending_decisions(repo, "demo") == []
    rec = [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]
    assert rec["choice"] == "approve" and rec["actor"] == "renato"
    assert gate.load_approved(repo, "demo") is not None  # approve ran


def test_terminal_approve_answers_the_pending_decision(repo):
    did = gate.cmd_ask(repo, "demo", "plan-approval", "approve?", ["approve", "edit", "stop"]).split()[1]
    gate.cmd_approve(repo, "demo")
    rec = [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]
    assert rec["actor"] == "terminal" and rec["choice"] == "approve"


def test_judge_decision_records_reviewer_judgement(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "privacy-reviewer", "BLOCKED", {"unjudged": 1})
    did = gate.cmd_ask(repo, "demo", "judge", "U1 retention of the audit log?", ["PASS", "FAIL"], cid=2, reviewer="privacy-reviewer").split()[1]
    gate.cmd_answer(repo, "demo", did, "PASS", "24m", actor="renato")
    recs = gate.reviews_for(repo, "demo", 2, gate.tree_hash(repo))
    assert recs["privacy-reviewer"]["result"] == "PASS" and recs["privacy-reviewer"]["human"] is True


def test_hub_token_allows_non_interactive_human_actions(repo, monkeypatch):
    save_plan(repo, PLAN)
    monkeypatch.setenv("TQ_BUILD_ACTOR", "renato")
    monkeypatch.setenv("TQ_BUILD_TOKEN", gate.mint_hub_token("approve", "demo", "renato"))
    assert gate.hub_actor_ok("approve", "demo") == "renato"
    assert gate.hub_actor_ok("finish", "demo") is None   # a token is per action
    assert gate.hub_actor_ok("approve", "other") is None # and per slug
    code = gate.main(["--root", str(repo), "approve", "demo"])
    assert code == 0 and gate.load_approved(repo, "demo") is not None


def test_forged_hub_token_is_refused(repo, monkeypatch):
    import hashlib, hmac as _hmac
    monkeypatch.setenv("TQ_BUILD_ACTOR", "mallory")
    monkeypatch.setenv("TQ_BUILD_TOKEN", _hmac.new(b"guess", b"hub:approve:demo:mallory", hashlib.sha256).hexdigest())
    assert gate.hub_actor_ok("approve", "demo") is None
    code, _, err = hook(repo, bash("python build-gate.py answer demo d1 --choice approve --actor x"))
    assert code == 2 and "human" in err.lower()


def test_stop_allowed_while_a_decision_is_pending(repo):
    own(repo)
    save_plan(repo, with_gate(PLAN, 2, behavior="failed"))
    assert stop(repo)[0] == 2                      # unproven checkpoint blocks once
    gate.cmd_ask(repo, "demo", "question", "which DB?", ["postgres", "sqlite"], cid=2)
    assert stop(repo)[0] == 0                      # waiting on a human: the turn may end
    assert stop(repo)[0] == 0


def test_state_reports_dod_freshness(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    assert gate.cmd_state(repo, "demo")["dod_fresh"] is False  # no record yet
    code, _ = gate.cmd_dod(repo, "demo")
    assert code == 0
    assert gate.cmd_state(repo, "demo")["dod_fresh"] is True
    (repo / "stray.txt").write_text("changed", encoding="utf-8")
    assert gate.cmd_state(repo, "demo")["dod_fresh"] is False


def test_state_json_shape(repo):
    _project(repo)
    save_plan(repo, {**PLAN, "workflow": "feature", "task": {"hub_id": "t-42", "external": {"provider": "linear", "id": "ACME-7", "url": "https://linear.app/x"}}})
    gate.cmd_approve(repo, "demo")
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    gate.cmd_ask(repo, "demo", "question", "ok?", ["yes", "no"], cid=2)
    st = gate.cmd_state(repo, "demo")
    assert st["slug"] == "demo" and st["workflow"] == "feature" and st["task"]["external"]["id"] == "ACME-7"
    assert st["approved"] is True and st["finished"] is False and st["dod"] is None
    cp2 = [c for c in st["checkpoints"] if c["id"] == 2][0]
    assert cp2["evidence"] == "fresh" and cp2["reviews"] == {"build-critic": "PASS"}
    assert st["pending_decisions"][0]["kind"] == "question"
    assert "app.py" not in json.dumps(st)  # names of checkpoints and reviewers only, no file paths or text
    out = gate.main(["--root", str(repo), "state", "demo", "--json"])
    assert out == 0


# --- fix round 1 -----------------------------------------------------------


def _set_token(monkeypatch, action, slug="demo", actor="renato", mint_actor=None):
    monkeypatch.setenv("TQ_BUILD_ACTOR", actor)
    monkeypatch.setenv("TQ_BUILD_TOKEN", gate.mint_hub_token(action, slug, mint_actor or actor))


def test_hub_token_comparison_is_exercised_with_a_key_present(repo, monkeypatch):
    import hashlib, hmac as _hmac
    gate._key(create=True)  # a real build has a key; without one hub_actor_ok exits early
    assert gate._key() is not None
    monkeypatch.setenv("TQ_BUILD_ACTOR", "mallory")
    monkeypatch.setenv("TQ_BUILD_TOKEN", _hmac.new(b"guess", b"hub:approve:demo:mallory", hashlib.sha256).hexdigest())
    assert gate.hub_actor_ok("approve", "demo") is None            # forged
    _set_token(monkeypatch, "approve", actor="mallory", mint_actor="renato")
    assert gate.hub_actor_ok("approve", "demo") is None            # token of another actor
    monkeypatch.setenv("TQ_BUILD_TOKEN", "")
    assert gate.hub_actor_ok("approve", "demo") is None            # empty
    _set_token(monkeypatch, "approve")
    assert gate.hub_actor_ok("approve", "demo") == "renato"        # correct


def test_non_ascii_token_fails_closed_without_traceback(repo, monkeypatch, capsys):
    save_plan(repo, PLAN)
    gate._key(create=True)
    monkeypatch.setenv("TQ_BUILD_ACTOR", "renato")
    monkeypatch.setenv("TQ_BUILD_TOKEN", "é" * 64)
    assert gate.hub_actor_ok("approve", "demo") is None
    assert gate.main(["--root", str(repo), "approve", "demo"]) == 2
    assert gate.load_approved(repo, "demo") is None


def test_hub_token_message_is_unambiguous(repo, monkeypatch):
    tok = gate.mint_hub_token("approve", "demo", "x:y")
    monkeypatch.setenv("TQ_BUILD_ACTOR", "y")
    monkeypatch.setenv("TQ_BUILD_TOKEN", tok)
    assert gate.hub_actor_ok("approve", "demo:x") is None
    monkeypatch.setenv("TQ_BUILD_ACTOR", "x:y")
    assert gate.hub_actor_ok("approve", "demo") == "x:y"


def test_hub_actor_cannot_be_overwritten_with_actor_flag(repo, monkeypatch):
    did = gate.cmd_ask(repo, "demo", "question", "ok?", ["yes", "no"]).split()[1]
    _set_token(monkeypatch, "answer")
    code = gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "yes", "--actor", "mallory"])
    assert code == 2 and gate.pending_decisions(repo, "demo")
    assert not [d for d in _decisions(repo) if d["status"] == "answered"]
    code = gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "yes", "--actor", "renato"])
    assert code == 0
    assert [d for d in _decisions(repo) if d["status"] == "answered"][0]["actor"] == "renato"


TOKEN_BRANCHES = [
    ("approve", ["approve", "demo"]),
    ("finish", ["finish", "demo"]),
    ("unlock-tests", ["unlock-tests", "demo", "tests/test_a.py"]),
    ("judge", ["judge", "demo", "2", "--reviewer", "build-critic", "--result", "PASS", "--note", "n"]),
    ("answer", ["answer", "demo", "d1", "--choice", "yes"]),
    ("dod", ["dod", "demo", "--override", "because"]),
]


@pytest.mark.parametrize("action,argv", TOKEN_BRANCHES, ids=[a for a, _ in TOKEN_BRANCHES])
def test_every_token_branch_checks_its_own_action(repo, monkeypatch, action, argv):
    save_plan(repo, PLAN)
    asked = []
    monkeypatch.setattr(gate, "human_confirm", lambda *a, **k: asked.append(a) or False)
    for other in {a for a, _ in TOKEN_BRANCHES} - {action}:
        asked.clear()
        _set_token(monkeypatch, other)
        assert gate.main(["--root", str(repo), *argv]) == 2, other   # wrong-action token: falls to the human gate
        assert asked, other
    asked.clear()
    _set_token(monkeypatch, action)
    try:
        gate.main(["--root", str(repo), *argv])
    except SystemExit:
        pass                                   # the command itself may fail; the human gate must not be asked
    assert not asked


def _active_build(repo):
    save_plan(repo, PLAN)
    gate.cmd_approve(repo, "demo")


@pytest.mark.parametrize("cmd", [
    'python -c "from tentaqles.workflow.gate import mint_hub_token"',
    'python -c "import tentaqles.workflow.gate as g; g.cmd_answer(1,2,3,4,5,6)"',
    'python -c "from tentaqles.workflow import gate; gate.cmd_approve(1, 2)"',
    "python plugin/tentaqles/workflow/gate.py approve demo",
    "python -m tentaqles.workflow.gate approve demo",
    "echo $TQ_BUILD_TOKEN",
])
def test_hook_refuses_in_process_gate_use_while_build_active(repo, cmd):
    _active_build(repo)
    code, _, err = hook(repo, bash(cmd))
    assert code == 2 and "human" in err.lower()


def test_hook_allows_gate_cli_state_and_mentions_without_build(repo):
    _active_build(repo)
    assert hook(repo, bash("python scripts/build-gate.py state demo --json"))[0] == 0


def test_hook_refuses_gate_mentions_even_before_approval(repo):
    # C1 (final review): an unapproved plan is exactly what the agent would approve itself
    assert hook(repo, bash('python -c "from tentaqles.workflow.gate import mint_hub_token"'))[0] == 2


# --- fix round 1: override lifecycle, answered decisions in state ---------------------------

def _dod_ready(repo, test_cmd=FAIL_CMD):
    _project(repo, test_cmd)
    own(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")


def _dod_rec(repo):
    return gate.read_json(repo / ".claude" / "build" / "demo" / "dod.json")


def test_plain_dod_keeps_override_on_same_tree_and_drops_it_on_change(repo):
    _dod_ready(repo)
    assert gate.cmd_dod(repo, "demo", override="human accepted")[0] == 0
    code, _ = gate.cmd_dod(repo, "demo")
    rec = _dod_rec(repo)
    assert gate.verified(rec) and rec["override"] == "human accepted" and rec["ok"] is True
    assert code == 0
    assert any(c["ok"] is False for c in rec["checks"])  # the checks were re-evaluated
    assert gate.dod_fresh(repo, "demo", gate.TreeHash(repo))[0]
    (repo / "app.py").write_text("def f():\n    return 9\n", encoding="utf-8")
    assert gate.cmd_dod(repo, "demo")[0] == 1
    rec = _dod_rec(repo)
    assert rec["override"] is None and rec["ok"] is False


def test_answer_override_question_runs_the_dod_override(repo, monkeypatch):
    _dod_ready(repo)
    did = gate.cmd_ask(repo, "demo", "question", "override: tests cannot pass", ["override", "stop"]).split()[1]
    with pytest.raises(SystemExit) as e:
        gate.cmd_answer(repo, "demo", did, "override", "", actor="renato")
    assert e.value.code == 2 and gate.pending_decisions(repo, "demo")
    gate.cmd_answer(repo, "demo", did, "override", "accepted by renato", actor="renato")
    rec = _dod_rec(repo)
    assert rec["override"] == "accepted by renato" and rec["ok"] is True
    assert gate.pending_decisions(repo, "demo") == []
    ans = [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"]
    assert len(ans) == 1 and ans[0]["actor"] == "renato"
    # a non-override choice only records
    did2 = gate.cmd_ask(repo, "demo", "question", "override again?", ["override", "stop"], cid=1).split()[1]
    gate.cmd_answer(repo, "demo", did2, "stop", "", actor="renato")
    assert gate.pending_decisions(repo, "demo") == []


def test_answer_result_approve_runs_finish(repo):
    approve_and_lock(repo)
    did = gate.cmd_ask(repo, "demo", "result", "ship it?", ["approve", "request-changes", "stop"]).split()[1]
    gate.cmd_answer(repo, "demo", did, "request-changes", "more", actor="renato")
    assert not gate.load_session(repo, "demo").get("finished")
    did = gate.cmd_ask(repo, "demo", "result", "ship it?", ["approve", "request-changes", "stop"]).split()[1]
    gate.cmd_answer(repo, "demo", did, "approve", "", actor="renato")
    assert gate.load_session(repo, "demo").get("finished")
    assert [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]["actor"] == "renato"


def test_terminal_dod_override_answers_pending_override_question(repo):
    _dod_ready(repo)
    did = gate.cmd_ask(repo, "demo", "question", "override: x", ["override", "stop"]).split()[1]
    other = gate.cmd_ask(repo, "demo", "question", "unrelated", ["a", "b"], cid=7).split()[1]
    gate.cmd_dod(repo, "demo", override="human accepted")
    pend = [p["id"] for p in gate.pending_decisions(repo, "demo")]
    assert did not in pend and other in pend
    rec = [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]
    assert rec["choice"] == "override" and rec["actor"] == "terminal"


def test_agent_still_cannot_run_answer_and_hub_token_can(repo, monkeypatch):
    code, _, err = hook(repo, bash("python build-gate.py answer demo d1 --choice override --note x"))
    assert code == 2 and "human" in err.lower()
    _dod_ready(repo)
    did = gate.cmd_ask(repo, "demo", "question", "override: x", ["override", "stop"]).split()[1]
    _set_token(monkeypatch, "answer")
    code = gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "override", "--note", "hub says ok"])
    assert code == 0
    rec = _dod_rec(repo)
    assert rec["override"] == "hub says ok" and rec["ok"] is True
    did = gate.cmd_ask(repo, "demo", "result", "ship?", ["approve", "stop"]).split()[1]
    assert gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "approve"]) == 0
    assert gate.load_session(repo, "demo").get("finished")


def test_state_lists_answered_decisions(repo):
    did = gate.cmd_ask(repo, "demo", "question", "which db?", ["a", "b"]).split()[1]
    st = gate.cmd_state(repo, "demo")
    assert [d["status"] for d in st["decisions"]] == ["pending"] and st["pending_decisions"][0]["id"] == did
    gate.cmd_answer(repo, "demo", did, "b", "because", actor="renato")
    st = gate.cmd_state(repo, "demo")
    assert st["pending_decisions"] == []
    d = [x for x in st["decisions"] if x["id"] == did][0]
    assert d["status"] == "answered" and d["choice"] == "b" and d["note"] == "because" and d["actor"] == "renato" and d["answered_at"]


def test_stop_message_does_not_tell_agent_to_run_override(repo):
    _dod_ready(repo)
    code, _, err = stop(repo)
    assert code == 2 and "--override" in err and "human" in err.lower()
    assert "when the human has said so" not in err


# --- fix round 2: explicit override questions, visible consequences -------------------------

def test_non_override_wording_question_does_not_waive_dod(repo):
    _dod_ready(repo)
    gate.cmd_dod(repo, "demo")
    before = (repo / ".claude" / "build" / "demo" / "dod.json").read_text(encoding="utf-8")
    did = gate.cmd_ask(repo, "demo", "question", "Let the env PORT override the default?", ["override", "keep"]).split()[1]
    gate.cmd_answer(repo, "demo", did, "override", "yes, env wins", actor="renato")
    assert (repo / ".claude" / "build" / "demo" / "dod.json").read_text(encoding="utf-8") == before
    assert _dod_rec(repo)["override"] is None
    assert "dod-override" not in (repo / "docs" / "LEARNINGS.md").read_text(encoding="utf-8") if (repo / "docs" / "LEARNINGS.md").exists() else True
    assert gate.pending_decisions(repo, "demo") == []
    # and a terminal override does not sweep such a question up either
    did2 = gate.cmd_ask(repo, "demo", "question", "Env PORT override?", ["override", "keep"], cid=5).split()[1]
    gate.cmd_dod(repo, "demo", override="human accepted")
    assert did2 in [p["id"] for p in gate.pending_decisions(repo, "demo")]


def test_override_prefix_is_case_insensitive_and_waives(repo):
    _dod_ready(repo)
    did = gate.cmd_ask(repo, "demo", "question", "  OVERRIDE: tests cannot pass", ["override", "stop"]).split()[1]
    gate.cmd_answer(repo, "demo", did, "override", "accepted", actor="renato")
    assert _dod_rec(repo)["override"] == "accepted" and _dod_rec(repo)["ok"] is True


def test_answer_confirm_names_the_consequence(repo, monkeypatch):
    _dod_ready(repo)
    gate.cmd_dod(repo, "demo")  # records the MISSING items (failing tests)
    did = gate.cmd_ask(repo, "demo", "question", "override: tests cannot pass", ["override", "stop"]).split()[1]
    seen = []
    monkeypatch.setattr(gate, "human_confirm", lambda action, *a, **k: seen.append(action) or True)
    code = gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "override", "--note", "ok"])
    assert code == 0
    assert "accepts the missing definition-of-done items for demo" in seen[0] and "tests" in seen[0]
    did = gate.cmd_ask(repo, "demo", "result", "ship?", ["approve", "stop"]).split()[1]
    gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "approve"])
    assert "finishes checkpoint/plan demo" in seen[1]


def test_hub_answer_records_the_consequence_in_the_note(repo, monkeypatch):
    _dod_ready(repo)
    gate.cmd_dod(repo, "demo")
    did = gate.cmd_ask(repo, "demo", "question", "override: tests cannot pass", ["override", "stop"]).split()[1]
    _set_token(monkeypatch, "answer")
    assert gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "override", "--note", "hub ok"]) == 0
    ans = [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]
    assert "accepts the missing definition-of-done items" in ans["note"] and "hub ok" in ans["note"]
    assert _dod_rec(repo)["override"] == "hub ok"


def test_failed_override_is_recorded_but_exit_nonzero(repo, capsys, monkeypatch):
    _dod_ready(repo)
    did = gate.cmd_ask(repo, "demo", "question", "override: x", ["override", "stop"]).split()[1]
    (repo / ".claude" / "build" / "demo" / "approved.json").unlink()  # cmd_dod: plan isn't approved
    code, msg = gate.answer_decision(repo, "demo", did, "override", "why", "renato")
    assert code == 1 and "did NOT apply" in msg
    assert [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]["choice"] == "override"
    # the CLI half: a confirmed human answer whose override fails exits 1, not 0 and not the refusal 2
    monkeypatch.setattr(gate, "human_confirm", lambda *a, **k: True)
    did = gate.cmd_ask(repo, "demo", "question", "override: y", ["override", "stop"], cid=2).split()[1]
    assert gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "override", "--note", "why"]) == 1
    assert "did NOT apply" in capsys.readouterr().out
    assert [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]["choice"] == "override"


def test_kept_override_logs_an_event(repo, tq_home):
    _dod_ready(repo)
    gate.cmd_dod(repo, "demo", override="human accepted")
    gate.cmd_dod(repo, "demo")
    events = [json.loads(l) for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]
    kinds = [e["kind"] for e in events]
    assert "dod-override-kept" in kinds and kinds[-1] == "dod"
    kept = [e for e in events if e["kind"] == "dod-override-kept"][0]
    assert kept["ok"] is True and "reason" not in kept


# --- final fix wave ------------------------------------------------------------------------


@pytest.fixture
def set_up(repo):
    """A repo after `build-gate setup`, as in a real worktree (state dir git-excluded)."""
    gate.cmd_setup(repo)
    return repo


def test_tree_hash_works_after_setup_and_a_checkpoint_verifies(set_up):
    (set_up / ".claude" / "build" / "demo").mkdir(parents=True, exist_ok=True)
    (set_up / ".claude" / "build" / "demo" / "x.json").write_text("{}", encoding="utf-8")
    assert gate.tree_hash(set_up) is not None
    approve_and_lock(set_up)
    code, msg = gate.cmd_verify(set_up, "demo", 2)
    assert code == 0 and "PASS" in msg, msg
    assert hook(set_up, write(plan_file(set_up), with_gate(PLAN, 2, behavior="passed")))[0] == 0


def test_tree_hash_ignores_the_state_dir_after_setup(set_up):
    h1 = gate.tree_hash(set_up)
    (set_up / ".claude" / "build" / "demo").mkdir(parents=True, exist_ok=True)
    (set_up / ".claude" / "build" / "demo" / "evidence.jsonl").write_text("x\n", encoding="utf-8")
    assert gate.tree_hash(set_up) == h1
    (set_up / "app.py").write_text("def f():\n    return 2\n", encoding="utf-8")
    assert gate.tree_hash(set_up) != h1


def test_stop_path_tree_hash_timeout_is_under_the_hook_timeout(repo, monkeypatch):
    _project(repo)
    own(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    assert gate.cmd_dod(repo, "demo")[0] == 0
    seen = []
    real = gate.tree_hash
    monkeypatch.setattr(gate, "tree_hash", lambda root, timeout=60: seen.append(timeout) or real(root, timeout=timeout))
    assert stop(repo)[0] == 0
    assert seen and max(seen) <= 10


# C1: the gate-module refusal holds before the first approval (plan or state dir exists)

@pytest.mark.parametrize("cmd", [
    'python -c "from pathlib import Path; from tentaqles.workflow import gate; gate.cmd_approve(Path(\'.\'),\'demo\')"',
    'python -c "import tentaqles.workflow.gate as g; g.cmd_answer(1,2,3,4,5,6)"',
    "python -m tentaqles.workflow.gate approve demo",
])
def test_gate_module_refused_before_approval(repo, cmd):
    assert gate.load_approved(repo, "demo") is None
    code, _, err = hook(repo, bash(cmd))
    assert code == 2 and "human" in err.lower(), err
    assert gate.load_approved(repo, "demo") is None


def test_gate_module_refused_with_only_a_state_dir(tmp_path):
    root = tmp_path / "p"
    (root / ".claude" / "build" / "other").mkdir(parents=True)
    _git(root, "init", "-q")
    code, _, _ = hook(root, bash('python -c "from tentaqles.workflow.gate import mint_hub_token"'))
    assert code == 2


def test_gate_mentions_allowed_once_every_build_is_finished(repo):
    _active_build(repo)
    gate.cmd_finish(repo, "demo")
    assert hook(repo, bash('python -c "from tentaqles.workflow.gate import mint_hub_token"'))[0] == 0


# C2: file content and target paths, split strings, key reads by any spelling

GATE_SRC = "from tentaqles.workflow.gate import cmd_approve, mint_hub_token\nprint(mint_hub_token('approve','demo','me'))\n"


@pytest.mark.parametrize("approved", [False, True])
def test_write_of_gate_importing_script_refused(repo, approved):
    if approved:
        _active_build(repo)
    payload = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "x.py"), "content": GATE_SRC}}
    code, _, err = hook(repo, payload)
    assert code == 2 and "human" in err.lower()


@pytest.mark.parametrize("content", [
    "import importlib as i\ng = i.import_module('tentaqles.workflow.' + 'ga' + 'te')\n",
    "g = __import__('tentaqles.workflow', fromlist=['gate'])\n",
    "import os\nprint(open(os.path.expanduser('~') + '/.tentaqles/build-' + 'gate/hmac.key', 'rb').read())\n",
    "p = os.path.join(os.environ['TQ_HOME'], 'build-gate', 'hm' 'ac.key')\n",
])
def test_write_content_with_split_strings_refused(repo, content):
    payload = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "x.py"), "content": content}}
    assert hook(repo, payload)[0] == 2, content


def test_edit_multiedit_notebook_content_scanned(repo):
    (repo / "x.py").write_text("A = 1\nB = 2\n", encoding="utf-8")
    edit = {"tool_name": "Edit", "tool_input": {"file_path": str(repo / "x.py"), "old_string": "A = 1",
                                                 "new_string": "from tentaqles.workflow import gate"}}
    assert hook(repo, edit)[0] == 2
    # split across two edits of one MultiEdit: the resulting file text is scanned
    multi = {"tool_name": "MultiEdit", "tool_input": {"file_path": str(repo / "x.py"), "edits": [
        {"old_string": "A = 1", "new_string": "import importlib; m = importlib.import_module('tentaqles.workflow.ga"},
        {"old_string": "B = 2", "new_string": "te')"}]}}
    assert hook(repo, multi)[0] == 2
    # an earlier edit left half of it in the file: the next edit completes it
    (repo / "x.py").write_text("m = 'tentaqles.workflow.ga'\nB = 2\n", encoding="utf-8")
    edit2 = {"tool_name": "Edit", "tool_input": {"file_path": str(repo / "x.py"), "old_string": "'\nB = 2", "new_string": "' + 'te'"}}
    assert hook(repo, edit2)[0] == 2
    nb = {"tool_name": "NotebookEdit", "tool_input": {"notebook_path": str(repo / "n.ipynb"),
                                                       "new_source": "from tentaqles.workflow.gate import sign"}}
    assert hook(repo, nb)[0] == 2


def test_ordinary_writes_still_allowed(repo):
    _active_build(repo)
    ok = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "app.py"), "content": "def f():\n    return 2\n"}}
    assert hook(repo, ok)[0] == 0
    doc = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "docs" / "notes.md"),
                                                 "content": "Run `build-gate verify demo 2`, then `build-gate status demo`.\n"}}
    assert hook(repo, doc)[0] == 0


def test_writes_under_the_gate_code_are_refused(repo):
    for target in (repo / "plugin" / "tentaqles" / "workflow" / "gate.py",
                   gate.plugin_root() / "tentaqles" / "workflow" / "gate.py",
                   gate.plugin_root() / "scripts" / "build-gate.py"):
        payload = {"tool_name": "Write", "tool_input": {"file_path": str(target), "content": "x = 1\n"}}
        assert hook(repo, payload)[0] == 2, target
        assert hook(repo, bash(f'cp /tmp/x "{target}"'))[0] == 2, target


@pytest.mark.parametrize("cmd,tool", [
    ('python -c "import importlib as i; g=i.import_module(\'tentaqles.workflow.\'+\'ga\'+\'te\'); print(g._key())"', "Bash"),
    ('python -c "print(open(__import__(\'os\').path.expanduser(\'~\')+\'/.tentaqles/build-\'+\'gate/hmac.key\',\'rb\').read())"', "Bash"),
    ("cat $(echo ~/.tentaqles/build-ga)te/hmac.key", "Bash"),
    ('cat ~/.tentaqles/build-gate/"hm"ac.key', "Bash"),
    (r"type %USERPROFILE%\.tentaqles\build-gate\hmac.key", "PowerShell"),
    (r"Get-Content (Join-Path $HOME '.tentaqles\build-gate\hmac.key')", "PowerShell"),
    ("Get-Content (Join-Path $env:TQ_HOME 'build-gate' 'hm' + 'ac.key')", "PowerShell"),
    ("ls ~/.tentaqles/build-gate/", "Bash"),
])
def test_shell_key_reads_and_split_gate_imports_refused(repo, cmd, tool):
    code, _, err = hook(repo, bash(cmd, tool))
    assert code == 2, cmd


# C4: git counts as a writer of the gate's state

def test_git_replay_of_signed_state_refused_at_the_first_step(repo):
    gate.cmd_approve(repo, "demo")
    code, _, err = hook(repo, bash("git add -f .claude/build/demo/approved.json"))
    assert code == 2 and ".claude/build" in err
    gate.cmd_lock_tests(repo, "demo", 2, [str(repo / "tests" / "test_a.py")], none=False)
    assert hook(repo, bash("git checkout -- .claude/build/demo/approved.json"))[0] == 2
    assert gate._locked_paths(repo) == {"tests/test_a.py": "demo"}


@pytest.mark.parametrize("cmd", [
    "git add .claude/build/demo/approved.json",
    "git add -f -- .claude/build",
    "git add --force .",
    "git add .claude",
    "git checkout HEAD~1 -- .claude/build/demo/approved.json",
    "git restore --source=HEAD~1 -- .claude/build",
    "git rm --cached .claude/build/demo/approved.json",
    "git mv .claude/build/demo/approved.json x.json",
    "git stash push -- .claude/build",
    "git stash --all",
    "git stash push --include-ignored",
    "git update-index --add .claude/build/demo/approved.json",
    "git apply --directory=.claude/build x.patch",
    "git clean -fdX",
    "git clean -fdx",
    "git -C .claude/build status",
    "git cat-file -p 0123abc > .claude/build/demo/approved.json",
    "git show HEAD:.claude/build/demo/approved.json",
    "rm -rf .claude",
    "Remove-Item -Recurse .claude",
])
def test_git_and_tree_commands_naming_state_refused(repo, cmd):
    gate.cmd_approve(repo, "demo")
    assert hook(repo, bash(cmd))[0] == 2, cmd


@pytest.mark.parametrize("cmd", [
    "git add docs/checkpoints/demo.json app.py",
    "git status",
    "git commit -m 'feat: x'",
    "git clean -fd",
    "git stash",
    "git diff HEAD -- app.py",
    "git add -A",
])
def test_ordinary_git_still_allowed(repo, cmd):
    gate.cmd_approve(repo, "demo")
    assert hook(repo, bash(cmd))[0] == 0, cmd


def test_tq_state_shell_rule_covers_git(repo):
    import re
    rule = next(r for r in gate.state_rules() if r["id"].endswith("state-shell"))["command"]
    for c in ("git add -f .claude/build/demo/approved.json", "git checkout -- .claude/build/demo/approved.json"):
        assert re.search(rule, c, re.IGNORECASE), c
    assert not re.search(rule, "git add docs/checkpoints/demo.json", re.IGNORECASE)


# C3: the frozen checkpoint ids in the signed approval are the source of truth

def _drop(plan, cid):
    p = copy.deepcopy(plan)
    p["checkpoints"] = [c for c in p["checkpoints"] if c["id"] != cid]
    return p


def test_plan_edit_dropping_or_renumbering_a_frozen_id_refused(repo):
    gate.cmd_approve(repo, "demo")
    code, _, err = hook(repo, write(plan_file(repo), _drop(PLAN, 3)))
    assert code == 2 and "#3" in err, err
    renum = copy.deepcopy(PLAN)
    renum["checkpoints"][2]["id"] = 4
    assert hook(repo, write(plan_file(repo), renum))[0] == 2
    # adding a checkpoint is fine (it needs approval before it can pass)
    added = copy.deepcopy(PLAN)
    added["checkpoints"].append(cp(4))
    assert hook(repo, write(plan_file(repo), added))[0] == 0


def test_replan_allowed_once_a_plan_approval_is_pending(repo):
    gate.cmd_approve(repo, "demo")
    gate.cmd_ask(repo, "demo", "plan-approval", "split #3 into #3 and #4", ["approve", "edit", "stop"])
    split = copy.deepcopy(PLAN)
    split["checkpoints"][2]["id"] = 4
    split["checkpoints"].insert(2, cp(3, title="first half"))
    assert hook(repo, write(plan_file(repo), _drop(PLAN, 3)))[0] == 0
    assert hook(repo, write(plan_file(repo), split))[0] == 0


def test_deleted_checkpoint_counts_as_not_passed(repo):
    _project(repo)
    own(repo)
    plan = with_gate({**PLAN, "workflow": "feature"}, 2, behavior="passed", review="passed", status="passed")
    save_plan(repo, plan)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    save_plan(repo, _drop(plan, 3))           # bypassing the hook, e.g. an older tool
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING checkpoints" in out and "missing from the plan" in out
    assert "2/3 passed" in out
    code, _, err = stop(repo)
    assert code == 2 and "missing from the plan" in err
    st = gate.cmd_state(repo, "demo")
    assert [c["id"] for c in st["checkpoints"]] == [1, 2, 3]
    assert [c for c in st["checkpoints"] if c["id"] == 3][0]["status"] == "missing"


def test_all_frozen_passed_still_passes_the_dod(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 0 and "3/3 passed" in out


def test_state_reads_workflow_and_hotfix_from_the_approval(repo):
    save_plan(repo, {**PLAN, "workflow": "feature"})
    gate.cmd_approve(repo, "demo")
    save_plan(repo, {**PLAN, "workflow": "build", "hotfix": True})
    st = gate.cmd_state(repo, "demo")
    assert st["workflow"] == "feature" and st["hotfix"] is False


# I1: a hotfix plan is written with review pending; the approval switches it to n/a

def _hotfix_plan(review="pending"):
    p = {"feature": "Fix", "slug": "demo", "workflow": "fix", "hotfix": True,
         "checkpoints": [cp(1, r=review)]}
    return p


def test_planner_hotfix_plan_goes_through_the_hook_and_approval_sets_na(set_up):
    path = plan_file(set_up)
    code, _, err = hook(set_up, write(path, _hotfix_plan("n/a")))
    assert code == 2 and "hotfix" in err        # n/a still only on an approved hotfix plan
    assert hook(set_up, write(path, _hotfix_plan("pending")))[0] == 0
    save_plan(set_up, _hotfix_plan("pending"))  # what the Write would have written
    gate.cmd_approve(set_up, "demo")
    assert gate.load_approved(set_up, "demo")["hotfix"] is True
    assert gate.gate(gate.load_plan(set_up, "demo")["checkpoints"][0], "review") == "n/a"
    gate.cmd_lock_tests(set_up, "demo", 1, [], none=True)
    assert gate.cmd_verify(set_up, "demo", 1)[0] == 0
    done = {**_hotfix_plan("n/a"), "checkpoints": [cp(1, "passed", "passed", "n/a")]}
    assert hook(set_up, write(path, done))[0] == 0


def test_non_hotfix_approval_leaves_review_pending(repo):
    gate.cmd_approve(repo, "demo")
    assert all(gate.gate(c, "review") in ("pending", "passed") for c in gate.load_plan(repo, "demo")["checkpoints"])


# I2: the human sees the frozen DoD commands; trivial or non-string commands are refused

def _project_cmds(repo, cmds):
    block = '# Demo\n\n```json tq-project\n' + json.dumps({"commands": cmds, "changelog": "CHANGELOG.md"}) + '\n```\n'
    (repo / "CLAUDE.md").write_text(block, encoding="utf-8")


@pytest.mark.parametrize("cmds", [{"test": True}, {"test": "true"}, {"test": ""}, {"test": "  "},
                                  {"test": PASS_CMD, "lint": 7}, {"test": PASS_CMD, "typecheck": ":"}])
def test_approve_refuses_trivial_or_non_string_dod_commands(repo, cmds):
    _project_cmds(repo, cmds)
    with pytest.raises(SystemExit, match=r"commands\.\w+ must be a shell command string"):
        gate.cmd_approve(repo, "demo")
    assert gate.load_approved(repo, "demo") is None


def test_approve_shows_the_frozen_dod_commands_before_confirming(repo, monkeypatch, capsys):
    _project_cmds(repo, {"test": "pytest -q", "lint": "ruff check ."})
    order = []
    monkeypatch.setattr(gate, "human_confirm", lambda action, *a, **k: order.append(capsys.readouterr().out) or True)
    assert gate.main(["--root", str(repo), "approve", "demo"]) == 0
    shown = order[0]
    assert "test: pytest -q" in shown and "lint: ruff check ." in shown and "typecheck: (not configured)" in shown
    assert PASS_CMD in shown and "changelog: CHANGELOG.md" in shown


def test_plan_approval_consequence_lists_what_is_frozen(repo, monkeypatch):
    _project_cmds(repo, {"test": "pytest -q"})
    did = gate.cmd_ask(repo, "demo", "plan-approval", "approve?", ["approve", "edit", "stop"]).split()[1]
    text = gate.answer_consequence(repo, "demo", did, "approve")
    assert "freezes" in text and "test: pytest -q" in text and PASS_CMD in text
    assert gate.answer_consequence(repo, "demo", did, "edit") == ""
    _set_token(monkeypatch, "answer")
    assert gate.main(["--root", str(repo), "answer", "demo", did, "--choice", "approve"]) == 0
    ans = [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]
    assert "test: pytest -q" in ans["note"]
