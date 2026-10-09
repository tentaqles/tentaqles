"""LLM callable for semantic-fact extraction, via the Claude Code CLI.

MemoryConsolidator takes ``llm_fn: Callable[[str], str]`` and without one the
semantic tier never fills. This provides one that shells out to

    claude -p --model haiku --output-format json      (prompt on stdin)

Rules:

  * Only for detached/background contexts (the session-end ``--worker`` and
    compaction-cron.py). A foreground hook must never wait on a model call.
  * The current environment is passed through, so CLAUDE_CONFIG_DIR -- and
    with it the active identity/account -- is the one that gets billed.
  * The prompt goes through ``tentaqles.privacy.redact_text`` before leaving
    the machine.
  * Any failure (no ``claude`` on PATH, non-zero exit, timeout, unparseable
    output, ``is_error``) returns "" -- no facts, no exception.
  * Opt out with TENTAQLES_SEMANTIC_FACTS=0.

The child gets TENTAQLES_HEADLESS_CHILD=1 and TENTAQLES_SEMANTIC_FACTS=0 so
the plugin's own hooks inside that headless session neither record it as a
workspace session nor start another extraction.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
from typing import Callable

DEFAULT_MODEL = "haiku"
DEFAULT_TIMEOUT = 60

_OFF_VALUES = {"0", "false", "no", "off"}


def semantic_facts_enabled() -> bool:
    return os.environ.get("TENTAQLES_SEMANTIC_FACTS", "").strip().lower() not in _OFF_VALUES


def find_claude() -> str | None:
    return shutil.which("claude")


def _redact(text: str) -> str:
    try:
        from tentaqles.privacy import redact_text

        return redact_text(text)[0]
    except Exception:
        return text


def _parse_result(stdout: str) -> str:
    """Extract the assistant text from `--output-format json` output."""
    try:
        data = json.loads(stdout)
    except (ValueError, TypeError):
        return ""
    if isinstance(data, list):  # older CLIs printed the whole message list
        data = next(
            (d for d in reversed(data) if isinstance(d, dict) and d.get("type") == "result"),
            None,
        )
    if not isinstance(data, dict) or data.get("is_error"):
        return ""
    result = data.get("result")
    return result if isinstance(result, str) else ""


def claude_cli_complete(
    prompt: str,
    *,
    binary: str | None = None,
    model: str = DEFAULT_MODEL,
    timeout: int = DEFAULT_TIMEOUT,
) -> str:
    """One headless completion. Returns "" on any failure."""
    exe = binary or find_claude()
    if not exe:
        return ""
    env = dict(os.environ)  # keeps CLAUDE_CONFIG_DIR (the active identity)
    env["TENTAQLES_HEADLESS_CHILD"] = "1"
    env["TENTAQLES_SEMANTIC_FACTS"] = "0"
    kwargs: dict = {}
    if sys.platform == "win32":
        kwargs["creationflags"] = getattr(subprocess, "CREATE_NO_WINDOW", 0)
    try:
        proc = subprocess.run(
            [exe, "-p", "--model", model, "--output-format", "json"],
            input=_redact(prompt).encode("utf-8"),
            capture_output=True,
            timeout=timeout,
            env=env,
            # Not a workspace: the headless session must not resolve to a
            # client manifest or write to anyone's memory.db.
            cwd=tempfile.gettempdir(),
            **kwargs,
        )
    except (OSError, subprocess.SubprocessError, ValueError):
        return ""
    if proc.returncode != 0:
        return ""
    return _parse_result(proc.stdout.decode("utf-8", errors="replace"))


def get_semantic_llm(
    model: str = DEFAULT_MODEL, timeout: int = DEFAULT_TIMEOUT
) -> Callable[[str], str] | None:
    """An ``llm_fn`` for MemoryConsolidator, or None when disabled/unavailable."""
    if not semantic_facts_enabled():
        return None
    if os.environ.get("TENTAQLES_HEADLESS_CHILD") == "1":
        return None
    exe = find_claude()
    if not exe:
        return None

    def llm_fn(prompt: str) -> str:
        return claude_cli_complete(prompt, binary=exe, model=model, timeout=timeout)

    return llm_fn
