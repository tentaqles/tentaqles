"""Overnight improvement loop (/tentaqles:loop), a hardened Karpathy-style loop.

Day (human present):  `loop-runner approve` freezes the feature's checks, the scorer
                      command and the expected test count.
Night (unattended):   `loop-runner run` creates a dedicated git worktree at the
                      approved commit and runs rounds. Each round is one fresh
                      `claude -p` call that makes one change; the runner (never the
                      model) scores it, keeps it with an explicit-path commit when the
                      score improves, or undoes it.
Morning:              summary.md / summary.json under .claude/loop/runs/<id>/, a
                      LATEST-<feature>.md copy, and optionally a tentaqles signal.

Safety rails, all enforced by this process, not by the prompt:
- identity: `tq run <workspace> -- <full path to claude>` sets CLAUDE_CONFIG_DIR and
  the workspace env (the full path stops tq adding the workspace permission mode;
  the loop sets its own: dontAsk + an allowlist);
- MCP off (`--strict-mcp-config` with an empty config), network tools off unless
  allowed, a PreToolUse guard (loopguard.py) on every tool call;
- tamper check after every round: any change to a locked path (checks, scorer,
  runner config, .claude/) undoes the round; a shrinking test count does too;
- scorer and round timeouts (process trees are killed), and budgets for dollars,
  tokens, wall-clock time and rounds; an unknown round cost counts as the full
  per-round cap;
- never `git add -A`: kept rounds stage the changed paths by name, skipping
  secret-shaped files, and run tq's commit secret scan first when tq is present;
- undo is `git reset --hard` + `git clean -fd`, only after asserting the worktree
  is on the loop branch.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import sys
import time
import uuid
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path

from tentaqles.workflow._common import (
    file_sha256,
    find_bash,
    find_git_root,
    git,
    glob_match,
    norm,
    read_json,
    read_text,
    redact,
    run,
    tail,
    write_json,
)

STATE = ".claude/loop"
DEFAULT_LOCKED = (
    "**/conftest.py", "pytest.ini", "pyproject.toml", "setup.cfg", "tox.ini", "noxfile.py",
    "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "uv.lock",
    "poetry.lock", "requirements*.txt", "vitest.config.*", "vitest.workspace.*", "vite.config.*",
    "jest.config.*", "playwright.config.*", "tsconfig*.json", ".github/**", ".claude/**",
    "score.mjs", "approve-checks.mjs", ".scores/**",
)
SKIP_COMMIT = (
    ".env", ".env.*", "**/.env", "**/.env.*", "*.pem", "**/*.pem", "*.key", "**/*.key",
    "*.p12", "**/*.p12", "*.pfx", "**/*.pfx", "**/id_rsa*", "id_rsa*", "**/id_ed25519*", "id_ed25519*",
    "**/credentials*", "credentials*", ".npmrc", "**/.npmrc", ".pypirc", "**/.pypirc",
)
# Build/test by-products: never committed, never "tampering".
JUNK = ("**/__pycache__/**", "**/*.pyc", "**/.pytest_cache/**", "**/node_modules/**",
        "**/.mypy_cache/**", "**/.ruff_cache/**", "**/coverage/**", "**/.coverage")
ENV_TEMPLATES = re.compile(r"\.env\.(?:example|sample|template|dist)$", re.IGNORECASE)
MAX_COMMIT_FILE = 1_000_000
TOOLS = ("Read", "Edit", "Write", "Glob", "Grep", "Bash")
NET_TOOLS = ("WebFetch", "WebSearch")


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


@dataclass
class Budget:
    usd: float = 10.0            # whole run
    per_round_usd: float = 2.0   # passed to claude as --max-budget-usd
    tokens: int = 5_000_000      # input + output + cache writes (cache reads excluded)
    hours: float = 6.0           # wall clock for the whole run
    rounds: int = 10
    round_timeout: float = 1800  # seconds per claude round
    score_timeout: float = 600   # seconds per scorer run
    max_turns: int | None = None


@dataclass
class Score:
    passed: int = 0
    total: int = 0
    crashed: bool = False
    timed_out: bool = False
    failing: list = field(default_factory=list)
    names: list = field(default_factory=list)

    def text(self) -> str:
        if self.timed_out:
            return "timeout"
        if self.crashed:
            return "0/?"
        return f"{self.passed}/{self.total}"


# ---------------------------------------------------------------------------
# Scoring
# ---------------------------------------------------------------------------

_DECODE = {"quot": '"', "amp": "&", "lt": "<", "gt": ">", "apos": "'", "#39": "'"}


def parse_junit(xml: str) -> tuple[int, int, list[str]]:
    passed, total, failing, _ = parse_junit_names(xml)
    return passed, total, failing


def parse_junit_names(xml: str) -> tuple[int, int, list[str], list[str]]:
    cases = re.findall(r"<testcase\b([^>]*?)(?:/>|>([\s\S]*?)</testcase>)", xml or "")
    passed, failing, names = 0, [], []
    for attrs, body in cases:
        m = re.search(r'\bname="([^"]*)"', attrs)
        name = re.sub(r"&(quot|amp|lt|gt|apos|#39);", lambda x: _DECODE[x.group(1)], m.group(1)) if m else "?"
        c = re.search(r'\bclassname="([^"]*)"', attrs)
        names.append(f"{c.group(1) if c else ''}::{name}")
        if re.search(r"<(failure|error|skipped)\b", body or ""):
            failing.append(name)
        else:
            passed += 1
    return passed, len(cases), failing, names


def parse_stdout_score(text: str) -> tuple[int, int, list[str]] | None:
    """Kit-compatible `score: X/Y`. Only the scorer's own LAST line counts, so a
    test that prints a fake score line mid-run can't set the result."""
    lines = [ln for ln in (text or "").splitlines() if ln.strip()]
    m_last = re.fullmatch(r"\s*score:\s*(\d+)\s*/\s*(\d+)\s*", lines[-1]) if lines else None
    if not m_last:
        return None
    x, y = m_last.group(1), m_last.group(2)
    failing = []
    m = re.findall(r"still failing:\s*(.+)", text or "")
    if m:
        failing = [s.strip() for s in m[-1].split(";") if s.strip()]
    return int(x), int(y), failing


def resolve_python(root: Path) -> str:
    for cand in (root / ".venv" / "Scripts" / "python.exe", root / ".venv" / "bin" / "python"):
        if cand.exists():
            return norm(cand)
    if (root / "uv.lock").exists() and shutil.which("uv"):
        return "uv run python"
    return norm(sys.executable)


def shell_argv(command: str) -> list[str]:
    bash = find_bash()
    if bash:
        return [bash, "-c", command]
    if os.name == "nt":
        return ["cmd", "/c", command]
    return ["/bin/sh", "-c", command]


def run_scorer(root: Path, scorer: dict, xml_path: Path, timeout: float) -> Score:
    cmd = scorer["command"].replace("{xml}", norm(xml_path)).replace("{python}", resolve_python(root))
    try:
        xml_path.unlink()
    except OSError:
        pass
    env = {**os.environ, "NODE_ENV": "test", "TQ_LOOP": "1", "PYTHONDONTWRITEBYTECODE": "1"}
    code, out, err, timed_out = run(shell_argv(cmd), cwd=root, env=env, timeout=timeout)
    if timed_out:
        return Score(timed_out=True, crashed=True)
    names: list = []
    if scorer.get("kind") == "stdout":
        parsed = parse_stdout_score(out)
    else:
        parsed = None
        if xml_path.exists():
            p_, t_, f_, names = parse_junit_names(read_text(xml_path))
            parsed = (p_, t_, f_)
    if not parsed or parsed[1] == 0:
        return Score(crashed=True)
    passed, total, failing = parsed
    return Score(passed=passed, total=total, failing=failing, names=names)


# ---------------------------------------------------------------------------
# git helpers
# ---------------------------------------------------------------------------


def changed_paths(wt: Path) -> list[str]:
    code, out = git(wt, "status", "--porcelain=v1", "-z", "--untracked-files=all")
    if code != 0:
        return []
    items, paths = out.split("\0"), []
    i = 0
    while i < len(items):
        entry = items[i]
        i += 1
        if len(entry) < 4:
            continue
        status, path = entry[:2], entry[3:]
        paths.append(path)
        if "R" in status or "C" in status:
            if i < len(items) and items[i]:
                paths.append(items[i])  # the rename source: stage its removal too
            i += 1
    return paths


def ignored_entries(wt: Path) -> set[str]:
    """Ignored, untracked paths (directories collapsed): files `git clean -fd` leaves."""
    code, out = git(wt, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory")
    return {ln.strip() for ln in out.splitlines() if ln.strip()} if code == 0 else set()


ALWAYS_TAMPER = (".claude/**", ".claude", "**/sitecustomize.py", "**/usercustomize.py", "**/*.pth",
                 "**/pytest.py", "**/_pytest/**", "**/conftest.py")


def tampered(wt: Path, commit: str, locked, ignored_baseline: set | None = None) -> list[str]:
    """Locked paths changed since `commit`, judged by the runner's own git calls:
    tracked diffs, untracked files, and new ignored files (which `git status`
    hides) such as a planted sitecustomize.py or .claude/settings.local.json."""
    code, out = git(wt, "diff", "--name-only", commit)
    names = set(out.splitlines()) if code == 0 else set()
    code, out = git(wt, "ls-files", "--others", "--exclude-standard")
    if code == 0:
        names |= set(out.splitlines())
    bad = {n for n in names if n and glob_match(n, locked) and not glob_match(n, JUNK)}
    if ignored_baseline is not None:
        for n in ignored_entries(wt) - ignored_baseline:
            stem = n.rstrip("/")
            if glob_match(stem, list(locked) + list(ALWAYS_TAMPER)) or glob_match(stem + "/x", list(locked) + list(ALWAYS_TAMPER)):
                bad.add(n)
    return sorted(bad)


def skip_for_commit(wt: Path, rel: str) -> str | None:
    base = rel.split("/")[-1]
    if glob_match(rel, JUNK):
        return "build by-product"
    if ENV_TEMPLATES.search(base):
        return None
    if glob_match(rel, SKIP_COMMIT):
        return "secret-shaped file name"
    try:
        if (wt / rel).is_file() and (wt / rel).stat().st_size > MAX_COMMIT_FILE:
            return "larger than 1 MB"
    except OSError:
        pass
    return None


def current_branch(wt: Path) -> str:
    code, out = git(wt, "rev-parse", "--abbrev-ref", "HEAD")
    return out.strip() if code == 0 else ""


def undo(wt: Path, branch: str, target: str = "HEAD", ignored_baseline: set | None = None) -> None:
    """Back to the runner's last kept commit (never to whatever HEAD the agent left)."""
    if branch in ("main", "master") or not branch.startswith("loop/"):
        raise RuntimeError(f"refusing to reset: '{branch}' is not a loop branch")
    cur = current_branch(wt)
    if cur != branch:
        if cur in ("main", "master") and target == "HEAD":
            raise RuntimeError(f"refusing to reset: worktree is on '{cur}', not the loop branch '{branch}'")
        code, out = git(wt, "checkout", "-q", "-f", branch, timeout=120)
        if code != 0 or current_branch(wt) != branch:
            raise RuntimeError(f"refusing to reset: worktree is on '{cur}', not the loop branch '{branch}'")
    git(wt, "reset", "-q", "--hard", target, timeout=120)
    git(wt, "clean", "-fdq", timeout=120)
    if ignored_baseline is not None:
        for n in ignored_entries(wt) - ignored_baseline:
            stem = n.rstrip("/")
            if glob_match(stem, ALWAYS_TAMPER) or glob_match(stem + "/x", ALWAYS_TAMPER):
                target_path = wt / stem
                if target_path.is_dir():
                    shutil.rmtree(target_path, ignore_errors=True)
                else:
                    try:
                        target_path.unlink()
                    except OSError:
                        pass


def tq_commit_scan(wt: Path, tq: str | None) -> str | None:
    """Reuse tq's commit secret scan; returns the block reason or None."""
    if not tq:
        return None
    payload = json.dumps({"tool_name": "Bash", "cwd": str(wt),
                          "tool_input": {"command": "git commit -m loop-round"}})
    code, out, err, _ = run([tq, "claude-hook", "pre-tool-use", "--json"], cwd=wt,
                            input_text=payload, timeout=30)
    if code == 2:
        try:
            return json.loads(out).get("rule") or "tq blocked the commit"
        except ValueError:
            return "tq blocked the commit"
    return None


def commit_round(wt: Path, branch: str, message: str, prefix: list[str], tq: str | None,
                 locked=(), base: str | None = None) -> tuple[str | None, list[str], str]:
    """Stage verified changed paths by name and commit. Returns (sha|None, skipped, error)."""
    if current_branch(wt) != branch:
        return None, [], "not on the loop branch"
    if base:
        code, head = git(wt, "rev-parse", "HEAD")
        if head.strip() != base:
            return None, [], "HEAD moved; refusing to commit"
    paths, skipped = [], []
    for rel in changed_paths(wt):
        if locked and glob_match(rel, list(locked)) and not glob_match(rel, JUNK):
            return None, skipped, f"refusing to commit a locked path: {rel}"
        why = skip_for_commit(wt, rel)
        if why == "build by-product":
            continue
        if why:
            skipped.append(f"{rel} ({why})")
        else:
            paths.append(rel)
    if not paths:
        return None, skipped, "nothing to commit"
    for i in range(0, len(paths), 100):
        code, out = git(wt, "add", "--", *paths[i:i + 100], timeout=120)
        if code != 0:
            git(wt, "reset", "-q")
            return None, skipped, f"git add failed: {tail(out, 3)}"
    reason = tq_commit_scan(wt, tq)
    if reason:
        git(wt, "reset", "-q")
        return None, skipped, f"commit secret scan: {reason}"
    code, out, err, _ = run([*prefix, "git", "commit", "-q", "-m", message], cwd=wt, timeout=120)
    if code != 0:
        git(wt, "reset", "-q")
        return None, skipped, f"git commit failed: {tail(out + err, 3)}"
    code, sha = git(wt, "rev-parse", "HEAD")
    return sha.strip(), skipped, ""


# ---------------------------------------------------------------------------
# Approval (daytime)
# ---------------------------------------------------------------------------


def state_root(repo: Path) -> Path:
    return repo / STATE


def approval_path(repo: Path, feature: str) -> Path:
    return state_root(repo) / "approved" / f"{feature}.json"


def git_exclude(repo: Path, line: str) -> None:
    code, p = git(repo, "rev-parse", "--git-path", "info/exclude")
    if code != 0:
        return
    path = Path(p.strip())
    if not path.is_absolute():
        path = repo / path
    text = read_text(path)
    if line not in text.splitlines():
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "a", encoding="utf-8", newline="\n") as f:
            if text and not text.endswith("\n"):
                f.write("\n")
            f.write(line + "\n")


def approve(repo: Path, feature: str, checks: list[str], scorer_cmd: str, kind: str = "junit",
            expect: int | None = None, extra_locked: list[str] | None = None,
            spec_file: str | None = None, notes: str = "", score_timeout: float = 600) -> dict:
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", feature):
        raise ValueError("feature names are kebab-case, e.g. guest-ordering")
    if not checks:
        raise ValueError("name the check files (they must already be committed)")
    rels = []
    for c in checks:
        rel = norm(os.path.relpath(Path(c).resolve(), repo.resolve())) if Path(c).is_absolute() else norm(c)
        code, _ = git(repo, "ls-files", "--error-unmatch", "--", rel)
        if code != 0:
            raise ValueError(f"{rel} is not committed; commit the approved checks first")
        rels.append(rel)
    code, out = git(repo, "status", "--porcelain", "--", *rels)
    if out.strip():
        raise ValueError("the check files have uncommitted changes; commit them first")
    code, head = git(repo, "rev-parse", "HEAD")
    if code != 0:
        raise ValueError("no commits yet")
    scorer = {"kind": kind, "command": scorer_cmd}
    xml = state_root(repo) / "approved" / f"{feature}.baseline.xml"
    xml.parent.mkdir(parents=True, exist_ok=True)
    baseline = run_scorer(repo, scorer, xml, score_timeout)
    total = expect or baseline.total
    if not total:
        raise ValueError("the baseline found no tests (a crash before the feature exists is normal): pass --expect N")
    locked = sorted(set(rels) | set(DEFAULT_LOCKED) | set(extra_locked or []))
    data = {
        "feature": feature,
        "approved_at": now_iso(),
        "commit": head.strip(),
        "checks": rels,
        "hashes": {r: file_sha256(repo / r) for r in rels},
        "locked": locked,
        "scorer": scorer,
        "expected_total": total,
        "baseline": asdict(baseline),
        # names pin WHICH tests count, not just how many (empty when the baseline crashed)
        "names": sorted(baseline.names) if not baseline.crashed and baseline.total == total else [],
        "spec_file": norm(spec_file) if spec_file else None,
        "notes": notes,
    }
    from tentaqles.workflow.gate import sign

    data = sign(data)  # HMAC with the per-user key outside the repo: an agent-written approval fails
    write_json(approval_path(repo, feature), data)
    git_exclude(repo, f"/{STATE}/")
    return data


def load_approval(repo: Path, feature: str) -> dict | None:
    from tentaqles.workflow.gate import verified

    data = read_json(approval_path(repo, feature))
    return data if verified(data) else None


# ---------------------------------------------------------------------------
# The run (night)
# ---------------------------------------------------------------------------


def build_prompt(feature: str, approved: dict, spec: str, best: Score, history: list[dict],
                 check_cmd: str) -> str:
    lines = [
        f'You are one round of an unattended improvement loop for the feature "{feature}".',
        "You are in a dedicated git worktree. Nobody will answer questions: decide and act.",
        "",
        "Fixed rules:",
        "- Make ONE focused change to the app that should raise the score. One idea per round.",
        f"- You may run the checks to see failures: {check_cmd}",
        "- Locked (never edit, move or delete): " + ", ".join(approved.get("locked", [])),
        "- Do not run git; the runner keeps or undoes your change after scoring it.",
        "- No network, no new dependencies, no .env files, no MCP tools.",
        "- When the change is made, stop. End your reply with exactly one line:",
        "  IDEA: <the approach you tried, in a few words>",
        "",
        f"Current best score: {best.text()} (expected total {approved.get('expected_total')}).",
    ]
    if best.failing:
        lines.append("Still failing: " + "; ".join(best.failing[:30]))
    if history:
        lines += ["", "Earlier rounds (don't repeat undone ideas):"]
        for h in history[-15:]:
            lines.append(f"- r{h['round']} {h['status']} {h['score']}: {h['idea']}")
    if approved.get("notes"):
        lines += ["", "How to work (from the human):", approved["notes"]]
    if spec:
        lines += ["", "Feature spec:", spec.strip()[:8000]]
    return "\n".join(lines)


def parse_claude_json(stdout: str) -> dict | None:
    text = (stdout or "").strip()
    for candidate in (text, *reversed([ln for ln in text.splitlines() if ln.strip().startswith("{")])):
        try:
            data = json.loads(candidate)
        except ValueError:
            continue
        if isinstance(data, dict):
            return data
    return None


def round_usage(data: dict | None, cap: float) -> tuple[float, int, str]:
    """(cost, tokens, result text). Unknown cost counts as the whole cap."""
    if not data:
        return cap, 0, ""
    cost = data.get("total_cost_usd", data.get("cost_usd"))
    try:
        cost = float(cost)
    except (TypeError, ValueError):
        cost = cap
    usage = data.get("usage") or {}
    tokens = 0
    for k in ("input_tokens", "output_tokens", "cache_creation_input_tokens"):
        try:
            tokens += int(usage.get(k) or 0)
        except (TypeError, ValueError):
            pass
    return cost, tokens, str(data.get("result") or "")


def idea_from(result: str) -> str:
    m = re.findall(r"^\s*IDEA:\s*(.+)$", result or "", re.MULTILINE)
    idea = (m[-1] if m else "(no IDEA line)").strip()
    idea = re.sub(r"\s+", " ", redact(idea))
    return idea[:100]


PINNED_PLUGIN_FILES = ("tentaqles/workflow/loopguard.py", "tentaqles/workflow/_common.py",
                       "tentaqles/workflow/__init__.py", "scripts/loop-guard.py", "scripts/tq_run.sh",
                       "scripts/tq_env.sh", "scripts/_path.py")


def plugin_root() -> Path:
    return Path(__file__).resolve().parents[2]


def protected_paths(run_dir: Path, repo: Path) -> list[str]:
    """Absolute paths the child must never write: its own guard/settings/budget
    files, the runner's state, the plugin code, the main repo's .git."""
    out = [run_dir, state_root(repo), plugin_root(), repo / ".git"]
    return [norm(os.path.normpath(str(p))) for p in out]


def _abs_rule(tool: str, p: str) -> str:
    # Claude Code: `//path` is an absolute path rule
    return f"{tool}(//{p.lstrip('/')}/**)"


def write_settings(run_dir: Path, wt: Path, repo: Path, locked: list[str], allow_network: bool,
                   guard_cfg: Path) -> tuple[Path, Path, list[str]]:
    tools = list(TOOLS) + (list(NET_TOOLS) if allow_network else [])
    runner = norm(plugin_root() / "scripts" / "tq_run.sh")
    protect = protected_paths(run_dir, repo)
    deny = ["Bash(git push:*)", "Bash(git commit:*)", "Bash(git reset:*)", "Bash(git checkout:*)",
            "Bash(git config:*)"]
    if not allow_network:
        deny += list(NET_TOOLS)
    for pat in locked:
        deny += [f"Edit({pat})", f"Write({pat})"]
    for pth in protect:
        deny += [_abs_rule("Edit", pth), _abs_rule("Write", pth)]
    settings = {
        "disableAllHooks": False,
        "permissions": {"allow": tools, "deny": deny},
        "hooks": {"PreToolUse": [{"matcher": ".*", "hooks": [
            {"type": "command", "command": f'bash "{runner}" loop-guard.py', "timeout": 10}]}]},
    }
    settings_path = run_dir / "settings.json"
    write_json(settings_path, settings)
    mcp_path = run_dir / "mcp-empty.json"
    write_json(mcp_path, {"mcpServers": {}})
    write_json(guard_cfg, {"root": norm(wt), "locked": locked, "allow_network": allow_network,
                           "protect": protect})
    return settings_path, mcp_path, deny


def pin_files(paths: list[Path]) -> dict[str, str | None]:
    return {norm(p): file_sha256(p) for p in paths}


def changed_pins(pins: dict[str, str | None]) -> list[str]:
    return [p for p, h in pins.items() if file_sha256(p) != h]


def claude_argv(claude: list[str], settings: Path, mcp: Path, round_usd: float, budget: Budget,
                allow_network: bool, disallowed: list[str] | None = None) -> list[str]:
    tools = ",".join(list(TOOLS) + (list(NET_TOOLS) if allow_network else []))
    argv = [*claude, "-p", "--output-format", "json", "--permission-mode", "dontAsk",
            "--tools", tools, "--allowedTools", tools,
            # project/local settings in the worktree are never loaded: the agent
            # could otherwise plant .claude/settings.local.json with disableAllHooks
            "--setting-sources", "user",
            "--strict-mcp-config", "--mcp-config", norm(mcp),
            "--settings", norm(settings),
            "--max-budget-usd", f"{max(round_usd, 0.01):.2f}",
            "--no-session-persistence"]
    if disallowed:
        argv += ["--disallowedTools", ",".join(disallowed)]
    if budget.max_turns:
        argv += ["--max-turns", str(budget.max_turns)]
    return argv


def emit_signal(workspace: str, message: str) -> str:
    try:
        from tentaqles.memory.signals import SignalBus

        return SignalBus().emit(from_workspace=workspace, to_workspace=workspace,
                                event_type="custom", message=message)
    except Exception as e:
        return f"signal not sent: {e}"


TSV_HEADER = "round\tstatus\tscore\tbest\tcost_usd\ttokens\tseconds\tidea\tfailing\tcommit\n"


def run_loop(repo: Path, feature: str, *, workspace: str | None = None, budget: Budget | None = None,
             claude_cmd: list[str] | None = None, use_tq: bool = True, allow_network: bool = False,
             signal: bool = False, run_id: str | None = None, clock=time.time,
             worktree_dir: Path | None = None) -> dict:
    budget = budget or Budget()
    start = clock()
    deadline = start + budget.hours * 3600
    approved = load_approval(repo, feature)
    if approved is None:
        raise SystemExit(f"no valid approval for '{feature}' (missing, edited or not signed). "
                         "The human runs `loop-runner approve` during the day.")
    run_id = run_id or datetime.now().strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:4]
    run_dir = state_root(repo) / "runs" / run_id
    run_dir.mkdir(parents=True, exist_ok=True)
    git_exclude(repo, f"/{STATE}/")
    tsv = run_dir / "results.tsv"
    tsv.write_text(TSV_HEADER, encoding="utf-8")
    summary = {"feature": feature, "run_id": run_id, "started": now_iso(), "workspace": workspace,
               "rounds": 0, "kept": 0, "undone": 0, "cost_usd": 0.0, "tokens": 0,
               "stop_reason": None, "branch": None, "worktree": None, "notes": []}

    def finish(reason: str) -> dict:
        summary["stop_reason"] = reason
        summary["finished"] = now_iso()
        summary["seconds"] = round(clock() - start, 1)
        write_json(run_dir / "summary.json", summary)
        md = render_summary(summary)
        (run_dir / "summary.md").write_text(md, encoding="utf-8")
        (state_root(repo) / f"LATEST-{feature}.md").write_text(md, encoding="utf-8")
        if signal and workspace:
            summary["signal"] = emit_signal(workspace, signal_message(summary))
            write_json(run_dir / "summary.json", summary)
        return summary

    # -- preflight ----------------------------------------------------------
    tq = shutil.which("tq") if use_tq else None
    if use_tq and workspace and not tq:
        return finish("preflight: tq not found (needed for the workspace identity)")
    prefix = [tq, "run", workspace, "--"] if (tq and workspace) else []
    if prefix:
        code, out, err, _ = run([*prefix, "git", "config", "user.email"], cwd=repo, timeout=30)
        if code != 0 or not out.strip():
            return finish(f"preflight: identity check failed for workspace '{workspace}': {tail(err or out, 2)}")
        code, out, err, _ = run([tq, "doctor"], cwd=repo, timeout=60)
        if code != 0:
            summary["notes"].append("tq doctor reported problems; see `tq doctor`")
    commit = approved["commit"]
    for rel, h in (approved.get("hashes") or {}).items():
        code, content = git(repo, "show", f"{commit}:{rel}")
        if code != 0:
            return finish(f"preflight: approved check {rel} missing at {commit[:10]}")
    if claude_cmd is None:
        found = shutil.which("claude")
        if not found:
            return finish("preflight: claude CLI not found")
        claude_cmd = [found]  # full path: tq run won't inject the workspace permission mode

    # -- worktree -------------------------------------------------------------
    branch = f"loop/{feature}-{run_id}"
    wt = worktree_dir or (repo.parent / f"{repo.name}-loop-{feature}-{run_id}")
    code, out = git(repo, "worktree", "add", "-q", "-b", branch, str(wt), commit, timeout=300)
    if code != 0:
        return finish(f"preflight: git worktree add failed: {tail(out, 3)}")
    summary["branch"], summary["worktree"] = branch, norm(wt)
    locked = list(approved.get("locked") or DEFAULT_LOCKED)
    guard_cfg = run_dir / "guard.json"
    settings, mcp, disallowed = write_settings(run_dir, wt, repo, locked, allow_network, guard_cfg)
    spec = ""
    if approved.get("spec_file"):
        code, spec = git(repo, "show", f"{commit}:{approved['spec_file']}")
        spec = spec if code == 0 else ""
    xml = run_dir / "score.xml"
    check_cmd = approved["scorer"]["command"].replace("{xml}", norm(run_dir / "check.xml")).replace(
        "{python}", resolve_python(wt))
    # Everything the runner trusts is hashed now, before any round, and checked
    # by the runner after every round.
    pins = pin_files([guard_cfg, settings, mcp] + [plugin_root() / f for f in PINNED_PLUGIN_FILES])
    ctx = Ctx(
        wt=wt, branch=branch, feature=feature, approved=approved, spec=spec, check_cmd=check_cmd,
        expected=int(approved.get("expected_total") or 0), names=set(approved.get("names") or []),
        budget=budget, deadline=deadline, clock=clock, prefix=prefix, claude_cmd=claude_cmd,
        settings=settings, mcp=mcp, disallowed=disallowed, allow_network=allow_network, xml=xml, tsv=tsv,
        summary=summary, commit=commit, locked=locked, tq=tq, pins=pins,
        ignored0=ignored_entries(wt), last_good=commit,
        env={**os.environ, "TENTAQLES_HEADLESS_CHILD": "1", "TQ_LOOP": "1", "TQ_LOOP_GUARD": norm(guard_cfg),
             "TQ_LOOP_GUARD_SHA": file_sha256(guard_cfg) or "", "PYTHONDONTWRITEBYTECODE": "1"},
    )
    best = run_scorer(wt, approved["scorer"], xml, budget.score_timeout)
    _log(tsv, 0, "baseline", best, best, 0.0, 0, 0.0, "baseline", "")
    summary["baseline"] = best.text()
    try:
        reason, best = _rounds(ctx, best)
    except Exception as e:  # a crash at 3 a.m. still leaves a summary behind
        reason = f"error: {e}"
        try:
            undo(wt, branch, ctx.last_good, ctx.ignored0)
        except Exception:
            pass
    best = summary.pop("_best", best)
    summary["best"] = best.text()
    summary["failing"] = best.failing[:30]
    return finish(reason)


@dataclass
class Ctx:
    wt: Path
    branch: str
    feature: str
    approved: dict
    spec: str
    check_cmd: str
    expected: int
    names: set
    budget: Budget
    deadline: float
    clock: object
    prefix: list
    claude_cmd: list
    settings: Path
    mcp: Path
    disallowed: list
    allow_network: bool
    xml: Path
    tsv: Path
    summary: dict
    commit: str
    locked: list
    tq: str | None
    pins: dict
    ignored0: set
    last_good: str
    env: dict


def verify_round(c: Ctx, score_fn) -> tuple[str, Score, str]:
    """Everything the runner trusts, re-derived by the runner after the agent ran:
    pinned files, HEAD, diff scope, then the scorer. Returns (status, score, note)."""
    moved = changed_pins(c.pins)
    if moved:
        return "config-tamper", Score(crashed=True), f"guard/config/runner files changed: {moved[:3]}"
    code, head = git(c.wt, "rev-parse", "HEAD")
    if code != 0 or head.strip() != c.last_good or current_branch(c.wt) != c.branch:
        return "tamper", Score(crashed=True), "HEAD or branch moved during the round"
    bad = tampered(c.wt, c.commit, c.locked, c.ignored0)
    if bad:
        return "tamper", Score(crashed=True), f"touched locked paths {bad[:5]}"
    score = score_fn()
    if changed_pins(c.pins):
        return "config-tamper", Score(crashed=True), "guard/config files changed while scoring"
    if tampered(c.wt, c.commit, c.locked, c.ignored0):
        return "tamper", Score(crashed=True), "the scorer run changed locked paths"
    if score.timed_out:
        return "score-timeout", score, ""
    if score.crashed:
        return "crash", score, ""
    if c.expected and score.total != c.expected:
        return "count-changed", score, f"test count {score.total} != approved {c.expected}"
    if c.names and set(score.names) != c.names:
        return "count-changed", score, "the set of test names differs from the approved checks"
    return "scored", score, ""


def _rounds(c: Ctx, best: Score) -> tuple[str, Score]:
    b, summary = c.budget, c.summary
    history: list[dict] = []
    reason = "max-rounds"
    for rnd in range(1, b.rounds + 1):
        if not best.crashed and c.expected and best.passed >= c.expected:
            reason = "done"
            break
        if summary["cost_usd"] >= b.usd - 0.005:
            reason = "budget-usd"
            break
        if summary["tokens"] >= b.tokens:
            reason = "budget-tokens"
            break
        remaining = c.deadline - c.clock()
        if remaining < b.score_timeout + 60:
            reason = "budget-time"
            break
        round_usd = min(b.per_round_usd, b.usd - summary["cost_usd"])
        prompt = build_prompt(c.feature, c.approved, c.spec, best, history, c.check_cmd)
        argv = [*c.prefix, *claude_argv(c.claude_cmd, c.settings, c.mcp, round_usd, b, c.allow_network,
                                        c.disallowed)]
        t0 = c.clock()
        code, out, err, timed_out = run(argv, cwd=c.wt, env=c.env, input_text=prompt,
                                        timeout=min(b.round_timeout, remaining - b.score_timeout))
        # cost/tokens come from the CLI's own JSON result, never from a file in the worktree
        cost, tokens, result = round_usage(None if timed_out else parse_claude_json(out), round_usd)
        summary["cost_usd"] = round(summary["cost_usd"] + cost, 4)
        summary["tokens"] += tokens
        summary["rounds"] = rnd
        idea = "(round timed out)" if timed_out else idea_from(result)
        status, score, note = verify_round(c, lambda: run_scorer(c.wt, c.approved["scorer"], c.xml, b.score_timeout))
        if note:
            summary["notes"].append(f"r{rnd}: {note}; undone")
        sha = ""
        if status == "scored":
            status = "keep" if score.passed > (0 if best.crashed else best.passed) else "undo"
        if status == "keep":
            msg = f"loop({c.feature}) r{rnd}: {score.text()} {idea}"
            sha, skipped, error = commit_round(c.wt, c.branch, msg, c.prefix, c.tq,
                                               locked=c.locked, base=c.last_good)
            if skipped:
                summary["notes"].append(f"r{rnd}: not committed: {skipped[:5]}")
            if sha:
                best = score
                c.last_good = sha
                summary["_best"] = best
                summary["kept"] += 1
            else:
                status = "commit-failed"
                summary["notes"].append(f"r{rnd}: {error}")
        if status != "keep":
            undo(c.wt, c.branch, c.last_good, c.ignored0)
            summary["undone"] += 1
        history.append({"round": rnd, "status": status, "score": score.text(), "idea": idea})
        _log(c.tsv, rnd, status, score, best, cost, tokens, c.clock() - t0, idea, sha or "")
        if status == "config-tamper":
            reason = "config-tamper"
            break
    else:
        if not best.crashed and c.expected and best.passed >= c.expected:
            reason = "done"
    return reason, best


def _log(tsv: Path, rnd: int, status: str, score: Score, best: Score, cost: float, tokens: int,
         seconds: float, idea: str, sha: str) -> None:
    clean = lambda s: re.sub(r"[\t\r\n]+", " ", str(s))  # noqa: E731
    row = [rnd, status, score.text(), best.text(), f"{cost:.4f}", tokens, f"{seconds:.0f}",
           clean(idea), clean("; ".join(score.failing[:20])), sha[:12]]
    with open(tsv, "a", encoding="utf-8", newline="\n") as f:
        f.write("\t".join(str(x) for x in row) + "\n")


def signal_message(s: dict) -> str:
    return (f"loop {s['feature']}: {s.get('best', '?')} ({s['stop_reason']}) in {s['rounds']} round(s), "
            f"{s['kept']} kept, {s['undone']} undone, ${s['cost_usd']:.2f}; branch {s.get('branch') or '-'}")


def render_summary(s: dict) -> str:
    lines = [
        f"# Loop: {s['feature']} ({s['run_id']})",
        "",
        f"- Result: **{s.get('best', '?')}** (baseline {s.get('baseline', '?')}), stopped: `{s['stop_reason']}`",
        f"- Rounds: {s['rounds']} ({s['kept']} kept, {s['undone']} undone)",
        f"- Spend: ${s['cost_usd']:.2f}, {s['tokens']:,} tokens, {s.get('seconds', 0):.0f} s",
        f"- Branch: `{s.get('branch') or '-'}` in `{s.get('worktree') or '-'}`",
    ]
    if s.get("failing"):
        lines.append("- Still failing: " + "; ".join(s["failing"]))
    if s.get("notes"):
        lines += ["", "## Notes", ""] + [f"- {n}" for n in s["notes"]]
    lines += ["", "Review the branch before merging; the loop never pushes or opens a PR.",
              "Per-round log: results.tsv next to this file.", ""]
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    import argparse
    import shlex

    from tentaqles.workflow._common import utf8_stdio

    utf8_stdio()
    p = argparse.ArgumentParser(prog="loop-runner", description="Overnight improvement loop (/tentaqles:loop)")
    p.add_argument("--repo", help="repo root (default: git root of cwd)")
    sub = p.add_subparsers(dest="cmd", required=True)

    a = sub.add_parser("approve", help="freeze checks, scorer and expected count (daytime)")
    a.add_argument("--feature", required=True)
    a.add_argument("--checks", nargs="+", required=True)
    a.add_argument("--score-cmd", required=True,
                   help="test command; {xml} = JUnit XML output path, {python} = project interpreter")
    a.add_argument("--kind", choices=("junit", "stdout"), default="junit")
    a.add_argument("--expect", type=int)
    a.add_argument("--lock", nargs="*", default=[], help="extra locked globs")
    a.add_argument("--spec-file")
    a.add_argument("--notes", default="")

    r = sub.add_parser("run", help="run the loop in a new worktree (night)")
    r.add_argument("--feature", required=True)
    r.add_argument("--workspace", help="tq workspace whose identity runs claude (recommended)")
    r.add_argument("--budget-usd", type=float, default=Budget.usd)
    r.add_argument("--per-round-usd", type=float, default=Budget.per_round_usd)
    r.add_argument("--max-tokens", type=int, default=Budget.tokens)
    r.add_argument("--max-hours", type=float, default=Budget.hours)
    r.add_argument("--max-rounds", type=int, default=Budget.rounds)
    r.add_argument("--round-timeout", type=float, default=Budget.round_timeout)
    r.add_argument("--score-timeout", type=float, default=Budget.score_timeout)
    r.add_argument("--max-turns", type=int)
    r.add_argument("--allow-network", action="store_true")
    r.add_argument("--signal", action="store_true", help="post the summary as a tentaqles signal")
    r.add_argument("--no-tq", action="store_true", help="run without tq (identity from the current env)")
    r.add_argument("--claude-cmd", help="override the claude command (testing)")

    s = sub.add_parser("status", help="print the latest summary for a feature")
    s.add_argument("--feature", required=True)
    args = p.parse_args(argv)

    repo = Path(args.repo) if args.repo else find_git_root(os.getcwd())
    if repo is None:
        print("loop-runner: not inside a git repository", file=sys.stderr)
        return 1
    if args.cmd == "approve":
        from tentaqles.workflow.gate import human_confirm

        if not human_confirm(f"approve the checks of '{args.feature}' for unattended runs"):
            return 2
        try:
            data = approve(repo, args.feature, args.checks, args.score_cmd, args.kind, args.expect,
                           args.lock, args.spec_file, args.notes)
        except ValueError as e:
            print(f"loop-runner: {e}", file=sys.stderr)
            return 1
        print(f"approved '{data['feature']}' at {data['commit'][:10]}: {len(data['checks'])} check file(s), "
              f"expected {data['expected_total']} test(s), baseline {Score(**data['baseline']).text()}")
        return 0
    if args.cmd == "status":
        print(read_text(state_root(repo) / f"LATEST-{args.feature}.md", "no run yet"))
        return 0
    budget = Budget(usd=args.budget_usd, per_round_usd=args.per_round_usd, tokens=args.max_tokens,
                    hours=args.max_hours, rounds=args.max_rounds, round_timeout=args.round_timeout,
                    score_timeout=args.score_timeout, max_turns=args.max_turns)
    claude_cmd = shlex.split(args.claude_cmd, posix=os.name != "nt") if args.claude_cmd else None
    summary = run_loop(repo, args.feature, workspace=args.workspace, budget=budget, claude_cmd=claude_cmd,
                       use_tq=not args.no_tq, allow_network=args.allow_network, signal=args.signal)
    print(render_summary(summary))
    return 0 if summary["stop_reason"] in ("done", "max-rounds") or summary["stop_reason"].startswith("budget") else 1
