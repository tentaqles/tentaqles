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


def _guard(path, env, cmd):
    return subprocess.run([sys.executable, str(SCRIPT)], input=json.dumps(sh(cmd)).encode("utf-8"),
                          capture_output=True, env=env, timeout=60)


def test_script_entry(cfg, tmp_path):
    from tentaqles.workflow._common import file_sha256

    path = tmp_path / "guard.json"
    path.write_text(json.dumps(cfg), encoding="utf-8")
    env = {**os.environ, "TQ_LOOP_GUARD": str(path), "TQ_LOOP_GUARD_SHA": file_sha256(path)}
    proc = _guard(path, env, "git push")
    assert proc.returncode == 2 and b"loop-guard" in proc.stderr
    assert _guard(path, env, "git status").returncode == 0


def test_edited_guard_config_blocks_everything(cfg, tmp_path):
    """The agent rewriting its own guard config (e.g. emptying `locked`) disables nothing:
    the hash pinned in the child's environment no longer matches."""
    from tentaqles.workflow._common import file_sha256

    path = tmp_path / "guard.json"
    path.write_text(json.dumps(cfg), encoding="utf-8")
    env = {**os.environ, "TQ_LOOP_GUARD": str(path), "TQ_LOOP_GUARD_SHA": file_sha256(path)}
    path.write_text(json.dumps({**cfg, "locked": [], "allow_network": True}), encoding="utf-8")
    proc = _guard(path, env, "git status")
    assert proc.returncode == 2 and b"changed" in proc.stderr
    unpinned = {k: v for k, v in env.items() if k != "TQ_LOOP_GUARD_SHA"}
    assert _guard(path, unpinned, "git status").returncode == 2


@pytest.fixture
def pcfg(cfg, tmp_path):
    run_dir = tmp_path / "repo" / ".claude" / "loop" / "runs" / "r1"
    run_dir.mkdir(parents=True)
    plugin = tmp_path / "plugin"
    plugin.mkdir()
    cfg["protect"] = [str(run_dir).replace("\\", "/"), str(plugin).replace("\\", "/")]
    return cfg


def test_guard_files_and_runner_out_of_reach(pcfg):
    run_dir, plugin = pcfg["protect"]
    cmds = [
        f'rm "{run_dir}/guard.json"',
        f"echo '{{}}' > {run_dir}/guard.json",
        f'python -c "open(\'{run_dir}/settings.json\',\'w\')"',
        f"Set-Content {run_dir.replace('/', chr(92))}\\guard.json x",
        f"cp evil.py {plugin}/tentaqles/workflow/loopguard.py",
        f"sed -i s/2/0/ {plugin}/tentaqles/workflow/loopguard.py",
        "echo x > C:/Users/Public/outside.txt" if os.name == "nt" else "echo x > /var/outside.txt",
        "cp app.py /c/Windows/x.py" if os.name == "nt" else "cp app.py /etc/x.py",
        'echo x > "$HOME/.bashrc"',
        'echo "unclosed',
        "true; (rm -rf checks)",
        "echo $(rm checks/test_add.py)",
    ]
    for c in cmds:
        assert decide(sh(c), pcfg)[0] == 2, c
    for path in (f"{run_dir}/guard.json", f"{plugin}/scripts/loop-guard.py"):
        assert decide(edit(path, "Write"), pcfg)[0] == 2, path
    # reading them is harmless, and writing temp files is fine
    assert decide(sh(f"cat {run_dir}/summary.md"), pcfg)[0] == 0
    import tempfile
    assert decide(sh(f"echo x > {tempfile.gettempdir().replace(chr(92), '/')}/scratch.txt"), pcfg)[0] == 0


def test_shadowing_the_test_runner_is_locked(cfg):
    root = cfg["root"]
    for rel in ("pytest.py", "sitecustomize.py", "lib/x.pth", ".venv/lib/site.py", "node_modules/vitest/index.js"):
        assert decide(edit(os.path.join(root, rel), "Write"), cfg)[0] == 2, rel
