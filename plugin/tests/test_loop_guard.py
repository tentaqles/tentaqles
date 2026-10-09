"""PreToolUse guard of the overnight loop's headless child."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from tentaqles.workflow.loopguard import decide

SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "loop-guard.py"


@pytest.fixture
def cfg(tmp_path):
    root = tmp_path / "wt"
    root.mkdir()
    return {"root": str(root), "locked": ["checks/**", "pytest.ini", "**/conftest.py"], "allow_network": False}


def sh(cmd, tool="Bash"):
    return {"tool_name": tool, "tool_input": {"command": cmd}}


def edit(path, tool="Edit"):
    return {"tool_name": tool, "tool_input": {"file_path": path, "old_string": "a", "new_string": "b"}}


@pytest.mark.parametrize("cmd,want", [
    ("git status", 0),
    ("git diff HEAD", 0),
    ("git -C . log --oneline", 0),
    ("git commit -m x", 2),
    ("git add -A", 2),
    ("git push origin main", 2),
    ("git reset --hard HEAD~1", 2),
    ("git checkout -- checks", 2),
    ("curl https://example.com", 2),
    ("npm install left-pad", 2),
    ("pip install requests", 2),
    ("npx vitest run", 2),
    ("cat .env", 2),
    ("cat .env.example", 0),
    ("printenv", 2),
    ("rm -rf checks", 2),
    ("sed -i s/a/b/ checks/test_add.py", 2),
    ("echo x > pytest.ini", 2),
    ("python -m pytest checks/test_add.py -q", 0),
    ("python -c 'print(process.env)'", 0),
    ("ls src && python -m pytest", 0),
])
def test_shell(cfg, cmd, want):
    assert decide(sh(cmd), cfg)[0] == want


def test_powershell_writes_to_locked_paths(cfg):
    assert decide(sh("Set-Content checks\\test_add.py 'x'", "PowerShell"), cfg)[0] == 2
    assert decide(sh("Invoke-WebRequest https://example.com", "PowerShell"), cfg)[0] == 2
    assert decide(sh("Get-ChildItem Env:", "PowerShell"), cfg)[0] == 2
    assert decide(sh("Get-Content src\\app.py", "PowerShell"), cfg)[0] == 0


def test_network_opt_in(cfg):
    cfg["allow_network"] = True
    assert decide(sh("curl https://example.com"), cfg)[0] == 0
    assert decide({"tool_name": "WebFetch", "tool_input": {"url": "https://example.com"}}, cfg)[0] == 0
    assert decide({"tool_name": "mcp__db__query", "tool_input": {}}, cfg)[0] == 2  # MCP stays off


def test_file_tools(cfg):
    root = cfg["root"]
    win = root.replace("/", "\\")
    assert decide(edit(os.path.join(root, "app.py")), cfg)[0] == 0
    assert decide(edit(win + "\\checks\\test_add.py"), cfg)[0] == 2           # backslashes normalized
    assert decide(edit(os.path.join(root, "sub", "conftest.py"), "Write"), cfg)[0] == 2
    assert decide(edit(os.path.join(root, ".claude", "settings.json"), "Write"), cfg)[0] == 2
    assert decide(edit(os.path.join(root, ".git", "config")), cfg)[0] == 2
    assert decide(edit(os.path.join(os.path.dirname(root), "elsewhere.py"), "Write"), cfg)[0] == 2
    assert decide(edit(os.path.join(root, ".env"), "Write"), cfg)[0] == 2
    read = lambda p: {"tool_name": "Read", "tool_input": {"file_path": p}}  # noqa: E731
    assert decide(read(os.path.join(root, ".env")), cfg)[0] == 2
    assert decide(read(os.path.join(root, ".env.example")), cfg)[0] == 0
    assert decide(read(os.path.join(root, "checks", "test_add.py")), cfg)[0] == 0


def test_other_tools(cfg):
    assert decide({"tool_name": "mcp__postgres__query", "tool_input": {}}, cfg)[0] == 2
    assert decide({"tool_name": "WebSearch", "tool_input": {"query": "x"}}, cfg)[0] == 2


def test_missing_config_blocks_all_but_reads():
    assert decide(sh("ls"), None)[0] == 2
    assert decide({"tool_name": "Read", "tool_input": {"file_path": "x"}}, None)[0] == 0


def test_script_entry(cfg, tmp_path):
    path = tmp_path / "guard.json"
    path.write_text(json.dumps(cfg), encoding="utf-8")
    env = {**os.environ, "TQ_LOOP_GUARD": str(path)}
    proc = subprocess.run([sys.executable, str(SCRIPT)], input=json.dumps(sh("git push")).encode("utf-8"),
                          capture_output=True, env=env, timeout=60)
    assert proc.returncode == 2 and b"loop-guard" in proc.stderr
    proc = subprocess.run([sys.executable, str(SCRIPT)], input=json.dumps(sh("git status")).encode("utf-8"),
                          capture_output=True, env=env, timeout=60)
    assert proc.returncode == 0
