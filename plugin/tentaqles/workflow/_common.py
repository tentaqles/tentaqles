"""Helpers shared by the build gate, the loop runner and the loop guard.

Standard library only. Every subprocess has a timeout, and a timed-out process is
killed together with its children (a test runner that hangs must not survive).
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import signal
import subprocess
import sys
import tempfile
from pathlib import Path

IS_WINDOWS = os.name == "nt"


# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------


def norm(path: str | os.PathLike | None) -> str:
    """Forward slashes, so one regex works on Windows and POSIX paths."""
    return str(path or "").replace("\\", "/")


def find_git_root(start: str | os.PathLike | None) -> Path | None:
    """Nearest ancestor (or self) holding a `.git` dir or file (worktrees use a file)."""
    if not start:
        return None
    p = Path(str(start))
    if not p.is_absolute():
        p = Path.cwd() / p
    if not p.exists():
        p = p.parent
    if p.is_file():
        p = p.parent
    for cand in (p, *p.parents):
        if (cand / ".git").exists():
            return cand
    return None


def rel_to(root: Path, path: str) -> str | None:
    """`path` relative to `root` in forward slashes, or None when outside it.

    Case-insensitive on Windows, where `C:\\Repo` and `c:/repo` are one folder.
    """
    if not path:
        return None
    p = Path(norm(path))  # normalize first: on POSIX a backslash is not a separator
    if not p.is_absolute():
        p = root / p
    a, b = norm(os.path.normpath(norm(p))), norm(os.path.normpath(norm(root)))
    if IS_WINDOWS:
        a_cmp, b_cmp = a.lower(), b.lower()
    else:
        a_cmp, b_cmp = a, b
    b_cmp = b_cmp.rstrip("/")
    if a_cmp == b_cmp:
        return ""
    if not a_cmp.startswith(b_cmp + "/"):
        return None
    return a[len(b_cmp) + 1:]


_GLOB_CACHE: dict[str, re.Pattern] = {}


def glob_to_regex(pattern: str) -> re.Pattern:
    """`**` crosses folders, `*` and `?` don't. Matches a forward-slash relative path."""
    if pattern in _GLOB_CACHE:
        return _GLOB_CACHE[pattern]
    pat = norm(pattern).lstrip("/")
    out, i = [], 0
    while i < len(pat):
        c = pat[i]
        if pat.startswith("**/", i):
            out.append(r"(?:.*/)?")
            i += 3
        elif pat.startswith("**", i):
            out.append(r".*")
            i += 2
        elif c == "*":
            out.append(r"[^/]*")
            i += 1
        elif c == "?":
            out.append(r"[^/]")
            i += 1
        else:
            out.append(re.escape(c))
            i += 1
    rx = re.compile("^" + "".join(out) + "$", re.IGNORECASE if IS_WINDOWS else 0)
    _GLOB_CACHE[pattern] = rx
    return rx


def glob_match(rel: str, patterns) -> str | None:
    """The first pattern matching `rel`, or None."""
    rel = norm(rel).lstrip("/")
    for pat in patterns:
        if glob_to_regex(pat).match(rel):
            return pat
    return None


# ---------------------------------------------------------------------------
# Files
# ---------------------------------------------------------------------------


def read_text(path: str | os.PathLike, default: str = "") -> str:
    try:
        return Path(path).read_text(encoding="utf-8", errors="replace")
    except OSError:
        return default


def read_json(path: str | os.PathLike, default=None):
    try:
        return json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return default


def write_json(path: str | os.PathLike, data) -> None:
    """Atomic UTF-8 JSON write (temp file + replace)."""
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=str(path.parent), prefix=".tmp-", suffix=".json")
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as f:
            json.dump(data, f, indent=2, ensure_ascii=False)
            f.write("\n")
        os.replace(tmp, path)
    except BaseException:
        try:
            os.remove(tmp)
        except OSError:
            pass
        raise


def append_jsonl(path: str | os.PathLike, row: dict) -> None:
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "a", encoding="utf-8", newline="\n") as f:
        f.write(json.dumps(row, ensure_ascii=False) + "\n")


def file_sha256(path: str | os.PathLike) -> str | None:
    """Content hash with CRLF folded to LF, so a line-ending flip is not 'tampering'."""
    try:
        data = Path(path).read_bytes()
    except OSError:
        return None
    return hashlib.sha256(data.replace(b"\r\n", b"\n")).hexdigest()


# ---------------------------------------------------------------------------
# Processes
# ---------------------------------------------------------------------------


def _kill_tree(proc: subprocess.Popen) -> None:
    try:
        if IS_WINDOWS:
            subprocess.run(
                ["taskkill", "/T", "/F", "/PID", str(proc.pid)],
                capture_output=True, timeout=15,
            )
        else:
            os.killpg(proc.pid, signal.SIGKILL)
    except Exception:
        pass
    try:
        proc.kill()
    except Exception:
        pass


def run(cmd, *, cwd=None, env=None, timeout: float = 30, input_text: str | None = None,
        shell: bool = False) -> tuple[int, str, str, bool]:
    """Run a command; returns (exit_code, stdout, stderr, timed_out).

    UTF-8 both ways. On timeout the whole process tree is killed and the exit
    code is 124 (the coreutils `timeout` convention). A missing executable is 127.
    """
    kwargs = dict(
        cwd=str(cwd) if cwd else None,
        env=env,
        stdin=subprocess.PIPE if input_text is not None else subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        shell=shell,
    )
    if IS_WINDOWS:
        kwargs["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP
    else:
        kwargs["start_new_session"] = True
    try:
        proc = subprocess.Popen(cmd, **kwargs)
    except (OSError, ValueError) as e:
        return 127, "", f"{e}", False
    try:
        out, err = proc.communicate(
            input=input_text.encode("utf-8") if input_text is not None else None,
            timeout=max(1.0, float(timeout)),
        )
        timed_out = False
        code = proc.returncode
    except subprocess.TimeoutExpired:
        _kill_tree(proc)
        try:
            out, err = proc.communicate(timeout=10)
        except Exception:
            out, err = b"", b""
        timed_out = True
        code = 124
    return (
        code,
        (out or b"").decode("utf-8", errors="replace"),
        (err or b"").decode("utf-8", errors="replace"),
        timed_out,
    )


def git(root, *args, timeout: float = 20, env=None) -> tuple[int, str]:
    code, out, err, _ = run(["git", *args], cwd=root, timeout=timeout, env=env)
    return code, out if code == 0 else (out + err)


def find_bash() -> str | None:
    """Git Bash on Windows (never WSL's System32 bash, which can't see C:\\ paths)."""
    import shutil

    for cand in (
        os.environ.get("TQ_BASH"),
        r"C:\Program Files\Git\bin\bash.exe",
        r"C:\Program Files (x86)\Git\bin\bash.exe",
    ):
        if cand and os.path.exists(cand):
            return cand
    found = shutil.which("bash")
    if found and "system32" in found.lower():
        return None
    return found


def tail(text: str, lines: int = 40) -> str:
    parts = (text or "").rstrip().splitlines()
    return "\n".join(parts[-lines:])


def redact(text: str) -> str:
    """Best-effort secret redaction through the plugin's privacy module."""
    try:
        from tentaqles.privacy import redact_text

        out = redact_text(text)
        return out[0] if isinstance(out, tuple) else out
    except Exception:
        return text


def utf8_stdio() -> None:
    """Windows consoles default to cp1252; hooks read and print UTF-8."""
    for name in ("stdout", "stderr"):
        stream = getattr(sys, name)
        try:
            stream.reconfigure(encoding="utf-8", errors="replace")
        except Exception:
            pass


def read_stdin_json() -> dict:
    try:
        raw = sys.stdin.buffer.read().decode("utf-8", errors="replace")
        data = json.loads(raw) if raw.strip() else {}
        return data if isinstance(data, dict) else {}
    except Exception:
        return {}


# ---------------------------------------------------------------------------
# Shell-command heuristics (string checks, not a parser: they catch honest
# mistakes, the evidence/tamper checks catch the rest)
# ---------------------------------------------------------------------------

_SEGMENT_SPLIT = re.compile(r"&&|\|\||;|\||\r?\n")


def segments(cmd: str) -> list[str]:
    return [s.strip() for s in _SEGMENT_SPLIT.split(cmd or "") if s.strip()]


# Explicit file-writing verbs, POSIX and PowerShell. `2>&1` and `>&2` are not writes.
_WRITE_VERBS = (
    r"(?:^|[^<0-9&>])>{1,2}(?!&)"
    r"|\btee\b|\bsed\s+(?:-[a-z]*\s+)*-[a-z]*i|\bperl\s+-[a-z]*i"
    r"|\b(?:mv|cp|rm|rmdir|unlink|truncate|dd|install|ln|touch|chmod|shred)\b"
    r"|\bgit\s+(?:checkout|restore|rm|mv|reset|stash|apply|am|clean|update-index)\b"
    r"|\b(?:set-content|add-content|out-file|new-item|remove-item|move-item|copy-item"
    r"|rename-item|clear-content|del|erase|ren|move|copy|xcopy|robocopy)\b"
    r"|\[(?:system\.)?io\.file\]|writealltext|writeallbytes"
)
WRITE_PLAIN = re.compile(_WRITE_VERBS, re.IGNORECASE)
# Stricter: also any interpreter, for files nobody legitimately runs code against
# (hook-owned state, the plan JSON).
WRITE_STRICT = re.compile(
    _WRITE_VERBS + r"|\b(?:python[0-9.]*|py|node|deno|bun|ruby|perl|php|jq|awk|pwsh|powershell)\b",
    re.IGNORECASE,
)


# ---------------------------------------------------------------------------
# Shell lexer: one normalisation pass shared by the gate and the loop guard.
#
# Splits compound commands outside quotes (&& || ; | & newline, subshells,
# $(...) and backticks become their own segments), records redirect targets,
# strips quotes. Backslash is literal outside double quotes so Windows paths
# survive; inside double quotes it escapes " \ $ `. Unclosed quotes raise
# ShellParseError, and callers fail closed.
# ---------------------------------------------------------------------------


class ShellParseError(ValueError):
    pass


class Segment:
    __slots__ = ("argv", "redirects", "raw")

    def __init__(self):
        self.argv: list[str] = []
        self.redirects: list[str] = []
        self.raw = ""

    def __repr__(self):  # pragma: no cover - debugging aid
        return f"Segment(argv={self.argv!r}, redirects={self.redirects!r})"


_DQ_ESCAPES = '"\\$`'


def lex_shell(cmd: str) -> list[Segment]:
    text = cmd or ""
    segs: list[Segment] = []
    state = {"cur": Segment(), "word": [], "have": False, "redir": False, "start": 0}

    def end_word():
        if state["have"]:
            w = "".join(state["word"])
            if state["redir"]:
                state["cur"].redirects.append(w)
                state["redir"] = False
            else:
                state["cur"].argv.append(w)
        state["word"], state["have"] = [], False

    def end_segment(upto):
        end_word()
        cur = state["cur"]
        cur.raw = text[state["start"]:upto]
        if cur.argv or cur.redirects:
            segs.append(cur)
        state["cur"] = Segment()
        state["redir"] = False

    i, n = 0, len(text)
    while i < n:
        c = text[i]
        if c == "'":
            j = text.find("'", i + 1)
            if j < 0:
                raise ShellParseError("unclosed single quote")
            state["word"].append(text[i + 1:j])
            state["have"] = True
            i = j + 1
            continue
        if c == '"':
            j, buf = i + 1, []
            while j < n and text[j] != '"':
                if text[j] == "\\" and j + 1 < n and text[j + 1] in _DQ_ESCAPES:
                    buf.append(text[j + 1])
                    j += 2
                    continue
                buf.append(text[j])
                j += 1
            if j >= n:
                raise ShellParseError("unclosed double quote")
            state["word"].append("".join(buf))
            state["have"] = True
            i = j + 1
            continue
        if c in " \t":
            end_word()
            i += 1
            continue
        if text.startswith("$(", i) or c in "`()":
            end_segment(i)
            i += 2 if text.startswith("$(", i) else 1
            state["start"] = i
            continue
        if c == "&" and text.startswith("&>", i):
            end_word()
            state["redir"] = True
            i += 3 if text.startswith("&>>", i) else 2
            continue
        if c == "&" and i > 0 and text[i - 1] == ">":
            # `>&N` duplicates a descriptor: not a file write
            state["redir"] = False
            i += 1
            while i < n and text[i].isdigit():
                i += 1
            continue
        if c in ";\r\n|&":
            end_segment(i)
            i += 2 if text[i:i + 2] in ("&&", "||") else 1
            state["start"] = i
            continue
        if c in "<>":
            if state["have"] and "".join(state["word"]).isdigit():
                state["word"], state["have"] = [], False  # fd prefix such as 2>
            end_word()
            if c == ">":
                state["redir"] = True
                i += 1
                while i < n and text[i] in ">|":
                    i += 1
            else:
                i += 1  # input redirect: the next word is read, not written
            continue
        state["word"].append(c)
        state["have"] = True
        i += 1
    end_segment(n)
    return segs


_NULL_TARGETS = {"/dev/null", "nul", "$null", "nul:"}


def is_null_target(t: str) -> bool:
    return norm(t).lower() in _NULL_TARGETS


def has_expansion(t: str) -> bool:
    """A word whose value the lexer can't know (variables, substitution, ~)."""
    return bool(re.search(r"\$|`|%[A-Za-z_]+%|^~", t or "")) and not is_null_target(t)


def canon_text(text: str) -> str:
    """Normalised command text for substring checks: / separators, no ./ or //,
    `x/../` collapsed, case-folded (folding only makes checks stricter)."""
    t = norm(text)
    t = re.sub(r"/+", "/", t)
    t = re.sub(r"(?:(?<=/)|^)\./", "", t)
    prev = None
    while prev != t:
        prev = t
        t = re.sub(r"[^/\s\"'=]+/\.\./", "", t)
    return t.lower()


def resolve_word(root, word: str) -> tuple[str | None, str]:
    """(path relative to root or None when outside, canonical absolute path).

    Strips an `--opt=` prefix and quotes, normalises separators, resolves `..`,
    folds case on Windows.
    """
    w = word
    if w.startswith("-") and "=" in w:
        w = w.split("=", 1)[1]
    w = norm(w).strip("'\"")
    p = Path(w)
    if not p.is_absolute() and not re.match(r"^[A-Za-z]:/", w):
        p = Path(norm(root)) / w
    absn = norm(os.path.normpath(norm(p)))
    rel = rel_to(Path(root), absn)
    if IS_WINDOWS:
        absn = absn.lower()
        rel = rel.lower() if rel is not None else None
    return rel, absn


WRITE_COMMANDS = {
    "mv", "cp", "rm", "rmdir", "unlink", "truncate", "dd", "install", "ln", "touch", "chmod",
    "chown", "shred", "tee", "mkdir", "rsync", "tar", "unzip", "patch",
    "set-content", "add-content", "out-file", "new-item", "remove-item", "move-item", "copy-item",
    "rename-item", "clear-content", "set-itemproperty", "export-csv", "export-clixml",
    "sc", "ac", "ni", "ri", "mi", "cpi", "rni", "del", "erase", "ren", "move", "copy", "xcopy",
    "robocopy", "rd", "md",
}
INTERPRETERS = {
    "python", "py", "node", "deno", "bun", "ruby", "perl", "php", "pwsh", "powershell",
    "bash", "sh", "zsh", "cmd", "jq", "awk", "gawk", "sed", "osascript", "lua", "tclsh",
}
GIT_WRITE_SUBS = {"checkout", "restore", "rm", "mv", "reset", "stash", "apply", "am", "clean",
                  "update-index", "update-ref", "config", "switch", "worktree", "filter-branch",
                  "replace", "read-tree", "checkout-index"}
_WRAPPERS = {"env", "sudo", "command", "exec", "nohup", "time", "xargs", "nice", "timeout", "."}


def command_word(argv: list[str]) -> tuple[str, list[str]]:
    """Base command name (lower, no .exe, python3.12 -> python) after env
    assignments and wrappers such as env/sudo/xargs."""
    i = 0
    while i < len(argv):
        a = argv[i]
        if re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", a):
            i += 1
            continue
        base = norm(a).rsplit("/", 1)[-1].lower()
        if base.endswith(".exe"):
            base = base[:-4]
        if base in _WRAPPERS:
            i += 1
            while i < len(argv) and (argv[i].startswith("-") or argv[i].isdigit()):
                i += 1
            continue
        if re.match(r"^python[0-9.]*$", base):
            base = "python"
        return base, argv[i + 1:]
    return "", []


_CODE_WRITE = re.compile(
    r"writealltext|writeallbytes|appendalltext|\[(?:system\.)?io\.file\]|open\s*\(|\.write|"
    r"writefile|appendfile|unlink|rmtree|os\.remove|os\.rename|shutil|fs\.|copyfile|rename\(",
    re.IGNORECASE,
)


def is_write_segment(seg: Segment) -> bool:
    """Does this segment write files (verb, redirect, in-place flag, inline code)?"""
    if any(not is_null_target(t) for t in seg.redirects):
        return True
    cmd, args = command_word(seg.argv)
    if cmd in WRITE_COMMANDS:
        return True
    if cmd in ("sed", "perl") and any(re.match(r"^-[a-z]*i", a) or a == "--in-place" for a in args):
        return True
    if cmd == "git":
        sub = next((a for a in args if not a.startswith("-")), "")
        if sub.lower() in GIT_WRITE_SUBS:
            return True
    if cmd in INTERPRETERS and _CODE_WRITE.search(seg.raw):
        return True
    return False
