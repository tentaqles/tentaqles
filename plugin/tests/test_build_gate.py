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
    save_plan(repo, {**PLAN, "workflow": "feature", "task": {"hub": "c-abcdefghijkl", "workspace": "acme", "kind": "feature", "external": {"provider": "linear", "id": "ACME-7", "url": "https://linear.app/x"}}})
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


def test_stop_message_does_not_tell_agent_to_run_override(repo, monkeypatch):
    monkeypatch.delenv("TENTAQLES_HEADLESS", raising=False)
    _dod_ready(repo)
    code, _, err = stop(repo)
    assert code == 2 and "--override" in err and "human" in err.lower()
    assert "in their own terminal" in err
    assert "when the human has said so" not in err


def test_headless_stop_message_points_at_the_hub_not_a_terminal(repo, monkeypatch):
    monkeypatch.setenv("TENTAQLES_HEADLESS", "1")
    _dod_ready(repo)
    code, _, err = stop(repo)
    assert code == 2 and "build-gate dod demo" in err
    assert "--override" not in err and "own terminal" not in err
    assert '--kind question --text "override:' in err and "--options override,stop" in err
    assert "hub card" in err and "/waiting" in err


# --- fix round 2: explicit override questions, visible consequences -------------------------

def test_non_override_wording_question_does_not_waive_dod(repo):
    _dod_ready(repo)
    gate.cmd_dod(repo, "demo")
    unbound = lambda r: {k: v for k, v in r.items() if k not in ("gen", "sig")}  # noqa: E731 - the answer re-signs it at the next gen
    before = unbound(_dod_rec(repo))
    did = gate.cmd_ask(repo, "demo", "question", "Let the env PORT override the default?", ["override", "keep"]).split()[1]
    gate.cmd_answer(repo, "demo", did, "override", "yes, env wins", actor="renato")
    assert unbound(_dod_rec(repo)) == before and gate.bound(repo, "demo", _dod_rec(repo))
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


# Fix round 6: the breaker's scratch directory moved out of the gate's state directory
# to <root>/.tq-breaker/<slug>/. No write into .claude/build/ is allowed while a build
# exists, a former breaker/ subpath included.
_B4 = ".claude/build/demo/breaker"
_TQB = ".tq-breaker/demo"


def _scratch_sources(repo):
    """Regular files to copy from, in the project and in the old scratch path."""
    (repo / "x").write_text("x", encoding="utf-8")
    (repo / "app.db").write_bytes(b"db")
    scratch = repo / ".claude" / "build" / "demo" / "breaker"
    scratch.mkdir(parents=True, exist_ok=True)
    (scratch / "app.db").write_bytes(b"db")
    (scratch / "reg").write_text("r", encoding="utf-8")
    return scratch


def _dir_link(target, link):
    """A directory link this box can make: a junction on Windows, else a symlink."""
    try:
        if os.name == "nt":
            import _winapi
            _winapi.CreateJunction(str(target), str(link))
        else:
            os.symlink(target, link, target_is_directory=True)
    except (OSError, NotImplementedError, AttributeError):
        pytest.skip("can't create a directory link here")


@pytest.mark.parametrize("cmd", [
    # formerly allowed by the carve-out: now refused
    f"mkdir -p {_B4}",
    f"pg_dump --clean --if-exists -w --file {_B4}/db.sql app",
    f"cp app.db {_B4}/app.db",
    f"rm -rf {_B4}/.",
    f"curl -sS --max-time 10 -o {_B4}/r.json -w '%{{http_code}}' 'http://localhost:3000/api/x?id=1'",
    f"setsid node server.js > {_B4}/app.log 2>&1",
    f"timeout 120 sqlite3 app.db .dump > {_B4}/app.sql",
    f"echo $! > {_B4}/app.pid",
    f"setsid nohup timeout 900 npm run dev > {_B4}/app.log 2>&1 & echo $! > {_B4}/app.pid",
    f"cp {_B4}/app.db ./app.db",
    f"cp -r fixtures/ {_B4}/", f"cp -t {_B4} app.db x", f"install -m 644 app.db {_B4}/app.db",
    f"mv x {_B4}/x", f"cp x {_B4}/x", f"mkdir -p {_B4}/logs", f"echo x > {_B4}/note",
    f"echo x >> {_B4}/note", f"touch {_B4}/t", f"echo x | tee {_B4}/t",
    f"echo x > {_B4}/reg", f"truncate -s0 {_B4}/reg", f"Set-Content {_B4}/reg x",
    f"echo x | Out-File {_B4}/reg", f"cp x {_B4}", f"cp x {_B4}/patch",
    # always refused
    "cp x .claude/build/demo/approved.json",
    f"echo y > {_B4}/../approved.json",
    f"cp .claude/build/demo/approved.json {_B4}/a",
    f"cp x {_B4}/a; cp x .claude/build/demo/evidence.jsonl",
    "mkdir -p .claude/build/other/breaker",
    f"echo x > {_B4}/a && echo y > .claude/build/demo/state.json",
    f"cp missing.db {_B4}/app.db",
    f"rsync -a fixtures/ {_B4}/f/",
    # output options write too
    "pg_dump -w -f .claude/build/demo/db.sql app", "pg_dump -w --file=.claude/build/demo/db.sql app",
    "curl -s -o.claude/build/demo/r.json http://127.0.0.1:3000/",
    "curl -s --output .claude/build/demo/r.json http://127.0.0.1:3000/",
    "curl -s -D .claude/build/demo/h.txt http://127.0.0.1:3000/",
    "wget -q -O .claude/build/demo/r.json http://127.0.0.1:3000/",
    'sqlite3 app.db ".backup .claude/build/demo/b.db"',
])
def test_no_write_into_the_state_dir_while_a_build_exists(repo, cmd):
    _active_build(repo)
    _scratch_sources(repo)
    (repo / "fixtures" / "sub").mkdir(parents=True)
    (repo / "fixtures" / "sub" / "a.json").write_text("{}", encoding="utf-8")
    assert hook(repo, bash(cmd))[0] == 2, cmd


def test_build_state_dir_stays_excluded_from_the_hash() -> None:
    assert gate.STATE_DIR in gate.HASH_EXCLUDES
    assert ".tq-breaker" in gate.HASH_EXCLUDES


@pytest.mark.parametrize("cmd", [
    f"mkdir -p {_TQB}",
    f"pg_dump --clean --if-exists -w --file {_TQB}/db.sql app",
    f"cp app.db {_TQB}/app.db",
    f"setsid node server.js > {_TQB}/app.log 2>&1",
    f"setsid nohup timeout 900 npm run dev > {_TQB}/app.log 2>&1 & echo $! > {_TQB}/app.pid",
    f"timeout 120 sqlite3 app.db .dump > {_TQB}/app.sql",
    f"PGCONNECT_TIMEOUT=5 timeout 120 psql -w -v ON_ERROR_STOP=1 --single-transaction -f {_TQB}/backup.sql app",
    f"cp {_TQB}/app.db ./app.db",
    f"curl -sS --max-time 10 -o {_TQB}/r.json -w '%{{http_code}}' 'http://localhost:3000/api/x?id=1'",
    f'sqlite3 app.db ".backup {_TQB}/b.db"',
    "ln -s src/a.py lib/a.py",
])
def test_breaker_scratch_outside_the_state_dir_is_allowed(repo, cmd):
    _active_build(repo)
    _scratch_sources(repo)
    (repo / ".tq-breaker" / "demo").mkdir(parents=True)
    (repo / ".tq-breaker" / "demo" / "app.db").write_bytes(b"db")
    assert hook(repo, bash(cmd))[0] == 0, cmd


@pytest.mark.parametrize("cmd", [
    f"echo y > {_TQB}/../../.claude/build/demo/approved.json",
    f"cp {_TQB}/a .claude/build/demo/approved.json",
    f"ln -s ../../.claude/build/demo/approved.json {_TQB}/l",
    f"ln .claude/build/demo/approved.json {_TQB}/h",
    f"cp -s ../../.claude/build/demo/approved.json {_TQB}/l",
    f"cp --link .claude/build/demo/approved.json {_TQB}/h",
    r"cmd /c mklink /J .tq-breaker\demo\j .claude\build",
    f"New-Item -ItemType Junction -Path {_TQB}/j -Target .claude/build/demo",
    f"New-Item -ItemType HardLink -Path {_TQB}/h -Target .claude/build/demo/approved.json",
    f"fsutil hardlink create {_TQB}/h .claude/build/demo/approved.json",
    r"fsutil hardlink create .tq-breaker\demo\h .claude\build\demo\approved.json",
    "fsutil.exe hardlink create lib/h .claude/build/demo/approved.json",
])
def test_links_from_the_scratch_dir_into_the_state_dir_are_refused(repo, cmd):
    _active_build(repo)
    assert hook(repo, bash(cmd))[0] == 2, cmd


def test_tree_hash_ignores_the_breaker_scratch_dir(repo):
    h1 = gate.tree_hash(repo)
    (repo / ".tq-breaker" / "demo").mkdir(parents=True)
    (repo / ".tq-breaker" / "demo" / "app.log").write_text("log\n", encoding="utf-8")
    (repo / ".tq-breaker" / "demo" / "db.sql").write_text("-- dump\n", encoding="utf-8")
    assert h1 is not None and gate.tree_hash(repo) == h1


def test_setup_git_excludes_the_breaker_scratch_dir(repo):
    gate.cmd_setup(repo)
    gate.cmd_setup(repo)
    exclude = subprocess.run(["git", "rev-parse", "--git-path", "info/exclude"], cwd=repo,
                             capture_output=True, text=True).stdout.strip()
    path = Path(exclude) if os.path.isabs(exclude) else repo / exclude
    lines = path.read_text(encoding="utf-8").splitlines()
    assert lines.count("/.tq-breaker/") == 1 and "/.claude/build/" in lines


# Former carve-out block cases: still refused.
@pytest.mark.parametrize("cmd", [
    f'cp {_B4}/a .claude/"build"/demo/approved.json',
    f'rm {_B4}/a .claude/b"uild"/demo/approved.json',
    f"cp {_B4}/a '.claude/build'/demo/approved.json",
    f"cp {_B4}/a .claude/buil[d]/demo/approved.json",
    f"dd if={_B4}/a of={_B4}/../approved.json",
    f"cp -t {_B4} x .claude/build/demo/approved.json",
    f"cp x --target-directory={_B4}/..",
    f"rm -rf {_B4}/a/../..",
    f"cp {_B4}/a $X",
    f"cp {_B4}/a .claude/build/*/approved.json",
    """python -c "import os;open(os.path.join('.claude/build/demo/breaker','..','approved.json'),'w')" """,
    """python -c "import urllib.parse as u;open(u.unquote('.claude/build/demo/breaker/..%2Fapproved.json'),'w')" """,
    """python3 -c "open('.claude/build/demo/breaker/x','w')" """,
    """node -e "require('fs').writeFileSync('.claude/build/demo/breaker/x','')" """,
    f"""bash -c 'cp x {_B4}/a' > {_B4}/log""",
    "cp x .claude/build/demo/BREAKER/a" if os.name != "nt" else "cp x .claude/build/demo/breakers/a",
    f"""setsid python -c "open('.claude/build/demo/breaker/x','w')" > {_B4}/log""",
    "ln -s ../approved.json .claude/build/demo/breaker/l",
    "ln .claude/build/demo/approved.json .claude/build/demo/breaker/h",
    "cp -s ../approved.json .claude/build/demo/breaker/l",
    "cp -sf ../approved.json .claude/build/demo/breaker/l",
    "cp --link .claude/build/demo/approved.json .claude/build/demo/breaker/h",
    "cp --symbolic-link ../approved.json .claude/build/demo/breaker/l",
    r"cmd /c mklink .claude\build\demo\breaker\l ..\approved.json",
    "New-Item -ItemType SymbolicLink -Path .claude/build/demo/breaker/l -Target ../approved.json",
    "New-Item -ItemType Junction -Path .claude/build/demo/breaker/j -Target .claude/build/demo",
    "fsutil hardlink create .claude/build/demo/breaker/h .claude/build/demo/approved.json",
    'ln -s ../b"uild"/demo/approved.json .claude/x',
    "ln -s build/demo/approved.json .claude/x",
    "ln -s ../approved.json lib/l && mv lib/l .claude/build/demo/breaker/l && echo x > .claude/build/demo/breaker/l",
    r"cmd /c mklink /J lib\j .claude && mv lib/j .claude/build/demo/breaker/j",
    f"cp -s x lib/l; cp x {_B4}/a",
    f"New-Item -ItemType Junction -Path lib/j -Target .claude; echo x > {_B4}/a",
    f"Move-Item x {_B4}/x", f"mi x {_B4}/x", f"Copy-Item x {_B4}/x", f"cpi x {_B4}/x",
    f"Rename-Item {_B4}/app.db b.db", f"rni {_B4}/app.db b.db", f"move x {_B4}/x",
    f"copy x {_B4}/x", f"ren {_B4}/app.db b.db", f"xcopy x {_B4}", f"robocopy . {_B4} x",
    f"rsync -rt --link-dest=.. src/ {_B4}/", f"rsync -rt --link-dest ../ src/ {_B4}/",
    f"rsync -rt --compare-dest=.. src/ {_B4}/", f"rsync -rt --copy-dest=.. src/ {_B4}/",
    f"rsync x {_B4}/x", f"rsync.exe x {_B4}/x", f"rsync --copy-links x {_B4}/x",
    f"cp --l x {_B4}/x", f"cp --li x {_B4}/x", f"cp --lin x {_B4}/x", f"cp --l=x x {_B4}/x",
])
def test_former_breaker_carve_out_block_cases_stay_blocked(repo, cmd):
    _active_build(repo)
    _scratch_sources(repo)
    (repo / "lib").mkdir(exist_ok=True)
    assert hook(repo, bash(cmd))[0] == 2, cmd


def test_writes_through_links_in_the_old_scratch_path_are_refused(repo):
    _active_build(repo)
    scratch = _scratch_sources(repo)
    _dir_link(repo / ".claude" / "build" / "demo", scratch / "j")
    for cmd in (f"echo x > {_B4}/j/approved.json", f"cp x {_B4}/j", f"cp x {_B4}/a",
                f"echo x > {_B4}/ok"):
        assert hook(repo, bash(cmd))[0] == 2, cmd


def test_moving_a_junction_into_the_old_scratch_path_is_refused(repo):
    _active_build(repo)
    _scratch_sources(repo)
    (repo / "lib").mkdir(exist_ok=True)
    _dir_link(repo / ".claude", repo / "lib" / "j")
    for cmd in (f"mv lib/j {_B4}/j; echo x > {_B4}/j/build/demo/approved.json",
                f"cp -a lib/ {_B4}/", f"cp -r lib {_B4}/", f"Move-Item lib/j {_B4}/j",
                f"robocopy lib {_B4} /SL /E", f"Copy-Item lib/j {_B4}/j", f"xcopy /B /E lib {_B4}"):
        assert hook(repo, bash(cmd))[0] == 2, cmd


# Fix round 7: the breaker's scratch directory holds data files only, so nothing there
# can be collected by the project's own test run.
@pytest.mark.parametrize("cmd", [
    f"echo x > {_TQB}/conftest.py",
    f"echo x > {_TQB}/test_a.py",
    f"echo x > {_TQB}/a_test.go",
    f"echo x > {_TQB}/a.spec.ts",
    f"echo x > {_TQB}/a.test.js",
    f"echo x > {_TQB}/run.mjs",
    f"echo x > {_TQB}/pytest.ini",
    f"echo x > {_TQB}/setup.cfg",
    f"echo x > {_TQB}/pyproject.toml",
    f"echo x > {_TQB}/evil.pth",
    f"cp x {_TQB}/a.cjs",
])
def test_scratch_refuses_code_test_and_config_files(repo, cmd):
    _active_build(repo)
    (repo / "x").write_text("x", encoding="utf-8")
    code, _, err = hook(repo, bash(cmd))
    assert code == 2 and "scratch dir holds data files only (sql, db, db-wal, db-shm, log, pid, json, txt, csv, tsv, dump, bak" in err, cmd


@pytest.mark.parametrize("tool,name", [("Write", "test_a.py"), ("Edit", "conftest.py"), ("Write", "a.ts")])
def test_scratch_file_tool_refuses_code_files(repo, tool, name):
    _active_build(repo)
    p = str(repo / ".tq-breaker" / "demo" / name)
    payload = {"tool_name": tool, "tool_input": {"file_path": p, "content": "x", "old_string": "a", "new_string": "b"}}
    code, _, err = hook(repo, payload)
    assert code == 2 and "scratch dir holds data files only" in err


@pytest.mark.parametrize("cmd", [
    f"pg_dump --file {_TQB}/db.sql app",
    f"echo 1 > {_TQB}/app.pid",
    f"echo 1 > {_TQB}/app.log",
    f"echo 1 > {_TQB}/r.json",
    f"echo 1 > {_TQB}/n.txt",
    f"echo 1 > {_TQB}/d.csv",
    f"cp app.db {_TQB}/app.db-wal",
    f"cp app.db {_TQB}/app.db-shm",
])
def test_scratch_data_files_stay_allowed(repo, cmd):
    _active_build(repo)
    assert hook(repo, bash(cmd))[0] == 0, cmd


def test_finish_removes_the_scratch_dir_and_logs(repo, tq_home):
    approve_and_lock(repo)
    d = repo / ".tq-breaker" / "demo"
    d.mkdir(parents=True)
    (d / "app.db").write_bytes(b"db")
    (repo / ".tq-breaker" / "other").mkdir()
    gate.cmd_finish(repo, "demo")
    assert not d.exists() and (repo / ".tq-breaker" / "other").exists()
    kinds = [json.loads(l)["kind"] for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]
    assert "breaker-scratch-removed" in kinds


def test_finish_without_a_scratch_dir_logs_nothing_extra(repo, tq_home):
    approve_and_lock(repo)
    gate.cmd_finish(repo, "demo")
    kinds = [json.loads(l)["kind"] for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]
    assert kinds == ["finish"]


# Final fix wave, I1: `finish` deletes only <root>/.tq-breaker/<slug> for a valid slug,
# never through a link, and the hook refuses links made at or under .tq-breaker/.
@pytest.mark.parametrize("slug", ["..", ".", "a/b", "a\\b", "../demo", "Demo", "-x", "", "demo/.."])
def test_finish_refuses_a_bad_slug_and_deletes_nothing(repo, tq_home, slug):
    approve_and_lock(repo)
    d = repo / ".tq-breaker" / "demo"
    d.mkdir(parents=True)
    (d / "app.db").write_bytes(b"db")
    with pytest.raises(SystemExit):
        gate.cmd_finish(repo, slug)
    assert (repo / ".git").exists() and (repo / "app.py").exists() and (d / "app.db").exists()
    assert (repo / ".claude" / "build").exists()


def _outside_scratch(tmp_path):
    outside = tmp_path / "outside"
    (outside / "demo").mkdir(parents=True)
    (outside / "demo" / "keep.txt").write_text("keep", encoding="utf-8")
    return outside


def _event_kinds(tq_home):
    return [json.loads(l)["kind"] for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]


def test_finish_does_not_follow_a_link_at_the_scratch_root(repo, tq_home, tmp_path):
    approve_and_lock(repo)
    outside = _outside_scratch(tmp_path)
    _dir_link(outside, repo / ".tq-breaker")
    gate.cmd_finish(repo, "demo")
    assert (outside / "demo" / "keep.txt").exists()
    kinds = _event_kinds(tq_home)
    assert "breaker-scratch-refused" in kinds and "breaker-scratch-removed" not in kinds


def test_finish_does_not_follow_a_link_at_the_slug_dir(repo, tq_home, tmp_path):
    approve_and_lock(repo)
    outside = _outside_scratch(tmp_path)
    (repo / ".tq-breaker").mkdir()
    _dir_link(outside / "demo", repo / ".tq-breaker" / "demo")
    gate.cmd_finish(repo, "demo")
    assert (outside / "demo" / "keep.txt").exists()
    kinds = _event_kinds(tq_home)
    assert "breaker-scratch-refused" in kinds and "breaker-scratch-removed" not in kinds


@pytest.mark.parametrize("cmd", [
    "ln -s ../x .tq-breaker/demo/l",
    "ln -s ../outside .tq-breaker",
    "ln -sfn /tmp .tq-breaker/demo",
    "ln app.py .tq-breaker/demo/h.txt",
    r"cmd /c mklink /J .tq-breaker C:\outside",
    r"cmd /c mklink /J .tq-breaker\demo C:\outside",
    r"mklink /D .tq-breaker\demo\d ..\..\lib",
    "New-Item -ItemType Junction -Path .tq-breaker -Target lib",
    "New-Item -ItemType SymbolicLink -Path .tq-breaker/demo/l -Target ../../lib",
    "cp -s app.py .tq-breaker/demo/l.txt",
    "tar -xf a.tar -C .tq-breaker/demo",
    "fsutil hardlink create .tq-breaker/demo/h.txt app.py",
])
def test_links_at_or_under_the_scratch_dir_are_refused(repo, cmd):
    _active_build(repo)
    (repo / "lib").mkdir(exist_ok=True)
    assert hook(repo, bash(cmd))[0] == 2, cmd


# Final fix wave, I2: scratch holds data files only (extension allowlist), checked on every
# write target the hook can see and, fail closed, by a bounded walk at verify and dod.
@pytest.mark.parametrize("cmd", [
    f"echo x > {_TQB}/jest.config.json",
    f"echo x > {_TQB}/vitest.config.mts",
    f"echo x > {_TQB}/package.json",
    f"echo x > {_TQB}/tsconfig.json",
    f"echo x > {_TQB}/run.sh",
    f"echo x > {_TQB}/notes",
    f"echo x > {_TQB}/a.rb",
    f"cp x {_TQB}/sub/a.go",
])
def test_scratch_refuses_names_outside_the_data_allowlist(repo, cmd):
    _active_build(repo)
    (repo / "x").write_text("x", encoding="utf-8")
    code, _, err = hook(repo, bash(cmd))
    assert code == 2 and "scratch dir holds data files only" in err, cmd


@pytest.mark.parametrize("cmd", [
    f"echo 1 > {_TQB}/a.tsv", f"pg_dump -w --file {_TQB}/db.dump app", f"cp app.db {_TQB}/app.db.bak",
    f"cp app.db {_TQB}/app-wal", f"cp app.db {_TQB}/app-shm", f"mkdir -p {_TQB}", "mkdir -p .tq-breaker",
])
def test_scratch_allowlisted_data_names_are_allowed(repo, cmd):
    _active_build(repo)
    (repo / "app.db").write_bytes(b"db")
    assert hook(repo, bash(cmd))[0] == 0, cmd


# The end state each hook bypass leaves behind (cp into a dir, cp -r, python -c / node -e,
# git show > file, cd && echo, tar -C, a config named *.json).
@pytest.mark.parametrize("rel", [
    "a.test.js",                 # cp src/a.test.js .tq-breaker/demo/
    "src/a.test.js",             # cp -r src .tq-breaker/demo/
    "conftest.py",               # python -c "open(...)" / git show HEAD:app.py > ... / cd ... && echo x > ...
    "logs.txt/x.spec.ts",        # nested under a data-named dir
    "pkg/mod.py",                # tar -xf a.tar -C .tq-breaker/demo
    "jest.config.json",
    "vitest.config.mts",
    "package.json",
])
def test_verify_refuses_code_left_in_the_scratch_dir(repo, rel):
    approve_and_lock(repo)
    p = repo / ".tq-breaker" / "demo" / rel
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text("x", encoding="utf-8")
    (repo / ".tq-breaker" / "demo" / "app.log").write_text("log", encoding="utf-8")
    code, msg = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and ".tq-breaker" in msg and p.name in msg, msg
    assert gate.norm(str(p.parent)) not in gate.norm(msg) and "app.log" not in msg
    ev = gate.latest_evidence(repo, "demo", 2)
    assert ev and ev["ok"] is False


def test_verify_refuses_a_link_in_the_scratch_dir(repo):
    approve_and_lock(repo)
    (repo / ".tq-breaker" / "demo").mkdir(parents=True)
    _dir_link(repo / "tests", repo / ".tq-breaker" / "demo" / "j")
    code, msg = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and "link" in msg


def test_verify_refuses_a_linked_scratch_root(repo, tmp_path):
    approve_and_lock(repo)
    outside = _outside_scratch(tmp_path)
    _dir_link(outside, repo / ".tq-breaker")
    code, msg = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and "link" in msg


def test_verify_fails_closed_when_the_scratch_walk_hits_its_cap(repo, monkeypatch):
    approve_and_lock(repo)
    monkeypatch.setattr(gate, "SCRATCH_WALK_CAP", 5)
    d = repo / ".tq-breaker" / "demo"
    d.mkdir(parents=True)
    for i in range(8):
        (d / f"{i}.log").write_text("x", encoding="utf-8")
    code, msg = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and "too many" in msg


def test_scratch_walk_cap_is_2000():
    assert gate.SCRATCH_WALK_CAP == 2000


def test_verify_is_unaffected_by_a_data_only_scratch_dir(repo):
    approve_and_lock(repo)
    d = repo / ".tq-breaker" / "demo"
    (d / "logs").mkdir(parents=True)
    for n in ("db.sql", "app.db", "app.db-wal", "app.db-shm", "x-wal", "x-shm", "logs/app.log", "app.pid",
              "r.json", "n.txt", "d.csv", "d.tsv", "db.dump", "app.db.bak"):
        (d / n).write_text("x", encoding="utf-8")
    assert gate.cmd_verify(repo, "demo", 2)[0] == 0


def test_dod_refuses_code_left_in_the_scratch_dir(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    (repo / ".tq-breaker" / "demo").mkdir(parents=True)
    (repo / ".tq-breaker" / "demo" / "conftest.py").write_text("x", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING breaker_scratch" in out and "conftest.py" in out
    (repo / ".tq-breaker" / "demo" / "conftest.py").unlink()
    assert gate.cmd_dod(repo, "demo")[0] == 0
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


# ---------------------------------------------------------------------------
# Round 2a item 1: plan schema and hook error handling
# ---------------------------------------------------------------------------


def _bad_plan(first):
    p = copy.deepcopy(PLAN)
    p["checkpoints"][0] = first
    return p


BAD_FIRST = [
    ({"id": 1, "gates": "x"}, "gates"),
    ({"id": [0]}, "id"),
    ({"id": ""}, "id"),
    ({"id": 2, "gates": {"behavior": "passed"}}, "gates"),
    ({"id": 1, "tests": "tests/x.py"}, "tests"),
    ({"id": 1, "tests": [1]}, "tests"),
]


@pytest.mark.parametrize("first,field", BAD_FIRST)
def test_check_plan_refuses_malformed_plan(repo, first, field):
    bad = _bad_plan(first)
    with pytest.raises(gate.Block) as e:
        gate.check_plan(json.dumps(bad), json.dumps(PLAN), repo, "demo", gate.TreeHash(repo))
    assert field in str(e.value)


def test_check_plan_refuses_non_dict_checkpoint_and_duplicate_ids(repo):
    for plan in ({"checkpoints": [1]}, {"checkpoints": [{"id": 1}, {"id": 1}]}):
        with pytest.raises(gate.Block):
            gate.check_plan(json.dumps(plan), "", repo, "demo", gate.TreeHash(repo))


@pytest.mark.parametrize("first,field", BAD_FIRST[:2])
def test_hook_on_existing_malformed_plan(repo, first, field):
    own(repo)
    save_plan(repo, _bad_plan(first))  # written behind the hooks' back
    code, _, err = hook(repo, agent("[checkpoint demo#2 implement]"))
    assert code == 2 and field in err and "Traceback" not in err
    assert stop(repo)[0] == 0


@pytest.mark.parametrize("first,field", BAD_FIRST[:2])
def test_state_and_dod_on_malformed_plan_give_a_clear_error(repo, first, field):
    gate.cmd_approve(repo, "demo")
    save_plan(repo, _bad_plan(first))
    with pytest.raises(SystemExit) as e:
        gate.cmd_state(repo, "demo")
    assert field in str(e.value)
    code, msg = gate.cmd_dod(repo, "demo")
    assert code == 1 and field in msg


def test_unexpected_exception_pretooluse_refuses_with_class_only(repo, monkeypatch):
    def boom(*a, **k):
        raise KeyError("secret-detail")
    monkeypatch.setattr(gate, "check_shell", boom)
    code, out, err = hook(repo, bash("ls"))
    assert code == 2
    assert "KeyError" in err and "secret-detail" not in err and "Traceback" not in err and "\n" not in err.strip()


def test_unexpected_exception_on_stop_allows_with_note(repo, monkeypatch):
    def boom(*a, **k):
        raise KeyError("secret-detail")
    monkeypatch.setattr(gate, "check_stop", boom)
    code, out, err = stop(repo)
    assert code == 0 and "KeyError" in err and "secret-detail" not in err


# ---------------------------------------------------------------------------
# Round 2a item 2: ordinary project files are never refused by a broad pattern
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("path,content", [
    ("workflow/gateway.ts", "export const gateway = 1;\n"),
    ("api/gate.py", "def gate():\n    return 1\n"),
    ("src/hmac.key.md", "notes on the hmac.key format\n"),
    ("src/auth.py", "def cmd_approve(x):\n    return x\n\ndef cmd_answer():\n    pass\n"),
    ("notes/tentaqles-workflow.md", "tentaqles/workflow notes: investigate the gateway\n"),
])
def test_ordinary_files_are_not_refused_while_a_build_is_active(repo, path, content):
    approve_and_lock(repo)
    payload = {"tool_name": "Write", "tool_input": {"file_path": str(repo / path), "content": content}}
    code, _, err = hook(repo, payload)
    assert code == 0, err


@pytest.mark.parametrize("cmd", [
    "cat src/workflow/gateway.ts",
    "cat ./workflow/gateway.ts",
    "python api/gate.py",
    "grep -rn investigate src",
    "grep cmd_approve src/auth.py",
    "echo hmac.key is mentioned in the docs",
])
def test_ordinary_commands_are_not_refused_while_a_build_is_active(repo, cmd):
    approve_and_lock(repo)
    code, _, err = hook(repo, bash(cmd))
    assert code == 0, err


@pytest.mark.parametrize("content", [
    "from tentaqles.workflow import gate\n",
    "import tentaqles.workflow.gate as g\n",
    "import gate\ngate.cmd_approve(root, 'x')\n",
    "from gate import cmd_answer\n",
])
def test_gate_module_references_are_still_refused(repo, content):
    approve_and_lock(repo)
    payload = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "x.py"), "content": content}}
    assert hook(repo, payload)[0] == 2, content


@pytest.mark.parametrize("cmd", [
    "python plugin/tentaqles/workflow/gate.py",
    "cat ~/.tentaqles/build-gate/hmac.key",
    "cat /x/build-gate/sub/hmac.key",
    "python -c \"import tentaqles.workflow.gate\"",
])
def test_gate_anchored_commands_are_still_refused(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2, cmd


# ---------------------------------------------------------------------------
# Round 2a item 3: a re-approval shows what was removed and changed, not only what is new
# ---------------------------------------------------------------------------


def _reapproval_plan(repo):
    gate.cmd_approve(repo, "demo")
    plan = copy.deepcopy(PLAN)
    plan["checkpoints"] = [c for c in plan["checkpoints"] if c["id"] != 3]  # #3 removed
    plan["checkpoints"][1]["verify"] = [FAIL_CMD]                            # #2 changed
    plan["checkpoints"].append(cp(4))                                        # #4 new
    save_plan(repo, plan)


def test_reapproval_preview_lists_removed_changed_and_new(repo):
    _reapproval_plan(repo)
    text = gate.approval_preview(repo, "demo")
    assert "removed since the last approval: #3 (" in text
    assert "changed: #2" in text and "new: #4" in text


def test_first_approval_preview_has_no_delta_lines(repo):
    text = gate.approval_preview(repo, "demo")
    assert "removed since" not in text and "changed:" not in text


def test_reapproval_consequence_for_a_hub_actor_lists_removed_and_changed(repo):
    _reapproval_plan(repo)
    did = gate.cmd_ask(repo, "demo", "plan-approval", "approve?", ["approve", "edit", "stop"]).split()[1]
    text = gate.answer_consequence(repo, "demo", did, "approve")
    assert "removed since the last approval: #3 (" in text and "changed: #2" in text


# ---------------------------------------------------------------------------
# Round 2b, item 1: shell path classification (cwd tracking, globs, escapes,
# nested shells, xargs, git add -f globs)
# ---------------------------------------------------------------------------

STATE_WRITES_VIA_CWD = [
    "cd .claude/build/demo && cp approved.json ../../../bk.json",
    "cd .claude/build/demo && echo x > approved.json",
    "cd .claude/build/demo; rm approved.json",
    "pushd .claude/build/demo; echo x > approved.json",
    "pushd .claude/build; pushd demo; echo x > approved.json",
    "cd .claude/build; echo x > demo/approved.json",
    "cd .claude/build/demo; cd ..; echo x > demo/approved.json",
    "cd .claude/build/demo; cd /tmp; cd -; echo x > approved.json",
    "cd .claude; mv build/demo/approved.json ../moved.json",
    "Set-Location .claude/build/demo; Set-Content approved.json x",
]


@pytest.mark.parametrize("cmd", STATE_WRITES_VIA_CWD)
def test_cwd_is_tracked_across_segments(repo, cmd):
    approve_and_lock(repo)
    code, _, err = hook(repo, bash(cmd))
    assert code == 2, cmd


def test_state_copy_out_then_copy_back_both_refused(repo):
    """The two-step sequence: approved.json copied out from inside the state dir, then back."""
    approve_and_lock(repo)
    assert hook(repo, bash("cd .claude/build/demo && cp approved.json ../../../bk.json"))[0] == 2
    assert hook(repo, bash("cp ../bk.json .claude/build/demo/approved.json"))[0] == 2
    assert hook(repo, bash("cd .claude/build/demo && cp ../../../bk.json approved.json"))[0] == 2


def test_unknown_cwd_with_a_relative_write_fails_closed(repo):
    approve_and_lock(repo)
    assert hook(repo, bash("cd $SOMEWHERE && echo x > approved.json"))[0] == 2
    assert hook(repo, bash("cd && echo x > approved.json"))[0] == 2
    assert hook(repo, bash("popd; echo x > approved.json"))[0] == 2
    # an absolute target after an unknown cwd is still resolved
    assert hook(repo, bash("cd $SOMEWHERE && echo x > /tmp/out.txt"))[0] == 0
    # no write, nothing to refuse
    assert hook(repo, bash("cd $SOMEWHERE && ls"))[0] == 0


def test_write_anywhere_on_a_line_with_a_state_word_anywhere_is_refused(repo):
    """A cd into the state dir is not itself a write, but the line writes."""
    approve_and_lock(repo)
    assert hook(repo, bash("cd .claude/build/demo && echo hi > /tmp/x"))[0] == 2
    assert hook(repo, bash("echo hi > /tmp/x; cat .claude/build/demo/approved.json"))[0] == 2
    assert hook(repo, bash("cd .claude/build/demo && ls"))[0] == 0
    assert hook(repo, bash("cat .claude/build/demo/approved.json"))[0] == 0


@pytest.mark.parametrize("cmd", [
    "cd src && echo x > a.txt",
    "cd tests && pytest -q",
    "cd tests && echo x > new_test.py",
    "echo x > app.py",
    "cp app.py app2.py",
    "pushd src; popd; echo x > approved.json",
    "cd .. && echo x > a.txt",
    "mkdir -p src/pkg && echo x > src/pkg/a.txt",
    "git commit -m 'x' && git status",
    "cd src; cd -; echo x > a.txt",
    "ls *.py && rm *.json",
])
def test_ordinary_project_writes_stay_allowed_with_cwd_tracking(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 0, cmd


@pytest.mark.parametrize("cmd", [
    "rm -r .clau*",
    "rm .claude/buil?/demo/approved.json",
    "cp x .cl*/build/demo/approved.json",
    "cp x .claude/build/*/approved.json",
    "cp x .claude/build/de[m]o/approved.json",
    "cd .claude/build && rm de*/approved.json",
    "rm -rf .c*e",
])
def test_glob_words_are_matched_against_the_state_dir(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2, cmd


@pytest.mark.parametrize("cmd", [
    "rm tes?s/*.py",
    "rm tests/test_*.py",
    "cp x t*sts/test_a.py",
    "cd tests && rm test_?.py",
    "cd tests && cp x test_[a].py",
])
def test_glob_words_are_matched_against_locked_tests(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2, cmd


def test_a_star_does_not_reach_dot_directories(repo):
    """Shell globs skip leading dots, so an ordinary `rm *.log` is not a state write."""
    approve_and_lock(repo)
    assert hook(repo, bash("rm *.log"))[0] == 0
    assert hook(repo, bash("cp *.py /tmp/backup"))[0] == 0


def test_backslash_before_a_character_is_a_shell_escape(repo):
    approve_and_lock(repo)
    assert hook(repo, bash(r"rm .claude/bu\ild/demo/approved.json"))[0] == 2
    assert hook(repo, bash(r"echo x > .claude/bu\ild/demo/approved.json"))[0] == 2
    assert hook(repo, bash(r"echo x > tes\ts/test_a.py"))[0] == 2
    assert hook(repo, bash(r"cd .cl\aude/build/demo && rm approved.json"))[0] == 2
    # the Windows spellings keep working
    assert hook(repo, bash(r"Remove-Item .claude\build\demo\approved.json", "PowerShell"))[0] == 2
    drive = str(repo).replace("/", "\\") + "\\.claude\\build\\demo\\approved.json"
    assert hook(repo, bash(f"Remove-Item {drive}", "PowerShell"))[0] == 2


def test_resolve_word_backslash_rule(tmp_path):
    from tentaqles.workflow import _common as c
    assert c.shell_word_variants(r"bu\ild")[0] == ("bu/ild" if c.IS_WINDOWS else "build")
    assert "build" in c.shell_word_variants(r"bu\ild")
    assert c.shell_word_variants(r"C:\a\b") == ["C:/a/b"]
    assert c.shell_word_variants(r"\\host\share") == ["//host/share"]
    assert c.shell_word_variants("a/b") == ["a/b"]
    _, absn = c.resolve_word(tmp_path, r"bu\ild/x", shell=True)
    assert absn.endswith("/build/x")
    # file-tool paths: every backslash is a separator
    _, absn = c.resolve_word(tmp_path, r"bu\ild/x")
    assert absn.endswith("/bu/ild/x")


@pytest.mark.parametrize("cmd", [
    "bash -c 'echo x > .claude/build/demo/approved.json'",
    'sh -c "cp x .claude/build/demo/approved.json"',
    "eval 'echo x > .claude/build/demo/approved.json'",
    "eval echo x '>' .claude/build/demo/approved.json",
    'pwsh -c "Set-Content .claude/build/demo/approved.json x"',
    'powershell -Command "Remove-Item .claude/build/demo/approved.json"',
    "bash -lc 'rm .claude/build/demo/approved.json'",
    "bash -c 'cd .claude/build/demo && rm approved.json'",
    "bash -c \"bash -c 'rm .claude/build/demo/approved.json'\"",
    "bash -c 'echo x > tests/test_a.py'",
    "cd tests && bash -c 'echo x > test_a.py'",
    'bash -c "$CMD"',
    "powershell -EncodedCommand AAAA",
])
def test_nested_shell_bodies_are_lexed_recursively(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2, cmd


@pytest.mark.parametrize("cmd", [
    "bash -c 'echo hi > out.txt'",
    "sh -c 'cd src && echo hi > a.txt'",
    "bash -c 'pytest -q'",
    "eval 'ls'",
])
def test_nested_shell_bodies_without_protected_paths_stay_allowed(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 0, cmd


@pytest.mark.parametrize("cmd,want", [
    ("xargs -a list cp -t .claude/build/demo", 2),
    ("ls | xargs rm", 2),
    ("find . -name '*.pyc' | xargs rm -f", 2),
    ("cat list | xargs -I{} cp {} /tmp", 2),
    ("cat list | xargs sh -c 'echo'", 2),
    ("cat list | xargs tests/test_a.py", 2),
    ("git ls-files | xargs grep -n foo", 0),
    ("git ls-files | xargs wc -l", 0),
])
def test_xargs_targets_are_unknown(repo, cmd, want):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == want, cmd


@pytest.mark.parametrize("cmd,want", [
    ("git add -f .cl*", 2),
    ("git add --force .claude/b*", 2),
    ("git add -f -- '*.json'", 2),
    ("git add -f src/*.py", 2),
    ("git add -f .claude/build/de?o", 2),
    ("git add -f app.py", 0),
    ("git add app.py", 0),
    ("git add src/*.py", 0),
])
def test_git_add_force_with_a_glob_pathspec_is_refused(repo, cmd, want):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == want, cmd


def test_squash_normalisation_applies_to_locked_test_names(repo):
    approve_and_lock(repo)
    cmd = "python -c \"open('te'+'sts/te'+'st_a.py','w')\""
    assert hook(repo, bash(cmd))[0] == 2
    assert hook(repo, bash("python -c \"open('te'+'sts/new_t'+'est.py','w')\""))[0] == 0


# ---------------------------------------------------------------------------
# Round 2b, item 2: the key directory through Read/Grep/Glob and shell globs
# ---------------------------------------------------------------------------


def _key_tool(tool, **inp):
    return {"tool_name": tool, "tool_input": inp}


def test_search_tools_refused_on_the_key_dir_and_its_ancestors(repo, tq_home):
    kd = gate.key_dir()
    assert hook(repo, _key_tool("Glob", pattern="**/hmac.key", path=str(tq_home)))[0] == 2
    assert hook(repo, _key_tool("Glob", pattern="*", path=str(tq_home.parent)))[0] == 2
    assert hook(repo, _key_tool("Grep", pattern=".", path=str(tq_home)))[0] == 2
    assert hook(repo, _key_tool("Grep", pattern=".", path=str(tq_home), glob="**/hmac.key"))[0] == 2
    assert hook(repo, _key_tool("Grep", pattern=".", path=str(kd)))[0] == 2
    assert hook(repo, _key_tool("Glob", pattern="*", path=str(kd)))[0] == 2
    assert hook(repo, _key_tool("Read", file_path=str(kd / "hmac.key")))[0] == 2
    assert hook(repo, _key_tool("Grep", pattern=".", path=str(kd / "hmac.key")))[0] == 2
    # the filesystem root and a drive root are ancestors too
    assert hook(repo, _key_tool("Grep", pattern=".", path=str(Path(kd.anchor))))[0] == 2


def test_glob_pattern_that_names_the_key_dir_is_refused(repo, tq_home):
    kd = str(gate.key_dir()).replace("\\", "/")
    assert hook(repo, _key_tool("Glob", pattern=kd + "/*"))[0] == 2
    assert hook(repo, _key_tool("Glob", pattern=kd + "/hm*.key"))[0] == 2
    assert hook(repo, _key_tool("Glob", pattern=str(tq_home).replace("\\", "/") + "/*/*.key"))[0] == 2
    assert hook(repo, _key_tool("Glob", pattern="**/*.py", path=str(repo)))[0] == 0


def test_search_tools_default_to_the_cwd_when_it_is_an_ancestor_of_the_key(repo, tq_home):
    payload = {"cwd": str(tq_home.parent), "tool_name": "Glob", "tool_input": {"pattern": "**/hmac.key"},
               "session_id": "s1"}
    # the project root cannot be found from here, but the key rule does not need it
    assert gate.hook(payload)[0] == 2


def test_search_tools_on_ordinary_paths_are_allowed(repo):
    approve_and_lock(repo)
    assert hook(repo, _key_tool("Grep", pattern="x", path=str(repo / "plugin")))[0] == 0
    assert hook(repo, _key_tool("Grep", pattern="x", path="plugin/"))[0] == 0
    assert hook(repo, _key_tool("Grep", pattern="x", path=str(repo)))[0] == 0
    assert hook(repo, _key_tool("Glob", pattern="**/*.py", path=str(repo)))[0] == 0
    assert hook(repo, _key_tool("Read", file_path=str(repo / "app.py")))[0] == 0
    assert hook(repo, _key_tool("Grep", pattern="x"))[0] == 0


@pytest.mark.parametrize("cmd,tool", [
    ("cat $TQ_HOME/*/*.key", "Bash"),
    ("cat ${TQ_HOME}/build-gate/hm*", "Bash"),
    ("cat $TQ_HOME/b*/hmac.key", "Bash"),
    ("cat %TQ_HOME%\\*\\*.key", "Bash"),
    ('find "$TQ_HOME" -name "hm*" -exec cat {} +', "Bash"),
    ('find "$TQ_HOME" -type f | xargs cat', "Bash"),
    ('grep -ra . "$TQ_HOME"', "Bash"),
    ("grep -rn secret $TQ_HOME", "Bash"),
    ("grep -R . ${TQ_HOME}", "Bash"),
    ("rg . $TQ_HOME", "Bash"),
    ("cp -r $TQ_HOME /tmp/x", "Bash"),
    ("cp $TQ_HOME/* /tmp/x -r", "Bash"),
    ("Get-ChildItem $env:TQ_HOME -Recurse | Get-Content", "PowerShell"),
    ("Get-Content $env:TQ_HOME\\*\\*.key", "PowerShell"),
])
def test_shell_reads_of_the_key_through_globs_variables_and_recursion(repo, cmd, tool):
    assert hook(repo, bash(cmd, tool))[0] == 2, cmd


def test_shell_key_reads_via_tilde_when_tq_home_is_the_default(repo, tmp_path, monkeypatch):
    home = tmp_path / "home"
    (home / ".tentaqles" / "build-gate").mkdir(parents=True)
    monkeypatch.delenv("TQ_HOME", raising=False)
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.setenv("USERPROFILE", str(home))
    for cmd in ("cat ~/.tentaqles/*/*.key", "grep -r x ~", "grep -ra . ~/.tentaqles",
                "cat $HOME/.tentaqles/build-gate/hm*", "cat ~/.tentaqles/build-gate/hmac.key"):
        assert hook(repo, bash(cmd))[0] == 2, cmd
    for cmd in ("cat ~/notes.txt", "ls ~", "grep x ~/.bashrc", "cat ~/.tentaqles/knowledge.yaml"):
        assert hook(repo, bash(cmd))[0] == 0, cmd


@pytest.mark.parametrize("cmd", [
    "cat $TQ_HOME/config.yaml",
    "ls $TQ_HOME",
    "grep -r foo plugin/",
    "grep -rn foo .",
    "find . -name '*.py' -exec wc -l {} +",
    "cat $HOME/notes.txt",
    "rm -r build/",
])
def test_shell_commands_beside_the_key_dir_stay_allowed(repo, cmd):
    assert hook(repo, bash(cmd))[0] == 0, cmd


# ---------------------------------------------------------------------------
# Rollback detection: a generation counter kept with the key
# ---------------------------------------------------------------------------


def _approved_path(root):
    return root / ".claude" / "build" / "demo" / "approved.json"


def _gen_record(root):
    return gate.gen_record_path(root, "demo")


def test_every_state_transition_increments_the_generation(repo):
    gate.cmd_approve(repo, "demo")
    a1 = json.loads(_approved_path(repo).read_text(encoding="utf-8"))
    assert a1["gen"] == 1 and gate.recorded_gen(repo, "demo") == 1
    gate.cmd_lock_tests(repo, "demo", 2, [str(repo / "tests" / "test_a.py")], none=False)
    assert gate.load_approved(repo, "demo")["gen"] == 2 == gate.recorded_gen(repo, "demo")
    gate.cmd_unlock_tests(repo, "demo", [str(repo / "tests" / "test_a.py")])
    assert gate.load_approved(repo, "demo")["gen"] == 3
    gate.cmd_approve(repo, "demo")
    assert gate.load_approved(repo, "demo")["gen"] == 4
    gate.cmd_finish(repo, "demo")
    assert gate.load_approved(repo, "demo")["gen"] == 5 == gate.recorded_gen(repo, "demo")
    # the record lives with the key, outside the repository
    assert gate.key_dir() in _gen_record(repo).parents


def test_dod_override_and_answers_increment_the_generation(repo):
    gate.cmd_approve(repo, "demo")
    g = gate.recorded_gen(repo, "demo")
    gate.cmd_dod(repo, "demo", "the human accepts it")
    assert gate.recorded_gen(repo, "demo") == g + 1 == gate.load_approved(repo, "demo")["gen"]
    did = gate.cmd_ask(repo, "demo", "question", "pick one", ["a", "b"]).split()[1]
    gate.answer_decision(repo, "demo", did, "a", "", "terminal")
    assert gate.recorded_gen(repo, "demo") == g + 2 == gate.load_approved(repo, "demo")["gen"]


def test_an_older_signed_approval_copied_back_counts_as_rolled_back(repo, monkeypatch):
    gate.cmd_approve(repo, "demo")
    gen1 = _approved_path(repo).read_text(encoding="utf-8")
    gate.cmd_lock_tests(repo, "demo", 2, [str(repo / "tests" / "test_a.py")], none=False)
    hook(repo, agent("[checkpoint demo#2 implement]"))  # this session owns the build
    assert gate.load_approved(repo, "demo")["gen"] == 2
    _approved_path(repo).write_text(gen1, encoding="utf-8")  # the gen-1 file, validly signed
    old = json.loads(gen1)
    assert gate.verified(old) is False
    assert gate.load_approved(repo, "demo") is None
    assert gate.rolled_back(repo, "demo") is True
    # the locked test stays refused (gen 1 had no locks)
    edit = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "tests" / "test_a.py"), "content": "x"}}
    code, _, err = hook(repo, edit)
    assert code == 2 and "rolled back" in err
    assert hook(repo, bash("echo x > tests/test_a.py"))[0] == 2
    assert hook(repo, agent("[checkpoint demo#2 implement]"))[0] == 2
    assert hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed")))[0] == 2
    # every gate decision refuses
    code, msg = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and "rolled back" in msg
    code, msg = gate.cmd_dod(repo, "demo")
    assert code == 1 and "rolled back" in msg
    st = gate.cmd_state(repo, "demo")
    assert st["rolled_back"] is True and st["approved"] is False
    monkeypatch.setenv("TQ_BUILD_ACTOR", "hub")
    monkeypatch.setenv("TQ_BUILD_TOKEN", gate.mint_hub_token("finish", "demo", "hub"))
    assert gate.hub_actor_ok("finish", "demo", repo) is None
    # Stop blocks once with the rollback message
    code, _, err = stop(repo)
    assert code == 2 and "build state was rolled back or altered; a human must re-approve" in err
    assert "build-gate approve demo" in err
    assert stop(repo)[0] == 0
    # the human re-approves: a fresh generation, everything normal again
    gate.cmd_approve(repo, "demo")
    a = gate.load_approved(repo, "demo")
    assert a["gen"] == 3 and gate.recorded_gen(repo, "demo") == 3
    assert gate.rolled_back(repo, "demo") is False and gate.cmd_state(repo, "demo")["rolled_back"] is False
    # the locks recorded with the generation survive the re-approval
    assert "tests/test_a.py" in a["locked_tests"]
    code, _, err = hook(repo, edit)
    assert code == 2 and "locked test" in err
    assert gate.cmd_verify(repo, "demo", 2)[0] == 0
    assert gate.hub_actor_ok("finish", "demo", repo) == "hub"
    assert hook(repo, bash("echo x > app2.py"))[0] == 0


def test_missing_generation_record_with_gen_above_zero_is_refused(repo):
    approve_and_lock(repo)
    _gen_record(repo).unlink()
    assert gate.load_approved(repo, "demo") is None
    assert gate.rolled_back(repo, "demo") is True
    assert gate.cmd_verify(repo, "demo", 2)[0] == 1
    edit = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "tests" / "test_a.py"), "content": "x"}}
    assert hook(repo, edit)[0] == 2


def test_generation_record_present_but_approval_absent_is_refused(repo):
    approve_and_lock(repo)
    _approved_path(repo).unlink()
    assert gate.rolled_back(repo, "demo") is True
    assert gate.cmd_state(repo, "demo")["rolled_back"] is True
    edit = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "tests" / "test_a.py"), "content": "x"}}
    code, _, err = hook(repo, edit)
    assert code == 2 and "rolled back" in err


def test_generation_record_is_per_project_and_slug(repo, tmp_path):
    gate.cmd_approve(repo, "demo")
    other = tmp_path / "other"
    other.mkdir()
    assert gate.gen_record_path(repo, "demo") != gate.gen_record_path(other, "demo")
    assert gate.gen_record_path(repo, "demo") != gate.gen_record_path(repo, "demo2")
    assert gate.project_id(repo) == gate.project_id(Path(str(repo).upper()) if os.name == "nt" else repo)


# ---------------------------------------------------------------------------
# Path canonicaliser: one function behind every path check
# ---------------------------------------------------------------------------

win_only = pytest.mark.skipif(os.name != "nt", reason="Windows path spellings")


def _short_name(p):
    """The 8.3 short spelling of an existing path, or None when the volume has none."""
    if os.name != "nt":
        return None
    import ctypes

    buf = ctypes.create_unicode_buffer(1024)
    n = ctypes.windll.kernel32.GetShortPathNameW(str(p), buf, 1024)
    if not n or Path(buf.value).name.lower() == Path(p).name.lower():
        return None
    return buf.value


def _junction(link, target):
    if os.name == "nt":
        subprocess.run(["cmd", "/c", "mklink", "/J", str(link), str(target)], check=True, capture_output=True)
    else:
        os.symlink(str(target), str(link))


def _under(path, top):
    p, t = gate.canon_path(path), gate.canon_path(top)
    return p == t or p.startswith(t.rstrip("/") + "/")


@win_only
@pytest.mark.parametrize("spelling", [
    r".claude.\build\demo\approved.json",
    r".claude. .\build\demo\approved.json",
    r".claude\build.\demo\approved.json",
    r".claude\build\demo\approved.json:x",
    r".claude\build\demo\approved.json::$DATA",
    r".claude\build\demo\approved.json. ",
])
def test_windows_spellings_resolve_under_the_state_dir(repo, spelling):
    assert _under(str(repo) + "\\" + spelling, repo / ".claude" / "build")
    code, _, err = hook(repo, {"tool_name": "Write", "tool_input": {"file_path": str(repo) + "\\" + spelling, "content": "{}"}})
    assert code == 2


@win_only
@pytest.mark.parametrize("prefix", ["\\\\?\\", "//?/", "\\\\.\\", "//./"])
def test_extended_prefixes_are_stripped(repo, tq_home, prefix):
    sep = "/" if prefix.startswith("//") else "\\"
    p = prefix + str(repo).replace("\\", sep) + sep + sep.join([".claude", "build", "demo", "approved.json"])
    assert _under(p, repo / ".claude" / "build")
    assert hook(repo, {"tool_name": "Write", "tool_input": {"file_path": p, "content": "{}"}})[0] == 2
    gate._key(create=True)
    k = prefix + str(tq_home).replace("\\", sep) + sep + "build-gate" + sep + "hmac.key"
    assert _under(k, gate.key_dir())
    assert hook(repo, {"tool_name": "Read", "tool_input": {"file_path": k}})[0] == 2


@win_only
def test_shell_spellings_resolve_under_the_state_dir(repo):
    gate.cmd_approve(repo, "demo")
    for c in ("echo x > .claude./build/demo/approved.json",
              "echo x > .claude/build/demo/approved.json:x",
              r"Set-Content -Path '.claude.\build\demo\approved.json' -Value x"):
        assert hook(repo, bash(c, "PowerShell" if c.startswith("Set-") else "Bash"))[0] == 2, c


@win_only
def test_alternate_data_stream_of_a_locked_test_is_the_locked_test(repo):
    approve_and_lock(repo)
    p = str(repo / "tests" / "test_a.py") + ":evil"
    code, _, err = hook(repo, {"tool_name": "Write", "tool_input": {"file_path": p, "content": "x"}})
    assert code == 2 and "locked test" in err


def test_a_junction_into_dot_claude_resolves_to_the_state_dir(repo):
    (repo / "lib").mkdir()
    _junction(repo / "lib" / "j", repo / ".claude")
    p = repo / "lib" / "j" / "build" / "demo" / "approved.json"
    assert _under(p, repo / ".claude" / "build")
    assert _under(repo / "lib" / "j" / "build" / "demo" / "new.json", repo / ".claude" / "build")
    assert hook(repo, {"tool_name": "Write", "tool_input": {"file_path": str(p), "content": "{}"}})[0] == 2
    assert hook(repo, bash("echo x > lib/j/build/demo/approved.json"))[0] == 2


def test_a_short_name_resolves_to_the_state_dir(repo):
    short = _short_name(repo / ".claude")
    if not short:
        pytest.skip("this volume has no 8.3 short names")
    p = short + r"\build\demo\approved.json"
    assert _under(p, repo / ".claude" / "build")
    assert hook(repo, {"tool_name": "Write", "tool_input": {"file_path": p, "content": "{}"}})[0] == 2


def test_a_short_name_resolves_to_the_key_dir(repo, tq_home):
    gate._key(create=True)
    short = _short_name(gate.key_dir())
    if not short:
        pytest.skip("this volume has no 8.3 short names")
    assert Path(short).name.upper().startswith("BUILD-")
    k = short + r"\hmac.key"
    assert _under(k, gate.key_dir())
    assert hook(repo, {"tool_name": "Read", "tool_input": {"file_path": k}})[0] == 2


def test_canon_path_leaves_ordinary_paths_alone(repo):
    for rel in ("app.py", "src/a.b/c.txt", "docs/x.md", "tests/test_a.py", "deep/new/file.txt"):
        got = gate.canon_path(repo / rel)
        want = gate.canon_path(repo) + "/" + (rel.lower() if os.name == "nt" else rel)
        assert got == want
    assert gate.canon_path(repo / "src" / ".." / "app.py") == gate.canon_path(repo / "app.py")
    if os.name == "nt":
        assert gate.canon_path("C:\\") == "c:/"
        assert gate.canon_path("C:/x/y.txt") == "c:/x/y.txt"
    assert hook(repo, {"tool_name": "Write", "tool_input": {"file_path": str(repo / "app.py"), "content": "x"}})[0] == 0



# ---------------------------------------------------------------------------
# Project root: every git root from the cwd upward, and CLAUDE_PROJECT_DIR
# ---------------------------------------------------------------------------


def _nested(repo):
    sub = repo / "lib" / "sub"
    sub.mkdir(parents=True)
    _git(sub, "init", "-q")
    return sub


def test_a_nested_git_repo_does_not_hide_the_outer_state_dir(repo):
    gate.cmd_approve(repo, "demo")
    sub = _nested(repo)
    for c in ("echo x > ../../.claude/build/demo/approved.json",
              "cp ../../x.json ../../.claude/build/demo/approved.json",
              "cd ../.. && echo x > .claude/build/demo/approved.json"):
        assert hook(repo, {**bash(c), "cwd": str(sub)})[0] == 2, c
    p = str(repo / ".claude" / "build" / "demo" / "approved.json")
    assert hook(repo, {"tool_name": "Write", "tool_input": {"file_path": p, "content": "{}"}, "cwd": str(sub)})[0] == 2
    # ordinary work inside the nested repo stays allowed
    assert hook(repo, {**bash("echo x > notes.txt"), "cwd": str(sub)})[0] == 0
    assert hook(repo, {"tool_name": "Write", "tool_input": {"file_path": str(sub / "a.py"), "content": "x"},
                       "cwd": str(sub)})[0] == 0


def test_a_nested_git_repo_does_not_hide_the_outer_locked_tests(repo):
    sub = _nested(repo)
    (sub / "test_x.py").write_text("def test_x():\n    assert True\n", encoding="utf-8")
    approve_and_lock(repo, paths=("lib/sub/test_x.py",))
    w = {"tool_name": "Write", "tool_input": {"file_path": str(sub / "test_x.py"), "content": "x"}, "cwd": str(sub)}
    code, _, err = hook(repo, w)
    assert code == 2 and "locked test" in err
    assert hook(repo, {**bash("echo x > test_x.py"), "cwd": str(sub)})[0] == 2


def test_claude_project_dir_is_judged_too(repo, tmp_path, monkeypatch):
    approve_and_lock(repo)
    other = tmp_path / "other"
    (other / ".claude" / "build").mkdir(parents=True)
    _git(other, "init", "-q")
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    target = (repo / "tests" / "test_a.py").as_posix()
    code, _, err = hook(repo, {**bash(f"echo x > '{target}'"), "cwd": str(other)})
    assert code == 2 and "locked test" in err
    assert hook(repo, {**bash("echo x > notes.txt"), "cwd": str(other)})[0] == 0



# ---------------------------------------------------------------------------
# Defence in depth: escapes, more writers, PowerShell bodies, locked-test parents
# ---------------------------------------------------------------------------

from tentaqles.workflow import _common  # noqa: E402


def test_backslash_is_a_shell_escape_under_bash():
    segs = _common.lex_shell("echo \\' ; cp a b ; echo \\'")
    assert [s.argv[0] for s in segs] == ["echo", "cp", "echo"]
    segs = _common.lex_shell('echo \\" ; cp a b ; echo \\"')
    assert [s.argv[0] for s in segs] == ["echo", "cp", "echo"]
    # Windows paths keep their backslashes for the word readings
    assert _common.lex_shell(r"cat C:\repo\x.txt")[0].argv == ["cat", r"C:\repo\x.txt"]
    assert _common.lex_shell(r"cat .claude\build\x")[0].argv == ["cat", r".claude\build\x"]


@pytest.mark.parametrize("cmd", [
    "echo \\' ; cp x.json .claude/build/demo/approved.json ; echo \\'",
    'echo \\" ; cp x.json .claude/build/demo/approved.json ; echo \\"',
    'echo \\" ; cp x.py tests/test_a.py ; echo \\"',
])
def test_escaped_quotes_do_not_hide_a_write(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2


def test_powershell_lexer_backtick_escape():
    segs = _common.lex_shell('Write-Output `"; Copy-Item x tests/t.py; Write-Output `"', ps=True)
    assert [s.argv[0] for s in segs] == ["Write-Output", "Copy-Item", "Write-Output"]
    segs = _common.lex_shell("Write-Output 'it''s; fine'", ps=True)
    assert len(segs) == 1 and segs[0].argv == ["Write-Output", "it's; fine"]
    assert _common.lex_shell(r"Get-Content C:\repo\x.txt", ps=True)[0].argv == ["Get-Content", r"C:\repo\x.txt"]
    segs = _common.lex_shell('Set-Content -Path "a`"b" -Value x > out.txt', ps=True)
    assert segs[0].argv[:3] == ["Set-Content", "-Path", 'a"b'] and segs[0].redirects == ["out.txt"]


@pytest.mark.parametrize("cmd,tool", [
    ("pwsh -c 'Write-Output `\"; Copy-Item x.py tests/test_a.py; Write-Output `\"'", "Bash"),
    ("powershell -Command 'Write-Output `\"; Copy-Item x.py tests/test_a.py; Write-Output `\"'", "Bash"),
    ('Write-Output `"; Copy-Item x.py tests/test_a.py; Write-Output `"', "PowerShell"),
])
def test_powershell_bodies_are_lexed_as_powershell(repo, cmd, tool):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd, tool))[0] == 2


@pytest.mark.parametrize("cmd", [
    "find .claude/build -name '*.json' -delete",
    "find .claude -name approved.json -exec cp x.json {} ;",
    "find tests -delete",
    "fsutil hardlink create .claude/build/demo/approved.json x.json",
    "fsutil file setZeroData offset=0 length=9 tests/test_a.py",
    "cmd /c mklink /J lib\\j .claude",
    "mklink /J lib\\j .claude",
    "cmd /c copy x.json .claude\\build\\demo\\approved.json",
    "cmd /c move x.py tests\\test_a.py",
])
def test_more_writers_are_writers(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2, cmd


def test_find_exec_and_delete_count_as_writes():
    for c in ("find . -name x -delete", "find . -exec rm {} ;", "find . -execdir touch {} +",
              "fsutil file createnew x 0", "mklink a b"):
        assert _common.is_write_segment(_common.lex_shell(c)[0]), c
    for c in ("find . -name x", "find . -type f -print"):
        assert not _common.is_write_segment(_common.lex_shell(c)[0]), c


@pytest.mark.parametrize("cmd", [
    "mv tests tests2",
    "rm -rf tests",
    "rm -rf ./tests/",
    "cp -r src tests",
    "Remove-Item -Recurse tests",
    "Move-Item tests tests2",
    "rm -rf .",
])
def test_writes_to_a_locked_tests_parent_dir_are_writes_to_the_locked_test(repo, cmd):
    approve_and_lock(repo)
    code, _, err = hook(repo, bash(cmd))
    assert code == 2 and "locked test" in err, cmd


@pytest.mark.parametrize("cmd", [
    "mv app.py app2.py",
    "rm -rf build",
    "ls tests",
    "cat tests/test_a.py",
    "mkdir -p src/tests",
    "find . -name '*.py'",
    "echo hi > notes.txt",
])
def test_ordinary_commands_beside_locked_tests_stay_allowed(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 0, cmd


# ---------------------------------------------------------------------------
# State-file binding: every signed state file and record names its build and generation
# ---------------------------------------------------------------------------


def shutil_copy(src, dst):
    import shutil

    shutil.copyfile(src, dst)


def _state(root, slug="demo"):
    return root / ".claude" / "build" / slug


def _second_build(root, slug):
    plan = copy.deepcopy(PLAN)
    plan["slug"] = slug
    (root / "docs" / "checkpoints" / f"{slug}.json").write_text(json.dumps(plan), encoding="utf-8")
    gate.cmd_approve(root, slug)
    gate.cmd_finish(root, slug)


def _settings_write(root):
    return {"tool_name": "Write", "tool_input": {"file_path": str(root / ".claude" / "settings.local.json"),
                                                 "content": "{}"}}


def test_finished_session_from_another_slug_does_not_end_the_build(repo):
    approve_and_lock(repo)
    hook(repo, agent("[checkpoint demo#2 implement]"))
    assert hook(repo, _settings_write(repo))[0] == 2
    _second_build(repo, "old")
    assert gate.load_session(repo, "old").get("finished")
    shutil_copy(_state(repo, "old") / "session.json", _state(repo) / "session.json")
    assert gate._build_exists(repo) is True
    assert gate.rolled_back(repo, "demo") is True
    assert gate.load_session(repo, "demo") == {}
    assert hook(repo, _settings_write(repo))[0] == 2
    # Stop blocks once with the re-approval message, whichever session asks
    code, _, err = stop(repo, session="other")
    assert code == 2 and "build-gate approve demo" in err
    assert stop(repo, session="other")[0] == 0


def test_finished_session_from_another_project_does_not_end_the_build(repo, tmp_path):
    approve_and_lock(repo)
    other = tmp_path / "other"
    (other / "docs" / "checkpoints").mkdir(parents=True)
    _git(other, "init", "-q", "-b", "work")
    (other / "docs" / "checkpoints" / "demo.json").write_text(json.dumps(PLAN), encoding="utf-8")
    gate.cmd_approve(other, "demo")
    gate.cmd_finish(other, "demo")
    shutil_copy(_state(other) / "session.json", _state(repo) / "session.json")
    assert gate._build_exists(repo) is True and gate.rolled_back(repo, "demo") is True
    assert hook(repo, _settings_write(repo))[0] == 2


def test_session_claiming_finished_against_the_record_is_a_rollback(repo):
    approve_and_lock(repo)
    rec = gate._gen_record(repo, "demo")
    assert rec["finished"] is False
    # a session the gate itself signed for this build and gen, but claiming finished
    gate.save_session(repo, "demo", {**gate.load_session(repo, "demo"), "finished": "2026-01-01"})
    assert gate.rolled_back(repo, "demo") is True and gate._build_exists(repo) is True
    gate.cmd_approve(repo, "demo")
    assert gate.rolled_back(repo, "demo") is False


def test_finish_is_recorded_in_the_generation_record(repo):
    approve_and_lock(repo)
    gate.cmd_finish(repo, "demo")
    assert gate._gen_record(repo, "demo")["finished"] is True
    assert gate._build_exists(repo) is False
    gate.cmd_approve(repo, "demo")
    assert gate._gen_record(repo, "demo")["finished"] is False and gate._build_exists(repo) is True


def test_an_old_dod_record_copied_back_is_not_fresh(repo):
    gate.cmd_approve(repo, "demo")
    gate.cmd_dod(repo, "demo", "the human accepts it")
    th = gate.TreeHash(repo)
    assert gate.dod_fresh(repo, "demo", th)[0] is True
    old = (_state(repo) / "dod.json").read_text(encoding="utf-8")
    gate.cmd_lock_tests(repo, "demo", 2, [str(repo / "tests" / "test_a.py")], none=False)
    assert gate.dod_fresh(repo, "demo", gate.TreeHash(repo))[0] is True  # carried to the new gen
    (_state(repo) / "dod.json").write_text(old, encoding="utf-8")
    ok, why = gate.dod_fresh(repo, "demo", gate.TreeHash(repo))
    assert ok is False and "definition-of-done" in why


def test_an_old_review_record_is_ignored(repo):
    gate.cmd_approve(repo, "demo")
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    tree = gate.tree_hash(repo)
    assert "build-critic" in gate.reviews_for(repo, "demo", 2, tree)
    old = (_state(repo) / "reviews.jsonl").read_text(encoding="utf-8")
    gate.cmd_lock_tests(repo, "demo", 2, [str(repo / "tests" / "test_a.py")], none=False)
    assert "build-critic" in gate.reviews_for(repo, "demo", 2, tree)  # live records follow the gen
    (_state(repo) / "reviews.jsonl").write_text(old, encoding="utf-8")
    assert gate.reviews_for(repo, "demo", 2, tree) == {}


def test_review_and_decision_records_of_another_slug_are_ignored(repo):
    _second_build(repo, "old")
    gate.cmd_approve(repo, "old")
    gate.cmd_record_review(repo, "old", 2, "build-critic", "PASS", {})
    gate.cmd_ask(repo, "old", "question", "pick", ["a", "b"])
    gate.cmd_approve(repo, "demo")
    for name in ("reviews.jsonl", "decisions.jsonl"):
        shutil_copy(_state(repo, "old") / name, _state(repo) / name)
    assert gate.reviews_for(repo, "demo", 2, gate.tree_hash(repo)) == {}
    assert gate.pending_decisions(repo, "demo") == []


def test_an_old_decision_record_is_ignored_and_live_ones_survive_a_transition(repo):
    gate.cmd_approve(repo, "demo")
    did = gate.cmd_ask(repo, "demo", "question", "pick", ["a", "b"]).split()[1]
    old = (_state(repo) / "decisions.jsonl").read_text(encoding="utf-8")
    gate.cmd_lock_tests(repo, "demo", 2, [str(repo / "tests" / "test_a.py")], none=False)
    assert [p["id"] for p in gate.pending_decisions(repo, "demo")] == [did]
    (_state(repo) / "decisions.jsonl").write_text(old, encoding="utf-8")
    assert gate.pending_decisions(repo, "demo") == []


def test_state_records_carry_project_slug_and_gen(repo):
    gate.cmd_approve(repo, "demo")
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    gate.cmd_ask(repo, "demo", "question", "pick", ["a", "b"])
    gate.cmd_dod(repo, "demo")
    g = gate.recorded_gen(repo, "demo")
    pid = gate.project_id(repo)
    recs = [json.loads(line) for name in ("reviews.jsonl", "decisions.jsonl")
            for line in (_state(repo) / name).read_text(encoding="utf-8").splitlines()]
    recs += [gate.read_json(_state(repo) / "session.json"), gate.read_json(_state(repo) / "dod.json")]
    for r in recs:
        assert (r["project_id"], r["slug"], r["gen"]) == (pid, "demo", g)


# ---------------------------------------------------------------------------
# State discovery: builds are found from the plan dir and the generation records too
# ---------------------------------------------------------------------------


def _locked_write(root):
    return {"tool_name": "Write", "tool_input": {"file_path": str(root / "tests" / "test_a.py"), "content": "x"}}


def test_removing_the_state_dir_does_not_hide_a_rollback(repo):
    import shutil

    approve_and_lock(repo)
    hook(repo, agent("[checkpoint demo#2 implement]"))
    shutil.rmtree(_state(repo))
    assert "demo" in gate.all_slugs(repo) and "demo" in gate.recorded_slugs(repo)
    assert gate.rolled_back_slugs(repo) == ["demo"]
    assert gate._build_exists(repo) is True
    code, _, err = hook(repo, _locked_write(repo))
    assert code == 2 and "rolled back" in err
    assert hook(repo, bash("echo x > tests/test_a.py"))[0] == 2
    code, _, err = stop(repo)
    assert code == 2 and "build-gate approve demo" in err
    assert stop(repo)[0] == 0
    assert stop(repo)[0] == 2  # the next turn is blocked once again


def test_a_build_with_only_a_generation_record_is_still_judged(repo):
    import shutil

    approve_and_lock(repo)
    shutil.rmtree(_state(repo))
    shutil.rmtree(repo / "docs" / "checkpoints")
    assert gate.all_slugs(repo) == ["demo"]
    code, _, err = hook(repo, _locked_write(repo))
    assert code == 2 and "rolled back" in err
    assert stop(repo)[0] == 2


def test_a_plan_without_state_or_record_is_not_rolled_back(repo):
    assert gate.all_slugs(repo) == ["demo"] and gate.rolled_back_slugs(repo) == []
    assert hook(repo, _locked_write(repo))[0] == 0
    assert stop(repo)[0] == 0


# ---------------------------------------------------------------------------
# Migration: an approval signed by the plugin before generations
# ---------------------------------------------------------------------------


def _legacy_approval(root):
    plan = gate.load_plan(root, "demo")
    sha = gate.file_sha256(root / "tests" / "test_a.py")
    data = {"slug": "demo", "approved_at": "2026-10-01T00:00:00+00:00", "approval_id": "0123456789abcdef",
            "base": None, "verify": {str(c["id"]): c["verify"] for c in plan["checkpoints"]},
            "fingerprints": {str(c["id"]): gate.plan_fingerprint(c) for c in plan["checkpoints"]},
            "locked_tests": {"tests/test_a.py": sha}, "tests_locked": {"2": ["tests/test_a.py"]},
            "workflow": "build", "hotfix": False}
    _state(root).mkdir(parents=True, exist_ok=True)
    gate.write_json(_state(root) / "approved.json", gate.sign(data))
    gate.write_json(_state(root) / "session.json", gate.sign({"session_id": "s1", "stage": "implement", "checkpoint": 2}))


def test_a_legacy_approval_is_reported_and_refused_until_migrated(repo):
    _legacy_approval(repo)
    assert not gate._gen_record(repo, "demo")
    assert gate.load_approved(repo, "demo") is None
    msg = gate.state_problem(repo, "demo")
    assert msg == "legacy approval; run `build-gate approve demo` to migrate"
    assert gate.rolled_back(repo, "demo") is True
    code, _, err = hook(repo, _locked_write(repo))
    assert code == 2 and "legacy approval" in err
    assert hook(repo, _settings_write(repo))[0] == 2
    code, out = gate.cmd_verify(repo, "demo", 2)
    assert code == 1 and "legacy approval" in out
    assert gate.cmd_state(repo, "demo")["rolled_back"] is True
    code, _, err = stop(repo)
    assert code == 2 and "legacy approval; run `build-gate approve demo` to migrate" in err
    assert stop(repo)[0] == 0


def test_the_human_approve_migrates_a_legacy_approval_with_its_locks(repo):
    _legacy_approval(repo)
    gate.cmd_approve(repo, "demo")
    a = gate.load_approved(repo, "demo")
    assert a["gen"] == 1 and gate.state_problem(repo, "demo") is None
    assert "tests/test_a.py" in a["locked_tests"] and a["tests_locked"] == {"2": ["tests/test_a.py"]}
    rec = gate._gen_record(repo, "demo")
    assert "tests/test_a.py" in rec["locked_tests"] and rec["finished"] is False
    assert gate.load_session(repo, "demo").get("session_id") == "s1"  # the legacy session carries over
    code, _, err = hook(repo, _locked_write(repo))
    assert code == 2 and "locked test" in err
    assert gate.cmd_verify(repo, "demo", 2)[0] == 0


def test_a_legacy_format_approval_over_a_recorded_build_is_a_rollback(repo):
    approve_and_lock(repo)
    data = {k: v for k, v in gate.load_approved(repo, "demo").items() if k not in ("kind", "gen", "project_id", "sig")}
    gate.write_json(_approved_path(repo), gate.sign(data))
    assert gate.state_problem(repo, "demo").startswith("build state was rolled back")


# ---------------------------------------------------------------------------
# Key directory: archive and sync tools, and any word naming the key dir or an ancestor
# ---------------------------------------------------------------------------


def _tq(tq_home):
    return str(tq_home).replace("\\", "/")


@pytest.mark.parametrize("form", [
    "tar cf t.tar {tq}",
    "tar cf t.tar -C {tq} build-gate",
    "tar --directory={tq} -cf t.tar build-gate",
    "tar cf t.tar -C {tq}/sub ../build-gate",
    "tar cf - {tq} | tar xf - -C out",
    "bsdtar cf t.tar {tq}",
    "7z a k.7z {tq}",
    "zip -r k.zip {tq}",
    "rsync -a {tq}/ bk/",
    "robocopy {tq} bk /E",
    "xcopy /E {tq} bk",
    "cpio -pdm {tq}",
    "pax -rw {tq} out",
    "tar cf t.tar {parent}",
    "cp -a {parent} bk",
    "du -a {tq}",
    "tar cf t.tar $TQ_HOME",
    "tar cf t.tar ${{TQ_HOME}}",
    "ls {tq} > list.txt",
    "ls -R {tq}",
])
def test_archive_and_sync_tools_cannot_read_the_key_dir(repo, tq_home, form):
    gate._key(create=True)
    (tq_home / "sub").mkdir(parents=True, exist_ok=True)
    cmd = form.format(tq=_tq(tq_home), parent=_tq(tq_home.parent))
    code, _, err = hook(repo, bash(cmd))
    assert code == 2 and "signing key" in err, cmd


@pytest.mark.parametrize("cmd", [
    "Compress-Archive -Path $env:TQ_HOME -DestinationPath k.zip",
    "tar cf t.tar $env:TQ_HOME",
    "Copy-Item -Recurse $env:TQ_HOME bk",
])
def test_powershell_archive_of_the_key_dir_is_refused(repo, cmd):
    gate._key(create=True)
    code, _, err = hook(repo, bash(cmd, "PowerShell"))
    assert code == 2 and "signing key" in err, cmd


def test_read_tool_refused_on_an_ancestor_of_the_key_dir(repo, tq_home):
    gate._key(create=True)
    for p in (tq_home, tq_home.parent):
        code, _, err = hook(repo, {"tool_name": "Read", "tool_input": {"file_path": str(p)}})
        assert code == 2 and "signing key" in err, p


@pytest.mark.parametrize("cmd", [
    "tar czf src.tgz src/",
    "ls ~",
    "ls {tq}",
    "cd {tq} && pwd",
    "stat {tq}",
    "test -d {tq}",
    "echo {tq}",
    "zip -r ../a.zip .",
])
def test_key_dir_ancestor_reads_by_listing_verbs_stay_allowed(repo, tq_home, cmd):
    gate._key(create=True)
    (repo / "src").mkdir()
    assert hook(repo, bash(cmd.format(tq=_tq(tq_home))))[0] == 0, cmd


def test_ls_home_is_allowed_when_the_key_is_under_home(repo, tmp_path, monkeypatch):
    home = tmp_path / "home"
    (home / ".tentaqles" / "build-gate").mkdir(parents=True)
    monkeypatch.delenv("TQ_HOME", raising=False)
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.setenv("USERPROFILE", str(home))
    assert hook(repo, bash("ls ~"))[0] == 0
    assert hook(repo, bash("ls -la ~"))[0] == 0
    for cmd in ("tar cf t.tar ~", "tar cf t.tar -C ~ .tentaqles", "rsync -a ~/ bk/", "zip -r h.zip $HOME"):
        assert hook(repo, bash(cmd))[0] == 2, cmd


# ---------------------------------------------------------------------------
# Locked-test parent dirs: only destination and removal operands count
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("cmd", [
    "pytest tests > out.txt",
    "ls tests > list.txt",
    "cp -r . ../bk",
    "tar czf ../src.tgz .",
    "echo . > out.txt",
    "echo tests > out.txt",
])
def test_reading_a_locked_tests_parent_dir_is_not_a_write_to_it(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 0, cmd


def test_echo_home_into_a_file_is_allowed_when_the_repo_is_under_home(tmp_path, monkeypatch):
    home = tmp_path / "home"
    root = home / "proj"
    (root / "docs" / "checkpoints").mkdir(parents=True)
    (root / "tests").mkdir()
    _git(root, "init", "-q", "-b", "work")
    (root / "tests" / "test_a.py").write_text("def test_a():\n    assert True\n", encoding="utf-8")
    save_plan(root, PLAN)
    (root / ".claude" / "build").mkdir(parents=True)
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.setenv("USERPROFILE", str(home))
    approve_and_lock(root)
    assert hook(root, bash("echo $HOME > out.txt"))[0] == 0
    code, _, err = hook(root, bash("rm -rf $HOME/proj/tests"))
    assert code == 2


@pytest.mark.parametrize("cmd", [
    "mv tests tests2",
    "rm -rf tests",
    "cp -r src tests",
    "rsync -a src/ tests/",
    "find tests -delete",
    "pytest > tests",
    "tar xf a.tar",
    "cp x.py -t tests",
])
def test_destination_and_removal_operands_on_a_locked_tests_parent_stay_refused(repo, cmd):
    approve_and_lock(repo)
    code, _, err = hook(repo, bash(cmd))
    assert code == 2 and "locked test" in err, cmd


# ---------------------------------------------------------------------------
# Round 5: cleaned-up finished builds, session binding, run-time file lists
# ---------------------------------------------------------------------------


def _clean_up(root, slug="demo", plan=True):
    import shutil

    shutil.rmtree(_state(root, slug), ignore_errors=True)
    if plan:
        (root / "docs" / "checkpoints" / f"{slug}.json").unlink()


def test_a_finished_build_whose_state_and_plan_were_removed_does_not_lock_the_project(repo):
    approve_and_lock(repo)
    gate.cmd_finish(repo, "demo")
    _clean_up(repo)
    assert "demo" in gate.recorded_slugs(repo)
    assert gate.rolled_back(repo, "demo") is False and gate.rolled_back_slugs(repo) == []
    assert gate._build_exists(repo) is False
    assert hook(repo, _settings_write(repo))[0] == 0
    assert hook(repo, bash("echo x > out.txt"))[0] == 0
    assert stop(repo)[0] == 0 and stop(repo)[0] == 0
    st = gate.cmd_state(repo, "demo")
    assert st["finished"] is True and st["cleaned"] is True and st["rolled_back"] is False


def test_a_finished_build_with_a_tampered_approval_is_still_a_rollback(repo):
    approve_and_lock(repo)
    gate.cmd_finish(repo, "demo")
    (_state(repo) / "approved.json").write_text("{}", encoding="utf-8")
    assert gate.rolled_back(repo, "demo") is True


def test_an_unfinished_build_whose_state_dir_was_removed_stays_a_rollback(repo):
    approve_and_lock(repo)
    _clean_up(repo)
    assert gate.rolled_back(repo, "demo") is True and gate._build_exists(repo) is True
    assert hook(repo, _settings_write(repo))[0] == 2
    assert stop(repo)[0] == 2


def test_approve_of_a_finished_cleaned_slug_says_to_write_a_new_plan(repo):
    approve_and_lock(repo)
    gate.cmd_finish(repo, "demo")
    _clean_up(repo)
    with pytest.raises(SystemExit) as e:
        gate.cmd_approve(repo, "demo")
    assert "build demo is finished; write a new plan to start another" in str(e.value)
    # a slug with no record keeps the old message
    with pytest.raises(SystemExit) as e2:
        gate.cmd_approve(repo, "nothing")
    assert "no plan" in str(e2.value)


def test_deleting_session_json_during_a_build_is_a_rollback_and_keeps_the_review_base(repo):
    approve_and_lock(repo)
    hook(repo, agent("[checkpoint demo#2 implement]"))
    base = gate.load_session(repo, "demo")["bases"]["2"]
    (repo / "app.py").write_text("def f():\n    return 2\n", encoding="utf-8")
    _git(repo, "commit", "-q", "-am", "impl")
    (_state(repo) / "session.json").unlink()
    assert gate.rolled_back(repo, "demo") is True and gate._build_exists(repo) is True
    code, _, err = hook(repo, _locked_write(repo))
    assert code == 2 and "rolled back" in err
    assert stop(repo)[0] == 2 and stop(repo)[0] == 0
    assert gate.review_bases(repo, "demo")["2"] == base
    head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, capture_output=True, text=True).stdout.strip()
    assert head != base
    out = gate.cmd_review_bundle(repo, "demo", 2)
    assert f"base={base}" in out and f"Base: {base}" in Path(out.splitlines()[0]).read_text(encoding="utf-8")
    # a human re-approval brings the session back; the next implement marker keeps the recorded base
    gate.cmd_approve(repo, "demo")
    assert hook(repo, agent("[checkpoint demo#2 implement]"))[0] == 0
    assert gate.load_session(repo, "demo")["bases"]["2"] == base


@pytest.mark.parametrize("cmd,want", [
    ("tar czf x.tgz -T list.txt", 2),
    ("tar -czf x.tgz --files-from=list.txt", 2),
    ("tar cf x.tar --files-from list.txt", 2),
    ("tar cf x.tar -T- < list.txt", 2),
    ("cpio -i < list.cpio", 2),
    ("cpio -o < list.txt > a.cpio", 2),
    ("tar czf x.tgz src/", 0),
])
def test_run_time_file_lists_for_archive_tools_are_refused_while_a_build_exists(repo, cmd, want):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == want, cmd


def test_echo_home_into_a_file_when_tq_home_is_under_home(tmp_path, monkeypatch):
    home = tmp_path / "home"
    root = home / "proj"
    (root / "docs" / "checkpoints").mkdir(parents=True)
    (root / "tests").mkdir()
    _git(root, "init", "-q", "-b", "work")
    (root / "tests" / "test_a.py").write_text("def test_a():\n    assert True\n", encoding="utf-8")
    save_plan(root, PLAN)
    (root / ".claude" / "build").mkdir(parents=True)
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.setenv("USERPROFILE", str(home))
    monkeypatch.setenv("TQ_HOME", str(home / ".tq"))
    approve_and_lock(root)
    assert hook(root, bash("echo $HOME > out.txt"))[0] == 0
    assert hook(root, bash('echo "$HOME" >> out.txt'))[0] == 0
    assert hook(root, bash("tar czf x.tgz $HOME"))[0] == 2
    assert hook(root, bash("cat $HOME/.tq/build-gate/hmac.key"))[0] == 2


# --- merge of the spine: link and option-write rules inside _ShellScan ------------------------


@pytest.mark.parametrize("cmd", [
    r"cd .tq-breaker && cmd /c mklink /J demo C:\outside",
    "cd .tq-breaker/demo && ln -s ../../app.py l.txt",
    "cd lib && ln -s ../.claude/build/demo/approved.json x",
    "cd .claude && ln -s build/demo/approved.json x",
])
def test_link_rule_follows_the_tracked_cwd(repo, cmd):
    (repo / "lib").mkdir()
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2


@pytest.mark.parametrize("cmd", [
    "mysqldump --result-file=tests/test_a.py app",
    "mysqldump -r tests/test_a.py app",
    "cd tests && pg_dump -w --file test_a.py app",
    "cd tests && curl -s -o test_a.py http://127.0.0.1:3000/",
    'cd tests && sqlite3 ../app.db ".backup main test_a.py"',
])
def test_option_writes_feed_the_destination_check(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2


@pytest.mark.parametrize("cmd", [
    "grep --file pats.txt tests/test_a.py",
    "docker build --file Dockerfile .",
    "git log --oneline -- tests/test_a.py",
    "tar -tf a.tar",
    "sort pats.txt",
])
def test_file_option_is_no_write_for_readers(repo, cmd):
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 0


@pytest.mark.parametrize("slug", ["..", ".", "Demo", "a/b", "-x", ""])
def test_approve_refuses_a_slug_finish_would_refuse(repo, slug):
    with pytest.raises(SystemExit, match="approve refused"):
        gate.cmd_approve(repo, slug)


# Pre-ship fixes: bundled short flags and the scratch class on destinations only.
@pytest.mark.parametrize("cmd", [
    "curl -sSo tests/test_a.py file:///etc/hosts",
    "wget -qO tests/test_a.py file:///etc/hosts",
    "pg_dump -wf .claude/build/demo/x.sql app",
    "sort -ro tests/test_a.py in.txt",
])
def test_bundled_short_flag_output_writes_are_judged(repo, cmd):
    _active_build(repo)
    approve_and_lock(repo)
    assert hook(repo, bash(cmd))[0] == 2, cmd


def test_bundled_short_flag_output_to_a_plain_file_is_allowed(repo):
    _active_build(repo)
    assert hook(repo, bash("curl -sS https://x -o out.json"))[0] == 0
    assert hook(repo, bash("curl -sSo out.json https://x"))[0] == 0


def test_scratch_class_applies_to_destinations_only(repo):
    _active_build(repo)
    (repo / ".tq-breaker" / "demo").mkdir(parents=True)
    assert hook(repo, bash("cd .tq-breaker/demo && echo x > a.log"))[0] == 0
    assert hook(repo, bash("cd .tq-breaker/demo && echo x > conftest.py"))[0] == 2
    assert hook(repo, bash("cd .tq-breaker/demo && pg_dump -w --file db.sql app"))[0] == 0
