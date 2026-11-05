"""Shared test setup.

The suite runs under Claude Code more often than not, so an agent marker is
inherited. In-process tests run as CI does: the markers are removed from the
environment (a test that wants one sets it itself), ``TQ_HOME`` is pointed at
an empty temp dir so no test reads the developer's real ``~/.tentaqles``, and
``tentaqles.tqhome.ALLOW_HOME_OVERRIDE`` is set so a test that does set a
marker keeps its temp ``TQ_HOME``.

Subprocess tests (hook scripts) inherit the environment: they set ``HOME``
and ``USERPROFILE`` to a temp dir themselves, and register their workspace's
base in that home's ``config.yaml``, because the hooks run under a marker.
"""

from __future__ import annotations

import pytest

from tentaqles import tqhome


@pytest.fixture(autouse=True)
def _isolated_tq_home(tmp_path_factory, monkeypatch):
    prev = tqhome.ALLOW_HOME_OVERRIDE
    tqhome.ALLOW_HOME_OVERRIDE = True
    for k in tqhome.AGENT_MARKERS:
        monkeypatch.delenv(k, raising=False)
    monkeypatch.setenv("TQ_HOME", str(tmp_path_factory.mktemp("tqhome")))
    try:
        yield
    finally:
        tqhome.ALLOW_HOME_OVERRIDE = prev
