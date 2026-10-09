"""PreToolUse guard for the overnight loop's headless `claude -p` child.

The runner writes a JSON config and points `TQ_LOOP_GUARD` at it:

    {"root": "<worktree>", "locked": ["checks/**", "pytest.ini", ...],
     "allow_network": false}

The child may edit app code inside the worktree and run tests. It may not touch
the scorer, the locked checks or test-runner config, run git commands that change
history (the runner owns git), reach the network, read `.env` files or dump the
environment. Like tq's guard these are string checks that catch honest mistakes;
the runner's tamper check after every round is what makes cheating pointless.

Exit 2 + stderr blocks the call. Missing or unreadable config blocks everything
except reads: a loop with no guard must not run.
"""

from __future__ import annotations

import json
import os
import re
import sys
from pathlib import Path

from tentaqles.workflow._common import (
    WRITE_PLAIN,
    glob_match,
    norm,
    read_json,
    read_stdin_json,
    rel_to,
    segments,
    utf8_stdio,
)

READ_ONLY_GIT = {"status", "diff", "log", "show", "ls-files", "rev-parse", "blame", "grep", "shortlog"}
NETWORK = re.compile(
    r"\b(?:curl|wget|invoke-webrequest|iwr|invoke-restmethod|irm|ssh|scp|sftp|rsync|nc|ncat|telnet|ftp"
    r"|gh|az|aws|gcloud|gsutil|kubectl|doctl|vercel|netlify|supabase|psql|mysql|mongosh|redis-cli)\b"
    r"|\b(?:npm|pnpm|yarn|bun)\s+(?:i|install|add|publish|update|upgrade|dlx)\b|\bnpx\b"
    r"|\b(?:pip|pip3)\s+(?:install|download)\b|\buv\s+(?:add|sync|pip\s+install|tool\s+install)\b"
    r"|\bpoetry\s+(?:add|install|update)\b|\bgo\s+(?:get|install)\b|\bcargo\s+(?:add|install)\b"
    r"|\b(?:apt|apt-get|brew|choco|winget|scoop)\b|\bdocker\s+(?:pull|push|login)\b",
    re.IGNORECASE,
)
ENV_FILE = re.compile(r"(?:^|[\s/\\\"'=])\.env(?:\.[\w-]+)?\b(?!\.?(?:example|sample|template|dist))", re.IGNORECASE)
ENV_TEMPLATE = re.compile(r"\.env\.(?:example|sample|template|dist)\b", re.IGNORECASE)
ENV_DUMP = re.compile(r"^\s*(?:printenv|env|set|export\s+-p|get-childitem\s+env:|gci\s+env:|dir\s+env:|ls\s+env:)\s*$",
                      re.IGNORECASE)
ALWAYS_LOCKED = (".claude/**", ".git/**", ".scores/**")


class Blocked(Exception):
    pass


def _git_sub(seg: str) -> str | None:
    m = re.match(r"\s*(?:\S*[/\\])?git(?:\.exe)?\s+((?:-[Cc]\s+\S+\s+|--?[\w-]+(?:=\S+)?\s+)*)([\w-]+)", seg)
    return m.group(2).lower() if m else None


def _is_env_file_mention(text: str) -> bool:
    cleaned = ENV_TEMPLATE.sub("", text)
    return bool(ENV_FILE.search(cleaned))


def check_shell(cmd: str, cfg: dict) -> None:
    text = norm(cmd)
    locked = list(cfg.get("locked") or []) + list(ALWAYS_LOCKED)
    for seg in segments(text):
        sub = _git_sub(seg)
        if sub is not None and sub not in READ_ONLY_GIT:
            raise Blocked(f"`git {sub}` is not allowed in the loop: the runner commits or undoes each round itself.")
        if not cfg.get("allow_network") and NETWORK.search(seg):
            raise Blocked("network and package-manager commands are off in the overnight loop (no new dependencies).")
        if ENV_DUMP.match(seg):
            raise Blocked("dumping the environment is not allowed (it can print secrets).")
        if _is_env_file_mention(seg):
            raise Blocked("`.env` files are off limits in the loop. Code reads config from the environment.")
        if WRITE_PLAIN.search(seg):
            for word in re.findall(r"[\w./\\*-]+", seg):
                w = norm(word).strip("'\"")
                if not w or w.startswith("-"):
                    continue
                rel = rel_to(Path(cfg["root"]), w) if cfg.get("root") else w
                if rel is None:
                    continue
                if glob_match(rel, locked) or glob_match(rel.rstrip("/") + "/x", locked):
                    raise Blocked(f"{rel} is locked (scorer, checks or test config). Change the app, not the checks.")


def check_file(tool: str, inp: dict, cfg: dict) -> None:
    raw = inp.get("file_path") or inp.get("notebook_path") or inp.get("path") or ""
    if not raw:
        return
    root = Path(cfg["root"])
    rel = rel_to(root, raw)
    if tool in ("Read", "Grep", "Glob"):
        if _is_env_file_mention("/" + norm(raw).split("/")[-1]):
            raise Blocked("`.env` files are off limits in the loop.")
        return
    if rel is None:
        raise Blocked(f"{norm(raw)} is outside the loop's worktree; edits stay inside it.")
    hit = glob_match(rel, list(cfg.get("locked") or []) + list(ALWAYS_LOCKED))
    if hit:
        raise Blocked(f"{rel} is locked ({hit}). Change the app, not the checks or the scorer.")
    if _is_env_file_mention("/" + rel.split("/")[-1]):
        raise Blocked("`.env` files are off limits in the loop.")


def decide(data: dict, cfg: dict | None) -> tuple[int, str]:
    tool = data.get("tool_name") or ""
    inp = data.get("tool_input") or {}
    if cfg is None or not cfg.get("root"):
        if tool in ("Read", "Grep", "Glob"):
            return 0, ""
        return 2, "loop-guard: no guard config (TQ_LOOP_GUARD); refusing to run unguarded."
    try:
        if tool.startswith("mcp__"):
            raise Blocked("MCP tools are disabled in the overnight loop.")
        if tool in ("WebFetch", "WebSearch") and not cfg.get("allow_network"):
            raise Blocked("network tools are disabled in the overnight loop.")
        if tool in ("Bash", "PowerShell"):
            check_shell(inp.get("command") or "", cfg)
        elif tool in ("Write", "Edit", "MultiEdit", "NotebookEdit", "Read", "Grep", "Glob"):
            check_file(tool, inp, cfg)
    except Blocked as e:
        return 2, f"loop-guard: {e}"
    return 0, ""


def main() -> int:
    utf8_stdio()
    data = read_stdin_json()
    path = os.environ.get("TQ_LOOP_GUARD")
    cfg = read_json(path) if path else None
    try:
        code, msg = decide(data, cfg if isinstance(cfg, dict) else None)
    except Exception as e:  # a crash must not open the gate in a loop: block
        code, msg = 2, f"loop-guard: internal error, blocking: {e}"
    if msg:
        print(msg, file=sys.stderr)
    return code


if __name__ == "__main__":
    sys.exit(main())
