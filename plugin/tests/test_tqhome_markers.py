"""The plugin ignores TQ_HOME under the same agent markers as tq.

tq keeps one list, ``internal/markers/markers.go`` (``paths.Home`` and the
human-only check read it). An installed plugin has no Go source, so
``tentaqles.tqhome.AGENT_MARKERS`` repeats it; this test parses the Go file
and requires the two to be the same names in the same order.
"""

from __future__ import annotations

import re
from pathlib import Path

from tentaqles import tqhome

REPO = Path(__file__).resolve().parents[2]
MARKERS_GO = REPO / "internal" / "markers" / "markers.go"


def _go_markers() -> list[str]:
    text = MARKERS_GO.read_text(encoding="utf-8")
    block = re.search(r"var Markers = \[\]string\{(.*?)\n\}", text, re.S)
    assert block, "internal/markers/markers.go has no Markers list"
    return re.findall(r'"([A-Z0-9_]+)"', block.group(1))


def test_agent_markers_are_tqs_list():
    go = _go_markers()
    assert len(go) == 11, go
    assert list(tqhome.AGENT_MARKERS) == go


def test_every_marker_makes_tq_home_ignore_TQ_HOME(tmp_path, monkeypatch):
    forged = tmp_path / "forged"
    home = tmp_path / "home"
    monkeypatch.setattr(Path, "home", classmethod(lambda cls: home))
    monkeypatch.setenv("TQ_HOME", str(forged))
    monkeypatch.setattr(tqhome, "ALLOW_HOME_OVERRIDE", False)
    for k in tqhome.AGENT_MARKERS:
        monkeypatch.delenv(k, raising=False)
    assert tqhome.tq_home() == forged, "no marker: TQ_HOME applies"
    for k in tqhome.AGENT_MARKERS:
        monkeypatch.setenv(k, "1")
        assert tqhome.tq_home() == home / ".tentaqles", k
        monkeypatch.delenv(k)
