"""Overnight loop (/tentaqles:loop): budgets, keep/undo, tamper, explicit-path commits,
MCP/network lockdown flags, and the scorer timeout. `claude -p` is replaced by
tests/fixtures/fake_claude.py; nothing here calls a model or the network."""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

from tentaqles.workflow import loop
from tentaqles.workflow.loop import Budget

FAKE = Path(__file__).resolve().parent / "fixtures" / "fake_claude.py"
SCORE_CMD = "{python} -m pytest checks/test_add.py -q -p no:cacheprovider --junitxml={xml}"

CHECKS = """from app import add


def test_positive():
    assert add(2, 3) == 5


def test_zero():
    assert add(0, 0) == 0


def test_negative():
    assert add(-2, -3) == -5
"""
STUB = "def add(a, b):\n    return 0\n"
PARTIAL = "def add(a, b):\n    return a + b if a >= 0 else 0\n"
FULL = "def add(a, b):\n    return a + b\n"
WORSE = "def add(a, b):\n    raise RuntimeError('nope')\n"


def _git(root, *args):
    return subprocess.run(["git", *args], cwd=root, check=True, capture_output=True, text=True).stdout


@pytest.fixture(autouse=True)
def tq_home(tmp_path, monkeypatch):
    """Approvals are signed with a key under TQ_HOME; never touch the real one."""
    monkeypatch.setenv("TQ_HOME", str(tmp_path / "tqhome"))


@pytest.fixture
def repo(tmp_path):
    root = tmp_path / "app"
    (root / "checks").mkdir(parents=True)
    _git(root, "init", "-q", "-b", "main")
    _git(root, "config", "user.email", "dev@example.com")
    _git(root, "config", "user.name", "Dev")
    (root / "app.py").write_text(STUB, encoding="utf-8")
    (root / "checks" / "test_add.py").write_text(CHECKS, encoding="utf-8")
    (root / "FEATURES.md").write_text("## add\nAdds two integers.\n", encoding="utf-8")
    _git(root, "add", "app.py", "checks/test_add.py", "FEATURES.md")
    _git(root, "commit", "-q", "-m", "checks")
    return root


def approve(repo, **kw):
    return loop.approve(repo, "add", ["checks/test_add.py"], SCORE_CMD, spec_file="FEATURES.md", **kw)


def fake(tmp_path, rounds):
    plan = tmp_path / "fake-plan.json"
    plan.write_text(json.dumps({"rounds": rounds, "counter": str(tmp_path / "counter"),
                                "argv_log": str(tmp_path / "argv.jsonl")}), encoding="utf-8")
    return plan


def calls(tmp_path):
    p = tmp_path / "argv.jsonl"
    return [json.loads(x) for x in p.read_text(encoding="utf-8").splitlines()] if p.exists() else []


def go(repo, tmp_path, monkeypatch, rounds, budget=None, **kw):
    monkeypatch.setenv("FAKE_CLAUDE_PLAN", str(fake(tmp_path, rounds)))
    return loop.run_loop(repo, "add", budget=budget or Budget(rounds=5, score_timeout=120),
                         claude_cmd=[sys.executable, str(FAKE)], use_tq=False,
                         worktree_dir=tmp_path / "wt", run_id="t1", **kw)


# ---------------------------------------------------------------------------
# Approval
# ---------------------------------------------------------------------------


def test_approve_records_baseline_and_locks(repo):
    data = approve(repo)
    assert data["expected_total"] == 3
    assert data["baseline"]["passed"] == 1  # add(0, 0) == 0 already holds
    assert "checks/test_add.py" in data["locked"] and "**/conftest.py" in data["locked"]
    assert (repo / ".claude" / "loop" / "approved" / "add.json").exists()


def test_approve_rejects_uncommitted_checks(repo):
    (repo / "checks" / "test_add.py").write_text(CHECKS + "\n# edit\n", encoding="utf-8")
    with pytest.raises(ValueError, match="uncommitted"):
        approve(repo)


def test_run_without_approval_refuses(repo, tmp_path):
    with pytest.raises(SystemExit):
        loop.run_loop(repo, "add", use_tq=False, claude_cmd=[sys.executable, str(FAKE)])


# ---------------------------------------------------------------------------
# Rounds: keep / undo / done
# ---------------------------------------------------------------------------


def test_keep_undo_done(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [
        {"write": {"app.py": PARTIAL}, "cost": 0.1, "idea": "handle positives"},
        {"write": {"app.py": WORSE}, "cost": 0.1, "idea": "break it"},
        {"write": {"app.py": FULL}, "cost": 0.1, "idea": "plain sum"},
    ])
    assert s["stop_reason"] == "done" and s["best"] == "3/3"
    assert s["rounds"] == 3 and s["kept"] == 2 and s["undone"] == 1
    wt = tmp_path / "wt"
    assert _git(wt, "status", "--porcelain") == ""
    log = _git(wt, "log", "--format=%s")
    assert "loop(add) r1: 2/3 handle positives" in log and "r3: 3/3 plain sum" in log
    assert _git(wt, "rev-parse", "--abbrev-ref", "HEAD").strip() == "loop/add-t1"
    assert _git(repo, "rev-parse", "--abbrev-ref", "HEAD").strip() == "main"  # main checkout untouched
    run_dir = repo / ".claude" / "loop" / "runs" / "t1"
    rows = (run_dir / "results.tsv").read_text(encoding="utf-8").splitlines()
    assert [r.split("\t")[1] for r in rows[1:]] == ["baseline", "keep", "undo", "keep"]
    assert (repo / ".claude" / "loop" / "LATEST-add.md").read_text(encoding="utf-8").startswith("# Loop: add")
    first = calls(tmp_path)[0]
    assert first["headless"] == "1" and first["env_guard"] and first["prompt_has_rules"]


def test_commit_stages_explicit_paths_never_secrets(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [
        {"write": {"app.py": FULL, ".env": "PLACEHOLDER=1\n", "deploy/server.pem": "not a key\n",
                   "helpers/util.py": "X = 1\n"}, "cost": 0.1},
    ])
    assert s["best"] == "3/3"
    files = _git(tmp_path / "wt", "show", "--name-only", "--format=", "HEAD").split()
    assert sorted(files) == ["app.py", "helpers/util.py"]
    assert any(".env" in n for n in s["notes"])


def test_tamper_with_checks_is_undone(repo, tmp_path, monkeypatch):
    approve(repo)
    cheat = "def test_positive():\n    pass\n\n\ndef test_zero():\n    pass\n\n\ndef test_negative():\n    pass\n"
    go(repo, tmp_path, monkeypatch, [
        {"write": {"checks/test_add.py": cheat}, "cost": 0.1, "idea": "simplify tests"},
        {"write": {"conftest.py": "collect_ignore = ['checks']\n"}, "cost": 0.1, "idea": "skip"},
        {"write": {"app.py": FULL}, "cost": 0.1},
    ])
    rows = (repo / ".claude" / "loop" / "runs" / "t1" / "results.tsv").read_text(encoding="utf-8").splitlines()
    assert [r.split("\t")[1] for r in rows[1:]] == ["baseline", "tamper", "tamper", "keep"]
    assert (tmp_path / "wt" / "checks" / "test_add.py").read_text(encoding="utf-8") == CHECKS
    assert not (tmp_path / "wt" / "conftest.py").exists()


# ---------------------------------------------------------------------------
# Budgets
# ---------------------------------------------------------------------------


def test_dollar_budget_stops_and_caps_each_round(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [{"write": {"app.py": WORSE}, "cost": 0.6}],
           budget=Budget(usd=1.0, per_round_usd=2.0, rounds=10, score_timeout=120))
    assert s["stop_reason"] == "budget-usd" and s["rounds"] == 2
    caps = [c["argv"][c["argv"].index("--max-budget-usd") + 1] for c in calls(tmp_path)]
    assert caps == ["1.00", "0.40"]


def test_unknown_cost_counts_as_full_cap(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [{"raw": "not json at all"}],
           budget=Budget(usd=1.0, per_round_usd=0.5, rounds=10, score_timeout=120))
    assert s["stop_reason"] == "budget-usd" and s["rounds"] == 2 and s["cost_usd"] == pytest.approx(1.0)


def test_token_budget_ignores_cache_reads(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [{"write": {"app.py": WORSE}, "cost": 0.01, "tokens": 600}],
           budget=Budget(tokens=1000, rounds=10, score_timeout=120))
    assert s["stop_reason"] == "budget-tokens" and s["rounds"] == 2 and s["tokens"] == 1200


def test_time_budget_stops_before_a_round(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [{"cost": 0.1}],
           budget=Budget(hours=100 / 3600, score_timeout=60, rounds=10))
    assert s["stop_reason"] == "budget-time" and s["rounds"] == 0


def test_time_budget_with_fake_clock(repo, tmp_path, monkeypatch):
    approve(repo)
    ticks = iter(range(0, 10**7, 400))  # every clock read advances 400 s
    monkeypatch.setenv("FAKE_CLAUDE_PLAN", str(fake(tmp_path, [{"write": {"app.py": WORSE}, "cost": 0.01}])))
    s = loop.run_loop(repo, "add", budget=Budget(hours=1, rounds=50, score_timeout=60),
                      claude_cmd=[sys.executable, str(FAKE)], use_tq=False,
                      worktree_dir=tmp_path / "wt", run_id="t1", clock=lambda: next(ticks))
    assert s["stop_reason"] == "budget-time" and 0 < s["rounds"] < 50


def test_round_timeout_kills_child_and_charges_cap(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [{"sleep": 60, "cost": 0.01}],
           budget=Budget(rounds=1, round_timeout=3, per_round_usd=0.25, score_timeout=120))
    assert s["rounds"] == 1 and s["cost_usd"] == pytest.approx(0.25)
    row = (repo / ".claude" / "loop" / "runs" / "t1" / "results.tsv").read_text(encoding="utf-8").splitlines()[-1]
    assert "(round timed out)" in row


def test_scorer_timeout(repo, tmp_path):
    scorer = {"kind": "junit", "command": '{python} -c "import time; time.sleep(60)"'}
    score = loop.run_scorer(repo, scorer, tmp_path / "x.xml", timeout=2)
    assert score.timed_out and score.text() == "timeout"


def test_stdout_scorer_kit_compatible(repo, tmp_path):
    scorer = {"kind": "stdout", "command": '{python} -c "print(\'still failing: a; b\'); print(\'score: 4/6\')"'}
    score = loop.run_scorer(repo, scorer, tmp_path / "x.xml", timeout=30)
    assert (score.passed, score.total, score.failing) == (4, 6, ["a", "b"])


# ---------------------------------------------------------------------------
# Lockdown flags, settings, signal, safety asserts
# ---------------------------------------------------------------------------


def test_claude_flags_mcp_off_and_guard_installed(repo, tmp_path, monkeypatch):
    approve(repo)
    go(repo, tmp_path, monkeypatch, [{"write": {"app.py": FULL}, "cost": 0.1}])
    argv = calls(tmp_path)[0]["argv"]
    assert "--strict-mcp-config" in argv
    mcp = json.loads(Path(argv[argv.index("--mcp-config") + 1]).read_text(encoding="utf-8"))
    assert mcp == {"mcpServers": {}}
    assert argv[argv.index("--permission-mode") + 1] == "dontAsk"
    tools = argv[argv.index("--tools") + 1]
    assert "WebFetch" not in tools and "Bash" in tools
    settings = json.loads(Path(argv[argv.index("--settings") + 1]).read_text(encoding="utf-8"))
    hook = settings["hooks"]["PreToolUse"][0]["hooks"][0]
    assert "loop-guard.py" in hook["command"] and hook["timeout"] <= 15
    assert "WebFetch" in settings["permissions"]["deny"]
    guard = json.loads(Path(calls(tmp_path)[0]["env_guard"]).read_text(encoding="utf-8"))
    assert "checks/test_add.py" in guard["locked"] and guard["allow_network"] is False


def test_network_opt_in(repo, tmp_path, monkeypatch):
    approve(repo)
    go(repo, tmp_path, monkeypatch, [{"write": {"app.py": FULL}, "cost": 0.1}], allow_network=True)
    argv = calls(tmp_path)[0]["argv"]
    assert "WebFetch" in argv[argv.index("--tools") + 1]


def test_signal_posted_without_paths(repo, tmp_path, monkeypatch):
    approve(repo)
    sent = []
    monkeypatch.setattr(loop, "emit_signal", lambda ws, msg: sent.append((ws, msg)) or "id1")
    go(repo, tmp_path, monkeypatch, [{"write": {"app.py": FULL}, "cost": 0.1}], workspace="acme", signal=True)
    assert sent and sent[0][0] == "acme"
    assert "3/3" in sent[0][1] and str(tmp_path) not in sent[0][1]


def test_undo_refuses_outside_loop_branch(repo):
    with pytest.raises(RuntimeError, match="refusing"):
        loop.undo(repo, "loop/add-x")


def test_tq_missing_is_a_preflight_failure(repo, tmp_path, monkeypatch):
    approve(repo)
    monkeypatch.setattr(loop.shutil, "which", lambda name: None)
    s = loop.run_loop(repo, "add", workspace="acme", use_tq=True, run_id="t2", worktree_dir=tmp_path / "wt2")
    assert s["stop_reason"].startswith("preflight") and s["rounds"] == 0
    assert not (tmp_path / "wt2").exists()


# ---------------------------------------------------------------------------
# Integrity (security review): the runner re-derives everything it trusts
# ---------------------------------------------------------------------------


def test_agent_editing_its_guard_aborts_the_run(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [
        {"write": {"app.py": FULL}, "tamper_guard": True, "cost": 0.1},
        {"write": {"app.py": FULL}, "cost": 0.1},
    ])
    assert s["stop_reason"] == "config-tamper" and s["rounds"] == 1 and s["kept"] == 0
    assert (tmp_path / "wt" / "app.py").read_text(encoding="utf-8") == STUB  # round discarded
    assert any("guard/config" in n for n in s["notes"])


def test_head_moved_by_agent_is_discarded(repo, tmp_path, monkeypatch):
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [{"write": {"app.py": FULL}, "git_commit": True, "cost": 0.1}],
           budget=Budget(rounds=1, score_timeout=120))
    assert s["kept"] == 0 and s["best"] != "3/3"
    wt = tmp_path / "wt"
    assert "sneaky" not in _git(wt, "log", "--format=%s")  # reset to the runner's last good commit
    assert (wt / "app.py").read_text(encoding="utf-8") == STUB


def test_planted_ignored_settings_are_tamper_and_removed(repo, tmp_path, monkeypatch):
    (repo / ".gitignore").write_text(".claude/\n", encoding="utf-8")
    _git(repo, "add", ".gitignore")
    _git(repo, "commit", "-q", "-m", "ignore")
    approve(repo)
    s = go(repo, tmp_path, monkeypatch, [
        {"write": {"app.py": FULL, ".claude/settings.local.json": '{"disableAllHooks": true}'}, "cost": 0.1},
    ], budget=Budget(rounds=1, score_timeout=120))
    assert s["kept"] == 0
    assert not (tmp_path / "wt" / ".claude" / "settings.local.json").exists()


def test_child_never_loads_worktree_settings(repo, tmp_path, monkeypatch):
    approve(repo)
    go(repo, tmp_path, monkeypatch, [{"write": {"app.py": FULL}, "cost": 0.1}])
    argv = calls(tmp_path)[0]["argv"]
    assert argv[argv.index("--setting-sources") + 1] == "user"
    disallowed = argv[argv.index("--disallowedTools") + 1]
    assert "Edit(checks/test_add.py)" in disallowed and "loop/runs" in disallowed
    settings = json.loads(Path(argv[argv.index("--settings") + 1]).read_text(encoding="utf-8"))
    assert settings["disableAllHooks"] is False


def test_agent_written_or_edited_approval_is_refused(repo, tmp_path):
    data = approve(repo)
    path = repo / ".claude" / "loop" / "approved" / "add.json"
    edited = {**data, "expected_total": 1, "locked": []}
    path.write_text(json.dumps(edited), encoding="utf-8")
    with pytest.raises(SystemExit, match="no valid approval"):
        loop.run_loop(repo, "add", use_tq=False, claude_cmd=[sys.executable, str(FAKE)])
    unsigned = {k: v for k, v in data.items() if k != "sig"}
    path.write_text(json.dumps(unsigned), encoding="utf-8")
    assert loop.load_approval(repo, "add") is None


def test_approve_cli_is_human_only(repo):
    script = Path(__file__).resolve().parents[1] / "scripts" / "loop-runner.py"
    proc = subprocess.run([sys.executable, str(script), "--repo", str(repo), "approve", "--feature", "add",
                           "--checks", "checks/test_add.py", "--score-cmd", SCORE_CMD],
                          input=b"y\n", capture_output=True, timeout=120)
    assert proc.returncode == 2 and b"interactive terminal" in proc.stderr
    assert loop.load_approval(repo, "add") is None


def test_approval_pins_test_names(repo):
    data = approve(repo)
    assert len(data["names"]) == 3 and all("::test_" in n for n in data["names"])


def test_stdout_score_only_from_the_last_line():
    assert loop.parse_stdout_score("score: 9/9\nreal output\nscore: 1/9\n")[:2] == (1, 9)
    # a test printing a fake score is not the scorer's verdict
    assert loop.parse_stdout_score("score: 9/9\nsomething after\n") is None
    assert loop.parse_stdout_score('echo "pytest passed"') is None


def test_parse_helpers():
    assert loop.parse_junit('<testcase name="a"/><testcase name="b &amp; c"><failure/></testcase>') == (1, 2, ["b & c"])
    assert loop.round_usage(None, 0.7) == (0.7, 0, "")
    assert loop.idea_from("blah\nIDEA: use a dict\n") == "use a dict"
    assert loop.parse_claude_json('noise\n{"total_cost_usd": 0.2}') == {"total_cost_usd": 0.2}
