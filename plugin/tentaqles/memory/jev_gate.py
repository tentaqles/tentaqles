"""Optional Jev gates on what memory keeps and what it surfaces -- shadow only.

Two places in the plugin decide what matters:

  * capture (``knowledge-capture.py``): a regex says a tool call recorded a
    decision or discovery, and the files it mentions are touched (which
    raises them in the hot-node ranking);
  * recall (``session-preamble.py``): the top semantic facts by strength and
    the newest decisions are printed at session start.

For both, this module can ask Jev (through ``tq decide ask``) the same
question as a yes/no probability and log what Jev *would* have decided next
to what the plugin did. It never changes behaviour: the gates are shadow
mode only until the logs show Jev agrees with a human often enough.

Rules (see plugin/CLAUDE.md):

  * Hooks never wait on the network. The hook only spawns a detached worker
    (``_detach.spawn_detached``) and returns; the worker makes the calls,
    each bounded by a hard timeout.
  * Only when the workspace manifest turns Jev on (``decision.backend:
    typesafe``) -- tq re-checks trust and policy itself -- and the opt-out
    ``TENTAQLES_JEV_MEMORY=0`` is not set. Never inside the plugin's own
    headless child.
  * Content goes to tq in a temp state file (tq redacts it again and frames
    it as untrusted), never on a command line. The log line written through
    ``tq decide log`` holds probabilities, ranks and counts -- never text.
  * Any failure (no tq, backend off, timeout, bad output) means "no opinion":
    nothing is logged as a decision and nothing changes.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

_OFF_VALUES = {"0", "false", "no", "off"}

# A yes-probability at or above this is "keep" / "surface".
KEEP_THRESHOLD = 0.5

# Bounds for the detached worker. tq's own request deadline is shorter than
# the subprocess timeout so tq reports the timeout instead of being killed.
TQ_REQUEST_TIMEOUT = "5s"
TQ_PROCESS_TIMEOUT = 8
TQ_LOG_TIMEOUT = 5

# State caps: Jev is accurate on short, focused state.
MAX_CAPTURE_TEXT = 6000
MAX_ITEM_TEXT = 400
MAX_RECALL_ITEMS = 25

CAPTURE_QUESTION = (
    "Does data.text record a decision, a root cause, a workaround or a discovery "
    "that would be worth remembering in a future session in this codebase?"
)

# Errors that only mean "Jev is not set up here": not worth a log line.
_QUIET_ERRORS = (
    "backend is off",
    "not in a trusted workspace",
    "no TYPESAFE_API_KEY",
)


def enabled() -> bool:
    """False when opted out or inside the plugin's own headless child."""
    if os.environ.get("TENTAQLES_JEV_MEMORY", "").strip().lower() in _OFF_VALUES:
        return False
    if os.environ.get("TENTAQLES_HEADLESS_CHILD") == "1":
        return False
    return True


def manifest_wants_jev(manifest: dict | None) -> bool:
    """Cheap pre-check so a workspace without Jev never spawns a worker."""
    if not isinstance(manifest, dict):
        return False
    decision = manifest.get("decision")
    return isinstance(decision, dict) and decision.get("backend") == "typesafe"


def should_gate(manifest: dict | None) -> bool:
    return enabled() and manifest_wants_jev(manifest)


def find_tq() -> str | None:
    """Resolve the tq binary the way scripts/tq_hook.sh does."""
    exe = ".exe" if sys.platform == "win32" else ""
    candidates: list[str] = []
    if os.environ.get("TQ_BIN"):
        candidates += [os.environ["TQ_BIN"], os.environ["TQ_BIN"] + exe]
    root = os.environ.get("CLAUDE_PLUGIN_ROOT") or str(Path(__file__).resolve().parents[2])
    candidates.append(os.path.join(root, "bin", "tq" + exe))
    found = shutil.which("tq")
    if found:
        candidates.append(found)
    home = Path.home()
    candidates += [str(home / ".tentaqles" / "bin" / ("tq" + exe)), str(home / ".local" / "bin" / ("tq" + exe))]
    if sys.platform == "win32" and os.environ.get("LOCALAPPDATA"):
        candidates.append(os.path.join(os.environ["LOCALAPPDATA"], "tentaqles", "bin", "tq.exe"))
    for c in candidates:
        if c and os.path.isfile(c) and os.access(c, os.X_OK):
            return c
    return None


def _redact(text: str) -> str:
    try:
        from tentaqles.privacy import redact_text

        return redact_text(text)[0]
    except Exception:
        return text


def _run(argv: list[str], cwd: str, timeout: float) -> subprocess.CompletedProcess | None:
    kwargs: dict = {}
    if sys.platform == "win32":
        kwargs["creationflags"] = getattr(subprocess, "CREATE_NO_WINDOW", 0)
    try:
        return subprocess.run(
            argv,
            capture_output=True,
            text=True,
            timeout=timeout,
            cwd=cwd if cwd and os.path.isdir(cwd) else None,
            stdin=subprocess.DEVNULL,
            **kwargs,
        )
    except (OSError, subprocess.SubprocessError, ValueError):
        return None


def ask_noul(
    cwd: str,
    state: dict,
    questions: dict[str, str],
    *,
    purpose: str,
    tq: str | None = None,
) -> tuple[dict[str, float] | None, str]:
    """Ask yes/no questions about state through ``tq decide ask --json``.

    Returns (probabilities, error). Probabilities is None on any failure;
    error is "" when the failure only means Jev is not configured here.
    """
    tq = tq or find_tq()
    if not tq:
        return None, ""
    fd, path = tempfile.mkstemp(prefix="tq-jev-", suffix=".json")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(state, f)
        argv = [tq, "decide", "ask", "--state-file", path, "--json",
                "--timeout", TQ_REQUEST_TIMEOUT, "--purpose", purpose]
        for qid, text in questions.items():
            argv += ["--q", f"{qid}=noul:{text}"]
        proc = _run(argv, cwd, TQ_PROCESS_TIMEOUT)
    finally:
        try:
            os.unlink(path)
        except OSError:
            pass
    if proc is None:
        return None, "tq timed out or could not start"
    if proc.returncode != 0:
        err = (proc.stderr or proc.stdout or "").strip().splitlines()
        msg = err[-1] if err else f"tq exit {proc.returncode}"
        if any(q in msg for q in _QUIET_ERRORS):
            return None, ""
        return None, msg[:120]
    try:
        answers = json.loads(proc.stdout)
    except (ValueError, TypeError):
        return None, "malformed tq output"
    out: dict[str, float] = {}
    for qid in questions:
        a = answers.get(qid) if isinstance(answers, dict) else None
        p = a.get("noul") if isinstance(a, dict) else None
        if not isinstance(p, (int, float)) or not 0 <= p <= 1:
            return None, f"no answer for {qid}"
        out[qid] = float(p)
    return out, ""


def log_judgment(
    cwd: str,
    kind: str,
    *,
    p: dict[str, float] | None = None,
    would: dict[str, str] | None = None,
    extra: dict[str, str] | None = None,
    error: str = "",
    tq: str | None = None,
) -> bool:
    """Append one content-free line via ``tq decide log``."""
    tq = tq or find_tq()
    if not tq:
        return False
    argv = [tq, "decide", "log", "--kind", kind, "--mode", "shadow"]
    for k, v in (p or {}).items():
        argv += ["--p", f"{k}={v:.4f}"]
    for k, v in (would or {}).items():
        argv += ["--would", f"{k}={v}"]
    for k, v in (extra or {}).items():
        argv += ["--extra", f"{k}={v}"]
    if error:
        argv += ["--error", error]
    proc = _run(argv, cwd, TQ_LOG_TIMEOUT)
    return proc is not None and proc.returncode == 0


# --- capture -------------------------------------------------------------


def capture_payload(cwd: str, tool: str, text: str, mentioned_paths: int) -> dict:
    return {
        "kind": "capture",
        "cwd": cwd,
        "tool": tool,
        "text": _redact(text)[:MAX_CAPTURE_TEXT],
        "paths": int(mentioned_paths),
    }


def run_capture(payload: dict, tq: str | None = None) -> dict | None:
    """Worker side of the capture gate. Returns the logged record (tests)."""
    cwd = payload.get("cwd") or ""
    tool = str(payload.get("tool") or "unknown")
    probs, err = ask_noul(cwd, {"tool": tool, "text": payload.get("text", "")},
                          {"worth": CAPTURE_QUESTION}, purpose="memory-capture", tq=tq)
    extra = {"tool": tool[:40] if tool.isidentifier() else "other",
             "paths": str(int(payload.get("paths") or 0)), "regex": "keep"}
    if probs is None:
        if err:
            log_judgment(cwd, "memory-capture", extra=extra, error=err, tq=tq)
            return {"error": err, "extra": extra}
        return None
    would = {"worth": "keep" if probs["worth"] >= KEEP_THRESHOLD else "drop"}
    log_judgment(cwd, "memory-capture", p=probs, would=would, extra=extra, tq=tq)
    return {"p": probs, "would": would, "extra": extra}


# --- recall --------------------------------------------------------------


def recall_payload(cwd: str, store, facts_shown: int = 5, decisions_shown: int = 3) -> dict | None:
    """Collect recall candidates; None when ranking could not change anything.

    Items are keyed by their current rank (f0.. for facts, d0.. for
    decisions), so the log can say how Jev would reorder them without
    naming any memory.
    """
    facts = store.get_semantic_facts(limit=15)
    decisions = store.get_recent_decisions(days=30)[:10]
    if len(facts) <= facts_shown and len(decisions) <= decisions_shown:
        return None  # everything is surfaced already: nothing to rank
    items: dict[str, str] = {}
    for i, f in enumerate(facts):
        items[f"f{i}"] = _redact(str(f.get("fact", "")))[:MAX_ITEM_TEXT]
    for i, d in enumerate(decisions):
        items[f"d{i}"] = _redact(f"{d.get('chosen', '')}: {d.get('rationale', '')}")[:MAX_ITEM_TEXT]
    last = store.get_last_session() or {}
    hot = [n.get("node_id", "") for n in store.get_active_nodes(limit=8)]
    return {
        "kind": "recall",
        "cwd": cwd,
        "recent": {"last_session": _redact(str(last.get("summary") or ""))[:1500], "hot_files": hot},
        "items": dict(list(items.items())[:MAX_RECALL_ITEMS]),
        "shown": {"f": facts_shown, "d": decisions_shown},
    }


def _recall_question(item_id: str) -> str:
    return (f"Is memory item data.items.{item_id} likely to be useful context at the start of "
            f"the next session, given the recent work in data.recent?")


def run_recall(payload: dict, tq: str | None = None) -> dict | None:
    """Worker side of the recall gate: how much would Jev's top-k differ?"""
    cwd = payload.get("cwd") or ""
    items = payload.get("items") or {}
    if not items:
        return None
    questions = {iid: _recall_question(iid) for iid in items}
    probs, err = ask_noul(cwd, {"recent": payload.get("recent", {}), "items": items},
                          questions, purpose="memory-recall", tq=tq)
    if probs is None:
        if err:
            log_judgment(cwd, "memory-recall", extra={"candidates": str(len(items))}, error=err, tq=tq)
            return {"error": err}
        return None
    shown = payload.get("shown") or {}
    extra = {"candidates": str(len(items))}
    would: dict[str, str] = {}
    for group, label in (("f", "facts"), ("d", "decisions")):
        ids = sorted((i for i in items if i.startswith(group)), key=lambda i: int(i[1:]))
        k = int(shown.get(group, 0))
        if not ids or k <= 0:
            continue
        current = set(ids[:k])
        # Jev's pick: highest probability first, current rank breaks ties.
        jev = sorted(ids, key=lambda i: (-probs[i], int(i[1:])))[:k]
        overlap = len(current & set(jev))
        extra[f"{label}_overlap"] = f"{overlap}/{min(k, len(ids))}"
        extra[f"{label}_kept"] = str(sum(1 for i in ids if probs[i] >= KEEP_THRESHOLD))
        would[label] = "same" if overlap == min(k, len(ids)) else "reorder"
    log_judgment(cwd, "memory-recall", p=probs, would=would, extra=extra, tq=tq)
    return {"p": probs, "would": would, "extra": extra}


# --- spawning ------------------------------------------------------------


WORKER = Path(__file__).resolve().parents[2] / "scripts" / "jev-memory-gate.py"


def spawn(payload: dict, spawn_detached) -> bool:
    """Hand a gate payload to a detached worker. Never raises.

    ``spawn_detached`` is ``scripts/_detach.spawn_detached``, passed in by
    the calling hook script (scripts/ is on its path, not on this package's).
    On failure the gate is simply skipped: never run inline in a hook.
    """
    try:
        spawn_detached([sys.executable, str(WORKER)], stdin_data=json.dumps(payload).encode("utf-8"))
        return True
    except Exception:
        return False
