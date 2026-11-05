"""Shared test setup.

The suite runs under Claude Code more often than not, so an agent marker is
set and ``tentaqles.tqhome.tq_home`` would ignore ``TQ_HOME``; the tests
point it at temp dirs on purpose, so the override is allowed for every
in-process test. ``TQ_HOME`` itself is pointed at an empty temp dir so no
test reads the developer's real ``~/.tentaqles`` (its ``config.yaml`` would
make the loader resolve only registered folders).

Subprocess tests (hook scripts) inherit the environment: they set ``HOME``
and ``USERPROFILE`` to a temp dir themselves when the state folder matters.
"""

from __future__ import annotations

import pytest

from tentaqles import tqhome


@pytest.fixture(autouse=True)
def _isolated_tq_home(tmp_path_factory, monkeypatch):
    prev = tqhome.ALLOW_HOME_OVERRIDE
    tqhome.ALLOW_HOME_OVERRIDE = True
    monkeypatch.setenv("TQ_HOME", str(tmp_path_factory.mktemp("tqhome")))
    try:
        yield
    finally:
        tqhome.ALLOW_HOME_OVERRIDE = prev
