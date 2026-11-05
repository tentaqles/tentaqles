"""Where tq's state folder is, as the plugin resolves it.

``$TQ_HOME`` or ``~/.tentaqles``. Under an agent marker ``TQ_HOME`` is
ignored and the default is used: the plugin's hooks inherit the harness
environment, and a workspace's agent-writable Claude settings can set
``TQ_HOME`` to a forged state folder carrying its own manifests. The markers
are the list of tq's ``internal/cmd/humanonly.go``.

``ALLOW_HOME_OVERRIDE`` lets ``TQ_HOME`` apply under a marker; the tests set
it (they run under Claude Code with ``TQ_HOME`` pointing at a temp dir) and
nothing else does.
"""

from __future__ import annotations

import os
from pathlib import Path

AGENT_MARKERS = (
    "TENTAQLES_HEADLESS",
    "CLAUDE_PLUGIN_ROOT",
    "CLAUDECODE",
    "TQ_HUB_TASK",
    "CODEX_SANDBOX",
    "CODEX_SANDBOX_NETWORK_DISABLED",
)

ALLOW_HOME_OVERRIDE = False


def agent_session() -> bool:
    """Whether this process runs under an agent marker."""
    return any(os.environ.get(k, "").strip() for k in AGENT_MARKERS)


def default_home() -> Path:
    """``~/.tentaqles`` whatever ``TQ_HOME`` says."""
    return Path.home() / ".tentaqles"


def tq_home() -> Path:
    """``$TQ_HOME``, or the default when it is unset or the process runs
    under an agent marker (unless ``ALLOW_HOME_OVERRIDE``)."""
    env = os.environ.get("TQ_HOME")
    if env and (ALLOW_HOME_OVERRIDE or not agent_session()):
        return Path(env)
    return default_home()
