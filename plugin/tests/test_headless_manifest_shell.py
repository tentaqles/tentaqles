"""The headless manifest guard blocks shell writes of .tentaqles.yaml, not the skills' own commands."""

import json
import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GUARD = ROOT / "scripts" / "headless-manifest-guard.py"


def _run(tool: str, command: str, headless: bool = True) -> int:
    env = {k: v for k, v in os.environ.items() if k != "TENTAQLES_HEADLESS"}
    if headless:
        env["TENTAQLES_HEADLESS"] = "1"
    payload = json.dumps({"tool_name": tool, "tool_input": {"command": command}})
    return subprocess.run([sys.executable, str(GUARD)], input=payload, capture_output=True, text=True,
                          env=env, timeout=20).returncode


def _fenced_bash(skill: str, heading: str) -> str:
    text = (ROOT / "skills" / skill / "SKILL.md").read_text(encoding="utf-8")
    section = text[text.index(heading):]
    m = re.search(r"```bash\n(.*?)```", section, re.S)
    assert m, heading
    return m.group(1)


def test_classify_data_inputs_block_is_allowed() -> None:
    block = _fenced_bash("classify-data", "## 1. Inputs")
    assert ".tentaqles.yaml" in block and ">/dev/null" in block
    assert _run("Bash", block) == 0


def test_skill_decision_commands_that_name_the_manifest_are_allowed() -> None:
    text = (ROOT / "skills" / "classify-data" / "SKILL.md").read_text(encoding="utf-8")
    ask = re.search(r"`(build-gate ask <slug> --kind question --text \"Review docs/privacy-manifest-proposal"
                    r"[^`]*)`", text)
    assert ask, "classify-data names its headless decision"
    line = ask.group(1).replace("<slug>", "demo").replace("<workspace>", "acme")
    full = '"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" ' + line.split(" ", 1)[1]
    assert _run("Bash", full) == 0
    assert _run("Bash", line) == 0


def test_reads_and_unrelated_redirects_are_allowed() -> None:
    for cmd in ("cat .tentaqles.yaml", "grep client .tentaqles.yaml >/dev/null",
                "grep -c data ../.tentaqles.yaml 2>&1", "yq '.data' .tentaqles.yaml",
                "cp .tentaqles.yaml /tmp/backup.yaml", "cat .tentaqles.yaml && pytest tests/x.py",
                "python -c \"import yaml; print(yaml.safe_load(open('.tentaqles.yaml')))\"",
                "diff .tentaqles.yaml docs/privacy-manifest-proposal.yaml > /tmp/d.txt",
                "echo 'apply it to .tentaqles.yaml' > docs/notes.md", "unbalanced 'quote .tentaqles.yaml"):
        assert _run("Bash", cmd) == 0, cmd


def test_shell_writes_are_blocked() -> None:
    for cmd in ("echo 'data: {}' >> .tentaqles.yaml", "echo x >.tentaqles.yaml", "echo x 1> ../.Tentaqles.yaml",
                "sed -i 's/a/b/' ../.tentaqles.yaml", "sed -i.bak 's/a/b/' .tentaqles.yaml",
                "yq -i '.data = {}' .tentaqles.yaml", "cat x | tee -a .tentaqles.yaml",
                "cp /tmp/x C:/ws/.tentaqles.yaml", "mv .tentaqles.yaml old.yaml", "rm -f .tentaqles.yaml",
                "dd if=/tmp/x of=.tentaqles.yaml", "git checkout -- .tentaqles.yaml",
                "python -c \"open('.tentaqles.yaml','a').write('x')\"",
                "\"$TENTAQLES_PY\" -c \"open('.tentaqles.yaml','w').write('x')\"",
                "node -e \"require('fs').appendFileSync('.tentaqles.yaml','x')\"",
                "ls && FOO=1 sed -i s/a/b/ .tentaqles.yaml"):
        assert _run("Bash", cmd) == 2, cmd


def test_powershell_writes_are_blocked_and_reads_allowed() -> None:
    for cmd in ("Set-Content -Path .tentaqles.yaml -Value x", "Add-Content .tentaqles.yaml 'data: {}'",
                "'x' | Out-File -FilePath C:\\ws\\.tentaqles.yaml", "Copy-Item C:\\tmp\\x -Destination .tentaqles.yaml",
                "Remove-Item .tentaqles.yaml", "'x' > .tentaqles.yaml"):
        assert _run("PowerShell", cmd) == 2, cmd
    for cmd in ("Get-Content .tentaqles.yaml", "Select-String client .tentaqles.yaml | Out-Null",
                "Copy-Item .tentaqles.yaml C:\\tmp\\backup.yaml"):
        assert _run("PowerShell", cmd) == 0, cmd


def test_interactive_shell_is_never_blocked() -> None:
    assert _run("Bash", "echo x > .tentaqles.yaml", headless=False) == 0
    assert _run("PowerShell", "Set-Content .tentaqles.yaml x", headless=False) == 0


def test_matcher_covers_shell_tools() -> None:
    hooks = json.loads((ROOT / "hooks" / "hooks.json").read_text(encoding="utf-8"))
    for group in hooks["hooks"]["PreToolUse"]:
        if any("headless-manifest-guard.py" in h["command"] for h in group["hooks"]):
            assert re.fullmatch(group["matcher"], "Bash") and re.fullmatch(group["matcher"], "PowerShell")
            assert re.fullmatch(group["matcher"], "NotebookEdit")
            return
    raise AssertionError("guard not registered")


def test_multiline_scripts_and_heredocs_are_judged_line_by_line() -> None:
    allowed = (
        'cp "$_tqe/templates/process/privacy.md" docs/privacy.md\ngrep -n \'^data\' .tentaqles.yaml',
        "cp a.txt b.txt\ncat .tentaqles.yaml",
        "rm -f /tmp/x\ngrep -q data .tentaqles.yaml && echo yes",
        "tee docs/notes.md <<EOF\nApply the block to .tentaqles.yaml, then run tq allow.\nEOF",
        "cat <<EOF > docs/x.md\nSteps; rm the old key from .tentaqles.yaml\nEOF",
        "cat > docs/x.md <<'EOF'\nname: it's in .tentaqles.yaml\nEOF\ngit add docs/x.md",
        "git commit -m \"docs: propose the data block\n\nApply it to .tentaqles.yaml by hand.\"",
        "python -c \"import yaml; print(yaml.safe_dump(yaml.safe_load(open('.tentaqles.yaml'))))\"",
        "git diff -- .tentaqles.yaml",
        "grep -r tentaqles.yaml .",
    )
    for cmd in allowed:
        assert _run("Bash", cmd) == 0, cmd
    blocked = (
        "echo ok\nrm .tentaqles.yaml",
        "echo ok\nsed -i s/a/b/ .tentaqles.yaml",
        "cat <<EOF > .tentaqles.yaml\nclient: x\nEOF",
        "cat > .tentaqles.yaml <<EOF\nname: it's here\nEOF",
        "echo x &> .tentaqles.yaml",
        "(echo x > .tentaqles.yaml)",
        "if true; then rm .tentaqles.yaml; fi",
        "perl -pi -e s/a/b/ .tentaqles.yaml",
        "bash -c 'echo x >> .tentaqles.yaml'",
        "find . -name .tentaqles.yaml -delete",
        "echo a \\\n  > .tentaqles.yaml",
    )
    for cmd in blocked:
        assert _run("Bash", cmd) == 2, cmd
    assert _run("PowerShell", "Get-Date\nRemove-Item .tentaqles.yaml") == 2
    assert _run("PowerShell", "Get-Date\nGet-Content .tentaqles.yaml") == 0


def test_code_fed_through_a_heredoc_and_dotnet_writes() -> None:
    assert _run("Bash", "python - <<EOF\nopen('.tentaqles.yaml','w').write('x')\nEOF") == 2
    assert _run("Bash", "bash <<'EOF'\necho x >> .tentaqles.yaml\nEOF") == 2
    assert _run("Bash", "python - <<EOF\nprint(open('.tentaqles.yaml').read())\nEOF") == 0
    assert _run("PowerShell", "[IO.File]::WriteAllText('.tentaqles.yaml','x')") == 2
    assert _run("PowerShell", "[IO.File]::ReadAllText('.tentaqles.yaml')") == 0
