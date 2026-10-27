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
import stat
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path

from tentaqles.workflow._common import (
    Segment,
    WRITE_COMMANDS,
    _WRAPPERS,
    ShellParseError,
    append_jsonl,
    canon_path,
    canon_text,
    command_word,
    has_expansion,
    is_null_target,
    is_write_segment,
    lex_shell,
    resolve_word,
    shell_word_variants,
    file_sha256,
    find_bash,
    find_git_root,
    find_git_roots,
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
BREAKER_DIR = ".tq-breaker"  # the breaker agent's scratch directory (git-excluded, not state)
SLUG_RE = re.compile(r"[a-z0-9][a-z0-9-]*")  # the plan slug charset (loop.py uses the same)
HASH_EXCLUDES = (PLAN_DIR, "docs/reviews", "docs/test-plans", STATE_DIR, BREAKER_DIR)
MAX_STOP_BLOCKS = 1  # block a stop once, then let it through
RULE_ID_PREFIX = "tentaqles-build/"
SETTINGS_TAG = "build-gate.py hook"
HEARTBEAT = ".hook-seen"
REVIEWERS = ("build-critic", "spec-critic", "security-reviewer", "privacy-reviewer", "llm-security-reviewer", "breaker")
RESULTS = ("PASS", "FAIL", "BLOCKED")
WORKFLOWS = ("build", "feature", "fix", "kickoff")
DOD_WORKFLOWS = ("feature", "fix", "kickoff")

# The hub's task block (spec A §13.1): TQ_HUB_TASK copied verbatim into the plan by the on-ramp.
TASK_KEYS = ("hub", "workspace", "kind", "external")
TASK_KINDS = ("project", "feature", "fix", "analysis", "hotfix", "chore")
EXTERNAL_KEYS = ("provider", "id", "url")
HUB_CARD_RE = re.compile(r"c-[a-z2-7]{12}")


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


def _sig_ok(data) -> bool:
    import hmac

    if not isinstance(data, dict) or not isinstance(data.get("sig"), str):
        return False
    key = _key()
    return bool(key) and hmac.compare_digest(data["sig"], _mac(data, key))


# A signature proves the gate wrote a file, not that it is the latest one: an older, validly
# signed approved.json copied back would drop the test locks. So every approval carries a
# generation `gen` (signed), every state transition rewrites it with gen + 1, and the latest
# gen is recorded with the key (closed to the agent's tools). An approval whose gen differs
# from the record, a record without the approval, or an approval claiming gen > 0 without a
# record, is a rolled-back state: every gate refuses until the human re-approves.
APPROVAL_KIND = "approval"
ROLLBACK_MSG = "build state was rolled back or altered; a human must re-approve (`build-gate approve {slug}`)"
LEGACY_MSG = "legacy approval; run `build-gate approve {slug}` to migrate"


def project_id(root) -> str:
    """Stable id of a project root: a hash of its canonical resolved path."""
    import hashlib

    return hashlib.sha256(canon_path(root).encode("utf-8")).hexdigest()[:16]


def _gen_file(pid: str, slug: str) -> Path:
    return key_dir() / "gen" / f"{pid}-{slug}"


def gen_record_path(root, slug: str) -> Path:
    return _gen_file(project_id(root), slug)


def _gen_record(root, slug: str) -> dict | None:
    data = read_json(gen_record_path(root, slug))
    if not _sig_ok(data) or not isinstance(data.get("gen"), int) or isinstance(data.get("gen"), bool):
        return None
    return data


def recorded_gen(root, slug: str) -> int | None:
    rec = _gen_record(root, slug)
    return rec["gen"] if rec else None


def recorded_slugs(root) -> list[str]:
    """Slugs with a generation record for this project (kept with the key, so removing the
    state dir does not hide a build)."""
    pid = project_id(root)
    d = key_dir() / "gen"
    try:
        names = [p.name for p in d.iterdir() if p.is_file()]
    except OSError:
        return []
    return sorted(n[len(pid) + 1:] for n in names if n.startswith(pid + "-") and len(n) > len(pid) + 1)


def _gen_ok(data: dict) -> bool:
    gen, pid, slug = data.get("gen"), data.get("project_id"), data.get("slug")
    if not isinstance(gen, int) or isinstance(gen, bool) or gen < 1 or not isinstance(pid, str) or not isinstance(slug, str):
        return False
    if not re.fullmatch(r"[0-9a-f]{16}", pid) or "/" in slug or "\\" in slug or slug in ("", ".", ".."):
        return False
    rec = read_json(_gen_file(pid, slug))
    return _sig_ok(rec) and rec.get("gen") == gen


def verified(data) -> bool:
    """Signed by the gate; an approval must also be the latest generation on record."""
    if not _sig_ok(data):
        return False
    if data.get("kind") == APPROVAL_KIND:
        return _gen_ok(data)
    return True


# Every other signed state file and record (session.json, dod.json, each reviews.jsonl and
# decisions.jsonl line) is bound to its build: it carries project_id, slug and the approval gen
# current when it was written, and counts only while all three match. Each generation bump
# re-signs the live records with the new gen, so a file copied back from an older generation, or
# from another slug or project, is ignored (reviews, decisions), stale (dod.json) or a rollback
# (session.json).


def current_gen(root, slug: str) -> int:
    return recorded_gen(root, slug) or 0


class _Binding:
    """(project_id, slug, gen) of one build, computed once for a batch of records."""

    __slots__ = ("pid", "slug", "gen")

    def __init__(self, root, slug: str, gen: int | None = None):
        self.pid, self.slug = project_id(root), slug
        self.gen = current_gen(root, slug) if gen is None else gen

    def ok(self, rec) -> bool:
        if not _sig_ok(rec) or rec.get("kind") == APPROVAL_KIND:
            return False
        g = rec.get("gen")
        return (rec.get("project_id") == self.pid and rec.get("slug") == self.slug
                and isinstance(g, int) and not isinstance(g, bool) and g == self.gen)

    def sign(self, data: dict) -> dict:
        return sign({**data, "project_id": self.pid, "slug": self.slug, "gen": self.gen})


def bound(root, slug: str, rec) -> bool:
    return _Binding(root, slug).ok(rec)


def sign_state(root, slug: str, data: dict) -> dict:
    return _Binding(root, slug).sign(data)


def load_approved(root: Path, slug: str) -> dict | None:
    data = read_json(state_dir(root, slug) / "approved.json")
    if not verified(data) or data.get("kind") != APPROVAL_KIND:
        return None
    if data.get("slug") != slug or data.get("project_id") != project_id(root):
        return None
    return data


def legacy_approval(root: Path, slug: str) -> dict | None:
    """An approval signed by the plugin before generations (no `kind`, no `gen`) and no record
    yet: neither approved nor rolled back until the human migrates it with `approve`."""
    if gen_record_path(root, slug).exists():
        return None
    raw = read_json(state_dir(root, slug) / "approved.json")
    if not _sig_ok(raw) or "kind" in raw or "gen" in raw or "project_id" in raw:
        return None
    if raw.get("slug") not in (None, slug):
        return None
    return raw


def _session_raw(root: Path, slug: str):
    p = state_dir(root, slug) / "session.json"
    return read_json(p, {}) if p.exists() else None


def state_problem(root: Path, slug: str) -> str | None:
    """Why the gate refuses this build until a human re-approves, or None."""
    rec = _gen_record(root, slug)
    if gen_record_path(root, slug).exists():
        if rec is None:
            return ROLLBACK_MSG.format(slug=slug)
        if rec.get("finished") and not (state_dir(root, slug) / "approved.json").exists():
            pass  # a finished build whose state was cleaned up (merge cleanup, re-clone): nothing left to protect
        elif load_approved(root, slug) is None:
            return ROLLBACK_MSG.format(slug=slug)
    else:
        if legacy_approval(root, slug) is not None:
            return LEGACY_MSG.format(slug=slug)
        raw = read_json(state_dir(root, slug) / "approved.json")
        gen = raw.get("gen") if isinstance(raw, dict) else None
        if _sig_ok(raw) and isinstance(gen, int) and gen > 0:
            return ROLLBACK_MSG.format(slug=slug)
    sess = _session_raw(root, slug)
    if sess is None:
        if rec is not None and rec.get("session") and not rec.get("finished"):
            return ROLLBACK_MSG.format(slug=slug)  # the record says a session existed: deleting it is a rollback
    else:
        if not bound(root, slug, sess):
            return ROLLBACK_MSG.format(slug=slug)
        if rec is not None and bool(sess.get("finished")) != bool(rec.get("finished")):
            return ROLLBACK_MSG.format(slug=slug)
    return None


def rolled_back(root: Path, slug: str) -> bool:
    """The build's signed state is not the latest the gate wrote, or is a legacy approval
    (see ROLLBACK_MSG, LEGACY_MSG)."""
    return state_problem(root, slug) is not None


def rollback_msg(slug: str, root: Path | None = None) -> str:
    if root is not None:
        msg = state_problem(root, slug)
        if msg:
            return msg
    return ROLLBACK_MSG.format(slug=slug)


def rolled_back_slugs(root: Path) -> list[str]:
    return [s for s in all_slugs(root) if rolled_back(root, s)]


def _restamp(root: Path, slug: str, old: _Binding, new: _Binding, legacy: bool) -> None:
    """Carry the live state of generation `old` (or, migrating, the unbound legacy records)
    over to generation `new`; anything else stays as it is and keeps failing the binding."""

    def live(rec) -> bool:
        if old.ok(rec):
            return True
        return (legacy and isinstance(rec, dict) and _sig_ok(rec) and "gen" not in rec and "project_id" not in rec
                and rec.get("kind") != APPROVAL_KIND and rec.get("slug") in (None, slug))

    d = state_dir(root, slug)
    for name in ("session.json", "dod.json"):
        p = d / name
        if p.exists():
            rec = read_json(p)
            if live(rec):
                write_json(p, new.sign(rec))
    for name in ("reviews.jsonl", "decisions.jsonl"):
        p = d / name
        if not p.exists():
            continue
        lines, changed = [], False
        for line in read_text(p).splitlines():
            try:
                rec = json.loads(line)
            except ValueError:
                lines.append(line)
                continue
            if live(rec):
                lines.append(json.dumps(new.sign(rec), ensure_ascii=False))
                changed = True
            else:
                lines.append(line)
        if changed:
            tmp = p.with_name(p.name + ".tmp")
            tmp.write_text("".join(x + "\n" for x in lines), encoding="utf-8", newline="\n")
            os.replace(tmp, p)


def save_approved(root: Path, slug: str, data: dict, finished: bool | None = None, legacy: bool = False) -> None:
    """Write the approval as the next generation and record that generation with the key.
    `finished` is kept in the record (None keeps the recorded value)."""
    rec = _gen_record(root, slug) or {}
    prev = rec.get("gen") or 0
    own = data.get("gen") if isinstance(data.get("gen"), int) and not isinstance(data.get("gen"), bool) else 0
    gen = max(prev, own) + 1
    pid = project_id(root)
    done = bool(rec.get("finished")) if finished is None else bool(finished)
    session, bases = bool(rec.get("session")), _clean_bases(rec.get("bases"))
    if rec.get("finished") and not (state_dir(root, slug) / "session.json").exists():
        session, bases = False, {}  # a new run of a slug whose finished build was cleaned up
    out = {**data, "kind": APPROVAL_KIND, "slug": slug, "project_id": pid, "gen": gen}
    write_json(state_dir(root, slug) / "approved.json", sign(out))
    write_json(_gen_file(pid, slug), sign({"gen": gen, "slug": slug, "project_id": pid, "ts": now_iso(),
                                           "finished": done, "session": session, "bases": bases,
                                           "locked_tests": out.get("locked_tests") or {},
                                           "tests_locked": out.get("tests_locked") or {}}))
    _restamp(root, slug, _Binding(root, slug, prev), _Binding(root, slug, gen), legacy)


def bump_gen(root: Path, slug: str) -> None:
    """A state transition that does not otherwise rewrite the approval still advances it."""
    approved = load_approved(root, slug)
    if approved:
        save_approved(root, slug, approved)


def load_session(root: Path, slug: str) -> dict:
    data = read_json(state_dir(root, slug) / "session.json", {})
    return data if bound(root, slug, data) else {}


def _clean_bases(raw) -> dict:
    return {str(k): v for k, v in raw.items() if isinstance(v, str) and v} if isinstance(raw, dict) else {}


def review_bases(root: Path, slug: str) -> dict:
    """The review base commit per checkpoint: the session's, overridden by what the gate recorded
    with the key (session.json may be missing, older or replaced)."""
    rec = _gen_record(root, slug) or {}
    return {**_clean_bases(load_session(root, slug).get("bases")), **_clean_bases(rec.get("bases"))}


def save_session(root: Path, slug: str, data: dict) -> None:
    rec = _gen_record(root, slug)
    if rec is not None:
        bases = {**_clean_bases(data.get("bases")), **_clean_bases(rec.get("bases"))}
        data = {**data, "bases": bases}
        write_json(_gen_file(project_id(root), slug), sign({**rec, "session": True, "bases": bases}))
    write_json(state_dir(root, slug) / "session.json", sign_state(root, slug, data))


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
    b = _Binding(root, slug)
    for line in read_text(reviews_path(root, slug)).splitlines():
        try:
            rec = json.loads(line)
        except ValueError:
            continue
        if not b.ok(rec) or rec.get("checkpoint") != cid or rec.get("tree") != tree:
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
    append_jsonl(reviews_path(root, slug), sign_state(root, slug, rec))
    log_event(root, "judge" if human else "review", slug, checkpoint=cid, reviewer=reviewer, result=result,
              blockers=rec["blockers"], majors=rec["majors"], minors=rec["minors"], unjudged=rec["unjudged"])
    if human:
        bump_gen(root, slug)
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
    b = _Binding(root, slug)
    for line in read_text(decisions_path(root, slug)).splitlines():
        try:
            rec = json.loads(line)
        except ValueError:
            continue
        if b.ok(rec):
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
    append_jsonl(decisions_path(root, slug), sign_state(root, slug, rec))
    log_event(root, "ask", slug, checkpoint=cid, decision=kind)
    return f"decision {rec['id']} (pending)"


def _answer(root: Path, slug: str, rec: dict, choice: str, note: str, actor: str) -> None:
    done = {**rec, "status": "answered", "answered_at": now_iso(), "choice": choice, "note": redact(note or "")[:400], "actor": actor}
    append_jsonl(decisions_path(root, slug), sign_state(root, slug, done))
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
                   for c in (last.get("checks") or []) if c.get("ok") is False] if bound(root, slug, last) else []
        listing = "; ".join(missing) if missing else "no definition-of-done run recorded yet; it runs now and whatever is missing is accepted"
        return f"this accepts the missing definition-of-done items for {slug}: {listing}"
    if rec["kind"] == "result" and choice == "approve":
        return f"this finishes checkpoint/plan {slug} (removes the gate hooks and test locks)"
    if rec["kind"] == "plan-approval" and choice == "approve":
        delta = reapproval_delta(root, slug, load_plan(root, slug) or {"checkpoints": []})
        note = f" ({'; '.join(delta)})" if delta else ""
        return f"this approves the plan{note} and freezes " + approval_summary(root, slug)
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
            except (Exception, SystemExit) as e:  # noqa: BLE001
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
    bump_gen(root, slug)
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


def plan_slugs(root: Path) -> list[str]:
    pd = root / PLAN_DIR
    return sorted(p.stem for p in pd.glob("*.json")) if pd.is_dir() else []


def all_slugs(root: Path) -> list[str]:
    """Every build this root may hold: state dirs, plan files and the generation records kept
    with the key (removing the state dir must not hide a build)."""
    return sorted(set(slugs(root)) | set(plan_slugs(root)) | set(recorded_slugs(root)))


def gate(cp: dict, name: str) -> str:
    return ((cp.get("gates") or {}).get(name) or {}).get("status", "pending")


def unpassed_before(cps: list, cid) -> list:
    return [c.get("id") for c in cps
            if isinstance(c.get("id"), int) and isinstance(cid, int)
            and c["id"] < cid and c.get("status") != "passed"]


def task_problem(task) -> str:
    """'' when the plan's `task` block is absent or has the hub's shape, else a one-line reason."""
    if task is None:
        return ""
    if not isinstance(task, dict):
        return "task must be an object"
    extra = sorted(set(task) - set(TASK_KEYS))
    if extra:
        return f"task has unknown key(s) {', '.join(extra)} (allowed: {', '.join(TASK_KEYS)})"
    missing = [k for k in TASK_KEYS if k not in task]
    if missing:
        return f"task is missing {', '.join(missing)}"
    if not isinstance(task["hub"], str) or not HUB_CARD_RE.fullmatch(task["hub"]):
        return "task.hub must be a hub card id (c- and 12 base32 characters)"
    if not isinstance(task["workspace"], str) or not task["workspace"].strip():
        return "task.workspace must be a non-empty string"
    if task["kind"] not in TASK_KINDS:
        return f"task.kind must be one of {', '.join(TASK_KINDS)}"
    ext = task["external"]
    if ext is not None and (not isinstance(ext, dict) or set(ext) != set(EXTERNAL_KEYS)
                            or not all(isinstance(ext[k], str) for k in EXTERNAL_KEYS)):
        return "task.external must be null or an object with string provider, id and url"
    return ""


def task_env_problem(task, env_value) -> str:
    """The hook's check: a task block is written only for a run the hub launched, verbatim."""
    if task is None:
        return ""
    if not (env_value or "").strip():
        return ("a task block is only written for a run the hub launched (TQ_HUB_TASK is not set): "
                "remove the top-level task")
    try:
        want = json.loads(env_value)
    except ValueError:
        return "TQ_HUB_TASK is not valid JSON; remove the top-level task"
    if task != want:
        return "task must be TQ_HUB_TASK verbatim"
    return ""


def plan_problem(doc) -> str:
    """'' when the plan document has the shape every lookup relies on, else a one-line
    reason naming the field: checkpoints is a list of dicts with unique int/non-empty-str
    ids, gates (when present) a dict of dicts, tests (when present) a list of str."""
    if not isinstance(doc, dict):
        return "the plan must be a JSON object"
    bad = task_problem(doc.get("task"))
    if bad:
        return bad
    cps = doc.get("checkpoints")
    if not isinstance(cps, list):
        return "checkpoints must be a list"
    seen: set = set()
    for i, c in enumerate(cps):
        if not isinstance(c, dict):
            return f"checkpoints[{i}] must be an object"
        cid = c.get("id")
        ok = (isinstance(cid, int) and not isinstance(cid, bool)) or (isinstance(cid, str) and cid.strip() != "")
        if not ok:
            return f"checkpoints[{i}].id must be an integer or a non-empty string"
        if str(cid) in seen:
            return f"checkpoints[{i}].id {cid!r} is not unique"
        seen.add(str(cid))
        g = c.get("gates")
        if g is not None and (not isinstance(g, dict) or not all(isinstance(v, dict) for v in g.values())):
            return f"checkpoints[{i}].gates must be an object whose values are objects"
        t = c.get("tests")
        if t is not None and (not isinstance(t, list) or not all(isinstance(x, str) for x in t)):
            return f"checkpoints[{i}].tests must be a list of strings"
    return ""


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
    except ValueError:
        return  # not valid JSON yet; the planner validates its own JSON
    if not isinstance(doc, dict) or "checkpoints" not in doc:
        return
    bad = plan_problem(doc)
    if bad:
        raise Block(f"invalid plan: {bad}.")
    if check_evidence:
        # Only a new or changed task must come from the environment; an unchanged one (a hub-launched
        # plan resumed in a terminal without TQ_HUB_TASK) is left alone.
        try:
            old_task = json.loads(old_text).get("task")
        except (ValueError, AttributeError):
            old_task = None
        if doc.get("task") is None or doc.get("task") != old_task:
            env_bad = task_env_problem(doc.get("task"), os.environ.get("TQ_HUB_TASK"))
            if env_bad:
                raise Block(f"invalid plan: {env_bad}.")
    new = doc["checkpoints"]
    # Hotfix is what the human approved, never what the plan under edit claims.
    if check_evidence and rolled_back(root, slug):
        raise Block(rollback_msg(slug, root) + ".")
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
    except (ValueError, KeyError, TypeError, AttributeError):
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
    for s in all_slugs(root):
        if rolled_back(root, s):
            return True  # a rolled-back or legacy state is never finished
        rec = _gen_record(root, s)
        if rec is not None:
            if not rec.get("finished"):
                return True  # the record kept with the key wins over session.json
        elif not load_session(root, s).get("finished"):
            return True
    return False


# Anchored on purpose: a bare "gate" or "cmd_approve" appears in ordinary project files.
GATE_INTERNALS = ("mint_hub_token", "tq_build_token", "tq_build_actor")
_GATE_MODULE_RE = re.compile(r"tentaqles[./\\]workflow[./\\]gate\b")
# squashed text has lost its word boundaries (a name split over edits reads whole), so only the
# one ordinary continuation, "gateway", is excluded there.
_GATE_SQUASHED_RE = re.compile(r"tentaqles[./\\]workflow[./\\]gate(?!way)")
_GATE_FILE_RE = re.compile(r"(?<!build-)workflow[/\\]gate\.py\b")
_GATE_WORKFLOW_IMPORT_RE = re.compile(r"workflow\s+import[^\n;]*\bgate\b")
_GATE_BARE_IMPORT_RE = re.compile(r"\b(?:from|import)\s+gate\b")
_GATE_FROMLIST_RE = re.compile(r"tentaqles[./\\]workflow[^\n]{0,40}fromlist=gate\b")
_GATE_FUNCS_RE = re.compile(r"\b(?:cmd_approve|cmd_answer|cmd_judge|cmd_finish|cmd_unlock_tests|cmd_dod)\b")
_KEY_FILE_RE = re.compile(r"build-gate/(?:[^/]*/)*hmac\.key")
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
    return canon_path(key_dir())


def _plugin_root_canon() -> str:
    return canon_path(plugin_root())


# The breaker's scratch dir holds data files only: a name must end in one of these
# extensions (an allowlist, so a new runner's config format can't slip through) and must
# not look like a test or a tool config (`jest.config.json`, `package.json`, `.eslintrc.json`).
SCRATCH_DATA_EXT = (".sql", ".db", ".db-wal", ".db-shm", "-wal", "-shm", ".log", ".pid", ".json", ".txt",
                    ".csv", ".tsv", ".dump", ".bak")
_SCRATCH_NOT_DATA = re.compile(
    r"^(conftest\..*|test_.*|.*_test\..*|.*\.spec\..*|.*\.test\..*|.*config.*|\..*rc(\..*)?|.*\.pth(\..*)?"
    r"|package(-lock)?\.json|composer\.json|deno\.jsonc?|project\.json|.*\.(py|js|ts|mjs|cjs|mts|cts)\..*)$")
SCRATCH_WALK_CAP = 2000  # entries walked under .tq-breaker/ before failing closed
# Commands that only remove entries or make empty directories: allowed in the scratch dir
# whatever the name (the agent must be able to delete a stray file the walk complains about).
_SCRATCH_CLEANERS = {"rm", "rmdir", "unlink", "del", "erase", "rd", "remove-item", "ri", "mkdir", "md"}


def scratch_name_ok(name: str) -> bool:
    n = name.lower()
    return n.endswith(SCRATCH_DATA_EXT) and not _SCRATCH_NOT_DATA.match(n)


def _lstat_is_link(path: str) -> bool:
    """True for a symlink, a junction or any other reparse point; True when it can't tell."""
    try:
        st = os.lstat(path)
    except OSError:
        return True
    if stat.S_ISLNK(st.st_mode):
        return True
    return bool(getattr(st, "st_file_attributes", 0) & 0x400)  # FILE_ATTRIBUTE_REPARSE_POINT


def scratch_problem(root: Path) -> str | None:
    """Why <root>/.tq-breaker/ can't stand next to recorded evidence, or None.

    The dir is excluded from the tree hash, so it may hold data files only. Bounded walk
    (SCRATCH_WALK_CAP entries), no link followed, fail closed. Names only the first
    offending entry, by its file name (never its path)."""
    base = os.path.join(str(root), BREAKER_DIR)
    if not os.path.lexists(base):
        return None
    if _lstat_is_link(base):
        return f"{BREAKER_DIR} is a link; the breaker scratch dir must be a plain directory"
    if not os.path.isdir(base):
        return f"{BREAKER_DIR} is not a directory"
    stack, seen = [base], 0
    while stack:
        d = stack.pop()
        try:
            entries = sorted(os.scandir(d), key=lambda e: e.name)
        except OSError:
            return f"can't read {BREAKER_DIR}/; failing closed"
        for e in entries:
            seen += 1
            if seen > SCRATCH_WALK_CAP:
                return f"{BREAKER_DIR}/ holds too many entries (over {SCRATCH_WALK_CAP}); clear it out"
            name = redact(e.name)
            if _lstat_is_link(e.path):
                return f"{BREAKER_DIR}/ may not hold a link ('{name}'); remove it"
            try:
                is_dir = e.is_dir(follow_symlinks=False)
                is_file = e.is_file(follow_symlinks=False)
            except OSError:
                return f"can't read {BREAKER_DIR}/; failing closed"
            if is_dir:
                stack.append(e.path)
            elif not is_file:
                return f"{BREAKER_DIR}/ may not hold a special file ('{name}'); remove it"
            elif not scratch_name_ok(e.name):
                return (f"{BREAKER_DIR}/ holds data files only ({', '.join(x.lstrip('.-') for x in SCRATCH_DATA_EXT)}); "
                        f"'{name}' is not one. Remove it and run again")
    return None


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
    if r.startswith(BREAKER_DIR + "/") and r.count("/") >= 2 and not scratch_name_ok(r.rsplit("/", 1)[-1]) \
            and (active or slugs(root)):
        return "scratch"  # .tq-breaker/<slug>/<name>: a data file name, or refused
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
    "scratch": ("scratch dir holds data files only (sql, db, db-wal, db-shm, log, pid, json, txt, csv, tsv, "
                "dump, bak; no code, test or config files)"),
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
    if (kd in canon or kd in sq or _KEY_FILE_RE.search(canon) or _KEY_FILE_RE.search(sq)
            or re.search(r"(?:\.tentaqles|tq_home)/?build-gate(?!\.py)|build-gate/", sq)):
        hits.append(("key", "the signing key"))
    if re.search(r"(^|[\s\"'=/])\.git/", canon):
        hits.append(("git", ".git/"))
    if STATE_DIR in canon:
        hits.append(("state", STATE_DIR))
    plugin_gate = _plugin_root_canon() + "/gate.py"
    if active and (any(m in canon or m in sq for m in GATE_INTERNALS)
                   or _GATE_MODULE_RE.search(canon) or _GATE_SQUASHED_RE.search(sq)
                   or _GATE_FROMLIST_RE.search(sq)
                   or _GATE_FILE_RE.search(canon) or plugin_gate in canon.lower()
                   or _GATE_WORKFLOW_IMPORT_RE.search(canon)
                   or ((_GATE_FUNCS_RE.search(canon) or _GATE_FUNCS_RE.search(sq))
                       and _GATE_BARE_IMPORT_RE.search(canon))):
        hits.append(("gatemod", "the gate's own code or hub credentials"))
    if active and any(p in canon for p in PROTECTED_CONFIG):
        hits.append(("config", ".claude/settings / tq-rules"))
    if re.search(re.escape(PLAN_DIR) + r"/[^\s/\"']+\.json", canon):
        hits.append(("plan", PLAN_DIR))
    for lk, slug in locked.items():
        if lk and (lk.lower() in canon or _squash(lk.lower()) in sq):
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
        specs = [a for a in rest if not a.startswith("-")]
        wide = [a for a in specs if norm(a).rstrip("/") in ("", ".", "*", ":/", ":", "./*")]
        if wide or not specs:
            raise Block(f"`git add --force` of a whole tree stages ignored files, which include {STATE_DIR}/; "
                        "name the files to force-add.")
        if any(re.search(r"[*?\[]", a) for a in specs):
            raise Block(f"`git add --force` with a glob pathspec can stage ignored files, which include {STATE_DIR}/; "
                        "name the files to force-add.")


_CD_CMDS = {"cd", "chdir", "pushd", "popd", "set-location", "sl", "push-location", "pop-location"}
_POP_CMDS = {"popd", "pop-location"}
_PUSH_CMDS = {"pushd", "push-location"}
_POSIX_SHELLS = {"bash", "sh", "zsh", "dash", "ksh"}
_PS_SHELLS = {"pwsh", "powershell"}
_MAX_NEST = 3
_MAX_CWDS = 8
_MAX_WALK = 500
# xargs may run these without changing any file; every other command it launches is a write to a
# target the hook cannot see (the arguments arrive on stdin), so it is refused while a build exists.
_XARGS_READONLY = {"grep", "egrep", "fgrep", "wc", "ls", "echo", "stat", "basename", "dirname", "du", "true"}
_XARGS_OPT_WITH_ARG = {"-a", "-d", "-E", "-I", "-L", "-n", "-P", "-s", "-R", "-S"}
_XARGS_LONG_WITH_ARG = {"--arg-file", "--delimiter", "--eof", "--max-lines", "--max-args", "--max-procs",
                        "--max-chars", "--process-slot-var"}
_GLOB_CHARS = re.compile(r"[*?]|\[[^\]]+\]")
_ENCODED_PS = re.compile(r"^-e(?:n(?:c(?:o(?:d(?:e(?:d(?:c(?:o(?:m(?:m(?:a(?:n(?:d)?)?)?)?)?)?)?)?)?)?)?)?)?$", re.I)
_COMMAND_PS = re.compile(r"^-c(?:o(?:m(?:m(?:a(?:n(?:d)?)?)?)?)?)?$", re.I)


def _is_abs(p: str) -> bool:
    """Absolute in the forward-slash form: a leading slash (also on Windows) or a drive."""
    return p.startswith("/") or bool(re.match(r"^[A-Za-z]:/", p))


def _nabs(path) -> str:
    """Canonical absolute form used for every comparison (see `canon_path`)."""
    return canon_path(path)


_TQ_VAR = re.compile(r"\$\{TQ_HOME\}|\$env:TQ_HOME|\$TQ_HOME(?![A-Za-z0-9_])|%TQ_HOME%", re.IGNORECASE)
_HOME_VAR = re.compile(r"\$\{(?:HOME|USERPROFILE)\}|\$env:(?:HOME|USERPROFILE)"
                       r"|\$(?:HOME|USERPROFILE)(?![A-Za-z0-9_])|%(?:HOME|USERPROFILE)%", re.IGNORECASE)


def _expand_known(word: str) -> str:
    """Expand the variables whose value the hook knows: `$TQ_HOME`, `${TQ_HOME}`, `%TQ_HOME%`,
    `$env:TQ_HOME`, `$HOME`/`$USERPROFILE` (all spellings) and a leading `~`. Anything else
    stays literal (and `has_expansion` still sees it)."""
    w = word
    home = norm(str(Path.home()))
    if "$" in w or "%" in w:
        tq = norm(os.environ.get("TQ_HOME") or str(Path.home() / ".tentaqles"))
        w = _TQ_VAR.sub(lambda m: tq, w)
        w = _HOME_VAR.sub(lambda m: home, w)
    if w == "~" or w.startswith(("~/", "~\\")):
        w = home + w[1:]
    return w


def _key_glob_candidates() -> list[tuple[str, str, str]]:
    """What a glob could name under the key directory: the key file, whatever is there, the
    directory itself (class "keydir") and each directory above it (class "keyanc"); the last
    two count unless the command only lists (see _KEY_SAFE_VERBS)."""
    kd = _nabs(key_dir())
    out = [("key", "the signing key", kd + "/hmac.key"), ("keydir", "the signing key's directory", kd)]
    out.extend(("key", "the signing key", _nabs(p)) for p in _walk_paths(key_dir()))
    cur = kd
    while True:
        parent = cur.rsplit("/", 1)[0] if "/" in cur.rstrip("/") else ""
        if not parent or parent == cur:
            break
        out.append(("keyanc", "the signing key (a directory above it)", parent))
        cur = parent
    return out


# Verbs that may name the key directory's ancestors: they list or test a name and never read
# what is under it (only without a redirect, and `ls` without -R).
_KEY_SAFE_VERBS = {"ls", "cd", "pwd", "stat", "test", "echo"}
# Archive and sync tools read every file under a directory word: recursive readers.
_ARCHIVE_TOOLS = {"tar", "bsdtar", "gtar", "7z", "7za", "7zr", "zip", "rsync", "robocopy", "xcopy",
                  "compress-archive", "cpio", "pax", "ditto"}


def _root_like(absn: str) -> bool:
    """`/` or a bare drive: an ancestor of everything, counted only for recursive readers."""
    return re.fullmatch(r"(?:[A-Za-z]:)?/?", absn) is not None


def _key_reach(absn: str) -> str | None:
    """'dir' when the canonical path is the key directory, 'anc' when it is above it, else None."""
    kd = _key_dir_canon()
    a, k = (absn.lower(), kd.lower()) if os.name == "nt" else (absn, kd)
    a = a.rstrip("/") or "/"
    if a == k:
        return "dir"
    if k.startswith(a.rstrip("/") + "/"):
        return "anc"
    return None


def _glob_segments_match(pat: str, cand: str, dotfiles: bool) -> bool:
    """Segment-wise glob match of two absolute forward-slash paths (both already case-folded).

    `*`, `?` and `[...]` stay inside one segment; `**` spans segments. Like a POSIX shell, a
    wildcard does not match a name that starts with a dot unless `dotfiles` (PowerShell and cmd
    expand `*` over such names), or the pattern segment starts with a literal dot.
    """
    ps, cs = pat.split("/"), cand.split("/")

    def hidden(name: str, p: str) -> bool:
        return (not dotfiles) and name.startswith(".") and not p.startswith(".")

    def m(i: int, j: int) -> bool:
        if i == len(ps):
            return j == len(cs)
        if ps[i] == "**":
            for k in range(j, len(cs) + 1):
                if k > j and hidden(cs[k - 1], "**"):
                    break
                if m(i + 1, k):
                    return True
            return False
        if j == len(cs) or hidden(cs[j], ps[i]):
            return False
        return fnmatch.fnmatchcase(cs[j], ps[i]) and m(i + 1, j + 1)

    return m(0, 0)


def _walk_paths(top: Path) -> list[Path]:
    out: list[Path] = []
    try:
        for dirpath, dirnames, filenames in os.walk(top):
            for n in dirnames + filenames:
                out.append(Path(dirpath) / n)
                if len(out) >= _MAX_WALK:
                    return out
    except OSError:
        pass
    return out


def _recursive_segment(argv: list[str], cmd_name: str) -> bool:
    """Does this command descend into directories (so a directory word means everything under it)?"""
    if cmd_name in ("rg", "ag", "ack", "tree") or cmd_name in _ARCHIVE_TOOLS:
        return True
    if cmd_name == "ls":  # -r is reverse there
        return any(t == "--recursive" or re.fullmatch(r"-[A-Za-z]*R[A-Za-z]*", t) for t in argv[1:])
    for t in argv[1:]:
        if t.lower() in ("--recursive", "--dereference-recursive", "-recurse", "-exec", "-execdir",
                         "-ok", "-okdir"):
            return True
        if re.fullmatch(r"-[A-Za-z]{1,4}", t) and re.search(r"[rR]", t):
            return True
    return False


_TAR_TOOLS = {"tar", "gtar", "bsdtar"}


def _runtime_file_list(cmd_name: str, cargs: list[str]) -> bool:
    """`tar -T <file>` / `--files-from`, `cpio -i/-o/-p`: the file list is read at run time, so the
    hook can't see which paths the command reaches."""
    if cmd_name in _TAR_TOOLS:
        for i, a in enumerate(cargs):
            if a == "--files-from" or a.startswith("--files-from="):
                return True
            if a.startswith("--"):
                continue
            if a.startswith("-"):
                if re.fullmatch(r"-[A-Za-z0-9]*T.*", a):
                    return True
            elif i == 0 and re.fullmatch(r"[A-Za-z]*T[A-Za-z]*", a):
                return True  # old-style bundle: `tar cTf list x.tar`
        return False
    if cmd_name == "cpio":
        return any(a in ("--extract", "--create", "--pass-through") or re.fullmatch(r"-[A-Za-z0-9]*[iop][A-Za-z0-9]*", a)
                   for a in cargs if not a.startswith("---"))
    return False


def _xargs_rest(argv: list[str]) -> list[str] | None:
    """The arguments after `xargs` when this segment runs it (behind env/sudo/nohup...), else None."""
    i = 0
    while i < len(argv):
        a = argv[i]
        if re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", a):
            i += 1
            continue
        base = norm(a).rsplit("/", 1)[-1].lower()
        if base.endswith(".exe"):
            base = base[:-4]
        if base == "xargs":
            return argv[i + 1:]
        if base in _WRAPPERS:
            i += 1
            while i < len(argv) and (argv[i].startswith("-") or argv[i].isdigit()):
                i += 1
            continue
        return None
    return None


def _xargs_command(rest: list[str]) -> str:
    """Base name of the command xargs launches (after its own options), or ''."""
    i = 0
    while i < len(rest):
        a = rest[i]
        if a == "--":
            i += 1
            break
        if a.startswith("--"):
            i += 2 if (a in _XARGS_LONG_WITH_ARG) else 1
        elif a.startswith("-") and len(a) > 1:
            i += 2 if a in _XARGS_OPT_WITH_ARG else 1
        else:
            break
    if i >= len(rest):
        return "echo"  # bare `xargs` runs echo
    base = norm(rest[i]).rsplit("/", 1)[-1].lower()
    return base[:-4] if base.endswith(".exe") else base


def _nested_body(cmd_name: str, cargs: list[str]) -> str | None:
    """The program text a shell or eval is given, '\0encoded' for a PowerShell encoded command, or None."""
    if cmd_name in _POSIX_SHELLS:
        for i, a in enumerate(cargs):
            if re.fullmatch(r"-[A-Za-z]*c[A-Za-z]*", a):
                return cargs[i + 1] if i + 1 < len(cargs) else ""
            if not a.startswith("-"):
                return None
        return None
    if cmd_name in _PS_SHELLS:
        for i, a in enumerate(cargs):
            if _ENCODED_PS.match(a):
                return "\0encoded"
            if _COMMAND_PS.match(a):
                return " ".join(cargs[i + 1:])
        return None
    if cmd_name == "cmd":
        for i, a in enumerate(cargs):
            if a.lower() in ("/c", "/k", "/r"):
                return " ".join(cargs[i + 1:])
        return None
    if cmd_name == "eval":
        return " ".join(cargs)
    return None


_REMOVERS = {"rm", "rmdir", "unlink", "shred", "truncate", "remove-item", "ri", "del", "erase", "rd",
             "clear-content"}
_MOVERS = {"mv", "move-item", "mi", "move", "ren", "rename-item", "rni"}
_COPIERS = {"cp", "install", "rsync", "copy-item", "cpi", "copy", "xcopy", "ln", "scp"}
_TARGET_OPTS = {"-t", "--target-directory", "-destination", "-dest"}


def _dest_words(seg, cmd_name: str, cargs: list[str]) -> set[str]:
    """The words of a writing segment that name what it writes or removes: redirect targets,
    every operand of rm/rmdir/mv (sources too: they vanish), the last operand (or -t) of
    cp/install/rsync, the start dirs of `find -delete/-exec`, the archive (and on extract the
    directory) of tar, and every operand of other writers. A command that writes only through a
    redirect (`pytest tests > out.txt`), and echo/printf data, name nothing else."""
    out = {t for t in seg.redirects if not is_null_target(t)}
    bare = Segment()
    bare.argv, bare.raw = seg.argv, seg.raw
    if cmd_name in ("echo", "printf") or not is_write_segment(bare):
        return out
    pos = [a for a in cargs if not a.startswith("-")]
    low = [a.lower() for a in cargs]
    if cmd_name in _REMOVERS or cmd_name in _MOVERS:
        return out | set(pos)
    if cmd_name == "robocopy":
        return out | ({pos[1]} if len(pos) > 1 else set(pos))
    if cmd_name in _COPIERS:
        for i, a in enumerate(low):
            if a in _TARGET_OPTS and i + 1 < len(cargs):
                return out | {cargs[i + 1]}
            if a.startswith("--target-directory="):
                return out | {cargs[i]}
        return out | ({pos[-1]} if pos else set())
    if cmd_name == "find":
        starts = []
        for a in cargs:
            if a.startswith(("-", "(", "!")):
                break
            starts.append(a)
        return out | set(starts or ["."])
    if cmd_name in ("tar", "bsdtar", "gtar"):
        mode = cargs[0] if cargs and not cargs[0].startswith("--") else ""
        extract = ("x" in mode.lstrip("-") and re.fullmatch(r"-?[A-Za-z]+", mode) is not None) or any(
            a in ("--extract", "--get") for a in cargs)
        for i, a in enumerate(cargs):
            short_f = re.fullmatch(r"-[A-Za-z]*f", a) or (i == 0 and re.fullmatch(r"[A-Za-z]*f", a))
            if (a == "--file" or short_f) and i + 1 < len(cargs):
                out.add(cargs[i + 1])
            elif a.startswith("--file="):
                out.add(a)
        if extract:
            cdirs = [cargs[i + 1] for i, a in enumerate(cargs) if a in ("-C", "--directory") and i + 1 < len(cargs)]
            cdirs += [a for a in cargs if a.startswith("--directory=")]
            out |= set(cdirs or ["."])
        return out
    if cmd_name == "unzip":
        d = [cargs[i + 1] for i, a in enumerate(cargs) if a == "-d" and i + 1 < len(cargs)]
        return out | set(d or ["."])
    return out | set(pos)


# Commands that can create links (or unpack them): while a build exists they may not make a
# link whose path or target lies under .claude/build/, or at or under .tq-breaker/.
_LINK_MAKERS = {"ln", "link", "mklink", "tar", "unzip", "cpio", "7z", "fsutil"}
_LINK_TYPES = ("symboliclink", "junction", "hardlink")


def _bases(seg) -> list[str]:
    out = []
    for w in seg.argv:
        b = norm(w).rsplit("/", 1)[-1].lower()
        b = b[:-4] if b.endswith(".exe") else b
        out.append("python" if re.match(r"^python[0-9.]*$", b) else b)
    return out


def _makes_links(seg) -> bool:
    bases = _bases(seg)
    if any(b in _LINK_MAKERS for b in bases):
        return True
    if any(b in ("new-item", "ni") for b in bases) and any(t in w.lower() for w in seg.argv for t in _LINK_TYPES):
        return True
    if "cp" in bases:
        for a in seg.argv:
            if re.match(r"^-[A-Za-z]*[sl][A-Za-z]*$", a):
                return True
            opt = a.split("=", 1)[0].lower()
            if len(opt) >= 3 and ("--link".startswith(opt) or "--symbolic-link".startswith(opt)):
                return True
    return False


# Options that name a file the command writes, per tool (`-o`, `-f` and `--file` mean other
# things elsewhere: `pytest -o`, `psql -f`, `grep --file`, `docker build --file`). A segment
# using one counts as a write and the option's value is a destination word, so the spine's
# destination rules (locked tests, a directory above them) apply to it. tar's `-f`/`--file` is
# not listed: tar is a write command already and `_dest_words` takes its archive operand.
_OUTPUT_OPTS = {
    "curl": ({"-o", "-D", "-c"}, {"--output", "--dump-header", "--cookie-jar", "--output-dir"}),
    "wget": ({"-O", "-o", "-a", "-P"}, {"--output-document", "--output-file", "--append-output",
                                        "--directory-prefix"}),
    "pg_dump": ({"-f"}, {"--file"}),
    "pg_dumpall": ({"-f"}, {"--file"}),
    "pg_basebackup": ({"-D"}, {"--pgdata"}),
    "mysqldump": ({"-r"}, {"--result-file"}),
    "sort": ({"-o"}, {"--output"}),
}
# Options that write with no path of their own (`curl -O` names the file after the URL).
_OUTPUT_FLAGS = {"curl": {"-O", "--remote-name", "--remote-name-all"}}
# Long options that name an output file for any tool (`--file` is not one; git is left to _check_git).
_OUTPUT_LONG_ANY = {"--output", "--output-file", "--output-dir", "--out", "--outfile", "--out-file", "--log-file"}
_SQLITE_WRITES = re.compile(r"(?:^|[\s;\"'])\.(?:backup|save|output|once)((?:[ \t]+[^\s;]+)*)", re.IGNORECASE)


def _option_writes(cmd_name: str, cargs: list[str]) -> tuple[bool, list[str]]:
    """(does an option make this segment write, the path words it writes through)."""
    writes, out = False, []
    if cmd_name == "sqlite3":
        for a in cargs:
            for m in _SQLITE_WRITES.finditer(a):
                writes = True
                ops = [t.strip("'\"") for t in m.group(1).split() if not t.startswith("-")]
                if ops:
                    out.append(ops[-1])  # `.backup ?DB? FILE`: the file is the last operand
        return writes, out
    if cmd_name == "git":
        return False, []
    shorts, longs = _OUTPUT_OPTS.get(cmd_name, (set(), set()))
    longs = longs | _OUTPUT_LONG_ANY
    flags = _OUTPUT_FLAGS.get(cmd_name, set())
    i = 0
    while i < len(cargs):
        a = cargs[i]
        nxt = cargs[i + 1] if i + 1 < len(cargs) else None
        if a in flags:
            writes = True
        elif a.startswith("--"):
            opt = a.split("=", 1)[0].lower()
            if opt in longs:
                writes = True
                if "=" in a:
                    out.append(a.split("=", 1)[1])
                elif nxt is not None:
                    out.append(nxt)
                    i += 1
        elif a in shorts:
            writes = True
            if nxt is not None:
                out.append(nxt)
                i += 1
        elif len(a) > 2 and a[0] == "-" and a[1] != "-" and a[1].isalpha():
            # a bundled short cluster (`-sSo path`, `-qO path`, `-wf path`, `-opath`): the first
            # letter that is one of the tool's output options takes the rest of the word, or the next word
            letters = {o[1] for o in shorts if len(o) == 2}
            for k in range(1, len(a)):
                if not a[k].isalpha():
                    break
                if a[k] in letters:
                    writes = True
                    rest = a[k + 1:]
                    if rest:
                        out.append(rest)
                    elif nxt is not None:
                        out.append(nxt)
                        i += 1
                    break
        i += 1
    return writes, out


class _Cwd:
    """The tracked working directory: candidate absolute paths, or None when unknown."""

    __slots__ = ("cwds", "prev", "stack")

    def __init__(self, cwds):
        self.cwds = cwds
        self.prev = None
        self.stack: list = []

    def copy(self) -> "_Cwd":
        c = _Cwd(list(self.cwds) if self.cwds is not None else None)
        c.prev = self.prev
        c.stack = list(self.stack)
        return c


class _ShellScan:
    """One pass over a command line (and, recursively, the shell programs inside it).

    Tracks the working directory across segments, resolves every word against it, matches glob
    words against the protected paths, and remembers whether any segment wrote and whether any
    word reached `.claude`/the state dir or the key; a line that does both is refused.
    """

    def __init__(self, root: Path, cwd, locked: dict, active: bool, tool: str):
        self.root, self.locked, self.active = root, locked, active
        self.rootn = _nabs(root)
        self.ps = tool == "PowerShell"  # `*` also matches dot names there
        self.cur_ps = self.ps  # the lexer of the program being scanned (a pwsh -c body is PowerShell)
        self._locked_abs: list[tuple[str, str, str]] | None = None
        self.start = _nabs(cwd) if cwd else self.rootn
        self.writes = False
        self.prot = False
        self._cands: list | None = None

    # -- protected candidates ------------------------------------------------

    def candidates(self) -> list[tuple[str, str, str]]:
        """(class, display, canonical abs path) of what a glob could name."""
        if self._cands is not None:
            return self._cands
        out: list[tuple[str, str, str]] = []
        for rel in (".claude", STATE_DIR):
            out.append(("state", rel, _nabs(self.root / rel)))
        for p in _walk_paths(self.root / STATE_DIR):
            out.append(("state", STATE_DIR, _nabs(p)))
        out.extend(self._key_candidates())
        for lk, slug in self.locked.items():
            out.append((f"locked:{slug}", lk, _nabs(self.root / lk)))
            parent = Path(lk).parent
            while str(parent) not in (".", ""):
                out.append((f"locked:{slug}", lk, _nabs(self.root / parent)))
                parent = parent.parent
        self._cands = out
        return out

    def _key_candidates(self) -> list[tuple[str, str, str]]:
        return _key_glob_candidates()

    # -- words ---------------------------------------------------------------

    def bases(self, st: _Cwd) -> list[str]:
        out = list(st.cwds or [])
        if self.rootn not in out:
            out.append(self.rootn)
        return out

    def variants(self, word: str) -> list[str]:
        w = word
        if w.startswith("-") and "=" in w:
            w = w.split("=", 1)[1]
        w = w.strip("'\"")
        return shell_word_variants(_expand_known(w))

    def resolved(self, word: str, bases: list[str]) -> list[tuple[str | None, str]]:
        out = []
        for v in self.variants(word):
            for b in bases:
                _, absn = resolve_word(b, v)
                out.append((rel_to(Path(self.rootn), absn), absn))
        return out

    def locked_abs(self) -> list[tuple[str, str, str]]:
        if self._locked_abs is None:
            self._locked_abs = [(f"locked:{slug}", lk, _nabs(self.root / lk)) for lk, slug in self.locked.items()]
        return self._locked_abs

    def word_hits(self, word: str, bases: list[str], recursive: bool, cmd_name: str, dest: bool = True,
                  key_safe: bool = False, key_bases: tuple = ()) -> list[tuple[str, str]]:
        """Protection hits of one word. `dest`: the word is a destination or removal operand (only
        those count for a directory above a locked test). `key_safe`: the verb only lists (see
        _KEY_SAFE_VERBS). `key_bases`: extra directories the word is relative to for the key check
        (`tar -C <dir>`, the first positional of an archive/sync tool)."""
        hits: list[tuple[str, str]] = []
        for rel, absn in self.resolved(word, bases):
            cls = classify(self.root, rel, absn, self.locked, self.active)
            if cls == "scratch" and not dest:
                cls = None  # the data-files-only class judges what a command writes, not every word
            if cls:
                hits.append((cls, rel or absn))
            hits.extend(self.ancestor_hits(absn, recursive, cmd_name, key_safe))
            if dest:
                # a directory above a locked test: `mv tests tests2`, `rm -rf tests`, `cp -r src tests`
                top = absn.rstrip("/") + "/"
                hits.extend((c, lk) for c, lk, la in self.locked_abs() if la.startswith(top))
        for _, absn in (self.resolved(word, list(key_bases)) if key_bases else []):
            if classify(self.root, None, absn, {}, False) == "key":
                hits.append(("key", "the signing key"))
            hits.extend(self.ancestor_hits(absn, recursive, cmd_name, key_safe))
        for v in self.variants(word):
            if not _GLOB_CHARS.search(v):
                continue
            for b in bases:
                pat = _nabs(v if _is_abs(v) else f"{b}/{v}")
                for cls, shown, cand in self.candidates():
                    if cls in ("keydir", "keyanc"):
                        if (key_safe and not recursive) or (cls == "keyanc" and _root_like(cand) and not recursive):
                            continue
                    if _glob_segments_match(pat, cand, self.ps or self.cur_ps):
                        hits.append(("key" if cls in ("keydir", "keyanc") else cls, shown))
        return hits

    def ancestor_hits(self, absn: str, recursive: bool, cmd_name: str, key_safe: bool = False) -> list[tuple[str, str]]:
        """A word naming the key directory or any directory above it reaches the key, unless the
        verb only lists it (`ls` without -R, `cd`, `pwd`, `stat`, `test`, `echo`, no redirect). A
        bare `/` or drive counts only for recursive readers (grep -r, find, tar, rsync...)."""
        reach = _key_reach(absn)
        if reach is None:
            return []
        rec = recursive or cmd_name == "find"
        if reach == "anc" and _root_like(absn) and not rec:
            return []
        if key_safe and not rec:
            return []
        return [("key", "the signing key (a read of its directory or a directory above it)")]

    def key_bases(self, cmd_name: str, cargs: list[str], bases: list[str]) -> list[str]:
        """Directories an archive/sync tool takes its other words relative to: `-C <dir>`,
        `--directory=<dir>` and the first positional (robocopy's source)."""
        if cmd_name not in _ARCHIVE_TOOLS:
            return []
        raw: list[str] = []
        for i, a in enumerate(cargs):
            if a in ("-C", "--directory") and i + 1 < len(cargs):
                raw.append(cargs[i + 1])
            elif a.lower().startswith("--directory="):
                raw.append(a.split("=", 1)[1])
        pos = [a for a in cargs if not a.startswith("-")]
        if pos:
            raw.append(pos[0])
        out: list[str] = []
        for r in raw:
            for _, absn in self.resolved(r, bases):
                if absn not in out:
                    out.append(absn)
        return out[:_MAX_CWDS]

    def check_links(self, seg, bases: list[str]) -> None:
        """Refuse a link whose path or target lands under .claude/build/, or at or under
        .tq-breaker/, while a build exists. Every word is resolved against the tracked cwd (and
        the root), and also from each other word and that word's directory, since a link's
        target is relative to the link's own directory."""
        if not (self.active or slugs(self.root)) or not _makes_links(seg):
            return
        words = [w for w in seg.argv[1:] + seg.redirects if w and not w.startswith("-")]
        words += [w.split("=", 1)[1] for w in seg.argv[1:] if w.startswith("-") and "=" in w]
        res = {i: self.resolved(w, bases) for i, w in enumerate(words)}
        cands: list[tuple[str, str | None]] = []
        for i, w in enumerate(words):
            cands.extend((w, rel) for rel, _ in res[i])
            link_dirs: list[str] = []
            for j in range(len(words)):
                if j == i:
                    continue
                for _, absd in res[j]:
                    a = absd.rstrip("/")
                    parent = a.rsplit("/", 1)[0] if "/" in a else a
                    for b in (a or "/", parent or "/"):
                        if b not in link_dirs:
                            link_dirs.append(b)
            if link_dirs:
                cands.extend((w, rel) for rel, _ in self.resolved(w, link_dirs))
        for w, rel in cands:
            r = (rel or "").lower() if os.name == "nt" else (rel or "")
            text = canon_text(w)
            if r == STATE_DIR or r.startswith(STATE_DIR + "/") or STATE_DIR in text:
                raise Block(f"links can't be created into or out of {STATE_DIR}/ while a build exists "
                            "(a link would let a later write land on the gate's evidence).")
            if r == BREAKER_DIR or r.startswith(BREAKER_DIR + "/") or BREAKER_DIR in text:
                raise Block(f"links can't be created at or under {BREAKER_DIR}/ while a build exists "
                            "(`finish` deletes that dir, and it holds plain data files only).")

    # -- working directory ---------------------------------------------------

    def apply_cd(self, cmd_name: str, cargs: list[str], st: _Cwd) -> None:
        path_args = [a for a in cargs if a == "-" or not a.startswith("-")]
        target = path_args[0] if path_args else None
        old = st.cwds
        if cmd_name in _POP_CMDS:
            st.cwds = st.stack.pop() if st.stack else None
            st.prev = old
            return
        if cmd_name in _PUSH_CMDS:
            if target is None:
                st.cwds = None  # swaps the top two entries: not tracked
                return
            st.stack.append(old)
        if target is None:
            st.cwds = None  # bare `cd` goes to $HOME
            st.prev = old
            return
        new: list[str] | None
        if target == "-":
            new = st.prev
        else:
            exp = _expand_known(target.strip("'\""))
            if has_expansion(exp) or _GLOB_CHARS.search(exp):
                new = None
            else:
                new = []
                for v in shell_word_variants(exp):
                    absolute = _is_abs(v)
                    for b in (st.cwds or []) if not absolute else [self.rootn]:
                        _, absn = resolve_word(b, v)
                        if absn not in new:
                            new.append(absn)
                new = new[:_MAX_CWDS] or None
        st.prev = old
        st.cwds = new

    # -- the scan ------------------------------------------------------------

    def scan(self, cmd: str, st: _Cwd, depth: int, ps: bool = False) -> None:
        if depth > _MAX_NEST:
            raise Block("shell programs nested more than three levels deep can't be checked; "
                        "run the steps as separate commands.")
        canon_all = canon_text(cmd)
        try:
            segs = lex_shell(cmd, ps=ps)
        except ShellParseError as e:
            if _text_mentions(self.root, canon_all, self.locked, self.active):
                raise Block(f"can't parse this command ({e}) and it names a protected path; failing closed.")
            return
        if any(c == "key" for c, _ in _text_mentions(self.root, canon_all, {}, False)):
            raise Block(_msg("key", "the signing key"))  # a name split across segments reads whole here
        for seg in segs:
            self.segment(seg, st, depth, ps)

    def segment(self, seg, st: _Cwd, depth: int, ps: bool = False) -> None:
        self.cur_ps = ps
        root, locked, active = self.root, self.locked, self.active
        sub, sub_args = _gate_invocation(seg)
        cmd_name, cargs = command_word(seg.argv)
        if cmd_name in WRITE_COMMANDS or cmd_name == "git":
            sub, sub_args = None, []  # `cp x .../build-gate.py` writes the gate script, it doesn't run it
        opt_writes, opt_dest = _option_writes(cmd_name, cargs)
        writes = is_write_segment(seg) or opt_writes
        recursive = _recursive_segment(seg.argv, cmd_name)
        bases = self.bases(st)
        words = [w for w in seg.argv[1:] + seg.redirects if not is_null_target(w)]
        redirected = any(not is_null_target(t) for t in seg.redirects)
        # echo/printf words are text even with a redirect (the redirect target is judged on its own)
        key_safe = cmd_name in _KEY_SAFE_VERBS and (not redirected or cmd_name in ("echo", "printf"))
        key_bases = tuple(self.key_bases(cmd_name, cargs, bases))
        dest = (_dest_words(seg, cmd_name, cargs) | set(opt_dest)) if writes else set()
        word_hits: list[tuple[str, str]] = []
        for w in words:
            word_hits.extend(self.word_hits(w, bases, recursive, cmd_name, w in dest, key_safe, key_bases))
        for w in sorted(dest - set(words)):  # an implied operand: `tar xf a.tar` extracts into "."
            word_hits.extend(self.word_hits(w, bases, recursive, cmd_name, True, key_safe, key_bases))
        raw_forms = [seg.raw, re.sub(r"\\(.)", r"\1", seg.raw, flags=re.DOTALL)]
        text_hits: list[tuple[str, str]] = []
        for raw in raw_forms:
            for h in _text_mentions(root, canon_text(raw), locked, active):
                if h not in text_hits:
                    text_hits.append(h)
        for cls, what in word_hits + text_hits:
            if cls == "key":
                raise Block(_msg("key", what))
            if cls == "gatemod":
                raise Block(_GATEMOD_MSG)
        self.check_links(seg, bases)
        if cmd_name in _CD_CMDS:
            self.apply_cd(cmd_name, cargs, st)
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
            return  # the gate writes its own (signed) state
        if writes:
            self.writes = True
        if any(c in ("state", "key") for c, _ in word_hits + text_hits):
            self.prot = True
        if active and _runtime_file_list(cmd_name, cargs):
            raise Block("`tar -T`/`--files-from` and `cpio` read their file list at run time, which the hook can't "
                        "check; they are refused while a build exists. Name the paths on the command line.")
        xrest = _xargs_rest(seg.argv)
        if xrest is not None and active and _xargs_command(xrest) not in _XARGS_READONLY:
            raise Block("`xargs` runs a command on arguments the hook can't see, which may be a write to the gate's "
                        "state or to locked tests; it is refused while a build exists. Name the files in the command.")
        body = _nested_body(cmd_name, cargs)
        if body is not None:
            if body == "\0encoded":
                raise Block("an encoded PowerShell command can't be checked; it is refused while a build exists.")
            if body.lstrip().startswith(("$", "`")):
                raise Block("this shell runs a program built from a variable, which can't be checked; "
                            "write the command out.")
            # each shell's own lexer: PowerShell for pwsh/powershell, POSIX for sh/bash/eval/cmd
            self.scan(body, st.copy(), depth + 1, ps=cmd_name in _PS_SHELLS)
        if cmd_name == "git":
            _check_git(seg, word_hits + text_hits)
        if not writes:
            return
        # Fail closed on write targets the lexer can't resolve.
        targets = seg.redirects + seg.argv[1:]
        if st.cwds is None:
            # for echo/printf/cat the words are data, only the redirect targets are paths
            for t in (seg.redirects if cmd_name in ("echo", "printf", "cat") else targets):
                if is_null_target(t) or t.startswith("-"):
                    continue
                v = self.variants(t)[0]
                if not _is_abs(v) and not has_expansion(v):
                    raise Block("this command writes after a `cd` to a directory the hook can't resolve (a variable, "
                                "no argument, an empty popd); use literal paths or a `cd` to a literal directory first.")
        if any(has_expansion(t) for t in seg.redirects if not is_null_target(t)):
            raise Block("this command writes to a path built from a variable; use a literal path.")
        if cmd_name in WRITE_COMMANDS and any(has_expansion(t) for t in targets[len(seg.redirects):] if not t.startswith("-")) \
                and (text_hits or locked):
            raise Block("this command writes to a path built from a variable while protected paths exist; use a literal path.")
        hits = word_hits + text_hits
        if cmd_name == "git":
            # git itself manages .git/ and the plan file (add/commit); history-changing
            # subcommands on locked tests are still refused
            hits = [(c, w) for c, w in hits if c.startswith("locked:")]
        elif cmd_name in _SCRATCH_CLEANERS:
            hits = [(c, w) for c, w in hits if c != "scratch"]
        for cls, what in hits:
            raise Block(_msg(cls, what))


def check_shell(cmd: str, root: Path, cwd=None, tool: str = "Bash") -> None:
    """Normalise once (lexer + canonical paths + tracked working directory), then decide per segment.

    `cwd` is the session's working directory when it differs from the project root. Every path
    is resolved against the tracked directory and also against the root, so a subshell `cd`
    cannot hide a protected path. Shell programs given to `bash -c`, `sh -c`, `pwsh -c`,
    `powershell -Command`, `cmd /c` and `eval` are lexed recursively (to a depth of three; deeper
    is refused), as are `$(...)` and backticks by the lexer itself. An encoded PowerShell command,
    a program built from a variable, and `xargs` with a command other than a short read-only list
    are refused. A line that writes anywhere and also reaches `.claude`/the state dir is refused.
    """
    scan = _ShellScan(root, cwd, _locked_paths(root), _build_exists(root), tool)
    scan.scan(cmd, _Cwd([scan.start]), 0, ps=tool == "PowerShell")
    if scan.writes:
        gone = rolled_back_slugs(root)
        if gone:
            raise Block(rollback_msg(gone[0], root) + "; until then no command may write files.")
    if scan.writes and scan.prot:
        raise Block(f"this command line writes files and also reaches {STATE_DIR}/ (hook-owned, signed state); "
                    "run the write and the state access as separate commands, and never write into the state dir.")


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
    bad = plan_problem(plan)
    if bad:
        raise Block(f"{PLAN_DIR}/{slug}.json is invalid: {bad}.")
    cps = plan["checkpoints"]
    cp = next((c for c in cps if c.get("id") == cid), None)
    if cp is None:
        raise Block(f"checkpoint #{cid} doesn't exist in {slug}.json.")
    if rolled_back(root, slug):
        raise Block(rollback_msg(slug, root) + ".")
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
    bases = sess["bases"] = review_bases(root, slug)
    if stage == "implement" and str(cid) not in bases:
        code, head = git(root, "rev-parse", "HEAD")
        if code == 0:
            bases[str(cid)] = head.strip()
    save_session(root, slug, sess)


# ---------------------------------------------------------------------------
# Hook: Stop
# ---------------------------------------------------------------------------


def stop_problems(root: Path, slug: str, th: TreeHash) -> list[str]:
    if rolled_back(root, slug):
        return [rollback_msg(slug, root)]
    plan = load_plan(root, slug)
    if plan is None or plan_problem(plan):
        return []  # unreadable or malformed: PreToolUse and the CLI say why; Stop never traps the turn
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
                if os.environ.get("TENTAQLES_HEADLESS") == "1":
                    # Headless: nobody is at a terminal; the stop goes to the human as a decision.
                    fallback = (f"or, if it cannot be fixed, write `build-gate ask {slug} --kind question --text \"override: <the MISSING items and why>\" "
                                "--options override,stop` and end the turn; the human answers it on the hub card (/waiting) and the hub "
                                "resumes the run (without a hub, whoever launched the run answers it). Never name a command for the human to run")
                else:
                    fallback = (f"or, if it cannot be fixed, ask the human to run `build-gate dod {slug} --override \"<their reason>\"` "
                                "in their own terminal (you never run it)")
                problems.append(f"definition of done not proven ({why}): run `build-gate dod {slug}` and fix what it lists, {fallback}")
    return problems


def _stop_counter(root: Path, slug: str, bump: bool = False, reset: bool = False) -> int:
    """Stop blocks of a rolled-back build, kept with the key (its session file can't be trusted)."""
    p = key_dir() / "stop" / f"{project_id(root)}-{slug}"
    rec = read_json(p)
    n = int(rec.get("blocks") or 0) if _sig_ok(rec) else 0
    if reset:
        n = 0
    elif bump:
        n += 1
    if bump or reset:
        write_json(p, sign({"blocks": n}))
    return n


def check_stop(data: dict, root: Path) -> None:
    if os.environ.get("TENTAQLES_HEADLESS_CHILD") == "1":
        return
    if data.get("stop_hook_active"):
        return  # we already blocked this stop once; never trap the session
    session_id = data.get("session_id") or ""
    th = TreeHash(root, timeout=STOP_GIT_TIMEOUT)
    for slug in all_slugs(root):
        problem = state_problem(root, slug)
        if problem:
            # whoever stops, and with or without a (trustworthy) session file: block once
            if _stop_counter(root, slug, bump=True) > MAX_STOP_BLOCKS:
                _stop_counter(root, slug, reset=True)
                continue
            raise Block(f"build '{slug}': {problem}. Stop and ask the human; "
                        "this stop is blocked once; the next one goes through.")
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


def _roots_for(data: dict) -> list[Path]:
    """Every project root the call is judged against: each git root from the file's directory
    upward, each git root from the session's cwd upward (a nested `git init lib/sub` must not
    hide the outer project's state), and CLAUDE_PROJECT_DIR (and its git roots) when set. The
    process cwd is the fallback when nothing else names a root."""
    out: list[Path] = []
    seen: set[str] = set()

    def add(r) -> None:
        if r is None:
            return
        k = canon_path(r)
        if k not in seen:
            seen.add(k)
            out.append(Path(r))

    tool_input = data.get("tool_input") or {}
    fp = tool_input.get("file_path") or tool_input.get("notebook_path")
    if fp:
        for r in find_git_roots(os.path.dirname(norm(fp)) or "."):
            add(r)
    cwd = data.get("cwd")
    if cwd:
        for r in find_git_roots(cwd):
            add(r)
    pd = os.environ.get("CLAUDE_PROJECT_DIR")
    if pd:
        if os.path.isdir(pd):
            add(Path(pd))
        for r in find_git_roots(pd):
            add(r)
    if not out:
        for r in find_git_roots(os.getcwd()):
            add(r)
    return out


def _heartbeat(root: Path) -> None:
    d = root / STATE_DIR
    if d.is_dir():
        try:
            write_json(d / HEARTBEAT, sign({"ts": time.time()}))
        except OSError:
            pass


def hook(data: dict) -> tuple[int, str, str]:
    """Run the dispatcher; an unexpected exception never silently permits an action.
    PreToolUse: refuse (exit 2). Stop: allow (the turn must never be stuck). Either way the
    one-line note names only the exception class, never the traceback."""
    try:
        return _hook(data)
    except Exception as e:  # noqa: BLE001
        name = type(e).__name__
        if (data or {}).get("hook_event_name") == "Stop":
            return 0, "", f"build-gate: internal error ({name}); allowing the stop"
        return 2, "", f"build-gate internal error ({name}); refusing"


def _search_reaches_key(tool: str, inp: dict, cwd) -> bool:
    """Grep/Glob search a tree: refuse when the searched path is the key directory, inside it, or
    any ancestor of it (a pattern such as `**/hmac.key` would reach in), also when `path` is
    omitted and the session's cwd is such an ancestor, and when a Glob pattern itself names it."""
    kd = _key_dir_canon()
    base = str(cwd or os.getcwd())
    raw = str(inp.get("path") or inp.get("file_path") or "")
    _, a = resolve_word(Path(base), raw or base)
    if a == kd or a.startswith(kd + "/") or kd.startswith(a.rstrip("/") + "/"):
        return True
    pat = norm(str(inp.get("pattern") or "")) if tool == "Glob" else ""
    if pat:
        full = pat if _is_abs(pat) else f"{a}/{pat}"
        fn = _nabs(full)
        if any(_glob_segments_match(fn, cand, True) for cls, _, cand in _key_glob_candidates()
               if cls == "key"):
            return True
    return False


def _hook(data: dict) -> tuple[int, str, str]:
    """Pure-ish dispatcher: returns (exit_code, stdout, stderr)."""
    inp0 = data.get("tool_input") or {}
    fp = inp0.get("file_path") or inp0.get("notebook_path") or inp0.get("path")
    if (data.get("tool_name") or "") in ("Grep", "Glob") and data.get("hook_event_name") != "Stop":
        if _search_reaches_key(data.get("tool_name") or "", inp0, data.get("cwd")):
            return 2, "", f"build-gate: {_MESSAGES['key']}"
    if fp and data.get("hook_event_name") != "Stop":
        _, absn = resolve_word(Path(data.get("cwd") or os.getcwd()), str(fp))
        kd = _key_dir_canon()
        a = absn.lower() if os.name == "nt" else absn
        if a == kd or a.startswith(kd + "/"):
            return 2, "", f"build-gate: {_MESSAGES['key']}"
        if (data.get("tool_name") or "") == "Read" and _key_reach(absn) == "anc":
            return 2, "", f"build-gate: {_MESSAGES['key']} (a directory above it)"
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
    roots = [r for r in _roots_for(data)
             if (r / STATE_DIR).is_dir() or (r / PLAN_DIR).is_dir() or recorded_slugs(r)]
    asked = ""
    for root in roots:
        code, out, err = _dispatch(data, root)
        if code != 0:
            return code, out, err
        asked = asked or out
    return 0, asked, ""


def _dispatch(data: dict, root: Path) -> tuple[int, str, str]:
    """Judge one call against one project root."""
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
            check_shell(inp.get("command") or "", root, data.get("cwd"), tool)
        elif tool in ("Write", "Edit", "MultiEdit", "NotebookEdit"):
            gone = rolled_back_slugs(root)
            if gone:
                raise Block(rollback_msg(gone[0], root) + "; until then no file may be written.")
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


DOD_COMMANDS = ("test", "lint", "typecheck")
_TRIVIAL_CMD = re.compile(r"^(?:true|:|exit(?:\s+0)?|/bin/true|/usr/bin/true|cmd\s+/c\s+exit(?:\s+0)?|\$true)$", re.IGNORECASE)


def _project_command_problem(project: dict, warning: str) -> str:
    """'' when the tq-project DoD commands may be frozen, else why not."""
    m = re.search(r"ignored (.+?) \(wrong type or unknown key\)", warning or "")
    dropped = m.group(1).split(", ") if m else []
    for name in DOD_COMMANDS:
        if f"commands.{name}" in dropped:
            return f"commands.{name} must be a shell command string (CLAUDE.md tq-project block)"
    if "commands" in dropped:
        return "commands must be an object of shell command strings (CLAUDE.md tq-project block)"
    for name in DOD_COMMANDS:
        c = (project.get("commands") or {}).get(name)
        if c is not None and _TRIVIAL_CMD.match(c.strip()):
            return f"commands.{name} must be a shell command string that runs the {name}s, not `{c.strip()}` (CLAUDE.md tq-project block)"
    return ""


def reapproval_delta(root: Path, slug: str, plan: dict) -> list[str]:
    """Lines for what a re-approval changes against the previous signed approval (its frozen
    ids and fingerprints): checkpoints removed (with the last status on record), changed, new.
    Empty on a first approval."""
    prev = load_approved(root, slug)
    if not prev or not prev.get("fingerprints"):
        return []
    fps = prev.get("fingerprints") or {}
    by_id = _ids_by_plan(plan.get("checkpoints") or [])
    removed = []
    for fid in frozen_ids(prev):
        if fid in by_id:
            continue
        ev = latest_evidence(root, slug, _id_label(fid))
        removed.append(f"#{fid} ({'passed' if ev and ev.get('ok') else 'not passed'})")
    changed = [f"#{i}" for i, c in by_id.items() if i in fps and fps[i] != plan_fingerprint(c)]
    new = [f"#{i}" for i in by_id if i not in fps]
    out = []
    if removed:
        out.append("removed since the last approval: " + ", ".join(removed))
    if changed:
        out.append("changed: " + ", ".join(changed))
    if new:
        out.append("new: " + ", ".join(new))
    return out


def approval_preview(root: Path, slug: str) -> str:
    """Everything an approval freezes, as the human should read it before confirming:
    each checkpoint's verify commands and the project's definition-of-done config."""
    plan = load_plan(root, slug) or {"checkpoints": []}
    project, pwarn = load_project_config(root)
    lines = [f"Approving '{slug}' freezes:"]
    lines += ["  " + d for d in reapproval_delta(root, slug, plan)]
    lines.append("  verify commands (what 'passing' means per checkpoint):")
    for cp in plan["checkpoints"]:
        cmds = [c for c in (cp.get("verify") or []) if isinstance(c, str) and c.strip()] or ["(none)"]
        lines += [f"    #{cp.get('id')}: {c}" for c in cmds]
    lines.append("  definition of done (CLAUDE.md tq-project block):")
    cmds = project.get("commands") or {}
    for name in DOD_COMMANDS:
        lines.append(f"    {name}: {cmds.get(name) or '(not configured)'}")
    for key in ("architecture_paths", "classified_paths"):
        lines.append(f"    {key}: {', '.join(project.get(key) or []) or '(none)'}")
    lines.append(f"    changelog: {project.get('changelog') or '(none)'}")
    if plan.get("hotfix") is True:
        lines.append("  hotfix: true (the review gate becomes n/a: no critic; the tests still run)")
    if pwarn:
        lines.append(f"  warning: {pwarn}")
    return "\n".join(lines)


def approval_summary(root: Path, slug: str) -> str:
    """approval_preview on one line, definition of done first (a hub note is truncated)."""
    plan = load_plan(root, slug) or {"checkpoints": []}
    project, _ = load_project_config(root)
    cmds = project.get("commands") or {}
    dod = "; ".join(f"{n}: {cmds.get(n) or '(not configured)'}" for n in DOD_COMMANDS)
    verify = "; ".join(f"#{cp.get('id')}: " + " && ".join(c for c in (cp.get("verify") or []) if isinstance(c, str))
                       for cp in plan["checkpoints"])
    extra = ", hotfix (no critic)" if plan.get("hotfix") is True else ""
    return (f"the definition of done ({dod}; changelog: {project.get('changelog') or '(none)'}) "
            f"and the verify commands ({verify}){extra}")


def cmd_approve(root: Path, slug: str) -> str:
    if not isinstance(slug, str) or not SLUG_RE.fullmatch(slug):
        # the same rule as finish: a slug that can't be finished can't be approved either
        raise SystemExit(f"approve refused: {slug!r} isn't a build slug (lowercase letters, digits and hyphens only).")
    plan = load_plan(root, slug)
    if plan is None:
        if _finished_and_cleaned(root, slug):
            raise SystemExit(f"build {slug} is finished; write a new plan to start another")
        raise SystemExit(f"no plan at {PLAN_DIR}/{slug}.json")
    project, pwarn = load_project_config(root)
    problem = _project_command_problem(project, pwarn)
    if problem:
        raise SystemExit(problem)
    verify, fingerprints = {}, {}
    for cp in plan["checkpoints"]:
        cmds = [c for c in (cp.get("verify") or []) if isinstance(c, str) and c.strip()]
        if not cmds:
            raise SystemExit(f"#{cp.get('id')} has no verify commands; every checkpoint needs at least one.")
        verify[str(cp.get("id"))] = cmds
        fingerprints[str(cp.get("id"))] = plan_fingerprint(cp)
    prev = load_approved(root, slug) or {}
    legacy = legacy_approval(root, slug)
    if not prev and legacy is not None:
        # Signed by the plugin before generations: its locks carry over into generation 1.
        prev = {k: legacy[k] for k in ("locked_tests", "tests_locked") if isinstance(legacy.get(k), dict)}
    elif not prev:
        # A rolled-back approval: the locks of the latest generation were recorded with the key.
        rec = _gen_record(root, slug) or {}
        prev = {k: rec[k] for k in ("locked_tests", "tests_locked") if isinstance(rec.get(k), dict)}
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
        "task": plan.get("task") if not task_problem(plan.get("task")) else None,
    }
    approved["project"] = project
    approved["project_warning"] = pwarn
    save_approved(root, slug, approved, finished=False, legacy=legacy is not None)
    if approved["hotfix"]:
        # The planner writes review "pending" (the hook allows n/a only on an approved hotfix
        # plan); the approval that records hotfix: true is what switches the critic off.
        changed = False
        for cp in plan["checkpoints"]:
            rv = (cp.setdefault("gates", {}).setdefault("review", {}))
            if isinstance(rv, dict) and rv.get("status", "pending") == "pending":
                rv["status"] = "n/a"
                changed = True
        if changed:
            write_json(plan_path(root, slug), plan)
    sess = load_session(root, slug)
    sess.pop("finished", None)
    save_session(root, slug, sess)
    write_tq_rules(root)
    _answer_pending(root, slug, "plan-approval", "approve", "terminal")
    lines = [f"approved '{slug}': {len(verify)} checkpoint(s), verify commands frozen"]
    for cid, cmds in verify.items():
        lines += [f"  #{cid}: {c}" for c in cmds]
    dcmds = project.get("commands") or {}
    lines += [f"  dod {name}: {dcmds.get(name) or '(not configured)'}" for name in DOD_COMMANDS]
    return "\n".join(lines)


def cmd_lock_tests(root: Path, slug: str, cid: int, paths: list[str], none: bool,
                   action: str = "ask") -> str:
    if rolled_back(root, slug):
        raise SystemExit(rollback_msg(slug, root) + ".")
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
    if rolled_back(root, slug):
        raise SystemExit(rollback_msg(slug, root) + ".")
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
    if rolled_back(root, slug):
        return 1, rollback_msg(slug, root) + "."
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
    # .tq-breaker/ is outside the tree hash, so it may hold data files only (checked before and after).
    scratch_bad = scratch_problem(root)
    if scratch_bad:
        ok = False
        out_lines.append("FAIL: " + scratch_bad)
    elif tampered:
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
    if ok:
        scratch_bad = scratch_problem(root)
        if scratch_bad:
            ok = False
            out_lines.append("FAIL: " + scratch_bad)
    summary = ("all verify commands passed" if ok else
               (scratch_bad if scratch_bad else
                ("locked tests changed" if tampered else f"exit {results[-1]['exit']}: {results[-1]['cmd']}")))
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


def _finished_and_cleaned(root: Path, slug: str) -> bool:
    rec = _gen_record(root, slug)
    return bool(rec and rec.get("finished")) and not rolled_back(root, slug)


def _test_entries(tests) -> list[dict]:
    out = []
    for t in tests or []:
        if isinstance(t, str):
            out.append({"file": t, "layer": ""})
        elif isinstance(t, dict):
            out.append({"file": str(t.get("file") or ""), "layer": str(t.get("layer") or "")})
    return out


def cmd_records(root: Path, slug: str) -> dict:
    """The gate's own records for the hub's review page (spec A §10): only rows this module signed;
    plan checkpoints are flagged by whether the human's approval still covers them."""
    plan = load_plan(root, slug) or {"checkpoints": []}
    approved = load_approved(root, slug)
    current = (approved or {}).get("approval_id")
    plan_out = {"summary": str(plan.get("summary") or ""), "plain_summary": str(plan.get("plain_summary") or ""),
                "spec": str(plan.get("spec") or ""),
                "checkpoints": [{"id": cp.get("id"), "title": str(cp.get("title") or ""),
                                 "approved": checkpoint_approved(approved, cp)[0], "tests": _test_entries(cp.get("tests"))}
                                for cp in plan.get("checkpoints") or [] if isinstance(cp, dict)]}
    ev = []
    for r in evidence(root, slug):
        ev.append({"checkpoint": r.get("checkpoint"), "ts": r.get("ts"), "tree": r.get("tree"), "ok": bool(r.get("ok")),
                   "current": bool(current) and r.get("approval_id") == current,
                   "summary": redact(str(r.get("summary") or ""))[:300],
                   "results": [{"cmd": str(x.get("cmd") or ""), "exit": x.get("exit"), "seconds": x.get("seconds"),
                                "timed_out": bool(x.get("timed_out"))}
                               for x in (r.get("results") or []) if isinstance(x, dict)]})
    b = _Binding(root, slug)
    revs = []
    for line in read_text(reviews_path(root, slug)).splitlines():
        try:
            rec = json.loads(line)
        except ValueError:
            continue
        if b.ok(rec):
            revs.append({k: rec.get(k) for k in ("checkpoint", "reviewer", "result", "tree", "ts", "human",
                                                 "blockers", "majors", "minors", "unjudged")})
    dod = read_json(dod_path(root, slug))
    dod_out = None
    if bound(root, slug, dod):
        dod_out = {"ok": bool(dod.get("ok")), "tree": dod.get("tree"), "ts": dod.get("ts"), "override": dod.get("override"),
                   "checks": [{"id": str(c.get("id")), "ok": c.get("ok"),
                               "detail": redact(str(c.get("detail") or "").splitlines()[0] if c.get("detail") else "")[:200]}
                              for c in (dod.get("checks") or []) if isinstance(c, dict)]}
    return {"plan": plan_out, "evidence": ev, "reviews": revs, "dod": dod_out}


def cmd_state(root: Path, slug: str) -> dict:
    plan = load_plan(root, slug)
    if plan is None and _finished_and_cleaned(root, slug) and load_approved(root, slug) is None:
        return {"slug": slug, "feature": slug, "workflow": "build", "hotfix": False, "task": None, "approved": False,
                "rolled_back": False, "gen": recorded_gen(root, slug), "halted": None, "finished": True,
                "cleaned": True, "checkpoints": [], "dod": None, "dod_fresh": False, "pending_decisions": [],
                "decisions": [], "bases": review_bases(root, slug), "events_path": norm(events_path())}
    if plan is None:
        raise SystemExit(f"no plan at {PLAN_DIR}/{slug}.json")
    bad = plan_problem(plan)
    if bad:
        raise SystemExit(f"{PLAN_DIR}/{slug}.json is invalid: {bad}.")
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
    dod_out = {"ok": bool(dod.get("ok")), "tree": dod.get("tree"), "override": dod.get("override")} if bound(root, slug, dod) else None
    is_rolled_back = rolled_back(root, slug)
    signed = approved if approved is not None else plan  # the hub sees what the human approved
    return {"slug": slug, "feature": plan.get("feature", slug),
            "workflow": signed.get("workflow") if signed.get("workflow") in WORKFLOWS else "build",
            "hotfix": signed.get("hotfix") is True, "task": signed.get("task") if approved is None or "task" in approved else plan.get("task"),
            "approved": approved is not None,
            "rolled_back": is_rolled_back, "gen": (approved or {}).get("gen"), "cleaned": False,
            "bases": review_bases(root, slug),
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
    approved = load_approved(root, slug) or {}
    base = review_bases(root, slug).get(str(cid)) or approved.get("base") or "HEAD"
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


def _build_base(root: Path, slug: str, approved: dict) -> tuple[str | None, str]:
    """(base, why): the earliest recorded commit of the build (the approval base or a checkpoint
    review base, whichever is an ancestor of all the others). base is None, with the reason, when
    nothing was recorded or any recorded commit is missing from the repository."""
    recorded = []
    for b in [approved.get("base"), *review_bases(root, slug).values()]:
        if isinstance(b, str) and b and b not in recorded:
            recorded.append(b)
    if not recorded:
        return None, "no recorded base commit for this build, so every open decision counts as new"
    for b in recorded:
        if git(root, "rev-parse", "--verify", "-q", b + "^{commit}")[0] != 0:
            return None, f"recorded base {b[:12]} is not in this repository (shallow clone or gc); every open decision counts as new"
    for c in recorded:
        if all(git(root, "merge-base", "--is-ancestor", c, o)[0] == 0 for o in recorded if o != c):
            return c, ""
    return recorded[0], ""


def open_prd_decisions(root: Path, base: str | None, why: str = "") -> tuple[list[str], list[str], str]:
    """(new, pre-existing, why): open NEEDS-DECISION lines of docs/prd.md section 10 now, split by
    whether the base already had them (compared without bullet, checkbox and spacing).
    Without a usable base every open line counts as new."""
    from tentaqles.workflow.prd import PRD_REL, open_decision_lines

    now = open_decision_lines(read_text(root / PRD_REL))
    if not base:
        return now, [], why or "no recorded base commit for this build, so every open decision counts as new"
    code, old_text = git(root, "show", f"{base}:{PRD_REL}")
    # A base without a PRD means the build created it: everything open is new.
    pool = open_decision_lines(old_text) if code == 0 else []
    new, old = [], []
    for ln in now:
        if ln in pool:
            pool.remove(ln)
            old.append(ln)
        else:
            new.append(ln)
    return new, old, ""


def prd_decisions_check(root: Path, slug: str, approved: dict) -> tuple[bool | None, str]:
    """The prd-decisions DoD item. Never raises: an unexpected error is a failed check."""
    try:
        from tentaqles.workflow.prd import PRD_REL

        if not (root / PRD_REL).exists():
            return None, "skip: no docs/prd.md"
        base, why = _build_base(root, slug, approved)
        new, old, why = open_prd_decisions(root, base, why)
        tail_old = ("; still open (pre-existing): " + " | ".join(old)) if old else ""
        if new:
            return False, ((why + ": ") if why else "") + f"{len(new)} NEEDS-DECISION opened during this build, settle or accept at the final review: " + " | ".join(new) + tail_old
        return True, "no new open NEEDS-DECISION" + tail_old
    except Exception as e:  # noqa: BLE001 - a crashed check must still be a recorded failure
        return False, f"could not check the PRD decisions ({type(e).__name__}): {str(e)[:200]}"


def cmd_dod(root: Path, slug: str, override: str | None = None, timeout: float = 900, actor: str = "terminal") -> tuple[int, str]:
    """Definition of done (spec §8.1): run the frozen project commands and check the
    obligations the diff created. Records signed dod.json; the Stop hook reads it."""
    if override is not None and not override.strip():
        raise SystemExit("--override needs a reason; it is logged to docs/LEARNINGS.md and counted by tq insights.")
    plan = load_plan(root, slug)
    if plan is None:
        return 1, f"no plan at {PLAN_DIR}/{slug}.json"
    bad = plan_problem(plan)
    if bad:
        return 1, f"{PLAN_DIR}/{slug}.json is invalid: {bad}."
    if rolled_back(root, slug):
        return 1, rollback_msg(slug, root) + "."
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
    scratch_bad = scratch_problem(root)
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
    scratch_bad = scratch_bad or scratch_problem(root)
    add("breaker_scratch", not scratch_bad, scratch_bad or f"{BREAKER_DIR}/ holds data files only")
    cps = plan.get("checkpoints") or []
    by_id = _ids_by_plan(cps)
    frozen = frozen_ids(approved)
    ids = frozen + [i for i in by_id if i not in frozen]  # frozen first; later additions need re-approval
    unpassed = [_id_label(i) for i in ids if (by_id.get(i) or {}).get("status") != "passed"]
    gone = [_id_label(i) for i in frozen if i not in by_id]
    add("checkpoints", not unpassed, f"{len(ids) - len(unpassed)}/{len(ids)} passed" + (f"; open: {unpassed}" if unpassed else "")
        + (f"; approved but missing from the plan: {gone}" if gone else ""))
    open_reviews = []
    rb = _Binding(root, slug)
    for cp in cps:
        latest: dict[str, dict] = {}
        for line in read_text(reviews_path(root, slug)).splitlines():
            try:
                rec = json.loads(line)
            except ValueError:
                continue
            if rb.ok(rec) and rec.get("checkpoint") == cp.get("id"):
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
    ok_pd, detail_pd = prd_decisions_check(root, slug, approved)
    add("prd-decisions", ok_pd, detail_pd)
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
        if (bound(root, slug, prior) and prior.get("override") and tree is not None and prior.get("tree") == tree
                and prior.get("approval_id") == approved.get("approval_id")):
            kept = prior["override"]
            log_event(root, "dod-override-kept", slug, ok=True)
    effective = override.strip() if override else kept
    rec = {"slug": slug, "ok": ok or effective is not None, "tree": tree, "ts": now_iso(), "checks": checks,
           "approval_id": approved.get("approval_id"), "override": effective}
    write_json(dod_path(root, slug), sign_state(root, slug, rec))
    lines = []
    for c in checks:
        tag = "ok  " if c["ok"] else ("skip" if c["ok"] is None else "MISSING")
        lines.append(f"{tag} {c['id']}: {c['detail']}")
    if override is not None:
        bump_gen(root, slug)
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
    if not bound(root, slug, rec):
        return False, "no definition-of-done record for the current generation of this build"
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
    excluded = _git_exclude(root, [f"/{STATE_DIR}/", f"/{BREAKER_DIR}/", "/.claude/settings.local.json"])
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


def _remove_tree(path: str) -> None:
    """rmtree that never follows a link or junction: a link is removed, not entered."""
    try:
        entries = list(os.scandir(path))
    except OSError:
        return
    for e in entries:
        try:
            if _lstat_is_link(e.path):
                try:
                    os.unlink(e.path)
                except OSError:
                    os.rmdir(e.path)  # a directory junction
            elif e.is_dir(follow_symlinks=False):
                _remove_tree(e.path)
            else:
                os.unlink(e.path)
        except OSError:
            pass
    try:
        os.rmdir(path)
    except OSError:
        pass


def _scratch_deletable(root: Path, slug: str) -> bool:
    """<root>/.tq-breaker/<slug> may be deleted: both are real directories (no link,
    no junction, no reparse point) and the slug dir sits directly in the scratch root."""
    base, scratch = root / BREAKER_DIR, root / BREAKER_DIR / slug
    if _lstat_is_link(str(base)) or not base.is_dir() or _lstat_is_link(str(scratch)) or not scratch.is_dir():
        return False
    try:
        base_r = base.resolve()
        return scratch.resolve().parent == base_r and base_r == root.resolve() / BREAKER_DIR
    except OSError:
        return False


def cmd_finish(root: Path, slug: str) -> str:
    if not isinstance(slug, str) or not SLUG_RE.fullmatch(slug):
        log_event(root, "finish-refused", "-", reason="bad-slug")
        raise SystemExit(f"finish refused: {slug!r} isn't a build slug (lowercase letters, digits and hyphens only).")
    if rolled_back(root, slug):
        raise SystemExit(rollback_msg(slug, root) + " before finishing.")
    sess = load_session(root, slug)
    sess["finished"] = now_iso()
    save_session(root, slug, sess)
    approved = load_approved(root, slug)
    if approved:
        approved["locked_tests"] = {}
        save_approved(root, slug, approved, finished=True)
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
    base, scratch = root / BREAKER_DIR, root / BREAKER_DIR / slug
    if os.path.lexists(scratch) or (os.path.lexists(base) and _lstat_is_link(str(base))):
        if _scratch_deletable(root, slug):
            _remove_tree(str(scratch))
            log_event(root, "breaker-scratch-removed", slug)
        else:
            log_event(root, "breaker-scratch-refused", slug)
            msg += f"\nbreaker scratch dir left in place: {BREAKER_DIR}/{slug} is a link or not a plain directory"
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


def hub_actor_ok(action: str, slug: str, root: Path | None = None) -> str | None:
    """The hub actor a valid token names, or None. A rolled-back state accepts no hub actor:
    the human re-approves in their own terminal."""
    actor = (os.environ.get("TQ_BUILD_ACTOR") or "").strip()
    tok = (os.environ.get("TQ_BUILD_TOKEN") or "").strip()
    key = _key()
    if not actor or not tok or key is None:
        return None
    if root is not None and rolled_back(root, slug):
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
        except Exception as e:  # noqa: BLE001  # input unreadable before any event is known: never stall
            code, out, err = 0, "", f"build-gate: internal error ({type(e).__name__}); allowing"
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
    stt.add_argument("--records", action="store_true", help="add the gate's signed records (the hub's review page)")
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
        print(approval_preview(root, a.slug))
        if not (hub_actor_ok("approve", a.slug, root) or human_confirm(f"approve the plan '{a.slug}' and freeze its verify commands")):
            return 2
        print(cmd_approve(root, a.slug))
    elif a.cmd == "status":
        print(cmd_status(root, a.slug))
    elif a.cmd == "finish":
        if not (hub_actor_ok("finish", a.slug, root) or human_confirm(f"finish '{a.slug}' and remove its hooks and test locks")):
            return 2
        print(cmd_finish(root, a.slug))
    elif a.cmd == "verify":
        code, msg = cmd_verify(root, a.slug, a.id, a.timeout)
        print(msg)
        return code
    elif a.cmd == "lock-tests":
        print(cmd_lock_tests(root, a.slug, a.id, a.paths, a.none, a.action))
    elif a.cmd == "unlock-tests":
        if not (hub_actor_ok("unlock-tests", a.slug, root) or human_confirm(f"unlock {', '.join(a.paths)} in '{a.slug}'")):
            return 2
        print(cmd_unlock_tests(root, a.slug, a.paths))
    elif a.cmd == "review-bundle":
        print(cmd_review_bundle(root, a.slug, a.id))
    elif a.cmd == "record-review":
        print(cmd_record_review(root, a.slug, a.id, a.reviewer, a.result,
                                {k: getattr(a, k) for k in ("blockers", "majors", "minors", "unjudged")}))
    elif a.cmd == "judge":
        if not (hub_actor_ok("judge", a.slug, root) or human_confirm(f"record a human judgement for {a.reviewer} on '{a.slug}' #{a.id}: {a.result}")):
            return 2
        print(cmd_judge(root, a.slug, a.id, a.reviewer, a.result, a.note))
    elif a.cmd == "dod":
        if a.override is not None and not (hub_actor_ok("dod", a.slug, root) or human_confirm(f"accept the missing definition-of-done items for '{a.slug}'")):
            return 2
        code, msg = cmd_dod(root, a.slug, a.override, a.timeout, actor=hub_actor_ok("dod", a.slug, root) or "terminal")
        print(msg)
        return code
    elif a.cmd == "ask":
        print(cmd_ask(root, a.slug, a.kind, a.text, a.options.split(","), a.checkpoint, a.reviewer))
    elif a.cmd == "answer":
        actor = hub_actor_ok("answer", a.slug, root)
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
        if a.records:
            st["records"] = cmd_records(root, a.slug)
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
