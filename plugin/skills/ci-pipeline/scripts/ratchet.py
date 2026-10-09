#!/usr/bin/env python3
"""Ratchet quality gate: fail CI when any metric gets worse than the baseline.

Stdlib only, Python 3.8+. Copy this file into the project (for example
``.github/quality/ratchet.py``) and commit a ``baseline.json`` next to it.

Subcommands
-----------
collect   Turn tool reports into a metrics file::

              ratchet.py collect --out current.json \\
                  --eslint eslint.json --coverage-summary coverage/coverage-summary.json \\
                  --jscpd jscpd/jscpd-report.json --metric largest_file_lines=812:lower

compare   Compare current metrics with the baseline. Exit 0 when nothing got
          worse, 1 on a regression, 2 on bad input::

              ratchet.py compare --baseline baseline.json --current current.json \\
                  [--update] [--require-tight] [--report report.json] [--summary summary.md]

          --update writes improvements (and new metrics) back into the
          baseline, only when nothing regressed. The baseline never loosens.

init      Create the first baseline from a metrics file (refuses to
          overwrite unless --force).

File format (baseline and current)::

    {"version": 1,
     "metrics": {"lint_errors":  {"value": 12,   "direction": "lower"},
                 "coverage_pct": {"value": 71.4, "direction": "higher", "tolerance": 0.1}}}

A current file may also be a flat ``{"lint_errors": 11}`` map; directions then
come from the baseline. ``tolerance`` is an absolute slack allowed before a
change counts as a regression (default 0).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import xml.etree.ElementTree as ET
from typing import Dict, List, Optional, Tuple

VERSION = 1
EPS = 1e-9

# Directions for metrics collect knows how to produce.
DEFAULT_DIRECTIONS = {
    "lint_errors": "lower",
    "lint_warnings": "lower",
    "duplication_pct": "lower",
    "complex_functions": "lower",
    "coverage_pct": "higher",
}

EXIT_OK, EXIT_REGRESSION, EXIT_USAGE = 0, 1, 2


class RatchetError(Exception):
    """Bad input: missing file, wrong format, unknown direction."""


# --------------------------------------------------------------------------
# metric files
# --------------------------------------------------------------------------


def _read_json(path: str):
    try:
        with open(path, encoding="utf-8") as fh:
            return json.load(fh)
    except FileNotFoundError:
        raise RatchetError(f"file not found: {path}")
    except json.JSONDecodeError as exc:
        raise RatchetError(f"{path}: invalid JSON ({exc})")


def normalize(data, source: str, defaults: Optional[Dict[str, dict]] = None) -> Dict[str, dict]:
    """Return {name: {"value": float, "direction": str, "tolerance": float}}."""
    if not isinstance(data, dict):
        raise RatchetError(f"{source}: expected a JSON object")
    raw = data.get("metrics", data)
    if not isinstance(raw, dict):
        raise RatchetError(f"{source}: 'metrics' must be an object")
    out: Dict[str, dict] = {}
    for name, spec in raw.items():
        if name == "version":
            continue
        if isinstance(spec, dict):
            value = spec.get("value")
            direction = spec.get("direction")
            tolerance = spec.get("tolerance", 0)
        else:
            value, direction, tolerance = spec, None, 0
        if direction is None:
            direction = ((defaults or {}).get(name) or {}).get("direction") or DEFAULT_DIRECTIONS.get(name)
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise RatchetError(f"{source}: metric {name!r} needs a numeric value")
        if direction not in ("lower", "higher"):
            raise RatchetError(f"{source}: metric {name!r} needs direction 'lower' or 'higher'")
        if isinstance(tolerance, bool) or not isinstance(tolerance, (int, float)) or tolerance < 0:
            raise RatchetError(f"{source}: metric {name!r} has an invalid tolerance")
        out[name] = {"value": value, "direction": direction, "tolerance": tolerance}
    return out


def load_metrics(path: str, defaults: Optional[Dict[str, dict]] = None) -> Dict[str, dict]:
    return normalize(_read_json(path), path, defaults)


def write_metrics(path: str, metrics: Dict[str, dict]) -> None:
    payload = {"version": VERSION, "metrics": {}}
    for name in sorted(metrics):
        spec = metrics[name]
        entry = {"value": spec["value"], "direction": spec["direction"]}
        if spec.get("tolerance"):
            entry["tolerance"] = spec["tolerance"]
        payload["metrics"][name] = entry
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8", newline="\n") as fh:
        json.dump(payload, fh, indent=2)
        fh.write("\n")
    os.replace(tmp, path)


# --------------------------------------------------------------------------
# compare
# --------------------------------------------------------------------------


def compare(baseline: Dict[str, dict], current: Dict[str, dict]) -> List[dict]:
    """One row per metric: status is regressed | improved | same | new | missing."""
    rows = []
    for name in sorted(set(baseline) | set(current)):
        b, c = baseline.get(name), current.get(name)
        if b is None:
            rows.append({"metric": name, "baseline": None, "current": c["value"],
                         "direction": c["direction"], "status": "new"})
            continue
        if c is None:
            rows.append({"metric": name, "baseline": b["value"], "current": None,
                         "direction": b["direction"], "status": "missing"})
            continue
        delta = c["value"] - b["value"]
        worse = delta if b["direction"] == "lower" else -delta
        if worse > b["tolerance"] + EPS:
            status = "regressed"
        elif worse < -EPS:
            status = "improved"
        else:
            status = "same"
        rows.append({"metric": name, "baseline": b["value"], "current": c["value"],
                     "direction": b["direction"], "delta": delta, "status": status})
    return rows


def tightened(baseline: Dict[str, dict], current: Dict[str, dict], rows: List[dict]) -> Dict[str, dict]:
    """Baseline with improved values locked in and new metrics added."""
    out = {k: dict(v) for k, v in baseline.items()}
    for row in rows:
        name = row["metric"]
        if row["status"] == "improved":
            out[name]["value"] = current[name]["value"]
        elif row["status"] == "new":
            out[name] = dict(current[name])
    return out


def _fmt(v) -> str:
    if v is None:
        return "-"
    if isinstance(v, float):
        return f"{v:.2f}".rstrip("0").rstrip(".")
    return str(v)


def render_markdown(rows: List[dict], failed: bool) -> str:
    icon = {"regressed": "FAIL", "missing": "FAIL", "improved": "better",
            "same": "ok", "new": "new"}
    lines = ["## Ratchet quality gate: " + ("FAILED" if failed else "passed"), "",
             "| Metric | Baseline | Current | Goal | Status |", "|---|---|---|---|---|"]
    for r in rows:
        goal = "lower is better" if r["direction"] == "lower" else "higher is better"
        lines.append(f"| {r['metric']} | {_fmt(r['baseline'])} | {_fmt(r['current'])} | {goal} | {icon[r['status']]} |")
    lines.append("")
    if any(r["status"] == "missing" for r in rows):
        lines.append("A baseline metric was not produced this run: the collector step is broken or a report is missing.")
    if any(r["status"] == "regressed" for r in rows):
        lines.append("A metric got worse. Fix it in this PR; the baseline only moves in the good direction.")
    if any(r["status"] in ("improved", "new") for r in rows):
        lines.append("Run `ratchet.py compare ... --update` and commit baseline.json to lock in the improvement.")
    return "\n".join(lines) + "\n"


# --------------------------------------------------------------------------
# collect: tool report parsers
# --------------------------------------------------------------------------


def parse_eslint(path: str) -> Dict[str, float]:
    data = _read_json(path)
    if not isinstance(data, list):
        raise RatchetError(f"{path}: expected ESLint JSON formatter output (a list)")
    return {"lint_errors": sum(int(f.get("errorCount", 0)) for f in data),
            "lint_warnings": sum(int(f.get("warningCount", 0)) for f in data)}


def parse_json_list_len(path: str, name: str = "lint_errors") -> Dict[str, float]:
    """ruff --output-format json, or any report that is a JSON list of issues."""
    data = _read_json(path)
    if isinstance(data, dict) and isinstance(data.get("Issues"), list):  # golangci-lint
        data = data["Issues"]
    if not isinstance(data, list):
        raise RatchetError(f"{path}: expected a JSON list of issues")
    return {name: len(data)}


def parse_coverage_summary(path: str) -> Dict[str, float]:
    """istanbul json-summary (Jest, Vitest, c8, nyc)."""
    data = _read_json(path)
    try:
        return {"coverage_pct": float(data["total"]["lines"]["pct"])}
    except (KeyError, TypeError, ValueError):
        raise RatchetError(f"{path}: expected istanbul json-summary with total.lines.pct")


def parse_cobertura(path: str) -> Dict[str, float]:
    """Cobertura XML (pytest-cov --cov-report=xml, coverage.py, many others).

    The file is produced by the CI job itself, not uploaded by users; the
    stdlib parser does not resolve external entities, and the script stays
    dependency-free on purpose (no defusedxml).
    """
    try:
        root = ET.parse(path).getroot()
    except FileNotFoundError:
        raise RatchetError(f"file not found: {path}")
    except ET.ParseError as exc:
        raise RatchetError(f"{path}: invalid XML ({exc})")
    rate = root.get("line-rate")
    if rate is None:
        raise RatchetError(f"{path}: no line-rate attribute on the root element")
    return {"coverage_pct": round(float(rate) * 100, 2)}


def parse_go_cover_func(path: str) -> Dict[str, float]:
    """Output of `go tool cover -func=cover.out` (last line: total: ... 81.3%)."""
    try:
        with open(path, encoding="utf-8") as fh:
            lines = fh.read().splitlines()
    except FileNotFoundError:
        raise RatchetError(f"file not found: {path}")
    for line in reversed(lines):
        if line.startswith("total:"):
            return {"coverage_pct": float(line.split()[-1].rstrip("%"))}
    raise RatchetError(f"{path}: no 'total:' line from go tool cover -func")


def parse_jscpd(path: str) -> Dict[str, float]:
    data = _read_json(path)
    try:
        return {"duplication_pct": float(data["statistics"]["total"]["percentage"])}
    except (KeyError, TypeError, ValueError):
        raise RatchetError(f"{path}: expected jscpd JSON report with statistics.total.percentage")


def parse_radon_cc(path: str, threshold: int = 10) -> Dict[str, float]:
    """radon cc -j: count blocks with cyclomatic complexity above threshold."""
    data = _read_json(path)
    if not isinstance(data, dict):
        raise RatchetError(f"{path}: expected radon cc -j output (an object)")
    count = 0
    for blocks in data.values():
        if not isinstance(blocks, list):
            continue  # {"error": ...} entries for unparsable files
        count += sum(1 for b in blocks if isinstance(b, dict) and b.get("complexity", 0) > threshold)
    return {"complex_functions": count}


def count_lines(path: str) -> int:
    """Non-empty lines: gocyclo -over N, staticcheck -f json (one issue per line)."""
    try:
        with open(path, encoding="utf-8") as fh:
            return sum(1 for line in fh if line.strip())
    except FileNotFoundError:
        raise RatchetError(f"file not found: {path}")


def _split_assignment(text: str, flag: str) -> Tuple[str, str]:
    if "=" not in text:
        raise RatchetError(f"{flag} expects NAME=VALUE, got {text!r}")
    name, value = text.split("=", 1)
    if not name:
        raise RatchetError(f"{flag} expects NAME=VALUE, got {text!r}")
    return name, value


def _split_direction(value: str, name: str, flag: str) -> Tuple[str, Optional[str]]:
    """Split an optional ':lower' / ':higher' suffix (paths may contain ':' on Windows)."""
    head, sep, tail = value.rpartition(":")
    if sep and tail in ("lower", "higher"):
        return head, tail
    return value, None


def collect(args) -> Dict[str, dict]:
    values: Dict[str, float] = {}
    directions: Dict[str, str] = {}
    for path in args.eslint or []:
        for k, v in parse_eslint(path).items():
            values[k] = values.get(k, 0) + v
    for path in args.ruff or []:
        values["lint_errors"] = values.get("lint_errors", 0) + parse_json_list_len(path)["lint_errors"]
    for path in args.golangci or []:
        values["lint_errors"] = values.get("lint_errors", 0) + parse_json_list_len(path)["lint_errors"]
    if args.coverage_summary:
        values.update(parse_coverage_summary(args.coverage_summary))
    if args.cobertura:
        values.update(parse_cobertura(args.cobertura))
    if args.go_cover:
        values.update(parse_go_cover_func(args.go_cover))
    if args.jscpd:
        values.update(parse_jscpd(args.jscpd))
    if args.radon_cc:
        values.update(parse_radon_cc(args.radon_cc, args.complexity_threshold))
    for item in args.count_lines or []:
        name, rest = _split_assignment(item, "--count-lines")
        path, direction = _split_direction(rest, name, "--count-lines")
        values[name] = count_lines(path)
        directions[name] = direction or "lower"
    for item in args.metric or []:
        name, rest = _split_assignment(item, "--metric")
        raw, direction = _split_direction(rest, name, "--metric")
        try:
            values[name] = float(raw) if any(ch in raw for ch in ".eE") else int(raw)
        except ValueError:
            raise RatchetError(f"--metric {name}: {raw!r} is not a number")
        if direction:
            directions[name] = direction
    if not values:
        raise RatchetError("collect: no inputs given")
    out = {}
    for name, value in values.items():
        direction = directions.get(name) or DEFAULT_DIRECTIONS.get(name)
        if direction is None:
            raise RatchetError(f"metric {name!r} needs a direction (NAME=VALUE:lower|higher)")
        out[name] = {"value": value, "direction": direction, "tolerance": 0}
    return out


# --------------------------------------------------------------------------
# CLI
# --------------------------------------------------------------------------


def _write_text(path: str, text: str) -> None:
    with open(path, "a" if path == os.environ.get("GITHUB_STEP_SUMMARY") else "w",
              encoding="utf-8", newline="\n") as fh:
        fh.write(text)


def cmd_compare(args) -> int:
    baseline = load_metrics(args.baseline)
    current = load_metrics(args.current, defaults=baseline)
    rows = compare(baseline, current)
    regressed = [r for r in rows if r["status"] in ("regressed", "missing")]
    loose = [r for r in rows if r["status"] in ("improved", "new")]
    failed = bool(regressed) or (args.require_tight and bool(loose) and not args.update)
    md = render_markdown(rows, failed)
    sys.stdout.write(md)
    if args.summary:
        _write_text(args.summary, md)
    if args.report:
        with open(args.report, "w", encoding="utf-8", newline="\n") as fh:
            json.dump({"passed": not failed, "rows": rows}, fh, indent=2)
            fh.write("\n")
    if args.update and not regressed and loose:
        write_metrics(args.baseline, tightened(baseline, current, rows))
        sys.stdout.write(f"baseline updated: {args.baseline}\n")
    if args.require_tight and loose and not args.update:
        sys.stdout.write("--require-tight: commit the tightened baseline (run with --update).\n")
    return EXIT_REGRESSION if failed else EXIT_OK


def cmd_collect(args) -> int:
    metrics = collect(args)
    write_metrics(args.out, metrics)
    for name in sorted(metrics):
        sys.stdout.write(f"{name} = {_fmt(metrics[name]['value'])}\n")
    return EXIT_OK


def cmd_init(args) -> int:
    if os.path.exists(args.baseline) and not args.force:
        raise RatchetError(f"{args.baseline} exists; use --force to overwrite (compare --update only ever tightens)")
    write_metrics(args.baseline, load_metrics(args.current))
    sys.stdout.write(f"baseline written: {args.baseline}\n")
    return EXIT_OK


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog="ratchet.py", description="Ratchet quality gate.")
    sub = p.add_subparsers(dest="cmd", required=True)

    c = sub.add_parser("compare", help="compare current metrics against the baseline")
    c.add_argument("--baseline", required=True)
    c.add_argument("--current", required=True)
    c.add_argument("--update", action="store_true", help="lock in improvements when nothing regressed")
    c.add_argument("--require-tight", action="store_true",
                   help="also fail when a metric improved but the baseline was not updated")
    c.add_argument("--report", help="write a machine-readable JSON report here")
    c.add_argument("--summary", help="write the Markdown table here (e.g. $GITHUB_STEP_SUMMARY)")
    c.set_defaults(func=cmd_compare)

    k = sub.add_parser("collect", help="build a metrics file from tool reports")
    k.add_argument("--out", required=True)
    k.add_argument("--eslint", action="append", help="ESLint -f json output")
    k.add_argument("--ruff", action="append", help="ruff check --output-format json output")
    k.add_argument("--golangci", action="append", help="golangci-lint JSON output")
    k.add_argument("--coverage-summary", help="istanbul coverage-summary.json")
    k.add_argument("--cobertura", help="Cobertura coverage.xml")
    k.add_argument("--go-cover", help="go tool cover -func output")
    k.add_argument("--jscpd", help="jscpd JSON report")
    k.add_argument("--radon-cc", help="radon cc -j output")
    k.add_argument("--complexity-threshold", type=int, default=10)
    k.add_argument("--count-lines", action="append", metavar="NAME=FILE[:dir]",
                   help="metric = non-empty lines in FILE (default direction lower)")
    k.add_argument("--metric", action="append", metavar="NAME=VALUE[:dir]", help="literal metric")
    k.set_defaults(func=cmd_collect)

    i = sub.add_parser("init", help="create the first baseline")
    i.add_argument("--baseline", required=True)
    i.add_argument("--current", required=True)
    i.add_argument("--force", action="store_true")
    i.set_defaults(func=cmd_init)
    return p


def main(argv: Optional[List[str]] = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        return args.func(args)
    except RatchetError as exc:
        sys.stderr.write(f"ratchet: {exc}\n")
        return EXIT_USAGE


if __name__ == "__main__":
    sys.exit(main())
