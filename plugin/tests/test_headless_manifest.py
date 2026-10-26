"""Headless runs never edit .tentaqles.yaml: skill text and the PreToolUse guard."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKILLS = ROOT / "skills"
GUARD = ROOT / "scripts" / "headless-manifest-guard.py"
PROPOSAL = "docs/privacy-manifest-proposal.yaml"


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_classify_data_headless_proposes_instead_of_editing() -> None:
    body = _body("classify-data")
    assert "Headless never edits `.tentaqles.yaml`" in body
    assert PROPOSAL in body
    section = body.split("## 3. Manifest", 1)[1].split("## 4.", 1)[0]
    headless, interactive = section.split("Interactive: edit", 1)
    assert PROPOSAL in headless and "do not edit `.tentaqles.yaml`" in headless
    assert "build-gate ask <slug> --kind question" in headless
    assert "NEEDS-DECISION" in headless
    assert "tq allow" in headless and "apply it" in headless
    assert "Edit tool" in interactive  # interactive behaviour unchanged


def test_kickoff_headless_names_proposal_and_never_edits_manifest() -> None:
    body = _body("kickoff")
    assert "Headless never edits `.tentaqles.yaml`" in body
    assert PROPOSAL in body
    assert "NEEDS-DECISION" in body and "tq allow <workspace>" in body


def test_other_manifest_writers_say_headless_does_not_edit() -> None:
    for name in ("client-settings", "rollback"):
        body = _body(name)
        assert "Headless (`TENTAQLES_HEADLESS=1`)" in body, name
        assert "never edit the manifest" in body or "do not restore" in body, name


def _run(payload: dict | str, headless: bool) -> subprocess.CompletedProcess:
    env = {k: v for k, v in os.environ.items() if k != "TENTAQLES_HEADLESS"}
    if headless:
        env["TENTAQLES_HEADLESS"] = "1"
    text = payload if isinstance(payload, str) else json.dumps(payload)
    return subprocess.run([sys.executable, str(GUARD)], input=text, capture_output=True, text=True,
                          env=env, timeout=20)


def test_guard_blocks_manifest_edit_when_headless() -> None:
    for tool in ("Edit", "Write", "MultiEdit"):
        r = _run({"tool_name": tool, "tool_input": {"file_path": "C:\\ws\\.tentaqles.yaml"}}, True)
        assert r.returncode == 2, tool
        assert r.stderr.startswith("BLOCKED: ") and PROPOSAL in r.stderr


def test_guard_allows_other_cases() -> None:
    manifest = {"tool_name": "Edit", "tool_input": {"file_path": "/ws/.tentaqles.yaml"}}
    assert _run(manifest, False).returncode == 0  # interactive
    assert _run({"tool_name": "Write", "tool_input": {"file_path": "/ws/" + PROPOSAL}}, True).returncode == 0
    assert _run({"tool_name": "Edit", "tool_input": {"file_path": "/ws/old.tentaqles.yaml.md"}}, True).returncode == 0
    assert _run({"tool_name": "Read", "tool_input": {"file_path": "/ws/.tentaqles.yaml"}}, True).returncode == 0
    assert _run("", True).returncode == 0
    assert _run("not json", True).returncode == 0


def test_guard_registered_with_short_timeout() -> None:
    hooks = json.loads((ROOT / "hooks" / "hooks.json").read_text(encoding="utf-8"))
    found = [h for g in hooks["hooks"]["PreToolUse"] for h in g["hooks"]
             if "headless-manifest-guard.py" in h["command"]]
    assert len(found) == 1 and found[0]["timeout"] <= 10


def test_guard_blocks_windows_spellings_of_the_manifest() -> None:
    for path in (r"C:\ws\.Tentaqles.YAML", r"C:\ws\.tentaqles.yaml.", r"C:\ws\.tentaqles.yaml ",
                 r"C:\ws\.tentaqles.yaml::$DATA", "C:/ws/.TENTAQLES.yaml:stream"):
        r = _run({"tool_name": "Write", "tool_input": {"file_path": path}}, True)
        assert r.returncode == 2, path
    r = _run({"tool_name": "NotebookEdit", "tool_input": {"notebook_path": "/ws/.tentaqles.yaml"}}, True)
    assert r.returncode == 2


def test_guard_resolves_real_path(tmp_path) -> None:
    real = tmp_path / ".tentaqles.yaml"
    real.write_text("client: x\n", encoding="utf-8")
    link = tmp_path / "alias.yaml"
    try:
        link.symlink_to(real)
    except OSError:
        return  # no symlink privilege on this box; the name checks still cover it
    r = _run({"tool_name": "Edit", "tool_input": {"file_path": str(link)}}, True)
    assert r.returncode == 2


def test_guard_blocks_bash_writes_but_allows_reads() -> None:
    for cmd in ("echo 'data: {}' >> .tentaqles.yaml", "sed -i 's/a/b/' ../.tentaqles.yaml",
                "python -c \"open('.tentaqles.yaml','a')\"", "cp /tmp/x C:/ws/.Tentaqles.yaml",
                "Set-Content -Path .tentaqles.yaml -Value x", "cat x | tee .tentaqles.yaml"):
        assert _run({"tool_name": "Bash", "tool_input": {"command": cmd}}, True).returncode == 2, cmd
    for cmd in ("cat .tentaqles.yaml", "grep client .tentaqles.yaml", "tq doctor 2>&1",
                "echo hi > notes.txt"):
        assert _run({"tool_name": "Bash", "tool_input": {"command": cmd}}, True).returncode == 0, cmd
    write = {"tool_name": "Bash", "tool_input": {"command": "echo x > .tentaqles.yaml"}}
    assert _run(write, False).returncode == 0  # interactive
