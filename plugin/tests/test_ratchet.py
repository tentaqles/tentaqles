"""Tests for the ci-pipeline skill's ratchet quality gate script."""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path

import pytest

SCRIPT = Path(__file__).resolve().parent.parent / "skills" / "ci-pipeline" / "scripts" / "ratchet.py"


@pytest.fixture(scope="module")
def ratchet():
    spec = importlib.util.spec_from_file_location("ratchet", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def _write(path: Path, data) -> Path:
    path.write_text(json.dumps(data), encoding="utf-8")
    return path


def _baseline(tmp_path: Path) -> Path:
    return _write(tmp_path / "baseline.json", {
        "version": 1,
        "metrics": {
            "lint_errors": {"value": 10, "direction": "lower"},
            "coverage_pct": {"value": 70.0, "direction": "higher", "tolerance": 0.1},
        },
    })


def _run(ratchet, *argv: str) -> int:
    return ratchet.main(list(argv))


# --------------------------------------------------------------------------
# compare
# --------------------------------------------------------------------------


def test_same_values_pass(ratchet, tmp_path, capsys):
    base = _baseline(tmp_path)
    cur = _write(tmp_path / "cur.json", {"lint_errors": 10, "coverage_pct": 70.0})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur)) == 0
    assert "passed" in capsys.readouterr().out


def test_regression_fails(ratchet, tmp_path):
    base = _baseline(tmp_path)
    cur = _write(tmp_path / "cur.json", {"lint_errors": 11, "coverage_pct": 70.0})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur)) == 1


def test_higher_is_better_regression_fails(ratchet, tmp_path):
    base = _baseline(tmp_path)
    cur = _write(tmp_path / "cur.json", {"lint_errors": 5, "coverage_pct": 69.5})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur)) == 1


def test_tolerance_absorbs_small_wobble(ratchet, tmp_path):
    base = _baseline(tmp_path)
    cur = _write(tmp_path / "cur.json", {"lint_errors": 10, "coverage_pct": 69.95})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur)) == 0


def test_missing_metric_fails(ratchet, tmp_path, capsys):
    base = _baseline(tmp_path)
    cur = _write(tmp_path / "cur.json", {"lint_errors": 10})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur)) == 1
    assert "collector" in capsys.readouterr().out


def test_improvement_passes_and_update_tightens(ratchet, tmp_path):
    base = _baseline(tmp_path)
    cur = _write(tmp_path / "cur.json", {"metrics": {
        "lint_errors": {"value": 7, "direction": "lower"},
        "coverage_pct": {"value": 72.5, "direction": "higher"},
        "files_over_500_lines": {"value": 3, "direction": "lower"},
    }})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur), "--update") == 0
    saved = json.loads(base.read_text(encoding="utf-8"))["metrics"]
    assert saved["lint_errors"]["value"] == 7
    assert saved["coverage_pct"]["value"] == 72.5
    assert saved["coverage_pct"]["tolerance"] == 0.1  # preserved
    assert saved["files_over_500_lines"] == {"value": 3, "direction": "lower"}


def test_update_never_loosens_and_skips_on_regression(ratchet, tmp_path):
    base = _baseline(tmp_path)
    before = base.read_text(encoding="utf-8")
    cur = _write(tmp_path / "cur.json", {"lint_errors": 3, "coverage_pct": 60.0})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur), "--update") == 1
    assert base.read_text(encoding="utf-8") == before


def test_require_tight_fails_until_baseline_committed(ratchet, tmp_path):
    base = _baseline(tmp_path)
    cur = _write(tmp_path / "cur.json", {"lint_errors": 9, "coverage_pct": 70.0})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur), "--require-tight") == 1
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur), "--require-tight", "--update") == 0
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur), "--require-tight") == 0


def test_report_and_summary_written(ratchet, tmp_path):
    base = _baseline(tmp_path)
    cur = _write(tmp_path / "cur.json", {"lint_errors": 12, "coverage_pct": 70.0})
    report, summary = tmp_path / "report.json", tmp_path / "summary.md"
    rc = _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur),
              "--report", str(report), "--summary", str(summary))
    assert rc == 1
    data = json.loads(report.read_text(encoding="utf-8"))
    assert data["passed"] is False
    statuses = {r["metric"]: r["status"] for r in data["rows"]}
    assert statuses == {"lint_errors": "regressed", "coverage_pct": "same"}
    assert "| lint_errors | 10 | 12 |" in summary.read_text(encoding="utf-8")


@pytest.mark.parametrize("payload", [
    [1, 2],
    {"metrics": {"x": {"value": "ten", "direction": "lower"}}},
    {"metrics": {"x": {"value": 1, "direction": "sideways"}}},
    {"metrics": {"x": {"value": 1}}},  # unknown metric, no direction
])
def test_bad_baseline_is_usage_error(ratchet, tmp_path, payload):
    base = _write(tmp_path / "baseline.json", payload)
    cur = _write(tmp_path / "cur.json", {"x": 1})
    assert _run(ratchet, "compare", "--baseline", str(base), "--current", str(cur)) == 2


def test_missing_file_is_usage_error(ratchet, tmp_path):
    assert _run(ratchet, "compare", "--baseline", str(tmp_path / "nope.json"),
                "--current", str(tmp_path / "nope2.json")) == 2


# --------------------------------------------------------------------------
# collect
# --------------------------------------------------------------------------


def test_collect_node_reports(ratchet, tmp_path):
    eslint = _write(tmp_path / "eslint.json", [
        {"filePath": "a.ts", "errorCount": 2, "warningCount": 1},
        {"filePath": "b.ts", "errorCount": 3, "warningCount": 0},
    ])
    cov = _write(tmp_path / "coverage-summary.json", {"total": {"lines": {"pct": 81.25}}})
    jscpd = _write(tmp_path / "jscpd-report.json", {"statistics": {"total": {"percentage": 4.5}}})
    out = tmp_path / "current.json"
    assert _run(ratchet, "collect", "--out", str(out), "--eslint", str(eslint),
                "--coverage-summary", str(cov), "--jscpd", str(jscpd),
                "--metric", "largest_file_lines=812:lower") == 0
    m = json.loads(out.read_text(encoding="utf-8"))["metrics"]
    assert m["lint_errors"] == {"value": 5, "direction": "lower"}
    assert m["lint_warnings"]["value"] == 1
    assert m["coverage_pct"] == {"value": 81.25, "direction": "higher"}
    assert m["duplication_pct"]["value"] == 4.5
    assert m["largest_file_lines"] == {"value": 812, "direction": "lower"}


def test_collect_python_reports(ratchet, tmp_path):
    ruff = _write(tmp_path / "ruff.json", [{"code": "F401"}, {"code": "E501"}])
    xml = tmp_path / "coverage.xml"
    xml.write_text('<?xml version="1.0" ?><coverage line-rate="0.8765" branch-rate="0"></coverage>',
                   encoding="utf-8")
    radon = _write(tmp_path / "radon.json", {
        "a.py": [{"name": "f", "complexity": 12}, {"name": "g", "complexity": 3}],
        "b.py": {"error": "invalid syntax"},
        "c.py": [{"name": "h", "complexity": 25}],
    })
    out = tmp_path / "current.json"
    assert _run(ratchet, "collect", "--out", str(out), "--ruff", str(ruff),
                "--cobertura", str(xml), "--radon-cc", str(radon)) == 0
    m = json.loads(out.read_text(encoding="utf-8"))["metrics"]
    assert m["lint_errors"]["value"] == 2
    assert m["coverage_pct"]["value"] == 87.65
    assert m["complex_functions"]["value"] == 2


def test_collect_go_reports(ratchet, tmp_path):
    cover = tmp_path / "cover.txt"
    cover.write_text("pkg/a.go:10:\tFoo\t100.0%\ntotal:\t\t\t(statements)\t63.4%\n", encoding="utf-8")
    static = tmp_path / "staticcheck.json"
    static.write_text('{"code":"SA1000"}\n{"code":"S1002"}\n\n', encoding="utf-8")
    cyclo = tmp_path / "gocyclo.txt"
    cyclo.write_text("", encoding="utf-8")
    out = tmp_path / "current.json"
    assert _run(ratchet, "collect", "--out", str(out), "--go-cover", str(cover),
                "--count-lines", f"lint_errors={static}",
                "--count-lines", f"complex_functions={cyclo}") == 0
    m = json.loads(out.read_text(encoding="utf-8"))["metrics"]
    assert m["coverage_pct"]["value"] == 63.4
    assert m["lint_errors"]["value"] == 2
    assert m["complex_functions"]["value"] == 0


def test_collect_needs_direction_for_unknown_metric(ratchet, tmp_path):
    assert _run(ratchet, "collect", "--out", str(tmp_path / "o.json"), "--metric", "bundle_kb=120") == 2


def test_collect_missing_report_is_usage_error(ratchet, tmp_path):
    assert _run(ratchet, "collect", "--out", str(tmp_path / "o.json"),
                "--eslint", str(tmp_path / "missing.json")) == 2


# --------------------------------------------------------------------------
# init
# --------------------------------------------------------------------------


def test_init_creates_and_refuses_overwrite(ratchet, tmp_path):
    cur = _write(tmp_path / "cur.json", {"lint_errors": 4})
    base = tmp_path / "baseline.json"
    assert _run(ratchet, "init", "--baseline", str(base), "--current", str(cur)) == 0
    assert json.loads(base.read_text(encoding="utf-8"))["metrics"]["lint_errors"]["value"] == 4
    assert _run(ratchet, "init", "--baseline", str(base), "--current", str(cur)) == 2
    assert _run(ratchet, "init", "--baseline", str(base), "--current", str(cur), "--force") == 0


def test_example_baseline_is_valid(ratchet):
    example = SCRIPT.parent.parent / "references" / "baseline.example.json"
    metrics = ratchet.load_metrics(str(example))
    assert metrics["coverage_pct"]["direction"] == "higher"
