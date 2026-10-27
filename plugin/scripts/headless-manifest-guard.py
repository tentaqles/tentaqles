#!/usr/bin/env python3
"""PreToolUse hook: a headless run never edits the client manifest.

Editing `.tentaqles.yaml` moves its trust hash, which blocks the workspace
until a human runs `tq allow`. When TENTAQLES_HEADLESS=1, exit 2 with a message
that points at the proposal-file path when:

- an Edit, Write, MultiEdit or NotebookEdit targets the manifest. The name is
  compared the way Windows resolves it: case-insensitively, without trailing
  dots and spaces or an NTFS stream suffix (`::$DATA`), and through the real
  path, so an 8.3 short name (`TENTAQ~1.YAM`) of an existing file matches too;
- a Bash or PowerShell command writes it. The command is split into its
  segments (`;`, `|`, `&&`, `||`) and words, and a segment blocks only when the
  manifest is what it writes: a redirect target, a `tee` file, a `sed -i` /
  `yq -i` file, the destination of `cp`/`install`/`Copy-Item`, an argument of
  `mv`/`rm`/`truncate`/`Move-Item`/`Remove-Item`/`Set-Content`/`Add-Content`/
  `Out-File`/`New-Item`, `dd of=`, a `git checkout/restore` path, or inline
  interpreter code (`python -c`, `node -e`, ...) that names it and writes.
  Reading it, naming it inside a message (`--text "... .tentaqles.yaml ..."`)
  and redirecting elsewhere (`>/dev/null`, `2>&1`) are allowed.

The runner also re-checks trust before every round, so this guard is the early,
cheap stop, not the only one.

Stdlib only, no network; the only file access is resolving a target path.
Any error allows (exit 0): this guard must never stall a run.

Exit codes: 0 allow, 2 block (message on stderr).
"""

import json
import os
import re
import shlex
import sys

MANIFEST = ".tentaqles.yaml"
FILE_TOOLS = ("Edit", "Write", "MultiEdit", "NotebookEdit")
SHELL_TOOLS = ("Bash", "PowerShell")
MESSAGE = (
    "BLOCKED: a headless run must not edit .tentaqles.yaml (it changes the "
    "manifest trust hash and blocks the workspace until a human runs `tq allow`). "
    "Write the proposed change to docs/privacy-manifest-proposal.yaml, record a "
    "human decision (build-gate ask ... --kind question, or a NEEDS-DECISION line "
    "in docs/privacy.md), and let the human review it, apply it and run "
    "`tq allow <workspace>`."
)

SEPARATORS = {";", "|", "||", "&&", "&", "\n"}
REDIRECTS = re.compile(r"^&?[0-9]*(>>?|>\|)$")
ANY_ARG = {"mv", "rm", "truncate", "move-item", "remove-item", "set-content", "add-content",
           "out-file", "new-item", "rename-item", "clear-content", "mi", "ri", "sc", "ac"}
LAST_ARG = {"cp", "install", "copy-item", "cpi", "ln"}
IN_PLACE = {"sed", "yq", "perl"}
INTERPRETERS = re.compile(r"^(python[0-9.]*|py|pypy[0-9.]*|perl|ruby|node|deno|bun|php|pwsh|powershell)(\.exe)?$")
KEYWORDS = {"then", "do", "else", "elif", "if", "while", "until", "!", "{", "(", "time"}
SHELLS = {"bash", "sh", "zsh", "dash", "ksh", "bash.exe", "sh.exe"}
INLINE_FLAGS = {"-c", "-e", "-E", "-r", "-command", "-Command", "-c:", "eval"}
CODE_WRITES = re.compile(
    r"write|replace\(|rename|unlink|remove|truncate|set-content|add-content|out-file"
    r"|open\([^)]*['\"][wax+]|['\"][wa]\+?['\"]|appendfile|writefile", re.I)


def _name(path: str) -> str:
    base = path.replace("\\", "/").rstrip("/").rsplit("/", 1)[-1]
    if base.count(":") and not re.match(r"^[A-Za-z]:$", base):
        base = base.split(":", 1)[0]  # NTFS stream suffix
    return base.rstrip(" .").strip("'\"").casefold()


def is_manifest(path: str, resolve: bool = True) -> bool:
    if not path or len(path) > 1024:
        return False
    if _name(path) == MANIFEST:
        return True
    if not resolve or any(c in path for c in " \t\n$*?<>|\"'"):
        return False
    try:  # an 8.3 short name or a link resolves to the real name
        return _name(os.path.realpath(path)) == MANIFEST
    except Exception:
        return False


def _words(command: str, posix: bool) -> list:
    lex = shlex.shlex(command, posix=posix, punctuation_chars=";&|<>")
    lex.whitespace_split = True
    lex.commenters = ""
    out = []
    for tok in lex:
        if not posix and len(tok) >= 2 and tok[0] == tok[-1] and tok[0] in "'\"":
            tok = tok[1:-1]
        out.append(tok)
    return out


def _segments(words: list) -> list:
    seg, out = [], []
    for w in words:
        if w in SEPARATORS:
            if seg:
                out.append(seg)
            seg = []
        else:
            seg.append(w)
    if seg:
        out.append(seg)
    return out


def _segment_writes(seg: list) -> bool:
    # redirect targets anywhere in the segment: `> f`, `>>f`, `2> f`
    for i, w in enumerate(seg):
        m = re.match(r"^(&?[0-9]*>>?\|?)(.*)$", w)
        if m and m.group(1):
            target = (m.group(2) or (seg[i + 1] if i + 1 < len(seg) else "")).strip("(){}")
            if target.startswith("&") or target in ("/dev/null", "nul", "NUL", "$null"):
                continue
            if is_manifest(target):
                return True
    words = [w for w in seg if not REDIRECTS.match(w)]
    words = [w.strip("(){}") for w in words]
    words = [w for w in words if w]
    while words and words[0] in KEYWORDS:
        words = words[1:]  # then rm ..., do sed -i ..., ! cmd
    while words and re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", words[0]):
        words = words[1:]  # FOO=bar cmd ...
    while words and words[0] in ("sudo", "env", "command", "exec", "nohup", "time", "xargs"):
        words = words[1:]
    if not words:
        return False
    cmd = words[0].replace("\\", "/").rsplit("/", 1)[-1].casefold()
    args = words[1:]
    plain = [a for a in args if not a.startswith("-")]
    if cmd == "tee":
        return any(is_manifest(a) for a in plain)
    if cmd in ANY_ARG:
        return any(is_manifest(a) for a in args if not a.startswith("-") or ":" in a)
    if cmd in LAST_ARG:
        dest = [a for a in args if not a.startswith("-")]
        for i, a in enumerate(args):  # Copy-Item x -Destination f
            if a.casefold() in ("-destination", "-target") and i + 1 < len(args):
                return is_manifest(args[i + 1])
        return bool(dest) and is_manifest(dest[-1])
    if cmd in SHELLS:
        for i, a in enumerate(args):
            if a in ("-c", "-lc", "-ic") and i + 1 < len(args):
                return shell_writes_manifest(args[i + 1])
    if cmd == "find" and "-delete" in args:
        return any(is_manifest(a) for a in args)
    if cmd == "dd":
        return any(a.startswith("of=") and is_manifest(a[3:]) for a in args)
    if cmd in IN_PLACE and any(a == "--in-place" or (a.startswith("-") and not a.startswith("--")
                                                       and "i" in a[1:]) for a in args):
        return any(is_manifest(a) for a in plain)
    if cmd == "git" and args and args[0] in ("checkout", "restore", "apply", "mv", "rm"):
        return any(is_manifest(a) for a in args[1:])
    if INTERPRETERS.match(cmd) or cmd.startswith("$"):  # "$TENTAQLES_PY" -c ...
        for i, a in enumerate(args):
            if a in INLINE_FLAGS and i + 1 < len(args):
                code = args[i + 1]
                if MANIFEST in code.casefold() and CODE_WRITES.search(code):
                    return True
    return False


HEREDOC = re.compile(r"<<-?\s*(['\"]?)([A-Za-z_][A-Za-z0-9_]*)\1")


def _strip_heredocs(command: str) -> tuple:
    """The lines of a script without heredoc bodies (a body is data, not commands), and the
    bodies that feed a program: (lines, [(program, body)]). The `<<` line itself stays, so
    `cat <<EOF > .tentaqles.yaml` is still seen."""
    out, bodies, delim, prog, body = [], [], None, "", []
    for line in command.replace("\r\n", "\n").split("\n"):
        if delim is not None:
            if line.strip() == delim:
                bodies.append((prog, "\n".join(body)))
                delim, body = None, []
            else:
                body.append(line)
            continue
        out.append(line)
        m = HEREDOC.search(line)
        if m:
            delim = m.group(2)
            first = line[:m.start()].strip().split()
            prog = first[-1] if first else ""
            if first and first[0] not in ("cat", "tee") and len(first) > 1 and first[-1] == "-":
                prog = first[0]
            elif first:
                prog = first[0]
    if delim is not None and body:
        bodies.append((prog, "\n".join(body)))
    return out, bodies


def _body_writes(prog: str, body: str) -> bool:
    cmd = prog.replace("\\", "/").rsplit("/", 1)[-1].casefold()
    if MANIFEST not in body.casefold():
        return False
    if cmd in SHELLS:
        return shell_writes_manifest(body)
    if INTERPRETERS.match(cmd) or cmd.startswith("$"):
        return bool(CODE_WRITES.search(body))
    return False


def _logical_lines(lines: list, posix: bool) -> list:
    """Join continuation lines and lines inside an open quote; each result parses on its own.
    A line that never balances is parsed alone (its quote error then skips only that line)."""
    out, buf = [], ""
    for line in lines:
        buf = buf + "\n" + line if buf else line
        if buf.endswith("\\") and posix:
            buf = buf[:-1]
            continue
        try:
            _words(buf, posix)
        except ValueError:
            if buf.count("\n") < 200:
                continue  # an open quote: keep reading
        out.append(buf)
        buf = ""
    if buf:
        out.extend(buf.split("\n"))
    return out


def shell_writes_manifest(command: str, powershell: bool = False) -> bool:
    low = command.casefold()
    if "tentaqles.yaml" not in low and "tentaq~" not in low:
        return False
    posix = not powershell
    lines, bodies = _strip_heredocs(command)
    if any(_body_writes(prog, body) for prog, body in bodies):
        return True
    if powershell and re.search(r"\[(system\.)?io\.file\]::(write|append|move|copy|delete|replace)",
                                low) and MANIFEST in low:
        return True
    for line in _logical_lines(lines, posix):
        if "tentaqles.yaml" not in line.casefold() and "tentaq~" not in line.casefold():
            continue
        try:
            words = _words(line, posix)
        except ValueError:
            continue  # unbalanced quotes: the shell would refuse this line too
        if any(_segment_writes(seg) for seg in _segments(words)):
            return True
    return False


def main() -> None:
    try:
        if os.environ.get("TENTAQLES_HEADLESS", "").strip() != "1":
            sys.exit(0)
        raw = sys.stdin.read()
        if not raw.strip():
            sys.exit(0)
        payload = json.loads(raw)
        if not isinstance(payload, dict):
            sys.exit(0)
        tool = payload.get("tool_name", "")
        ti = payload.get("tool_input")
        if not isinstance(ti, dict):
            ti = payload
        if tool in SHELL_TOOLS:
            cmd = ti.get("command") or ""
            if isinstance(cmd, str) and shell_writes_manifest(cmd, powershell=tool == "PowerShell"):
                sys.stderr.write(MESSAGE + "\n")
                sys.exit(2)
            sys.exit(0)
        path = ti.get("file_path") or ti.get("notebook_path") or ti.get("path") or ""
        if not isinstance(path, str) or not path:
            sys.exit(0)
        if (not tool or tool in FILE_TOOLS) and is_manifest(path):
            sys.stderr.write(MESSAGE + "\n")
            sys.exit(2)
    except SystemExit:
        raise
    except Exception:
        pass
    sys.exit(0)


if __name__ == "__main__":
    main()
