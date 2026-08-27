"""Terminal service — manages PTY sessions using pywinpty."""

from __future__ import annotations

import os
from dataclasses import dataclass

from winpty import PtyProcess


@dataclass
class TerminalSession:
    """A terminal session backed by pywinpty."""

    pty: PtyProcess
    workspace_path: str
    label: str
    launch_command: str | None = None

    def write(self, data: str) -> None:
        """Write user input to the PTY."""
        if self.pty.isalive():
            self.pty.write(data)

    def read(self, size: int = 4096) -> str:
        """Read output from the PTY. May block."""
        return self.pty.read(size)

    def is_alive(self) -> bool:
        """Check if the PTY process is still running."""
        return self.pty.isalive()

    def close(self) -> None:
        """Close the PTY session."""
        try:
            self.pty.close()
        except Exception:
            pass


def create_session(
    workspace_path: str,
    launch_command: str | None = None,
    shell: str | None = None,
    rows: int = 24,
    cols: int = 120,
) -> TerminalSession:
    """Create a new terminal session in the given workspace directory.

    Args:
        workspace_path: Working directory for the terminal.
        launch_command: Optional command to run after shell starts (e.g., "claude").
        shell: Shell to use. Defaults to PowerShell.
        rows: Terminal rows.
        cols: Terminal columns.
    """
    if shell is None:
        # Prefer PowerShell 7, fall back to Windows PowerShell
        pwsh = os.path.join(os.environ.get("ProgramFiles", ""), "PowerShell", "7", "pwsh.exe")
        if os.path.exists(pwsh):
            shell = pwsh
        else:
            shell = "powershell.exe"

    # Build a clean environment: strip Claude Code env vars so nested
    # claude sessions don't think they're already inside one.
    clean_env = {k: v for k, v in os.environ.items() if not k.startswith("CLAUDE")}

    pty = PtyProcess.spawn(
        [shell, "-NoLogo", "-NoProfile"],
        cwd=workspace_path,
        dimensions=(rows, cols),
        env=clean_env,
    )

    label = os.path.basename(workspace_path)
    if launch_command:
        label = f"{label} ({launch_command})"

    session = TerminalSession(
        pty=pty,
        workspace_path=workspace_path,
        label=label,
        launch_command=launch_command,
    )

    # If a launch command was specified, send it after a brief delay
    if launch_command:
        pty.write(f"{launch_command}\r\n")

    return session


def destroy_session(session: TerminalSession) -> None:
    """Destroy a terminal session."""
    session.close()
