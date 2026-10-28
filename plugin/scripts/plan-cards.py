#!/usr/bin/env python3
"""Turn one PRD release into hub feature cards (spec B §9, /tentaqles:plan-cards).

For every FR of --release that has no card id in PRD §11 yet, runs

    tq hub add "<FR-id> <title>" --kind feature --parent <card> --project <dir> --body-file <tmp> --json

and records the new card id in §11 right away, so an interrupted run resumes without
duplicates. Never tags a card agent-ready (that is /tentaqles:refine). --dry-run prints
the plan and touches nothing.

Prints one JSON object: {release, dry_run, created, skipped, planned}.
Exit 0 done, 1 a tq call failed (cards made so far are recorded), 2 usage or PRD error,
3 the PRD is not planning-ready. Stdlib only.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import argparse
import json
import re
import shutil
import subprocess
import tempfile
from pathlib import Path

from tentaqles.workflow.prd import (
    CARD_ID_RE,
    card_body,
    fr_cards,
    parse_frs,
    record_cards,
    release_frs,
    text_status,
)

TQ_CALL_TIMEOUT = 60  # seconds per tq hub add (the call itself gets --timeout 20s)


class UsageError(Exception):
    pass


def _tq() -> str:
    found = os.environ.get("TQ_BIN") or shutil.which("tq")
    if not found:
        raise UsageError("tq is not installed or not on PATH")
    return found


def _check_args(a: argparse.Namespace) -> None:
    if not CARD_ID_RE.fullmatch(a.parent):
        raise UsageError(f"--parent must be a hub card id like c-xxxxxxxxxxxx, got {a.parent!r}")
    proj = a.project.replace("\\", "/").strip()
    if not proj or proj.startswith("/") or re.match(r"^[A-Za-z]:", proj) or ".." in proj.split("/"):
        raise UsageError(f"--project must be a folder relative to the workspace root, got {a.project!r}")
    a.project = proj.rstrip("/")


def _read(prd: Path) -> str:
    try:
        raw = prd.read_bytes()
    except OSError as e:
        raise UsageError(f"cannot read the PRD {prd}: {e.strerror or e}") from None
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError:
        raise UsageError(f"the PRD {prd} is not UTF-8") from None


def _write(prd: Path, text: str) -> None:
    tmp = prd.with_name(f".{prd.name}.plan-cards.tmp")
    tmp.write_bytes(text.encode("utf-8"))
    os.replace(tmp, prd)


def _command(title: str, a: argparse.Namespace, body_file: str) -> list[str]:
    return ["hub", "add", title, "--kind", "feature", "--parent", a.parent, "--project", a.project,
            "--body-file", body_file, "--json", "--timeout", "20s"]


def _add(tq: str, cmd: list[str], body: str) -> str:
    fd, path = tempfile.mkstemp(prefix="tq-plan-card-", suffix=".md")
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as f:
            f.write(body)
        cmd = [*cmd[:cmd.index("--body-file") + 1], path, *cmd[cmd.index("--body-file") + 2:]]
        try:
            p = subprocess.run([tq, *cmd], capture_output=True, text=True, encoding="utf-8",
                               errors="replace", timeout=TQ_CALL_TIMEOUT)
        except subprocess.TimeoutExpired:
            raise RuntimeError(f"tq hub add timed out after {TQ_CALL_TIMEOUT}s") from None
        except OSError as e:
            raise RuntimeError(f"cannot run tq: {e}") from None
        if p.returncode != 0:
            msg = (p.stderr or p.stdout).strip().splitlines()
            raise RuntimeError(msg[-1] if msg else f"tq hub add exited {p.returncode}")
        try:
            card = json.loads(p.stdout)["id"]
        except (ValueError, KeyError, TypeError):
            raise RuntimeError("tq hub add did not print a card as JSON") from None
        if not isinstance(card, str) or not CARD_ID_RE.fullmatch(card):
            raise RuntimeError(f"tq hub add printed an unexpected card id {card!r}")
        return card
    finally:
        try:
            os.unlink(path)
        except OSError:
            pass


def main() -> int:
    ap = argparse.ArgumentParser(prog="plan-cards", description=__doc__.split("\n\n")[0])
    ap.add_argument("--prd", default=os.path.join("docs", "prd.md"), help="the PRD file (default docs/prd.md)")
    ap.add_argument("--release", required=True, help='release name from PRD §11 ("1", "Release 1"), or "all"')
    ap.add_argument("--parent", required=True, help="the project card the feature cards hang under")
    ap.add_argument("--project", required=True, help="the project folder relative to the workspace root")
    ap.add_argument("--dry-run", action="store_true", help="print the plan; call nothing, write nothing")
    a = ap.parse_args()

    out: dict = {"release": a.release, "dry_run": a.dry_run, "created": [], "skipped": [], "planned": []}
    try:
        _check_args(a)
        prd = Path(a.prd)
        text = _read(prd)
        st = text_status(text)
        if not st["planning_ready"]:
            why = []
            if st["open_decisions"]:
                why.append(f"{st['open_decisions']} open NEEDS-DECISION item(s) in §10")
            if not st["has_section_10"]:
                why.append("no §10")
            if st["frs_without_checks"]:
                why.append("no acceptance checks on " + ", ".join(st["frs_without_checks"]))
            if not st["frs"]:
                why.append("no FRs in §4")
            print(f"plan-cards: the PRD is not planning-ready ({'; '.join(why)}); run /tentaqles:prd first",
                  file=sys.stderr)
            return 3
        try:
            ids = release_frs(text, a.release)
        except KeyError as e:
            raise UsageError(e.args[0]) from None
        except ValueError as e:
            raise UsageError(str(e)) from None
        frs = {fr["id"]: fr for fr in parse_frs(text)}
        have = fr_cards(text)
        todo = []
        for fr_id in ids:
            if fr_id in have:
                out["skipped"].append({"fr": fr_id, "card": have[fr_id]})
            else:
                todo.append(frs[fr_id])
        try:
            prd_rel = prd.resolve().relative_to(Path.cwd().resolve()).as_posix()
        except ValueError:
            prd_rel = a.prd.replace("\\", "/")
        if a.dry_run:
            for fr in todo:
                title = f"{fr['id']} {fr['title']}".strip()
                out["planned"].append({"fr": fr["id"], "title": title, "body": card_body(fr, prd_rel, a.release),
                                       "command": ["tq", *_command(title, a, "<body file>")]})
            print(json.dumps(out, ensure_ascii=False, indent=2))
            return 0
        tq = _tq() if todo else ""
    except UsageError as e:
        print(f"plan-cards: {e}", file=sys.stderr)
        return 2

    for fr in todo:
        title = f"{fr['id']} {fr['title']}".strip()
        try:
            card = _add(tq, _command(title, a, "-"), card_body(fr, prd_rel, a.release))
        except RuntimeError as e:
            print(f"plan-cards: {fr['id']}: {e}", file=sys.stderr)
            print(json.dumps(out, ensure_ascii=False, indent=2))
            return 1
        # Record at once: a later failure must not leave a card the PRD does not know about.
        out["created"].append({"fr": fr["id"], "card": card, "title": title})
        try:
            _write(prd, record_cards(_read(prd), {fr["id"]: card}))
        except (OSError, UsageError, ValueError) as e:
            print(f"plan-cards: made {card} for {fr['id']} but could not record it in §11 ({e}); "
                  f"add the line '- {fr['id']}: {card}' to the tq:cards block by hand", file=sys.stderr)
            print(json.dumps(out, ensure_ascii=False, indent=2))
            return 1
    print(json.dumps(out, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
