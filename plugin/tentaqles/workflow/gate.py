"""Evidence-based checkpoint gate for /tentaqles:build.

A ported and fixed version of the checkpoint-gate idea (spec -> checkpoint plan ->
tests -> implement -> isolated review -> fix). The model edits the plan JSON; the
gate decides whether an edit is allowed by looking at evidence this module
recorded itself, never at what the model says.

Layout in a project (the build runs inside a dedicated git worktree):

    docs/checkpoints/<slug>.json          plan, edited by the model (Edit/Write only)
    .claude/build/<slug>/approved.json    hook-owned: verify commands frozen at approval,
                                          locked test files and their hashes
    .claude/build/<slug>/evidence.jsonl   hook-owned: one line per `verify` run
    .claude/build/<slug>/session.json     hook-owned: owning session, stage, stop counter
    .claude/build/.hook-seen              heartbeat, proves the hook is installed

Gate rule: a checkpoint's behavior gate (and its final "passed" status) can be set
only when the latest evidence for that checkpoint has every frozen verify command
at exit 0 *for the current tree hash*. Any code change after the run changes the
hash, so the gate resets until `verify` runs again.

The tree hash is the git tree id of the working tree (tracked + untracked, minus
ignored files and the build's own bookkeeping), computed in a throwaway index, so
it is the same before and after a commit of the same content.
"""

from __future__ import annotations

import html
import json
import os
import re
import shutil
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path

from tentaqles.workflow._common import (
    WRITE_PLAIN,
    WRITE_STRICT,
    append_jsonl,
    file_sha256,
    find_bash,
    find_git_root,
    git,
    norm,
    read_json,
    read_text,
    redact,
    rel_to,
    run,
    segments,
    tail,
    write_json,
)

GATES = ("behavior", "review")
OK_STATES = ("passed", "n/a")
STAGES = ("tests", "implement", "behavior", "review", "fix")
# Stages after which the tests of the checkpoint must be locked.
LOCKED_STAGES = ("implement", "behavior", "review", "fix")
MARKER = re.compile(r"^\s*\[checkpoint\s+([\w.-]+)#(\d+)\s+(\w+)\]")
PLAN_DIR = "docs/checkpoints"
STATE_DIR = ".claude/build"
# Bookkeeping that must not count as a code change.
HASH_EXCLUDES = (PLAN_DIR, "docs/reviews", "docs/test-plans", STATE_DIR)
MAX_STOP_BLOCKS = 1  # block a stop once, then let it through
RULE_ID_PREFIX = "tentaqles-build/"
SETTINGS_TAG = "build-gate.py hook"
HEARTBEAT = ".hook-seen"


class Block(Exception):
    """Refuse the tool call (exit 2, message on stderr)."""


class Ask(Exception):
    """Ask the human (PreToolUse permissionDecision: ask)."""


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


# ---------------------------------------------------------------------------
# State access
# ---------------------------------------------------------------------------


def state_dir(root: Path, slug: str) -> Path:
    return root / STATE_DIR / slug


def plan_path(root: Path, slug: str) -> Path:
    return root / PLAN_DIR / f"{slug}.json"


def load_plan(root: Path, slug: str):
    data = read_json(plan_path(root, slug))
    if not isinstance(data, dict) or not isinstance(data.get("checkpoints"), list):
        return None
    return data


def load_approved(root: Path, slug: str) -> dict | None:
    data = read_json(state_dir(root, slug) / "approved.json")
    return data if isinstance(data, dict) else None


def load_session(root: Path, slug: str) -> dict:
    data = read_json(state_dir(root, slug) / "session.json", {})
    return data if isinstance(data, dict) else {}


def save_session(root: Path, slug: str, data: dict) -> None:
    write_json(state_dir(root, slug) / "session.json", data)


def evidence(root: Path, slug: str) -> list[dict]:
    rows = []
    for line in read_text(state_dir(root, slug) / "evidence.jsonl").splitlines():
        try:
            row = json.loads(line)
        except ValueError:
            continue
        if isinstance(row, dict):
            rows.append(row)
    return rows


def latest_evidence(root: Path, slug: str, cid: int) -> dict | None:
    rows = [r for r in evidence(root, slug) if r.get("checkpoint") == cid]
    return rows[-1] if rows else None


def slugs(root: Path) -> list[str]:
    base = root / STATE_DIR
    if not base.is_dir():
        return []
    return sorted(p.name for p in base.iterdir() if p.is_dir())


def gate(cp: dict, name: str) -> str:
    return ((cp.get("gates") or {}).get(name) or {}).get("status", "pending")


def unpassed_before(cps: list, cid) -> list:
    return [c.get("id") for c in cps
            if isinstance(c.get("id"), int) and isinstance(cid, int)
            and c["id"] < cid and c.get("status") != "passed"]


def active_checkpoint(plan: dict) -> dict | None:
    for cp in plan.get("checkpoints", []):
        if cp.get("status") != "passed":
            return cp
    return None


# ---------------------------------------------------------------------------
# Tree hash
# ---------------------------------------------------------------------------


def tree_hash(root: Path) -> str | None:
    """Content id of the working tree, independent of commits.

    Copies the real index (keeps git's stat cache, so this is fast on big repos)
    into a temp file, stages everything into that copy and writes a tree.
    The real index is never touched.
    """
    code, idx = git(root, "rev-parse", "--git-path", "index")
    if code != 0:
        return None
    idx_path = Path(idx.strip())
    if not idx_path.is_absolute():
        idx_path = root / idx_path
    with tempfile.TemporaryDirectory() as td:
        tmp_idx = os.path.join(td, "index")
        if idx_path.exists():
            shutil.copyfile(idx_path, tmp_idx)
        env = {**os.environ, "GIT_INDEX_FILE": tmp_idx}
        excludes = [f":(exclude){p}" for p in HASH_EXCLUDES]
        code, out = git(root, "add", "--all", "--", ".", *excludes, env=env, timeout=60)
        if code != 0:
            return None
        # Drop anything under the excluded paths that the real index already tracked.
        git(root, "rm", "-r", "-q", "--cached", "--ignore-unmatch", "--", *HASH_EXCLUDES, env=env)
        code, out = git(root, "write-tree", env=env)
        return out.strip() if code == 0 else None


class TreeHash:
    """Lazily computed once per hook call."""

    def __init__(self, root: Path):
        self.root = root
        self._value = None
        self._done = False

    def get(self):
        if not self._done:
            self._value = tree_hash(self.root)
            self._done = True
        return self._value


def evidence_fresh(root: Path, slug: str, cid: int, th: TreeHash) -> tuple[bool, str]:
    ev = latest_evidence(root, slug, cid)
    if ev is None:
        return False, f"no verify evidence for #{cid}"
    if not ev.get("ok"):
        return False, f"the last verify run for #{cid} failed ({ev.get('summary', 'see build-gate status')})"
    current = th.get()
    if current is None:
        return False, "could not compute the tree hash (is this a git repo?)"
    if ev.get("tree") != current:
        return False, f"code changed since the last passing verify of #{cid} (gate reset)"
    return True, ""


# ---------------------------------------------------------------------------
# Plan rules
# ---------------------------------------------------------------------------


def _snap(cp: dict) -> tuple:
    return (cp.get("status"), tuple(gate(cp, g) for g in GATES))


def _rank(status: str) -> int:
    return {"pending": 0, "failed": 0, "n/a": 1, "passed": 2}.get(status, 0)


def _is_downgrade(old: tuple, new: tuple) -> bool:
    """Moving gates/status back toward pending is always allowed (reopening)."""
    pairs = [(old[0] or "pending", new[0] or "pending")] + list(zip(old[1], new[1]))
    return all(_rank(n) <= _rank(o) for o, n in pairs)


def check_plan(new_text: str, old_text: str, root: Path, slug: str, th: TreeHash,
               check_evidence: bool = True) -> None:
    """Raise Block when the edited plan breaks a rule.

    check_evidence=False runs the structural rules only (used by the Stop hook,
    where earlier checkpoints' evidence is legitimately stale).
    """
    try:
        new = json.loads(new_text)["checkpoints"]
        if not isinstance(new, list):
            raise TypeError
    except (ValueError, KeyError, TypeError):
        return  # not a valid plan yet; the planner validates its own JSON
    old: dict = {}
    try:
        old = {c["id"]: c for c in json.loads(old_text)["checkpoints"]}
    except (ValueError, KeyError, TypeError):
        pass
    approved = None
    for cp in new:
        if not isinstance(cp, dict):
            continue
        cid = cp.get("id")
        g = {name: gate(cp, name) for name in GATES}
        if g["review"] == "passed" and g["behavior"] != "passed":
            raise Block(f"#{cid}: review can't pass before behavior has passed.")
        if cp.get("status") == "passed" and not all(v in OK_STATES for v in g.values()):
            raise Block(f"#{cid}: status 'passed' needs every gate passed or n/a (gates: {g}).")
        before = old.get(cid)
        old_snap = _snap(before) if before else ("pending", tuple("pending" for _ in GATES))
        new_snap = _snap(cp)
        if old_snap == new_snap or _is_downgrade(old_snap, new_snap):
            continue
        todo = unpassed_before(new, cid)
        if todo:
            raise Block(f"#{cid}: can't advance gates or status while checkpoint(s) {todo} haven't passed.")
        rising = [name for name in GATES if g[name] == "passed" and gate(before or {}, name) != "passed"]
        if cp.get("status") == "passed" and (before or {}).get("status") != "passed":
            rising.append("status")
        if not rising or not check_evidence:
            continue
        if approved is None:
            approved = load_approved(root, slug) or {}
        if not approved:
            raise Block(f"#{cid}: the plan isn't approved yet. Ask the human, then run `build-gate approve {slug}`.")
        if str(cid) not in (approved.get("verify") or {}):
            raise Block(f"#{cid} has no frozen verify commands (added after approval). Re-run `build-gate approve {slug}` so the human sees them.")
        ok, why = evidence_fresh(root, slug, cid, th)
        if not ok:
            raise Block(f"#{cid}: can't mark {', '.join(rising)} passed: {why}. Run `build-gate verify {slug} {cid}` and only then update the plan.")


# ---------------------------------------------------------------------------
# Hook: PreToolUse
# ---------------------------------------------------------------------------


def _locked_paths(root: Path) -> dict[str, str]:
    """rel path -> slug, across every build in this root."""
    out = {}
    for slug in slugs(root):
        appr = load_approved(root, slug) or {}
        for rel in (appr.get("locked_tests") or {}):
            out[norm(rel).lstrip("/")] = slug
    return out


def _build_active(root: Path) -> bool:
    for slug in slugs(root):
        if load_approved(root, slug) and not load_session(root, slug).get("finished"):
            return True
    return False


PROTECTED_CONFIG = (".claude/settings.json", ".claude/settings.local.json", ".claude/tq-rules.yaml")


def check_file(tool: str, inp: dict, root: Path, th: TreeHash) -> None:
    raw = inp.get("file_path") or inp.get("notebook_path") or inp.get("path") or ""
    rel = rel_to(root, raw)
    if rel is None:
        return
    rel_l = rel.lower() if os.name == "nt" else rel
    if rel_l == STATE_DIR or rel_l.startswith(STATE_DIR + "/"):
        raise Block(f"{STATE_DIR}/ is hook-owned evidence. Use the build-gate commands; never write it directly.")
    if rel_l in PROTECTED_CONFIG and _build_active(root):
        raise Block(f"{rel} is locked while a build is active (it holds the gate's hooks and test locks). Run `build-gate finish <slug>` first.")
    locked = _locked_paths(root)
    key = rel_l if os.name != "nt" else next((k for k in locked if k.lower() == rel_l), rel_l)
    if key in locked:
        raise Block(f"{rel} is a locked test of build '{locked[key]}'. If the test itself is wrong, report it; the human can run `build-gate unlock-tests {locked[key]} {rel}`.")
    m = re.match(re.escape(PLAN_DIR) + r"/([^/]+)\.json$", rel, re.IGNORECASE)
    if not m or tool not in ("Write", "Edit", "MultiEdit"):
        return
    slug = m.group(1)
    full = root / rel
    old_text = read_text(full)
    if tool == "Write":
        new_text = inp.get("content", "")
    else:
        edits = inp.get("edits") if tool == "MultiEdit" else [inp]
        new_text = old_text
        for e in edits or []:
            o, n = e.get("old_string", ""), e.get("new_string", "")
            if not o or o not in new_text:
                return  # the edit will fail on its own
            new_text = new_text.replace(o, n) if e.get("replace_all") else new_text.replace(o, n, 1)
    check_plan(new_text, old_text, root, slug, th)


_GATE_CLI = re.compile(r"build-gate(?:\.py)?[\"']?\s+([a-z-]+)", re.IGNORECASE)
_ASK_SUBCOMMANDS = {"approve": "freezes the plan's verify commands (what 'passing' means)",
                    "unlock-tests": "unlocks test files the implementer must not edit",
                    "finish": "removes the gate's hooks and test locks"}


def check_shell(cmd: str, root: Path) -> None:
    text = norm(cmd)
    asks = []
    locked = _locked_paths(root)
    for seg in segments(text):
        m = _GATE_CLI.search(seg)
        if m:
            sub = m.group(1).lower()
            if sub in _ASK_SUBCOMMANDS:
                asks.append(f"`build-gate {sub}` {_ASK_SUBCOMMANDS[sub]}")
            continue  # the gate's own CLI writes its state itself
        low = seg.lower()
        if STATE_DIR in low and WRITE_STRICT.search(seg):
            raise Block(f"don't modify {STATE_DIR}/ from the shell; it is hook-owned evidence.")
        if re.search(re.escape(PLAN_DIR) + r"/[^\s/\"']+\.json", seg, re.IGNORECASE) and WRITE_STRICT.search(seg) \
                and not re.match(r"\s*git\s+(?:add|commit|diff|log|show|status)\b", seg):
            raise Block(f"don't modify {PLAN_DIR}/*.json from the shell. Use Edit or Write so the gate rules are checked.")
        for rel in locked:
            if rel and rel.lower() in low and WRITE_PLAIN.search(seg):
                raise Block(f"{rel} is a locked test; the shell can't change it either. Report a wrong test instead.")
        if _build_active(root) and any(p in low for p in PROTECTED_CONFIG) and WRITE_STRICT.search(seg):
            raise Block("the gate's settings and tq-rules are locked while a build is active.")
    if asks:
        raise Ask("tentaqles build: " + "; ".join(asks) + ". Approve only if you reviewed it.")


def check_agent(prompt: str, root: Path, session_id: str) -> None:
    m = MARKER.match(prompt or "")
    if not m:
        return
    slug, cid, stage = m.group(1), int(m.group(2)), m.group(3).lower()
    if stage not in STAGES:
        raise Block(f"unknown stage '{stage}'. Use one of: {', '.join(STAGES)}.")
    plan = load_plan(root, slug)
    if plan is None:
        raise Block(f"can't read {PLAN_DIR}/{slug}.json (missing or not a plan).")
    cps = plan["checkpoints"]
    cp = next((c for c in cps if c.get("id") == cid), None)
    if cp is None:
        raise Block(f"checkpoint #{cid} doesn't exist in {slug}.json.")
    todo = unpassed_before(cps, cid)
    if todo:
        raise Block(f"can't start #{cid} ({stage}): checkpoint(s) {todo} haven't passed.")
    if cp.get("status") == "passed":
        raise Block(f"#{cid} already passed. Reopen it in the plan (set the failing gate to \"failed\", status to \"pending\") first.")
    approved = load_approved(root, slug)
    if not approved:
        raise Block(f"plan '{slug}' isn't approved. Show it to the human, then run `build-gate approve {slug}`.")
    if str(cid) not in (approved.get("verify") or {}):
        raise Block(f"#{cid} was added after approval. Re-run `build-gate approve {slug}`.")
    if stage in LOCKED_STAGES and str(cid) not in (approved.get("tests_locked") or {}):
        raise Block(f"#{cid}: lock its tests before {stage}: `build-gate lock-tests {slug} {cid} <test files>` (no files: `--none`).")
    if stage == "review":
        if gate(cp, "behavior") != "passed":
            raise Block(f"#{cid}: review needs the behavior gate passed first (it is '{gate(cp, 'behavior')}').")
        ok, why = evidence_fresh(root, slug, cid, TreeHash(root))
        if not ok:
            raise Block(f"#{cid}: review needs fresh evidence: {why}. Run `build-gate verify {slug} {cid}`.")
    sess = load_session(root, slug)
    sess.update({"session_id": session_id or sess.get("session_id"), "stage": stage,
                 "checkpoint": cid, "updated": now_iso()})
    bases = sess.setdefault("bases", {})
    if stage == "implement" and str(cid) not in bases:
        code, head = git(root, "rev-parse", "HEAD")
        if code == 0:
            bases[str(cid)] = head.strip()
    save_session(root, slug, sess)


# ---------------------------------------------------------------------------
# Hook: Stop
# ---------------------------------------------------------------------------


def stop_problems(root: Path, slug: str, th: TreeHash) -> list[str]:
    plan = load_plan(root, slug)
    if plan is None:
        return []
    if (plan.get("halted") or {}).get("reason"):
        return []  # circuit breaker: stopping for a human, with the reason on record
    problems = []
    try:
        check_plan(json.dumps(plan), "", root, slug, th, check_evidence=False)
    except Block as e:
        problems.append(f"plan is inconsistent: {e}")
    cp = active_checkpoint(plan)
    sess = load_session(root, slug)
    if cp is not None:
        gates = {g: gate(cp, g) for g in GATES}
        attempts = any(((cp.get("gates") or {}).get(g) or {}).get("attempts") for g in GATES)
        started = (any(s in ("passed", "failed") for s in gates.values()) or attempts
                   or sess.get("checkpoint") == cp.get("id"))
        if started:
            open_gates = ", ".join(f"{g}={s}" for g, s in gates.items() if s not in OK_STATES) or "status"
            problems.append(f"#{cp.get('id')} {cp.get('title', '')}: not proven ({open_gates})")
    return problems


def check_stop(data: dict, root: Path) -> None:
    if os.environ.get("TENTAQLES_HEADLESS_CHILD") == "1":
        return
    if data.get("stop_hook_active"):
        return  # we already blocked this stop once; never trap the session
    session_id = data.get("session_id") or ""
    th = TreeHash(root)
    for slug in slugs(root):
        sess = load_session(root, slug)
        if sess.get("finished") or not sess.get("session_id") or sess.get("session_id") != session_id:
            continue
        problems = stop_problems(root, slug, th)
        if not problems:
            if sess.get("stop_blocks"):
                sess["stop_blocks"] = 0
                save_session(root, slug, sess)
            continue
        blocks = int(sess.get("stop_blocks") or 0)
        if blocks >= MAX_STOP_BLOCKS:
            sess["stop_blocks"] = 0
            save_session(root, slug, sess)
            continue
        sess["stop_blocks"] = blocks + 1
        save_session(root, slug, sess)
        raise Block(
            f"build '{slug}' is mid-checkpoint: " + "; ".join(problems)
            + ". Keep going. To stop for a human (circuit breaker only), set the top-level "
            + '"halted": {"reason": "...", "checkpoint": <id>} in the plan first. '
            + "This stop is blocked once; the next one goes through."
        )


# ---------------------------------------------------------------------------
# Hook entry
# ---------------------------------------------------------------------------


def _root_for(data: dict) -> Path | None:
    tool_input = data.get("tool_input") or {}
    fp = tool_input.get("file_path") or tool_input.get("notebook_path")
    if fp:
        r = find_git_root(os.path.dirname(norm(fp)) or ".")
        if r:
            return r
    for cand in (data.get("cwd"), os.environ.get("CLAUDE_PROJECT_DIR"), os.getcwd()):
        r = find_git_root(cand) if cand else None
        if r:
            return r
    return None


def _heartbeat(root: Path) -> None:
    d = root / STATE_DIR
    if d.is_dir():
        try:
            (d / HEARTBEAT).write_text(now_iso(), encoding="utf-8")
        except OSError:
            pass


def hook(data: dict) -> tuple[int, str, str]:
    """Pure-ish dispatcher: returns (exit_code, stdout, stderr)."""
    root = _root_for(data)
    if root is None or not ((root / STATE_DIR).is_dir() or (root / PLAN_DIR).is_dir()):
        return 0, "", ""
    _heartbeat(root)
    try:
        if data.get("hook_event_name") == "Stop":
            check_stop(data, root)
            return 0, "", ""
        tool = data.get("tool_name") or ""
        inp = data.get("tool_input") or {}
        if tool in ("Agent", "Task"):
            check_agent(inp.get("prompt") or "", root, data.get("session_id") or "")
        elif tool in ("Bash", "PowerShell"):
            check_shell(inp.get("command") or "", root)
        elif tool in ("Write", "Edit", "MultiEdit", "NotebookEdit"):
            check_file(tool, inp, root, TreeHash(root))
    except Block as e:
        return 2, "", f"build-gate: {e}"
    except Ask as e:
        out = {"hookSpecificOutput": {"hookEventName": "PreToolUse",
                                      "permissionDecision": "ask",
                                      "permissionDecisionReason": str(e)}}
        return 0, json.dumps(out), ""
    return 0, "", ""


# ---------------------------------------------------------------------------
# CLI commands (run by the orchestrator through Bash)
# ---------------------------------------------------------------------------


def _shell_argv(command: str) -> list[str]:
    bash = find_bash()
    if bash:
        return [bash, "-c", command]
    if os.name == "nt":
        return ["cmd", "/c", command]
    return ["/bin/sh", "-c", command]


def cmd_approve(root: Path, slug: str) -> str:
    plan = load_plan(root, slug)
    if plan is None:
        raise SystemExit(f"no plan at {PLAN_DIR}/{slug}.json")
    verify = {}
    for cp in plan["checkpoints"]:
        cmds = [c for c in (cp.get("verify") or []) if isinstance(c, str) and c.strip()]
        if not cmds:
            raise SystemExit(f"#{cp.get('id')} has no verify commands; every checkpoint needs at least one.")
        verify[str(cp.get("id"))] = cmds
    prev = load_approved(root, slug) or {}
    code, head = git(root, "rev-parse", "HEAD")
    approved = {
        "slug": slug,
        "approved_at": now_iso(),
        "base": head.strip() if code == 0 else None,
        "verify": verify,
        "locked_tests": prev.get("locked_tests", {}),
        "tests_locked": prev.get("tests_locked", {}),
    }
    write_json(state_dir(root, slug) / "approved.json", approved)
    sess = load_session(root, slug)
    sess.pop("finished", None)
    save_session(root, slug, sess)
    lines = [f"approved '{slug}': {len(verify)} checkpoint(s), verify commands frozen"]
    for cid, cmds in verify.items():
        lines += [f"  #{cid}: {c}" for c in cmds]
    return "\n".join(lines)


def cmd_lock_tests(root: Path, slug: str, cid: int, paths: list[str], none: bool,
                   action: str = "ask") -> str:
    approved = load_approved(root, slug)
    if not approved:
        raise SystemExit(f"plan '{slug}' isn't approved yet (build-gate approve {slug}).")
    if not paths and not none:
        raise SystemExit("name the test files to lock, or pass --none for a checkpoint without test files.")
    locked = approved.setdefault("locked_tests", {})
    added = []
    for p in paths:
        rel = rel_to(root, p)
        if rel is None:
            raise SystemExit(f"{p} is outside the repo.")
        h = file_sha256(root / rel)
        if h is None:
            raise SystemExit(f"{rel} doesn't exist; write the tests first.")
        if rel in locked and locked[rel] != h:
            raise SystemExit(f"{rel} is already locked with different content. The human can unlock it (build-gate unlock-tests).")
        locked[rel] = h
        added.append(rel)
    approved.setdefault("tests_locked", {})[str(cid)] = added
    write_json(state_dir(root, slug) / "approved.json", approved)
    msg = write_tq_rules(root, action)
    return f"locked {len(added)} test file(s) for #{cid}" + (f"\n{msg}" if msg else "")


def cmd_unlock_tests(root: Path, slug: str, paths: list[str]) -> str:
    approved = load_approved(root, slug)
    if not approved:
        raise SystemExit(f"plan '{slug}' isn't approved.")
    locked = approved.get("locked_tests") or {}
    gone = []
    for p in paths:
        rel = rel_to(root, p)
        if rel in locked:
            locked.pop(rel)
            gone.append(rel)
    write_json(state_dir(root, slug) / "approved.json", approved)
    write_tq_rules(root)
    return f"unlocked: {', '.join(gone) or 'nothing'}"


def tests_intact(root: Path, approved: dict) -> list[str]:
    changed = []
    for rel, h in (approved.get("locked_tests") or {}).items():
        if file_sha256(root / rel) != h:
            changed.append(rel)
    return changed


def cmd_verify(root: Path, slug: str, cid: int, timeout: float = 900) -> tuple[int, str]:
    """Run the frozen verify commands of one checkpoint and record the evidence."""
    approved = load_approved(root, slug)
    if not approved:
        return 1, f"plan '{slug}' isn't approved (build-gate approve {slug})."
    cmds = (approved.get("verify") or {}).get(str(cid))
    if not cmds:
        return 1, f"#{cid} has no frozen verify commands."
    results, out_lines, ok = [], [], True
    tampered = tests_intact(root, approved)
    if tampered:
        ok = False
        out_lines.append("locked tests changed since they were locked: " + ", ".join(tampered))
    else:
        for c in cmds:
            started = time.time()
            code, out, err, timed_out = run(_shell_argv(c), cwd=root, timeout=timeout)
            results.append({"cmd": c, "exit": code, "seconds": round(time.time() - started, 1),
                            "timed_out": timed_out})
            if code != 0:
                ok = False
                out_lines.append(f"FAIL (exit {code}{', timed out' if timed_out else ''}): {c}")
                out_lines.append(redact(tail(out + "\n" + err, 40)))
                break
            out_lines.append(f"ok: {c}")
    tree = tree_hash(root)
    summary = ("all verify commands passed" if ok else
               ("locked tests changed" if tampered else f"exit {results[-1]['exit']}: {results[-1]['cmd']}"))
    append_jsonl(state_dir(root, slug) / "evidence.jsonl", {
        "checkpoint": cid, "ts": now_iso(), "tree": tree, "ok": ok,
        "results": results, "tampered": tampered, "summary": summary,
    })
    head = f"#{cid} {'PASS' if ok else 'FAIL'} (tree {str(tree)[:12]})"
    return (0 if ok else 1), "\n".join([head, *out_lines])


def cmd_status(root: Path, slug: str) -> str:
    plan = load_plan(root, slug)
    if plan is None:
        return f"no plan at {PLAN_DIR}/{slug}.json"
    approved = load_approved(root, slug)
    th = TreeHash(root)
    lines = [f"{plan.get('feature', slug)} [{slug}] approved={'yes' if approved else 'no'}"]
    if plan.get("halted"):
        lines.append(f"HALTED: {plan['halted']}")
    for cp in plan["checkpoints"]:
        cid = cp.get("id")
        ev = latest_evidence(root, slug, cid)
        fresh = "-"
        if ev:
            ok, why = evidence_fresh(root, slug, cid, th)
            fresh = "fresh" if ok else ("stale" if ev.get("ok") else "failing")
        g = " ".join(f"{name}={gate(cp, name)}" for name in GATES)
        lines.append(f"  #{cid} {cp.get('status', 'pending'):8} {g}  evidence={fresh}  {cp.get('title', '')}")
        if approved:
            plan_v = [c for c in (cp.get("verify") or [])]
            frozen = (approved.get("verify") or {}).get(str(cid))
            if frozen is not None and plan_v != frozen:
                lines.append("      note: plan verify differs from the frozen commands; the frozen ones run")
    if approved:
        bad = tests_intact(root, approved)
        if bad:
            lines.append("  locked tests CHANGED: " + ", ".join(bad))
    return "\n".join(lines)


def cmd_review_bundle(root: Path, slug: str, cid: int, max_bytes: int = 200_000) -> str:
    """Write the diff + evidence a read-only reviewer needs, return its path."""
    sess = load_session(root, slug)
    approved = load_approved(root, slug) or {}
    base = (sess.get("bases") or {}).get(str(cid)) or approved.get("base") or "HEAD"
    code, diff = git(root, "diff", base, "--", ".", *[f":(exclude){p}" for p in HASH_EXCLUDES], timeout=60)
    code2, untracked = git(root, "ls-files", "--others", "--exclude-standard")
    parts = [f"# Review bundle: {slug} #{cid}", "", f"Base: {base}", ""]
    ev = latest_evidence(root, slug, cid)
    if ev:
        parts += ["## Latest verify evidence", "", f"ok={ev.get('ok')} tree={ev.get('tree')} at {ev.get('ts')}"]
        parts += [f"- exit {r.get('exit')}: {r.get('cmd')}" for r in ev.get("results", [])]
        parts.append("")
    new_files = [f for f in untracked.splitlines() if f and not any(f.startswith(p) for p in HASH_EXCLUDES)]
    if new_files:
        parts += ["## Untracked files (read them in full)", ""] + [f"- {f}" for f in new_files] + [""]
    body = redact(diff if code == 0 else "")
    if len(body) > max_bytes:
        body = body[:max_bytes] + "\n[diff truncated; read the files directly]\n"
    parts += ["## Diff", "", "```diff", body, "```", ""]
    out = state_dir(root, slug) / f"review-{cid}.md"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text("\n".join(parts), encoding="utf-8")
    return f"{norm(out)}\nbase={base}"


def cmd_ping(root: Path, max_age: float = 30) -> tuple[int, str]:
    hb = root / STATE_DIR / HEARTBEAT
    try:
        age = time.time() - hb.stat().st_mtime
    except OSError:
        age = None
    if age is not None and age <= max_age:
        return 0, "build-gate hooks are active"
    return 1, ("build-gate hooks are NOT active in this session. Hooks are read at session start: "
               "open /hooks to review them, or restart Claude Code in this folder, then ping again.")


def plugin_root() -> Path:
    return Path(__file__).resolve().parents[2]


def hook_command() -> str:
    runner = norm(plugin_root() / "scripts" / "tq_run.sh")
    return f'bash "{runner}" {SETTINGS_TAG}'


def cmd_setup(root: Path) -> str:
    """Install the gate hooks in .claude/settings.local.json and git-exclude the state."""
    (root / STATE_DIR).mkdir(parents=True, exist_ok=True)
    settings_path = root / ".claude" / "settings.local.json"
    settings = read_json(settings_path, {}) or {}
    hooks = settings.setdefault("hooks", {})
    entry_cmd = {"type": "command", "command": hook_command(), "timeout": 20}
    for event, matcher in (("PreToolUse", "^(Agent|Task|Bash|PowerShell|Edit|Write|MultiEdit|NotebookEdit)$"),
                           ("Stop", None)):
        groups = [g for g in hooks.get(event, [])
                  if not any(SETTINGS_TAG in (h.get("command") or "") for h in g.get("hooks", []))]
        group = {"hooks": [dict(entry_cmd)]}
        if matcher:
            group = {"matcher": matcher, **group}
        groups.append(group)
        hooks[event] = groups
    write_json(settings_path, settings)
    excluded = _git_exclude(root, [f"/{STATE_DIR}/", "/.claude/settings.local.json"])
    return (f"hooks written to {norm(settings_path)}\n"
            f"git-excluded: {', '.join(excluded) or 'already excluded'}\n"
            "Next: run `build-gate ping`. If it says inactive, open /hooks (or restart the session).")


def _git_exclude(root: Path, lines: list[str]) -> list[str]:
    code, p = git(root, "rev-parse", "--git-path", "info/exclude")
    if code != 0:
        return []
    path = Path(p.strip())
    if not path.is_absolute():
        path = root / path
    existing = read_text(path)
    added = [ln for ln in lines if ln not in existing.splitlines()]
    if added:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "a", encoding="utf-8", newline="\n") as f:
            if existing and not existing.endswith("\n"):
                f.write("\n")
            f.write("\n".join(added) + "\n")
    return added


TQ_RULES_HEADER = "# Generated by tentaqles build-gate. tq's guard reads it; project rules can only add.\n"


def _yaml_str(s: str) -> str:
    return json.dumps(s)  # a JSON string is a valid YAML double-quoted scalar


def write_tq_rules(root: Path, action: str = "ask") -> str:
    """Mirror every locked test into .claude/tq-rules.yaml (second, tq-enforced layer)."""
    if action not in ("ask", "deny"):
        action = "ask"
    locked = sorted(_locked_paths(root))
    path = root / ".claude" / "tq-rules.yaml"
    existing = read_text(path)
    rules = []
    if locked:
        alt = "|".join(re.escape(p) for p in locked)
        rules.append({
            "id": RULE_ID_PREFIX + "locked-tests",
            "action": action,
            "tool": "Edit|Write|MultiEdit|NotebookEdit",
            "path": f"(^|/)({alt})$",
            "reason": "locked test of a tentaqles build: the implementer must not edit it; report a wrong test instead",
        })
    if existing and not existing.startswith(TQ_RULES_HEADER):
        return _merge_foreign_rules(root, path, existing, rules)
    if not rules:
        if existing:
            path.unlink()
        return ""
    lines = [TQ_RULES_HEADER.rstrip("\n"), "rules:"]
    for r in rules:
        lines.append(f"  - id: {_yaml_str(r['id'])}")
        for k in ("action", "tool", "path", "reason"):
            lines.append(f"    {k}: {_yaml_str(r[k])}")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    _git_exclude(root, ["/.claude/tq-rules.yaml"])
    return f"tq rule written: {norm(path)} ({action} on edits to {len(locked)} locked file(s))"


def _merge_foreign_rules(root: Path, path: Path, existing: str, rules: list[dict]) -> str:
    try:
        import yaml  # plugin dependency, only needed when the project has its own rules file
    except ImportError:
        return f"{norm(path)} is the project's own file and PyYAML is missing; add the locked-tests rule by hand."
    try:
        data = yaml.safe_load(existing) or {}
    except Exception as e:
        return f"{norm(path)} doesn't parse ({e}); left it alone."
    if not isinstance(data, dict):
        return f"{norm(path)} isn't a mapping; left it alone."
    kept = [r for r in (data.get("rules") or [])
            if not (isinstance(r, dict) and str(r.get("id", "")).startswith(RULE_ID_PREFIX))]
    data["rules"] = kept + rules
    backup = root / STATE_DIR / "tq-rules.yaml.bak"
    backup.parent.mkdir(parents=True, exist_ok=True)
    if not backup.exists():
        backup.write_text(existing, encoding="utf-8")
    path.write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True), encoding="utf-8")
    return f"merged the locked-tests rule into the project's {norm(path)} (backup in {STATE_DIR}/)"


def cmd_finish(root: Path, slug: str) -> str:
    sess = load_session(root, slug)
    sess["finished"] = now_iso()
    save_session(root, slug, sess)
    approved = load_approved(root, slug)
    if approved:
        approved["locked_tests"] = {}
        write_json(state_dir(root, slug) / "approved.json", approved)
    msg = write_tq_rules(root) or "tq rule removed"
    if not _build_active(root):
        settings_path = root / ".claude" / "settings.local.json"
        settings = read_json(settings_path, None)
        if isinstance(settings, dict):
            hooks = settings.get("hooks") or {}
            for event in list(hooks):
                hooks[event] = [g for g in hooks[event]
                                if not any(SETTINGS_TAG in (h.get("command") or "") for h in g.get("hooks", []))]
                if not hooks[event]:
                    hooks.pop(event)
            write_json(settings_path, settings)
            msg += "\ngate hooks removed from settings.local.json"
    return f"build '{slug}' finished\n{msg}"


# ---------------------------------------------------------------------------
# Local viewer (127.0.0.1 only, serves nothing from disk but the plan render)
# ---------------------------------------------------------------------------


def render_html(root: Path, slug: str) -> str:
    status = cmd_status(root, slug)
    return ("<!doctype html><html><head><meta charset='utf-8'><meta http-equiv='refresh' content='5'>"
            f"<title>build {html.escape(slug)}</title>"
            "<style>body{font:14px/1.5 ui-monospace,monospace;margin:16px;background:#fff;color:#111}"
            "@media (prefers-color-scheme:dark){body{background:#111;color:#eee}}</style></head>"
            f"<body><pre>{html.escape(status)}</pre></body></html>")


def make_server(root: Path, slug: str, port: int = 8765):
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):  # noqa: N802
            if self.path.split("?")[0] == "/":
                body, ctype = render_html(root, slug).encode("utf-8"), "text/html; charset=utf-8"
            elif self.path.split("?")[0] == "/plan.json":
                body, ctype = json.dumps(load_plan(root, slug) or {}).encode("utf-8"), "application/json"
            else:
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *args):
            pass

    return ThreadingHTTPServer(("127.0.0.1", port), Handler)


# ---------------------------------------------------------------------------
# argparse entry
# ---------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    import argparse

    from tentaqles.workflow._common import read_stdin_json, utf8_stdio

    utf8_stdio()
    argv = sys.argv[1:] if argv is None else argv
    if argv[:1] == ["hook"]:
        try:
            code, out, err = hook(read_stdin_json())
        except Exception as e:  # never stall or crash a session: fail open, say why
            code, out, err = 0, "", f"build-gate: internal error, allowing: {e}"
        if out:
            print(out)
        if err:
            print(err, file=sys.stderr)
        return code

    p = argparse.ArgumentParser(prog="build-gate", description="Evidence-based checkpoint gate for /tentaqles:build")
    p.add_argument("--root", help="repo root (default: git root of cwd)")
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("setup", help="install the gate hooks into .claude/settings.local.json")
    sub.add_parser("ping", help="check that the hooks are active in this session")
    for name in ("approve", "status", "finish"):
        sub.add_parser(name).add_argument("slug")
    v = sub.add_parser("verify")
    v.add_argument("slug")
    v.add_argument("id", type=int)
    v.add_argument("--timeout", type=float, default=900)
    lk = sub.add_parser("lock-tests")
    lk.add_argument("slug")
    lk.add_argument("id", type=int)
    lk.add_argument("paths", nargs="*")
    lk.add_argument("--none", action="store_true")
    lk.add_argument("--action", choices=("ask", "deny"), default="ask")
    ul = sub.add_parser("unlock-tests")
    ul.add_argument("slug")
    ul.add_argument("paths", nargs="+")
    rb = sub.add_parser("review-bundle")
    rb.add_argument("slug")
    rb.add_argument("id", type=int)
    sv = sub.add_parser("serve", help="local read-only progress view on 127.0.0.1")
    sv.add_argument("slug")
    sv.add_argument("--port", type=int, default=8765)
    a = p.parse_args(argv)

    root = Path(a.root) if a.root else find_git_root(os.getcwd())
    if root is None:
        print("build-gate: not inside a git repository", file=sys.stderr)
        return 1
    if a.cmd == "setup":
        print(cmd_setup(root))
    elif a.cmd == "ping":
        code, msg = cmd_ping(root)
        print(msg)
        return code
    elif a.cmd == "approve":
        print(cmd_approve(root, a.slug))
    elif a.cmd == "status":
        print(cmd_status(root, a.slug))
    elif a.cmd == "finish":
        print(cmd_finish(root, a.slug))
    elif a.cmd == "verify":
        code, msg = cmd_verify(root, a.slug, a.id, a.timeout)
        print(msg)
        return code
    elif a.cmd == "lock-tests":
        print(cmd_lock_tests(root, a.slug, a.id, a.paths, a.none, a.action))
    elif a.cmd == "unlock-tests":
        print(cmd_unlock_tests(root, a.slug, a.paths))
    elif a.cmd == "review-bundle":
        print(cmd_review_bundle(root, a.slug, a.id))
    elif a.cmd == "serve":
        srv = make_server(root, a.slug, a.port)
        print(f"serving http://127.0.0.1:{srv.server_address[1]}/ (Ctrl+C to stop)")
        try:
            srv.serve_forever()
        except KeyboardInterrupt:
            pass
    return 0
