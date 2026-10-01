"""Start a process that outlives the hook that started it.

Claude Code aborts every SessionEnd hook still running when one shared budget
expires (1500ms by default; a plugin's `timeout` in hooks.json does not raise
it). Work that cannot be bounded that tightly has to leave the hook: the hook
hands its input to a detached process and returns at once.

Standard library only, and no `tentaqles.*` imports — this runs on the hot
path of a hook, before the plugin's own imports are paid for.
"""

from __future__ import annotations

import subprocess
import sys
import tempfile


def spawn_detached(argv: list[str], stdin_data: bytes = b"") -> None:
    """Run argv detached from this process, feeding it stdin_data.

    Returns as soon as the child has been started and handed its input. The
    child survives this process exiting, the terminal closing, and Claude Code
    tearing down the hook. Raises OSError if it could not be started or died
    before taking its input, so the caller can do the work itself instead.
    """
    kwargs: dict = {}
    if sys.platform == "win32":
        # CREATE_NO_WINDOW gives the child a hidden console of its own: it is
        # off the terminal's console (closing the window does not reach it),
        # and anything *it* spawns inherits that console instead of popping up
        # a new window — which DETACHED_PROCESS would cause.
        kwargs["creationflags"] = (
            subprocess.CREATE_NO_WINDOW | subprocess.CREATE_NEW_PROCESS_GROUP
        )
    else:
        # Own session: no SIGHUP when the terminal goes, and outside the
        # process group a hook runner signals on abort.
        kwargs["start_new_session"] = True

    proc = subprocess.Popen(
        argv,
        stdin=subprocess.PIPE,
        # Never the hook's own stdout/stderr: the hook is not over until every
        # holder of those pipes is gone.
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        close_fds=True,
        # A working directory is held open for the life of the process, and on
        # Windows that blocks deleting it — e.g. a worktree removed at exit.
        cwd=tempfile.gettempdir(),
        **kwargs,
    )
    proc.stdin.write(stdin_data)
    proc.stdin.close()
