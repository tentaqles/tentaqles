#!/usr/bin/env python3
"""
Fallback identity guard (PreToolUse / Bash).

This script only runs when the `tq` binary is NOT installed: `tq_hook.sh`
resolves `tq` first and exec's `tq claude-hook pre-tool-use`, and only falls
back here (with TQ_FALLBACK=1) when no binary is found. It is deliberately
fail-closed and dependency-free: it does not read manifests, does not shell
out to git/gh, and does not import `tentaqles.*`.

Rules:
- self-protection (ADR-0005, PRD v2 §8.2): a file tool (Write, Edit,
  MultiEdit, NotebookEdit) that writes under tq's state folder
  (``~/.tentaqles``) or the installed tentaqles plugin
  (``<config dir>/plugins/cache/tentaqles``), a Read or Grep under the state
  folder, and a shell command that names either one (a text match on the
  expanded home; it cannot tell a read from a write) are blocked. The root
  marker ``.tentaqles.yaml`` is not matched here: tq protects it.
- identity: any remote mutation — `git push|fetch|pull|clone` in ANY chained
  git segment, any `gh` invocation, any cloud CLI (az aws gcloud gsutil bq
  doctl).
Everything else is allowed.

Exit codes:
  0 — allow
  2 — BLOCK (message on stderr, starts with "BLOCKED")
"""

import json
import os
import re
import sys

INSTALL_URL = "https://github.com/tentaqles/tentaqles#install"

# Mirrors internal/guard/guard.go sepPattern: classic separators, line breaks,
# and command-substitution/grouping openers.
_SEP = r"&&|&|\|\||;|\||\n|\r|\$\(|`|\(|\)|\{|\}"
_SEP_RE = re.compile(_SEP)

CLOUD_CLIS = ("az", "aws", "gcloud", "gsutil", "bq", "doctl")
REMOTE_GIT_SUBS = ("push", "fetch", "pull", "clone")


def _command_starts_with(command: str, prefix: str) -> bool:
    """Check if the command starts with a given CLI tool name."""
    # Match the tool name at word boundary: "git commit" matches "git", "github" does not
    pattern = r"(?:^|" + _SEP + r")\s*" + re.escape(prefix.strip()) + r"(?:\s|$)"
    return bool(re.search(pattern, command))


def _git_subcommands(command: str):
    """Yield the sub-word of every git invocation in command (mirrors gitSegments)."""
    for seg in _SEP_RE.split(command):
        fields = seg.split()
        if not fields or fields[0] != "git":
            continue
        i = 1
        while i < len(fields):
            arg = fields[i]
            if arg in ("-C", "-c"):
                i += 2
                continue
            if arg.startswith("-"):
                i += 1
                continue
            yield arg
            break


def _is_remote_mutation(command: str) -> bool:
    if _command_starts_with(command, "gh"):
        return True
    for cli in CLOUD_CLIS:
        if _command_starts_with(command, cli):
            return True
    for sub in _git_subcommands(command):
        if sub in REMOTE_GIT_SUBS:
            return True
    return False


_WINDOWS = os.name == "nt"
FILE_WRITE_TOOLS = ("Write", "Edit", "MultiEdit", "NotebookEdit")
FILE_READ_TOOLS = ("Read", "Grep", "NotebookRead")
SHELL_TOOLS = ("Bash", "PowerShell")
PROTECTED_ID = "self-protection/tq/protected-path"


def _norm(p: str) -> str:
    """Forward slashes, no trailing slash, lower case on Windows."""
    p = p.replace("\\", "/").rstrip("/")
    return p.lower() if _WINDOWS else p


def _homes():
    """The user's home as Python and as the shell spell it."""
    out = []
    for h in (os.path.expanduser("~"), os.environ.get("USERPROFILE", ""), os.environ.get("HOME", "")):
        h = (h or "").strip()
        if not h:
            continue
        m = re.match(r"^/([A-Za-z])(/|$)", h)
        if _WINDOWS and m:  # an MSYS home (/c/Users/x)
            h = m.group(1) + ":/" + h[len(m.group(0)):]
        h = _norm(h)
        if h and h not in out:
            out.append(h)
    return out


def _roots():
    """(state roots, plugin roots), normalised."""
    homes = _homes()
    state = [h + "/.tentaqles" for h in homes]
    config_dirs = [h + "/.claude" for h in homes]
    if os.environ.get("CLAUDE_CONFIG_DIR"):
        config_dirs.append(_norm(os.environ["CLAUDE_CONFIG_DIR"]))
    plugin = [d + "/plugins/cache/tentaqles" for d in config_dirs]
    return state, plugin


def _under(path: str, roots) -> bool:
    return any(path == r or path.startswith(r + "/") for r in roots)


def _file_path(tool_input: dict, cwd: str) -> str:
    for key in ("file_path", "notebook_path", "path"):
        v = tool_input.get(key)
        if isinstance(v, str) and v.strip():
            p = os.path.expanduser(v.strip())
            if not os.path.isabs(p) and cwd:
                p = os.path.join(cwd, p)
            return _norm(os.path.normpath(p))
    return ""


def _expand(command: str) -> str:
    """Expand ~ and the home variables the way a shell would spell them."""
    homes = _homes()
    home = homes[0] if homes else ""
    if not home:
        return command
    command = re.sub(r"\$\{?(HOME|USERPROFILE)\}?\b|\$env:(HOME|USERPROFILE)\b|%(HOME|USERPROFILE)%", home, command, flags=re.IGNORECASE)
    return re.sub(r"(^|[\s\"'=(,;:])~(?=[/\\]|$)", lambda m: m.group(1) + home, command)


def _shell_names(command: str, roots) -> bool:
    text = _norm(_expand(command))
    forms = list(roots)
    if _WINDOWS:  # the MSYS spelling of a drive path (/c/users/x)
        forms += ["/" + r[0] + r[2:] for r in roots if re.match(r"^[a-z]:/", r)]
    for r in forms:
        if re.search(re.escape(r) + r"(?=$|[/\s\"'`;|&)<>,])", text):
            return True
    return False


def _protected(tool: str, tool_input: dict, cwd: str) -> str:
    """The deny reason when the call touches a protected path, else ""."""
    state, plugin = _roots()
    if tool in FILE_WRITE_TOOLS or tool in FILE_READ_TOOLS:
        p = _file_path(tool_input, cwd)
        if not p:
            return ""
        if _under(p, state):
            return p + " is under tq's state folder, a protected path"
        if tool in FILE_WRITE_TOOLS and _under(p, plugin):
            return p + " is the installed tentaqles plugin, a protected path"
        return ""
    if tool in SHELL_TOOLS or not tool:
        command = tool_input.get("command", "")
        if isinstance(command, str) and command and _shell_names(command, state + plugin):
            return "the command names tq's state folder or the installed tentaqles plugin, a protected path"
    return ""


def main() -> None:
    try:
        raw = sys.stdin.read()
    except Exception:
        raw = "{}"

    try:
        payload = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, TypeError, ValueError):
        payload = {}
    if not isinstance(payload, dict):
        payload = {}

    tool_input = payload.get("tool_input", {})
    if isinstance(tool_input, str):
        try:
            tool_input = json.loads(tool_input)
        except (json.JSONDecodeError, TypeError, ValueError):
            tool_input = {}
    if not isinstance(tool_input, dict):
        tool_input = {}

    tool = payload.get("tool_name", "")
    if not isinstance(tool, str):
        tool = ""
    cwd = payload.get("cwd", "")
    if not isinstance(cwd, str):
        cwd = ""
    why = _protected(tool, tool_input, cwd)
    if why:
        print(
            f"BLOCKED [{PROTECTED_ID}]: {why}; tq denies a session's change to it "
            f"(tq is not installed: this is the fallback guard). Install tq: {INSTALL_URL}",
            file=sys.stderr,
        )
        sys.exit(2)

    command = tool_input.get("command", "")
    if not command or not isinstance(command, str):
        sys.exit(0)

    if _is_remote_mutation(command):
        print(
            f"BLOCKED: tq is not installed; refusing '{command}' without a "
            f"verified identity. Install tq: {INSTALL_URL}",
            file=sys.stderr,
        )
        sys.exit(2)

    sys.exit(0)


if __name__ == "__main__":
    main()
