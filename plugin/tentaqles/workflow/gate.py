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

import fnmatch
import hmac
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
    WRITE_COMMANDS,
    ShellParseError,
    append_jsonl,
    canon_text,
    command_word,
    has_expansion,
    is_null_target,
    is_write_segment,
    lex_shell,
    resolve_word,
    file_sha256,
    find_bash,
    find_git_root,
    git,
    glob_match,
    norm,
    read_json,
    read_text,
    redact,
    rel_to,
    run,
    tail,
    write_json,
)
from tentaqles.workflow.project import load_project_config

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
REVIEWERS = ("build-critic", "spec-critic", "security-reviewer", "privacy-reviewer", "llm-security-reviewer", "breaker")
RESULTS = ("PASS", "FAIL", "BLOCKED")
WORKFLOWS = ("build", "feature", "fix", "kickoff")
DOD_WORKFLOWS = ("feature", "fix", "kickoff")


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


# Every record the gate trusts (approval, evidence, session) carries an HMAC made
# with a per-user key that lives outside the repository. A file the agent writes
# or edits fails verification and counts as absent, so the gate stays closed.


def key_dir() -> Path:
    base = os.environ.get("TQ_HOME") or str(Path.home() / ".tentaqles")
    return Path(base) / "build-gate"


def key_path() -> Path:
    return key_dir() / "hmac.key"


def _key(create: bool = False) -> bytes | None:
    p = key_path()
    try:
        return p.read_bytes()
    except OSError:
        if not create:
            return None
    import secrets

    p.parent.mkdir(parents=True, exist_ok=True)
    k = secrets.token_bytes(32)
    fd = os.open(str(p), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "wb") as f:
        f.write(k)
    return k


def _mac(data: dict, key: bytes) -> str:
    import hashlib
    import hmac

    body = json.dumps({k: v for k, v in data.items() if k != "sig"}, sort_keys=True,
                      ensure_ascii=False, separators=(",", ":"))
    return hmac.new(key, body.encode("utf-8"), hashlib.sha256).hexdigest()


def sign(data: dict) -> dict:
    out = {k: v for k, v in data.items() if k != "sig"}
    out["sig"] = _mac(out, _key(create=True))
    return out


def verified(data) -> bool:
    import hmac

    if not isinstance(data, dict) or not isinstance(data.get("sig"), str):
        return False
    key = _key()
    return bool(key) and hmac.compare_digest(data["sig"], _mac(data, key))


def load_approved(root: Path, slug: str) -> dict | None:
    data = read_json(state_dir(root, slug) / "approved.json")
    return data if verified(data) else None


def save_approved(root: Path, slug: str, data: dict) -> None:
    write_json(state_dir(root, slug) / "approved.json", sign(data))


def load_session(root: Path, slug: str) -> dict:
    data = read_json(state_dir(root, slug) / "session.json", {})
    return data if verified(data) else {}


def save_session(root: Path, slug: str, data: dict) -> None:
    write_json(state_dir(root, slug) / "session.json", sign(data))


def events_path() -> Path:
    return key_dir() / "events.jsonl"


def log_event(root: Path, kind: str, slug: str, **fields) -> None:
    """Aggregate-only process event for `tq insights`: never a file name or text."""
    try:
        plan = load_plan(root, slug) or {}
        row = {"ts": now_iso(), "kind": kind, "slug": slug, "project": Path(root).name,
               "workflow": plan.get("workflow") if plan.get("workflow") in WORKFLOWS else "build"}
        row.update({k: v for k, v in fields.items() if v is not None})
        append_jsonl(events_path(), row)  # creates the parent directory
    except Exception:
        pass  # logging never blocks a gate


def evidence(root: Path, slug: str) -> list[dict]:
    rows = []
    for line in read_text(state_dir(root, slug) / "evidence.jsonl").splitlines():
        try:
            row = json.loads(line)
        except ValueError:
            continue
        if verified(row):
            rows.append(row)
    return rows


def plan_fingerprint(cp: dict) -> str:
    """What an approval of one checkpoint covers: its id, verify commands, tests."""
    import hashlib

    body = json.dumps({"id": cp.get("id"), "verify": cp.get("verify") or [], "tests": cp.get("tests") or []},
                      sort_keys=True, ensure_ascii=False)
    return hashlib.sha256(body.encode("utf-8")).hexdigest()


def frozen_ids(approved: dict | None) -> list[str]:
    """The checkpoint ids the human approved, in plan order (the signed source of truth:
    the plan file is agent-editable, the approval is not)."""
    return [str(k) for k in ((approved or {}).get("fingerprints") or {})]


def _id_label(cid: str):
    return int(cid) if str(cid).lstrip("-").isdigit() else cid


def _ids_by_plan(cps: list) -> dict[str, dict]:
    return {str(c.get("id")): c for c in cps if isinstance(c, dict)}


def _replan_pending(root: Path, slug: str) -> bool:
    return any(p.get("kind") == "plan-approval" for p in pending_decisions(root, slug))


def checkpoint_approved(approved: dict | None, cp: dict) -> tuple[bool, str]:
    if not approved:
        return False, "the plan isn't approved"
    cid = str(cp.get("id"))
    if cid not in (approved.get("verify") or {}):
        return False, f"#{cid} was added after approval"
    if (approved.get("fingerprints") or {}).get(cid) != plan_fingerprint(cp):
        return False, f"#{cid} changed (verify commands or tests) since the human approved it"
    return True, ""


def latest_evidence(root: Path, slug: str, cid: int) -> dict | None:
    rows = [r for r in evidence(root, slug) if r.get("checkpoint") == cid]
    return rows[-1] if rows else None


def reviews_path(root: Path, slug: str) -> Path:
    return state_dir(root, slug) / "reviews.jsonl"


def reviews_for(root: Path, slug: str, cid: int, tree: str | None) -> dict[str, dict]:
    """Latest verified record per reviewer for checkpoint cid at this tree hash."""
    out: dict[str, dict] = {}
    for line in read_text(reviews_path(root, slug)).splitlines():
        try:
            rec = json.loads(line)
        except ValueError:
            continue
        if not verified(rec) or rec.get("checkpoint") != cid or rec.get("tree") != tree:
            continue
        out[rec.get("reviewer", "")] = rec
    return out


def _record_review(root: Path, slug: str, cid: int, reviewer: str, result: str, counts: dict, human: bool, note: str = "") -> str:
    if reviewer not in REVIEWERS:
        raise SystemExit(f"unknown reviewer '{reviewer}'. Use one of: {', '.join(REVIEWERS)}.")
    if result not in RESULTS or (human and result == "BLOCKED"):
        allowed = ("PASS, FAIL" if human else ", ".join(RESULTS))
        raise SystemExit(f"result must be one of {allowed} (got '{result}').")
    plan = load_plan(root, slug)
    if plan is None or not any(c.get("id") == cid for c in plan["checkpoints"]):
        raise SystemExit(f"checkpoint #{cid} doesn't exist in {PLAN_DIR}/{slug}.json.")
    tree = tree_hash(root)
    rec = {"checkpoint": cid, "reviewer": reviewer, "result": result, "tree": tree, "ts": now_iso(), "human": human}
    for k in ("blockers", "majors", "minors", "unjudged"):
        rec[k] = int(counts.get(k) or 0)
    if note:
        rec["note"] = redact(note)[:400]
    append_jsonl(reviews_path(root, slug), sign(rec))
    log_event(root, "judge" if human else "review", slug, checkpoint=cid, reviewer=reviewer, result=result,
              blockers=rec["blockers"], majors=rec["majors"], minors=rec["minors"], unjudged=rec["unjudged"])
    if human:
        _answer_pending(root, slug, "judge", result, "terminal", cid, reviewer)
    return f"#{cid} {reviewer}: {result} recorded (tree {str(tree)[:12]}{', human' if human else ''})"


def cmd_record_review(root: Path, slug: str, cid: int, reviewer: str, result: str, counts: dict) -> str:
    return _record_review(root, slug, cid, reviewer, result, counts, human=False)


def cmd_judge(root: Path, slug: str, cid: int, reviewer: str, result: str, note: str) -> str:
    return _record_review(root, slug, cid, reviewer, result, {}, human=True, note=note)


def decisions_path(root: Path, slug: str) -> Path:
    return state_dir(root, slug) / "decisions.jsonl"


def _decisions(root: Path, slug: str) -> list[dict]:
    out = []
    for line in read_text(decisions_path(root, slug)).splitlines():
        try:
            rec = json.loads(line)
        except ValueError:
            continue
        if verified(rec):
            out.append(rec)
    return out


def pending_decisions(root: Path, slug: str) -> list[dict]:
    latest: dict[str, dict] = {}
    for rec in _decisions(root, slug):
        latest[rec["id"]] = rec
    return [r for r in latest.values() if r.get("status") == "pending"]


def latest_decisions(root: Path, slug: str) -> list[dict]:
    latest: dict[str, dict] = {}
    for rec in _decisions(root, slug):
        latest[rec["id"]] = rec
    return list(latest.values())


def cmd_ask(root: Path, slug: str, kind: str, text: str, options: list[str], cid: int | None = None, reviewer: str | None = None) -> str:
    if kind not in DECISION_KINDS:
        raise SystemExit(f"unknown decision kind '{kind}'. Use one of: {', '.join(DECISION_KINDS)}.")
    if load_plan(root, slug) is None:
        raise SystemExit(f"no plan at {PLAN_DIR}/{slug}.json")
    for p in pending_decisions(root, slug):
        if p["kind"] == kind and p.get("checkpoint") == cid and p.get("reviewer") == reviewer:
            return f"decision {p['id']} (pending)"
    import secrets as _secrets
    rec = {"id": "d" + _secrets.token_hex(4), "kind": kind, "checkpoint": cid, "reviewer": reviewer,
           "text": redact(text)[:4000], "options": [o.strip() for o in options if o.strip()],
           "status": "pending", "asked_at": now_iso()}
    append_jsonl(decisions_path(root, slug), sign(rec))
    log_event(root, "ask", slug, checkpoint=cid, decision=kind)
    return f"decision {rec['id']} (pending)"


def _answer(root: Path, slug: str, rec: dict, choice: str, note: str, actor: str) -> None:
    done = {**rec, "status": "answered", "answered_at": now_iso(), "choice": choice, "note": redact(note or "")[:400], "actor": actor}
    append_jsonl(decisions_path(root, slug), sign(done))
    log_event(root, "answer", slug, checkpoint=rec.get("checkpoint"), decision=rec["kind"], actor=actor)


_ANSWERING = False  # cmd_answer's side effects must not auto-answer the record as "terminal"


def _answer_pending(root: Path, slug: str, kind: str, choice: str, actor: str, cid: int | None = None, reviewer: str | None = None) -> None:
    if _ANSWERING:
        return
    for p in pending_decisions(root, slug):
        if p["kind"] == kind and (cid is None or p.get("checkpoint") == cid) and (reviewer is None or p.get("reviewer") == reviewer):
            _answer(root, slug, p, choice, "", actor)


def _answer_pending_option(root: Path, slug: str, kind: str, option: str, choice: str, actor: str) -> None:
    if _ANSWERING:
        return
    for p in pending_decisions(root, slug):
        if p["kind"] == kind and option in p["options"] and _is_override_question(p):
            _answer(root, slug, p, choice, "", actor)


def _is_override_question(rec: dict) -> bool:
    """Only a question the orchestrator wrote as `override: ...` can waive the DoD."""
    return (rec.get("kind") == "question" and "override" in (rec.get("options") or [])
            and str(rec.get("text") or "").strip().lower().startswith("override:"))


def answer_consequence(root: Path, slug: str, decision_id: str, choice: str) -> str:
    """What answering with `choice` will do beyond recording it ('' when nothing)."""
    rec = next((p for p in pending_decisions(root, slug) if p["id"] == decision_id), None)
    if rec is None:
        return ""
    if _is_override_question(rec) and choice == "override":
        last = read_json(dod_path(root, slug))
        missing = [f"{c['id']}: {str(c.get('detail', '')).splitlines()[0][:80]}"
                   for c in (last.get("checks") or []) if c.get("ok") is False] if verified(last) else []
        listing = "; ".join(missing) if missing else "no definition-of-done run recorded yet; it runs now and whatever is missing is accepted"
        return f"this accepts the missing definition-of-done items for {slug}: {listing}"
    if rec["kind"] == "result" and choice == "approve":
        return f"this finishes checkpoint/plan {slug} (removes the gate hooks and test locks)"
    return ""


def answer_decision(root: Path, slug: str, decision_id: str, choice: str, note: str, actor: str) -> tuple[int, str]:
    global _ANSWERING
    rec = next((p for p in pending_decisions(root, slug) if p["id"] == decision_id), None)
    if rec is None:
        raise SystemExit(f"no pending decision '{decision_id}' in '{slug}'.")
    if rec["options"] and choice not in rec["options"]:
        raise SystemExit(f"choice must be one of {', '.join(rec['options'])}.")
    is_override = _is_override_question(rec) and choice == "override"
    if is_override and not (note or "").strip():
        print("build-gate: answering `override` needs a reason in --note (it is logged to docs/LEARNINGS.md).", file=sys.stderr)
        raise SystemExit(2)
    consequence = answer_consequence(root, slug, decision_id, choice)
    rc, extra = 0, []
    _ANSWERING = True
    try:
        if rec["kind"] == "plan-approval" and choice == "approve":
            cmd_approve(root, slug)
        if is_override:
            rc, dmsg = cmd_dod(root, slug, note, actor=actor)
            if rc != 0:
                extra.append(f"the override did NOT apply: {dmsg.splitlines()[0] if dmsg else 'dod failed'}")
            else:
                extra.append(dmsg)
        if rec["kind"] == "result" and choice == "approve":
            try:
                extra.append(cmd_finish(root, slug))
            except Exception as e:  # noqa: BLE001
                rc = rc or 1
                extra.append(f"finish did NOT apply: {e}")
        if rec["kind"] == "judge" and choice in ("PASS", "FAIL") and rec.get("reviewer") and rec.get("checkpoint") is not None:
            cmd_judge(root, slug, int(rec["checkpoint"]), rec["reviewer"], choice, note or f"answered by {actor}")
    finally:
        _ANSWERING = False
    rec_note = note
    if consequence and actor != "terminal":
        rec_note = f"[{consequence}] {note}".strip()  # a hub has no prompt: the record carries what the answer did
    _answer(root, slug, rec, choice, rec_note, actor)
    return rc, chr(10).join([f"decision {decision_id}: {choice} by {actor}", *extra])


def cmd_answer(root: Path, slug: str, decision_id: str, choice: str, note: str, actor: str) -> str:
    return answer_decision(root, slug, decision_id, choice, note, actor)[1]


def review_problems(root: Path, slug: str, cid: int, th: TreeHash) -> str:
    """'' when the review gate may pass at the current tree, else why not."""
    recs = reviews_for(root, slug, cid, th.get())
    bad = [f"{r} {rec['result']}" for r, rec in sorted(recs.items()) if rec.get("result") != "PASS"]
    if bad:
        return ("reviewer(s) not at PASS for the current code: " + ", ".join(bad)
                + ". Fix and re-review, or for BLOCKED have the human run `build-gate judge`.")
    if "build-critic" not in recs:
        return (f"no build-critic PASS recorded for the current code: run the critic and "
                f"`build-gate record-review {slug} {cid} --reviewer build-critic --result PASS`.")
    return ""


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


STOP_GIT_TIMEOUT = 10  # the Stop hook runs under a 20 s hook timeout (cmd_setup)


def tree_hash(root: Path, timeout: float = 60) -> str | None:
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
            shutil.copy2(idx_path, tmp_idx)
        env = {**os.environ, "GIT_INDEX_FILE": tmp_idx}
        # No `:(exclude)` pathspecs here: once `setup` git-excludes the state dir, git 2.55
        # exits 1 ("paths are ignored") for an exclude pathspec that names an ignored path.
        # The bookkeeping paths are dropped from the temp index below instead.
        # Only tracked .gitignore files and .git/info/exclude (both protected) may hide
        # files: a user-level excludesFile is switched off, and a .gitignore that
        # ignores itself is added anyway, so ignoring new code changes the hash.
        noglobal = ["-c", "core.excludesFile="]
        code, out = git(root, *noglobal, "add", "--all", "--", ".", env=env, timeout=timeout)
        if code != 0:
            return None
        code, ign = git(root, *noglobal, "ls-files", "--others", "--ignored", "--exclude-standard",
                        "--", ":(glob)**/.gitignore", env=env)
        hidden = [f for f in ign.splitlines() if f.strip()] if code == 0 else []
        if hidden:
            git(root, "add", "-f", "--", *hidden, env=env, timeout=timeout)
        # Drop the bookkeeping paths (staged above, or already tracked by the real index).
        code, _ = git(root, "rm", "-r", "-q", "--cached", "--ignore-unmatch", "--", *HASH_EXCLUDES, env=env, timeout=timeout)
        if code != 0:
            return None
        code, out = git(root, "write-tree", env=env)
        return out.strip() if code == 0 else None


class TreeHash:
    """Lazily computed once per hook call."""

    def __init__(self, root: Path, timeout: float = 60):
        self.root = root
        self.timeout = timeout
        self._value = None
        self._done = False

    def get(self):
        if not self._done:
            self._value = tree_hash(self.root, timeout=self.timeout)
            self._done = True
        return self._value


def evidence_fresh(root: Path, slug: str, cid: int, th: TreeHash) -> tuple[bool, str]:
    ev = latest_evidence(root, slug, cid)
    if ev is None:
        return False, f"no verify evidence for #{cid}"
    approved = load_approved(root, slug) or {}
    if not approved.get("approval_id") or ev.get("approval_id") != approved.get("approval_id"):
        return False, f"the evidence for #{cid} predates the current approval"
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
        doc = json.loads(new_text)
        new = doc["checkpoints"]
        if not isinstance(new, list):
            raise TypeError
    except (ValueError, KeyError, TypeError):
        return  # not a valid plan yet; the planner validates its own JSON
    # Hotfix is what the human approved, never what the plan under edit claims.
    approved = load_approved(root, slug) or {} if check_evidence else None
    hotfix = bool(approved) and approved.get("hotfix") is True
    if approved:
        present = _ids_by_plan(new)
        dropped = [f"#{f}" for f in frozen_ids(approved) if f not in present]
        if dropped and not _replan_pending(root, slug):
            raise Block(f"approved checkpoint(s) {', '.join(dropped)} can't be removed or renumbered: the human froze them. "
                        f"To split or re-plan, first record `build-gate ask {slug} --kind plan-approval --text \"<the new plan>\" "
                        "--options approve,edit,stop`; the human's approval then freezes the new plan.")
    old: dict = {}
    try:
        old = {c["id"]: c for c in json.loads(old_text)["checkpoints"]}
    except (ValueError, KeyError, TypeError):
        pass
    for cp in new:
        if not isinstance(cp, dict):
            continue
        cid = cp.get("id")
        g = {name: gate(cp, name) for name in GATES}
        if check_evidence and g["review"] == "n/a" and not hotfix:
            raise Block(f"#{cid}: review 'n/a' is only allowed when the human approved the plan as a \"hotfix\": true plan (a hotfix skips the critic, never the test).")
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
        ok, why = checkpoint_approved(approved, cp)
        if not ok:
            raise Block(f"#{cid}: {why}. The human approves in their own terminal: `build-gate approve {slug}`.")
        ok, why = evidence_fresh(root, slug, cid, th)
        if not ok:
            raise Block(f"#{cid}: can't mark {', '.join(rising)} passed: {why}. Run `build-gate verify {slug} {cid}` and only then update the plan.")
        if "review" in rising:
            why = review_problems(root, slug, cid, th)
            if why:
                raise Block(f"#{cid}: can't mark review passed: {why}")


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


def _build_exists(root: Path) -> bool:
    """Guards that protect the gate itself hold whenever any build in this root has a
    plan file or a state dir and is not finished, not only after the first approval
    (before it, the plan is exactly what the agent would like to approve itself)."""
    names = set(slugs(root))
    pd = root / PLAN_DIR
    if pd.is_dir():
        names |= {p.stem for p in pd.glob("*.json")}
    return any(not load_session(root, s).get("finished") for s in names)


GATE_INTERNALS = ("tentaqles.workflow.gate", "workflow/gate", "mint_hub_token", "cmd_answer", "cmd_approve",
                  "tq_build_token", "tq_build_actor")
PROTECTED_CONFIG = (".claude/settings.json", ".claude/settings.local.json", ".claude/tq-rules.yaml")
HUMAN_ONLY = {"approve": "freezes the plan's verify commands (what 'passing' means)",
              "unlock-tests": "unlocks test files the implementer must not edit",
              "finish": "removes the gate's hooks and test locks",
              "judge": "settles a reviewer's verdict with a human judgement",
              "answer": "settles a decision the human must take",
              "token": "mints a hub actor token that stands in for the human"}
DECISION_KINDS = ("plan-approval", "result", "judge", "question")
HUB_ACTIONS = ("approve", "unlock-tests", "finish", "judge", "answer", "dod")


def _key_dir_canon() -> str:
    k = norm(os.path.normpath(str(key_dir())))
    return k.lower() if os.name == "nt" else k


def _plugin_root_canon() -> str:
    k = norm(os.path.normpath(str(plugin_root())))
    return k.lower() if os.name == "nt" else k


def classify(root: Path, rel: str | None, absn: str, locked: dict, active: bool) -> str | None:
    """The protection class of one resolved path, or None.

    Classes: key (the HMAC key, outside the repo), git (any .git internals, also
    the shared .git of a worktree), gatecode (the gate's own code, while a build
    exists), state, config, plan, locked:<slug>. `active` means a build exists.
    """
    a = absn.lower() if os.name == "nt" else absn
    kd = _key_dir_canon()
    if a == kd or a.startswith(kd + "/"):
        return "key"
    if "/.git/" in a + "/" and not a.endswith("/.gitignore"):
        return "git"
    if active:
        pr = _plugin_root_canon()
        if "/tentaqles/workflow/" in a.lower() or a == pr or a.startswith(pr + "/"):
            return "gatecode"
    if rel is None:
        return None
    r = rel.lower() if os.name == "nt" else rel
    if r in (".claude", STATE_DIR) or r.startswith(STATE_DIR + "/"):
        return "state"
    if r in PROTECTED_CONFIG and active:
        return "config"
    if re.match(re.escape(PLAN_DIR) + r"/[^/]+\.json$", r, re.IGNORECASE):
        return "plan"
    for lk, slug in locked.items():
        if (lk.lower() if os.name == "nt" else lk) == r:
            return f"locked:{slug}"
    return None


_MESSAGES = {
    "key": "the build gate's signing key is off limits to the agent",
    "git": "git internals (.git/) can't be written directly; use git commands",
    "state": f"{STATE_DIR}/ is hook-owned evidence; only the build-gate commands write it",
    "gatecode": "the gate's own code is off limits to the agent while a build exists",
    "config": "the gate's settings and tq rules are locked while a build is active",
    "plan": f"{PLAN_DIR}/*.json changes only through Edit/Write, so the gate rules run",
}


_GATEMOD_MSG = ("the gate's own code, key and hub credentials are human-only: only the human runs the gate "
                "steps (approve, answer, judge, token), through `build-gate` in their own terminal. "
                "Use the `build-gate` CLI for anything else.")


def _msg(cls: str, rel: str) -> str:
    if cls.startswith("locked:"):
        slug = cls.split(":", 1)[1]
        return (f"{rel} is a locked test of build '{slug}'. If the test itself is wrong, report it; "
                f"the human can run `build-gate unlock-tests {slug} {rel}` in their own terminal.")
    return _MESSAGES[cls]


MAX_SCAN_BYTES = 2_000_000


def _content_after(tool: str, inp: dict, root: Path) -> str:
    """The text a file tool writes: Write content, notebook cell source, or for
    Edit/MultiEdit the target file with the edits applied (so a name split across
    edits is seen whole). Only the target file is read, and only up to MAX_SCAN_BYTES."""
    if tool == "Write":
        return str(inp.get("content") or "")
    if tool == "NotebookEdit":
        return str(inp.get("new_source") or "")
    edits = (inp.get("edits") if tool == "MultiEdit" else [inp]) or []
    news = "\n".join(str(e.get("new_string") or "") for e in edits if isinstance(e, dict))
    raw = str(inp.get("file_path") or "")
    try:
        target = Path(raw) if os.path.isabs(raw) else root / raw
        if not raw or target.stat().st_size > MAX_SCAN_BYTES:
            return news
        text = read_text(target)
    except OSError:
        return news
    for e in edits:
        if not isinstance(e, dict):
            continue
        o, n = str(e.get("old_string") or ""), str(e.get("new_string") or "")
        if o and o in text:
            text = text.replace(o, n) if e.get("replace_all") else text.replace(o, n, 1)
    return text + "\n" + news


def check_content(tool: str, inp: dict, root: Path, guarded: bool) -> None:
    """The shell's gate-module and key checks, run on what a file tool writes: a script
    that imports the gate or reads the key would otherwise run with `python x.py`."""
    if tool not in ("Write", "Edit", "MultiEdit", "NotebookEdit"):
        return
    canon = canon_text(_content_after(tool, inp, root))
    for cls, what in _text_mentions(root, canon, {}, guarded):
        if cls == "key":
            raise Block(_msg("key", what) + " (named in the file content)")
        if cls == "gatemod":
            raise Block(_GATEMOD_MSG + " (named in the file content)")


def check_file(tool: str, inp: dict, root: Path, th: TreeHash) -> None:
    raw = inp.get("file_path") or inp.get("notebook_path") or inp.get("path") or ""
    if not raw:
        return
    rel, absn = resolve_word(root, raw)
    guarded = _build_exists(root)
    cls = classify(root, rel, absn, _locked_paths(root), guarded)
    if tool in ("Read", "Grep", "Glob"):
        if cls == "key":
            raise Block(_msg("key", rel or absn))
        return
    if cls and cls != "plan":
        raise Block(_msg(cls, rel or absn))
    check_content(tool, inp, root, guarded)
    if rel is None:
        return
    m = re.match(re.escape(PLAN_DIR) + r"/([^/]+)\.json$", rel, re.IGNORECASE)
    if not m or tool not in ("Write", "Edit", "MultiEdit"):
        if m:
            raise Block(_msg("plan", rel))
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


def _gate_invocation(seg) -> tuple[str | None, list[str]]:
    """(subcommand, its arguments) when this segment runs the gate CLI, else (None, [])."""
    for i, w in enumerate(seg.argv):
        if norm(w).rsplit("/", 1)[-1].lower() == "build-gate.py":
            rest = seg.argv[i + 1:]
            while rest and rest[0].startswith("--root"):
                rest = rest[2:] if rest[0] == "--root" else rest[1:]
            return ((rest[0].lower() if rest else "") or "help"), rest
    return None, []


def _squash(canon: str) -> str:
    """Canonical text with quotes, concatenation and grouping removed, so a name split
    into pieces ('build-' + 'gate', "hm"ac, $(...)) reads whole again. Textual and bounded:
    it only makes the substring checks stricter."""
    return re.sub(r"[\s'\"+`,()\[\]{}$^]", "", canon)


def _text_mentions(root: Path, canon: str, locked: dict, active: bool) -> list[tuple[str, str]]:
    """Protected paths named anywhere in the (normalised) command text, e.g. inside
    a quoted `python -c` program, where the lexer can't see a path word.
    `active` means a build exists (see _build_exists)."""
    hits = []
    kd = _key_dir_canon().lower()
    sq = _squash(canon)
    if (kd in canon or kd in sq or "build-gate/hmac.key" in canon or "hmac.key" in sq.replace("/", "")
            or re.search(r"(?:\.tentaqles|tq_home)/?build-gate(?!\.py)|build-gate/", sq)):
        hits.append(("key", "the signing key"))
    if re.search(r"(^|[\s\"'=/])\.git/", canon):
        hits.append(("git", ".git/"))
    if STATE_DIR in canon:
        hits.append(("state", STATE_DIR))
    if active and (any(m in canon or m in sq for m in GATE_INTERNALS) or re.search(r"(?<!build-)\bgate\.py", canon)
                   or re.search(r"workflow\s+import[^\n;]*\bgate\b", canon)
                   or (re.search(r"tentaqles[./]workflow", sq) and "gate" in sq.replace("build-gate", ""))):
        hits.append(("gatemod", "the gate's own code or hub credentials"))
    if active and any(p in canon for p in PROTECTED_CONFIG):
        hits.append(("config", ".claude/settings / tq-rules"))
    if re.search(re.escape(PLAN_DIR) + r"/[^\s/\"']+\.json", canon):
        hits.append(("plan", PLAN_DIR))
    for lk, slug in locked.items():
        if lk and lk.lower() in canon:
            hits.append((f"locked:{slug}", lk))
    return hits


_GIT_GLOBAL_WITH_ARG = ("-c", "-C", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--config-env")


def _check_git(seg, hits: list[tuple[str, str]]) -> None:
    """git is a writer of the gate's state too: a signed approval staged before the tests
    are locked can be checked out again afterwards, which drops the locks. So git may not
    name the state dir (or the key) at all, and the subcommands that reach ignored files
    without naming them (clean -x/-X, stash --all, add --force of a whole tree) are refused."""
    if any(c in ("state", "key") for c, _ in hits):
        raise Block(f"git may not touch {STATE_DIR}/ (hook-owned, signed state) while a build exists; "
                    "only the build-gate commands write it, and it is never staged, restored or stashed.")
    args = seg.argv[1:]
    i = 0
    while i < len(args) and args[i].startswith("-"):
        i += 2 if args[i] in _GIT_GLOBAL_WITH_ARG else 1
    sub = args[i].lower() if i < len(args) else ""
    rest = args[i + 1:]
    if sub == "clean" and any(re.match(r"^-[A-Za-z]*[xX]", a) for a in rest if not a.startswith("--")):
        raise Block(f"`git clean -x/-X` deletes ignored files, which include {STATE_DIR}/ (the gate's signed state).")
    if sub == "stash" and any(a in ("-a", "--all", "--include-ignored") for a in rest):
        raise Block(f"`git stash --all` takes ignored files, which include {STATE_DIR}/ (the gate's signed state).")
    if sub == "add" and any(a in ("-f", "--force") or re.match(r"^-[a-z]*f", a) for a in rest if a != "--"):
        wide = [a for a in rest if not a.startswith("-") and norm(a).rstrip("/") in ("", ".", "*", ":/", ":", "./*")]
        if wide or not [a for a in rest if not a.startswith("-")]:
            raise Block(f"`git add --force` of a whole tree stages ignored files, which include {STATE_DIR}/; "
                        "name the files to force-add.")


def check_shell(cmd: str, root: Path) -> None:
    """Normalise once (lexer + canonical paths), then decide per segment."""
    locked = _locked_paths(root)
    active = _build_exists(root)
    canon_all = canon_text(cmd)
    try:
        segs = lex_shell(cmd)
    except ShellParseError as e:
        if _text_mentions(root, canon_all, locked, active):
            raise Block(f"can't parse this command ({e}) and it names a protected path; failing closed.")
        return
    for seg in segs:
        sub, sub_args = _gate_invocation(seg)
        cmd_name, _ = command_word(seg.argv)
        if cmd_name in WRITE_COMMANDS or cmd_name == "git":
            sub, sub_args = None, []  # `cp x .../build-gate.py` writes the gate script, it doesn't run it
        writes = is_write_segment(seg)
        canon = canon_text(seg.raw)
        word_hits = []
        for w in seg.argv[1:] + seg.redirects:
            if is_null_target(w):
                continue
            rel, absn = resolve_word(root, w)
            cls = classify(root, rel, absn, locked, active)
            if cls:
                word_hits.append((cls, rel or absn))
        text_hits = _text_mentions(root, canon, locked, active)
        for cls, what in word_hits + text_hits:
            if cls == "key":
                raise Block(_msg("key", what))
            if cls == "gatemod":
                raise Block(_GATEMOD_MSG)
        if sub is not None:
            if sub == "dod" and any(len(a.split("=", 1)[0]) >= 3 and "--override".startswith(a.split("=", 1)[0].lower()) for a in sub_args[1:]):
                raise Block("`build-gate dod --override` accepts missing definition-of-done items, so only the human "
                            "runs it, in their own terminal (it asks for a typed confirmation). "
                            f"Ask them to run: build-gate {' '.join(sub_args)}")
            if sub in HUMAN_ONLY:
                raise Block(f"`build-gate {sub}` {HUMAN_ONLY[sub]}, so only the human runs it, in their own "
                            f"terminal (it asks for a typed confirmation). Ask them to run: "
                            f"build-gate {' '.join(sub_args)}")
            if any(not is_null_target(t) for t in seg.redirects):
                raise Block("don't redirect the gate CLI's output into files.")
            if any(c in ("state", "git", "plan", "config") for c, _ in word_hits):
                raise Block("the gate CLI never takes its own state paths as arguments.")
            continue  # the gate writes its own (signed) state
        if cmd_name == "git":
            _check_git(seg, word_hits + text_hits)
        if not writes:
            continue
        # Fail closed on write targets the lexer can't resolve.
        targets = seg.redirects + seg.argv[1:]
        if any(has_expansion(t) for t in seg.redirects if not is_null_target(t)):
            raise Block("this command writes to a path built from a variable; use a literal path.")
        if cmd_name in WRITE_COMMANDS and any(has_expansion(t) for t in targets[len(seg.redirects):] if not t.startswith("-")) \
                and (text_hits or locked):
            raise Block("this command writes to a path built from a variable while protected paths exist; use a literal path.")
        globbed = [t for t in targets if re.search(r"[*?\[]", t)]
        for g in globbed:
            pat = canon_text(g if os.path.isabs(norm(g)) else f"{norm(root)}/{g}")
            for lk, slug in locked.items():
                if fnmatch.fnmatch(canon_text(f"{norm(root)}/{lk}"), pat):
                    raise Block(_msg(f"locked:{slug}", lk))
        hits = word_hits + text_hits
        if cmd_name == "git":
            # git itself manages .git/ and the plan file (add/commit); history-changing
            # subcommands on locked tests are still refused
            hits = [(c, w) for c, w in hits if c.startswith("locked:")]
        for cls, what in hits:
            raise Block(_msg(cls, what))


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
    ok, why = checkpoint_approved(approved, cp)
    if not ok:
        raise Block(f"#{cid}: {why}. Show the plan to the human; they approve in their own terminal: `build-gate approve {slug}`.")
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
    if pending_decisions(root, slug):
        return []  # waiting on a human: the orchestrator must be able to end its turn
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
    appr = load_approved(root, slug) or {}
    frozen = frozen_ids(appr)
    by_id = _ids_by_plan(plan.get("checkpoints") or [])
    missing = [f"#{f}" for f in frozen if f not in by_id]
    if missing and not sess.get("finished"):
        problems.append(f"approved checkpoint(s) {', '.join(missing)} are missing from the plan (the human froze them; "
                        "a removed checkpoint counts as not passed)")
    if (appr.get("workflow") or "build") in DOD_WORKFLOWS and not sess.get("finished"):
        # Only the checkpoints frozen at approval count; ones added to the plan later need re-approval.
        if all((by_id.get(f) or {}).get("status") == "passed" for f in frozen):
            ok, why = dod_fresh(root, slug, th)
            if not ok:
                problems.append(f"definition of done not proven ({why}): run `build-gate dod {slug}` and fix what it lists, "
                                f"or, if it cannot be fixed, ask the human to run `build-gate dod {slug} --override \"<their reason>\"` in their own terminal (you never run it)")
    return problems


def check_stop(data: dict, root: Path) -> None:
    if os.environ.get("TENTAQLES_HEADLESS_CHILD") == "1":
        return
    if data.get("stop_hook_active"):
        return  # we already blocked this stop once; never trap the session
    session_id = data.get("session_id") or ""
    th = TreeHash(root, timeout=STOP_GIT_TIMEOUT)
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
            write_json(d / HEARTBEAT, sign({"ts": time.time()}))
        except OSError:
            pass


def hook(data: dict) -> tuple[int, str, str]:
    """Pure-ish dispatcher: returns (exit_code, stdout, stderr)."""
    inp0 = data.get("tool_input") or {}
    fp = inp0.get("file_path") or inp0.get("notebook_path") or inp0.get("path")
    if fp and data.get("hook_event_name") != "Stop":
        _, absn = resolve_word(Path(data.get("cwd") or os.getcwd()), str(fp))
        kd = _key_dir_canon()
        a = absn.lower() if os.name == "nt" else absn
        if a == kd or a.startswith(kd + "/"):
            return 2, "", f"build-gate: {_MESSAGES['key']}"
        # The gate's code may live in another git repo (a plugin checkout), where the
        # root found from the file has no build: judge it by the session's own root.
        if (data.get("tool_name") or "") not in ("Read", "Grep", "Glob"):
            here = None
            for cand in (data.get("cwd"), os.environ.get("CLAUDE_PROJECT_DIR")):
                here = find_git_root(cand) if cand else None
                if here:
                    break
            if here and classify(here, None, absn, {}, True) == "gatecode" and _build_exists(here):
                return 2, "", f"build-gate: {_MESSAGES['gatecode']}"
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
    verify, fingerprints = {}, {}
    for cp in plan["checkpoints"]:
        cmds = [c for c in (cp.get("verify") or []) if isinstance(c, str) and c.strip()]
        if not cmds:
            raise SystemExit(f"#{cp.get('id')} has no verify commands; every checkpoint needs at least one.")
        verify[str(cp.get("id"))] = cmds
        fingerprints[str(cp.get("id"))] = plan_fingerprint(cp)
    prev = load_approved(root, slug) or {}
    code, head = git(root, "rev-parse", "HEAD")
    import secrets

    approved = {
        "slug": slug,
        "approved_at": now_iso(),
        "approval_id": secrets.token_hex(8),  # evidence from an older approval no longer counts
        "base": head.strip() if code == 0 else None,
        "verify": verify,
        "fingerprints": fingerprints,
        "locked_tests": prev.get("locked_tests", {}),
        "tests_locked": prev.get("tests_locked", {}),
        "workflow": plan.get("workflow") if plan.get("workflow") in WORKFLOWS else "build",
        "hotfix": plan.get("hotfix") is True,
    }
    project, pwarn = load_project_config(root)
    approved["project"] = project
    approved["project_warning"] = pwarn
    save_approved(root, slug, approved)
    sess = load_session(root, slug)
    sess.pop("finished", None)
    save_session(root, slug, sess)
    write_tq_rules(root)
    _answer_pending(root, slug, "plan-approval", "approve", "terminal")
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
    plan = load_plan(root, slug) or {"checkpoints": []}
    cp = next((c for c in plan["checkpoints"] if c.get("id") == cid), None)
    if cp is None:
        raise SystemExit(f"#{cid} isn't in the plan.")
    ok, why = checkpoint_approved(approved, cp)
    if not ok:
        raise SystemExit(f"#{cid}: {why}.")
    # The approved plan decides which files must be locked, not the caller.
    required = {norm(t.get("file", "")).lstrip("./") for t in (cp.get("tests") or [])
                if isinstance(t, dict) and t.get("file")}
    given = {rel_to(root, p) for p in paths}
    missing = sorted(r for r in required if r not in given)
    if missing:
        raise SystemExit(f"#{cid}: the approved plan lists test files that must be locked too: {', '.join(missing)}")
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
    save_approved(root, slug, approved)
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
    save_approved(root, slug, approved)
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
    plan = load_plan(root, slug) or {"checkpoints": []}
    cp = next((c for c in plan["checkpoints"] if c.get("id") == cid), None)
    ok_appr, why = checkpoint_approved(approved, cp or {"id": cid})
    if not ok_appr:
        return 1, f"#{cid}: {why}; the human re-approves in their own terminal."
    results, out_lines, ok = [], [], True
    before = tree_hash(root)
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
    if ok and (tree is None or tree != before or tests_intact(root, approved)):
        # code under test rewrote files (or the locked tests) while it ran
        ok, summary = False, "files changed while the verify commands ran"
        out_lines.append("FAIL: " + summary)
    append_jsonl(state_dir(root, slug) / "evidence.jsonl", sign({
        "checkpoint": cid, "ts": now_iso(), "tree": tree, "ok": ok,
        "approval_id": approved.get("approval_id"),
        "results": results, "tampered": tampered, "summary": summary,
    }))
    log_event(root, "verify", slug, checkpoint=cid, ok=ok)
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


def cmd_state(root: Path, slug: str) -> dict:
    plan = load_plan(root, slug)
    if plan is None:
        raise SystemExit(f"no plan at {PLAN_DIR}/{slug}.json")
    approved = load_approved(root, slug)
    sess = load_session(root, slug)
    th = TreeHash(root)
    cps = []
    for cp in plan.get("checkpoints") or []:
        cid = cp.get("id")
        ev = latest_evidence(root, slug, cid)
        fresh = "-"
        if ev:
            ok, _ = evidence_fresh(root, slug, cid, th)
            fresh = "fresh" if ok else ("stale" if ev.get("ok") else "failing")
        cps.append({"id": cid, "title": cp.get("title", ""), "status": cp.get("status", "pending"),
                    "gates": {g: gate(cp, g) for g in GATES}, "evidence": fresh,
                    "reviews": {r: rec["result"] for r, rec in reviews_for(root, slug, cid, th.get()).items()}})
    present = {str(c["id"]) for c in cps}
    for i, fid in enumerate(frozen_ids(approved)):  # approved but removed from the plan: not passed
        if fid not in present:
            cps.insert(min(i, len(cps)), {"id": _id_label(fid), "title": "", "status": "missing",
                                          "gates": {g: "pending" for g in GATES}, "evidence": "-", "reviews": {}})
    dod = read_json(dod_path(root, slug))
    dod_out = {"ok": bool(dod.get("ok")), "tree": dod.get("tree"), "override": dod.get("override")} if verified(dod) else None
    signed = approved if approved is not None else plan  # the hub sees what the human approved
    return {"slug": slug, "feature": plan.get("feature", slug),
            "workflow": signed.get("workflow") if signed.get("workflow") in WORKFLOWS else "build",
            "hotfix": signed.get("hotfix") is True, "task": plan.get("task"), "approved": approved is not None,
            "halted": plan.get("halted"), "finished": bool(sess.get("finished")), "checkpoints": cps, "dod": dod_out,
            "dod_fresh": dod_fresh(root, slug, th)[0],
            "pending_decisions": [{"id": p["id"], "kind": p["kind"], "checkpoint": p.get("checkpoint"),
                                   "reviewer": p.get("reviewer"), "text": p["text"], "options": p["options"], "asked_at": p["asked_at"]}
                                  for p in pending_decisions(root, slug)],
            "decisions": [{"id": d["id"], "kind": d["kind"], "checkpoint": d.get("checkpoint"), "reviewer": d.get("reviewer"),
                           "status": d.get("status"), "options": d.get("options"), "choice": d.get("choice"),
                           "note": d.get("note"), "actor": d.get("actor"), "asked_at": d.get("asked_at"),
                           "answered_at": d.get("answered_at")} for d in latest_decisions(root, slug)],
            "events_path": norm(events_path())}


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


MIGRATION_DIR_RE = re.compile(r"(^|/)(migrations|migrate|alembic/versions|supabase/migrations|prisma/migrations|db/migrate|flyway|sql/migrations)/")


def changed_files(root: Path, base: str | None) -> list[str]:
    files: set[str] = set()
    if base:
        code, out = git(root, "-c", "core.quotePath=false", "diff", "--name-only", "--no-renames", base, "--", ".")
        if code == 0:
            files.update(out.splitlines())
    code, out = git(root, "-c", "core.quotePath=false", "diff", "--name-only", "--no-renames", "HEAD", "--", ".")
    if code == 0:
        files.update(out.splitlines())
    code, out = git(root, "-c", "core.quotePath=false", "ls-files", "--others", "--exclude-standard")
    if code == 0:
        files.update(out.splitlines())
    out_files = []
    for f in files:
        f = norm(f).strip()
        if f and not any(f.startswith(p + "/") or f == p for p in HASH_EXCLUDES):
            out_files.append(f)
    return sorted(out_files)


def dod_path(root: Path, slug: str) -> Path:
    return state_dir(root, slug) / "dod.json"


def _next_learning_id(text: str, prefix: str) -> int:
    ids = [int(m) for m in re.findall(rf"^- `?{prefix}(\d+) ·", text, flags=re.MULTILINE)]
    return (max(ids) + 1) if ids else 1


def _append_learning(root: Path, line: str) -> None:
    path = root / "docs" / "LEARNINGS.md"
    text = read_text(path)
    if not text.strip():
        text = "# Learnings\n\n## Rules\n\n## Log\n"
    if "## Log" not in text:
        text = text.rstrip("\n") + "\n\n## Log\n"
    text = text.rstrip("\n") + "\n" + line + "\n"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def cmd_dod(root: Path, slug: str, override: str | None = None, timeout: float = 900, actor: str = "terminal") -> tuple[int, str]:
    """Definition of done (spec §8.1): run the frozen project commands and check the
    obligations the diff created. Records signed dod.json; the Stop hook reads it."""
    if override is not None and not override.strip():
        raise SystemExit("--override needs a reason; it is logged to docs/LEARNINGS.md and counted by tq insights.")
    plan = load_plan(root, slug)
    if plan is None:
        return 1, f"no plan at {PLAN_DIR}/{slug}.json"
    approved = load_approved(root, slug) or {}
    if not approved:
        return 1, f"plan '{slug}' isn't approved (build-gate approve {slug})."
    project = approved.get("project") or {}
    checks: list[dict] = []

    def add(cid: str, ok: bool | None, detail: str) -> None:
        checks.append({"id": cid, "ok": ok, "detail": detail})

    if project:
        add("project_config", True, "CLAUDE.md tq-project block frozen at approval")
    else:
        add("project_config", False, approved.get("project_warning") or "no ```json tq-project block in CLAUDE.md (add one with the test command)")
    cmds = project.get("commands") or {}
    learning_id = None
    if override is not None:
        # Written before the tree is hashed: the learning line is part of the tree the record signs.
        reason = re.sub(r"[`\r\n]+", " ", redact(override.strip())).strip()
        learning_id = _next_learning_id(read_text(root / "docs" / "LEARNINGS.md"), "D")
        _append_learning(root, f"- `D{learning_id} · {now_iso()[:10]} · {slug} · dod-override · {reason}`")
    before = tree_hash(root)
    for name in ("test", "lint", "typecheck"):
        c = cmds.get(name)
        cid = "tests" if name == "test" else name
        if not c:
            add(cid, None if name != "test" else False, "skip: not configured" if name != "test" else "no test command configured")
            continue
        code, out, err, timed_out = run(_shell_argv(c), cwd=root, timeout=timeout)
        add(cid, code == 0, f"exit {code}{' (timed out)' if timed_out else ''}: {c}" + ("" if code == 0 else "\n" + redact(tail(out + "\n" + err, 20))))
    tampered = tests_intact(root, approved)
    add("locked_tests", not tampered, "unchanged" if not tampered else f"{len(tampered)} locked test file(s) changed")
    cps = plan.get("checkpoints") or []
    by_id = _ids_by_plan(cps)
    frozen = frozen_ids(approved)
    ids = frozen + [i for i in by_id if i not in frozen]  # frozen first; later additions need re-approval
    unpassed = [_id_label(i) for i in ids if (by_id.get(i) or {}).get("status") != "passed"]
    gone = [_id_label(i) for i in frozen if i not in by_id]
    add("checkpoints", not unpassed, f"{len(ids) - len(unpassed)}/{len(ids)} passed" + (f"; open: {unpassed}" if unpassed else "")
        + (f"; approved but missing from the plan: {gone}" if gone else ""))
    open_reviews = []
    for cp in cps:
        latest: dict[str, dict] = {}
        for line in read_text(reviews_path(root, slug)).splitlines():
            try:
                rec = json.loads(line)
            except ValueError:
                continue
            if verified(rec) and rec.get("checkpoint") == cp.get("id"):
                latest[rec["reviewer"]] = rec
        open_reviews += [f"#{cp.get('id')} {r} {rec['result']}" for r, rec in latest.items() if rec.get("result") != "PASS"]
    add("reviews", not open_reviews, "no open FAIL/BLOCKED" if not open_reviews else "open: " + ", ".join(open_reviews))
    files = changed_files(root, approved.get("base"))
    arch = [f for f in files if glob_match(f, project.get("architecture_paths") or [])]
    adr_changed = any(f.startswith("docs/adr/") for f in files)
    add("adr", (not arch) or adr_changed,
        "no architecture path changed" if not arch else (f"{len(arch)} architecture file(s) changed, ADR " + ("present" if adr_changed else "missing under docs/adr/")))
    classified = [f for f in files if glob_match(f, project.get("classified_paths") or [])]
    add("privacy", (not classified) or "docs/privacy.md" in files,
        "no classified path changed" if not classified else (f"{len(classified)} classified file(s) changed, docs/privacy.md " + ("updated" if "docs/privacy.md" in files else "not updated")))
    mig = [f for f in files if MIGRATION_DIR_RE.search(f)]
    spec_rel = plan.get("spec")
    default_spec = f"docs/specs/{slug}.md"
    if not isinstance(spec_rel, str) or not spec_rel.strip():
        spec_rel = default_spec
    else:
        sp = norm(spec_rel).strip()
        if sp.startswith("/") or re.match(r"^[A-Za-z]:", sp) or ".." in sp.split("/"):
            spec_rel = default_spec
        else:
            spec_rel = sp
    has_plan = bool(re.search(r"^##+ .*migration plan", read_text(root / spec_rel), flags=re.IGNORECASE | re.MULTILINE))
    add("migration", (not mig) or has_plan,
        "no migration files changed" if not mig else (f"{len(mig)} migration file(s) changed, '## Migration plan' in {spec_rel} " + ("present" if has_plan else "missing")))
    changelog = project.get("changelog")
    if changelog and (root / changelog).exists():
        add("changelog", changelog in files, f"{changelog} " + ("changed" if changelog in files else "not changed"))
    else:
        add("changelog", None, "skip: project keeps no changelog")
    tree = tree_hash(root)
    if tree is None or before is None:
        add("stable_tree", False, "could not compute the tree hash (is this a git repo?)")
    elif tree != before:
        add("stable_tree", False, "files changed while the DoD commands ran")
    ok = all(c["ok"] is not False for c in checks)
    kept = None
    if override is None:
        # A human override stays valid for the same code and approval: re-checking must not erase it.
        prior = read_json(dod_path(root, slug))
        if (verified(prior) and prior.get("override") and tree is not None and prior.get("tree") == tree
                and prior.get("approval_id") == approved.get("approval_id")):
            kept = prior["override"]
            log_event(root, "dod-override-kept", slug, ok=True)
    effective = override.strip() if override else kept
    rec = {"slug": slug, "ok": ok or effective is not None, "tree": tree, "ts": now_iso(), "checks": checks,
           "approval_id": approved.get("approval_id"), "override": effective}
    write_json(dod_path(root, slug), sign(rec))
    lines = []
    for c in checks:
        tag = "ok  " if c["ok"] else ("skip" if c["ok"] is None else "MISSING")
        lines.append(f"{tag} {c['id']}: {c['detail']}")
    if override is not None:
        log_event(root, "dod-override", slug, ok=ok)
        _answer_pending_option(root, slug, "question", "override", "override", actor)
        head = f"DoD {slug}: OVERRIDDEN by the human (logged as D{learning_id}); {sum(1 for c in checks if c['ok'] is False)} item(s) were missing"
        return 0, "\n".join([head, *lines])
    log_event(root, "dod", slug, ok=ok)
    if kept is not None and not ok:
        head = f"DoD {slug}: human override still in force for this code; {sum(1 for c in checks if c['ok'] is False)} item(s) missing"
        return 0, chr(10).join([head, *lines])
    head = f"DoD {slug}: {'PASS' if ok else 'FAIL'} (tree {str(tree)[:12]})"
    return (0 if ok else 1), "\n".join([head, *lines])


def dod_fresh(root: Path, slug: str, th: TreeHash) -> tuple[bool, str]:
    rec = read_json(dod_path(root, slug))
    if not verified(rec):
        return False, "no definition-of-done record"
    if not rec.get("ok"):
        return False, "the last definition-of-done run had missing items"
    approved = load_approved(root, slug) or {}
    if rec.get("slug") != slug or not approved.get("approval_id") or rec.get("approval_id") != approved.get("approval_id"):
        return False, "the definition-of-done record predates the current approval"
    current = th.get()
    if current is None or rec.get("tree") is None:
        return False, "could not compute the tree hash (is this a git repo?)"
    if rec.get("tree") != current:
        return False, "code changed since the last definition-of-done run"
    return True, ""


def cmd_ping(root: Path, max_age: float = 30) -> tuple[int, str]:
    hb = read_json(root / STATE_DIR / HEARTBEAT)
    age = time.time() - float(hb["ts"]) if verified(hb) else None
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
    for event, matcher in (("PreToolUse", "^(Agent|Task|Bash|PowerShell|Edit|Write|MultiEdit|NotebookEdit|Read|Grep|Glob)$"),
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
    _key(create=True)
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


_STATE_PATHS_RX = r"\.claude[\\/]+(?:build(?:[\\/]|$)|tq-rules\.yaml|settings(?:\.local)?\.json)"
_KEY_RX = r"build-gate[\\/]+hmac\.key"
_SHELL_WRITE_RX = (r"(?:>|\btee\b|\bgit\b|\b(?:cp|mv|rm|dd|truncate|install|ln|touch|sed|perl)\b|set-content|add-content|out-file"
                   r"|new-item|remove-item|move-item|copy-item|rename-item|clear-content|\bpython[0-9.]*\b|\bpy\b"
                   r"|\bnode\b|\bpwsh\b|\bpowershell\b|writealltext|\bopen\()")


def state_rules() -> list[dict]:
    """tq guard rules protecting the gate's own state, rules file, settings and key.

    Project rules can only add, so these hold even if the gate's hook is inactive.
    """
    return [
        {"id": RULE_ID_PREFIX + "state-edit", "action": "deny",
         "tool": "Edit|Write|MultiEdit|NotebookEdit", "path": "(^|/)" + _STATE_PATHS_RX.replace("[\\\\/]+", "/"),
         "reason": "the build gate's approvals, evidence, hooks and rules are not editable by the agent"},
        {"id": RULE_ID_PREFIX + "state-shell", "action": "deny", "tool": "Bash|PowerShell",
         "command": f"(?:{_SHELL_WRITE_RX}).*{_STATE_PATHS_RX}|{_STATE_PATHS_RX}.*(?:{_SHELL_WRITE_RX})",
         "reason": "the build gate's approvals, evidence, hooks and rules are not writable from the shell"},
        {"id": RULE_ID_PREFIX + "key", "action": "deny", "tool": ".*", "path": _KEY_RX.replace("[\\\\/]+", "/"),
         "reason": "the build gate's signing key is off limits"},
        {"id": RULE_ID_PREFIX + "key-shell", "action": "deny", "tool": "Bash|PowerShell", "command": _KEY_RX,
         "reason": "the build gate's signing key is off limits"},
    ]


def write_tq_rules(root: Path, action: str = "ask") -> str:
    """Mirror every locked test into .claude/tq-rules.yaml (second, tq-enforced layer)."""
    if action not in ("ask", "deny"):
        action = "ask"
    locked = sorted(_locked_paths(root))
    path = root / ".claude" / "tq-rules.yaml"
    existing = read_text(path)
    rules = []
    if _build_active(root) or locked:
        rules += state_rules()
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
        for k in ("action", "tool", "path", "command", "reason"):
            if k in r:
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
        save_approved(root, slug, approved)
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
    log_event(root, "finish", slug)
    _answer_pending(root, slug, "result", "approve", "terminal")
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


def _hub_mac(key: bytes, action: str, slug: str, actor: str) -> str:
    msg = json.dumps(["hub", action, slug, actor], ensure_ascii=True, separators=(",", ":"))  # unambiguous fields
    return hmac.new(key, msg.encode("ascii"), "sha256").hexdigest()


def mint_hub_token(action: str, slug: str, actor: str) -> str:
    return _hub_mac(_key(create=True), action, slug, actor)


def hub_actor_ok(action: str, slug: str) -> str | None:
    actor = (os.environ.get("TQ_BUILD_ACTOR") or "").strip()
    tok = (os.environ.get("TQ_BUILD_TOKEN") or "").strip()
    key = _key()
    if not actor or not tok or key is None:
        return None
    want = _hub_mac(key, action, slug, actor)
    return actor if hmac.compare_digest(want.encode("ascii"), tok.encode("utf-8", "replace")) else None


def human_confirm(action: str, stdin=None, stdout=None) -> bool:
    """Approvals are a human step: an interactive terminal plus a one-time code.

    The agent's Bash tool has no TTY, and its hook refuses these subcommands
    anyway; the code stops a piped or scripted "yes".
    """
    import secrets

    stdin = stdin or sys.stdin
    stdout = stdout or sys.stdout
    if not (stdin.isatty() and stdout.isatty()):
        print("build-gate: this step needs a human at an interactive terminal (PowerShell, Windows Terminal, "
              "macOS/Linux terminal; in Git Bash's mintty prefix the command with `winpty`). Refusing.",
              file=sys.stderr)
        return False
    code = secrets.token_hex(2)
    stdout.write(f"\nAbout to {action}.\nType {code} to confirm: ")
    stdout.flush()
    answer = (stdin.readline() or "").strip()
    if answer != code:
        print("build-gate: not confirmed.", file=sys.stderr)
        return False
    return True


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

    p = argparse.ArgumentParser(prog="build-gate", description="Evidence-based checkpoint gate for /tentaqles:build", allow_abbrev=False)
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
    rr = sub.add_parser("record-review", help="record one reviewer's verdict for a checkpoint (tied to the tree hash)")
    rr.add_argument("slug")
    rr.add_argument("id", type=int)
    rr.add_argument("--reviewer", required=True, choices=REVIEWERS)
    rr.add_argument("--result", required=True, choices=RESULTS)
    for k in ("blockers", "majors", "minors", "unjudged"):
        rr.add_argument(f"--{k}", type=int, default=0)
    jd = sub.add_parser("judge", help="human only: settle a BLOCKED (or disputed) reviewer verdict")
    jd.add_argument("slug")
    jd.add_argument("id", type=int)
    jd.add_argument("--reviewer", required=True, choices=REVIEWERS)
    jd.add_argument("--result", required=True, choices=("PASS", "FAIL"))
    jd.add_argument("--note", required=True)
    dd = sub.add_parser("dod", allow_abbrev=False, help="definition of done: run the frozen project commands and check ADR/privacy/migration/changelog obligations")
    dd.add_argument("slug")
    dd.add_argument("--override", default=None, help="the human's reason for accepting missing items (logged)")
    dd.add_argument("--timeout", type=float, default=900)
    ak = sub.add_parser("ask", help="record a decision the human must take (the orchestrator then ends its turn)")
    ak.add_argument("slug")
    ak.add_argument("--kind", required=True, choices=DECISION_KINDS)
    ak.add_argument("--text", required=True)
    ak.add_argument("--options", default="", help="comma-separated")
    ak.add_argument("--checkpoint", type=int, default=None)
    ak.add_argument("--reviewer", default=None, choices=(None, *REVIEWERS))
    an = sub.add_parser("answer", help="human only: answer a pending decision")
    an.add_argument("slug")
    an.add_argument("id")
    an.add_argument("--choice", required=True)
    an.add_argument("--note", default="")
    an.add_argument("--actor", default=None)
    stt = sub.add_parser("state", help="machine-readable run state for the hub")
    stt.add_argument("slug")
    stt.add_argument("--json", action="store_true")
    tk = sub.add_parser("token", help="human only: mint a hub actor token for one action")
    tk.add_argument("action", choices=HUB_ACTIONS)
    tk.add_argument("slug")
    tk.add_argument("--actor", required=True)
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
        print(cmd_status(root, a.slug))
        if not (hub_actor_ok("approve", a.slug) or human_confirm(f"approve the plan '{a.slug}' and freeze its verify commands")):
            return 2
        print(cmd_approve(root, a.slug))
    elif a.cmd == "status":
        print(cmd_status(root, a.slug))
    elif a.cmd == "finish":
        if not (hub_actor_ok("finish", a.slug) or human_confirm(f"finish '{a.slug}' and remove its hooks and test locks")):
            return 2
        print(cmd_finish(root, a.slug))
    elif a.cmd == "verify":
        code, msg = cmd_verify(root, a.slug, a.id, a.timeout)
        print(msg)
        return code
    elif a.cmd == "lock-tests":
        print(cmd_lock_tests(root, a.slug, a.id, a.paths, a.none, a.action))
    elif a.cmd == "unlock-tests":
        if not (hub_actor_ok("unlock-tests", a.slug) or human_confirm(f"unlock {', '.join(a.paths)} in '{a.slug}'")):
            return 2
        print(cmd_unlock_tests(root, a.slug, a.paths))
    elif a.cmd == "review-bundle":
        print(cmd_review_bundle(root, a.slug, a.id))
    elif a.cmd == "record-review":
        print(cmd_record_review(root, a.slug, a.id, a.reviewer, a.result,
                                {k: getattr(a, k) for k in ("blockers", "majors", "minors", "unjudged")}))
    elif a.cmd == "judge":
        if not (hub_actor_ok("judge", a.slug) or human_confirm(f"record a human judgement for {a.reviewer} on '{a.slug}' #{a.id}: {a.result}")):
            return 2
        print(cmd_judge(root, a.slug, a.id, a.reviewer, a.result, a.note))
    elif a.cmd == "dod":
        if a.override is not None and not (hub_actor_ok("dod", a.slug) or human_confirm(f"accept the missing definition-of-done items for '{a.slug}'")):
            return 2
        code, msg = cmd_dod(root, a.slug, a.override, a.timeout, actor=hub_actor_ok("dod", a.slug) or "terminal")
        print(msg)
        return code
    elif a.cmd == "ask":
        print(cmd_ask(root, a.slug, a.kind, a.text, a.options.split(","), a.checkpoint, a.reviewer))
    elif a.cmd == "answer":
        actor = hub_actor_ok("answer", a.slug)
        if actor and a.actor and a.actor != actor:
            print(f"build-gate: --actor '{a.actor}' differs from the hub token's actor '{actor}'. Refusing.", file=sys.stderr)
            return 2
        if not actor:
            conseq = answer_consequence(root, a.slug, a.id, a.choice)
            if not human_confirm(f"answer decision {a.id} in '{a.slug}' with '{a.choice}'" + (f" -- {conseq}" if conseq else "")):
                return 2
            actor = "terminal"
        code, msg = answer_decision(root, a.slug, a.id, a.choice, a.note, a.actor or actor)
        print(msg)
        return code
    elif a.cmd == "state":
        st = cmd_state(root, a.slug)
        if a.json:
            print(json.dumps(st, ensure_ascii=False, indent=2))
        else:
            print(f"{st['slug']}: {st['workflow']}, approved={st['approved']}, finished={st['finished']}, "
                  f"{len(st['checkpoints'])} checkpoint(s), {len(st['pending_decisions'])} pending decision(s)")
    elif a.cmd == "token":
        if not human_confirm(f"mint a hub token for '{a.action}' on '{a.slug}' as {a.actor}"):
            return 2
        print(mint_hub_token(a.action, a.slug, a.actor))
    elif a.cmd == "serve":
        srv = make_server(root, a.slug, a.port)
        print(f"serving http://127.0.0.1:{srv.server_address[1]}/ (Ctrl+C to stop)")
        try:
            srv.serve_forever()
        except KeyboardInterrupt:
            pass
    return 0
