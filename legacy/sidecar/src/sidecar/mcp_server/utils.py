"""Subprocess helpers for git, az, gh, and PowerShell commands."""

from __future__ import annotations

import asyncio
import re


async def run_command(
    cmd: list[str],
    cwd: str | None = None,
    timeout: float = 15.0,
) -> tuple[str, str, int]:
    """Run a subprocess command asynchronously.

    Returns (stdout, stderr, returncode).
    """
    try:
        process = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            cwd=cwd,
        )
        stdout, stderr = await asyncio.wait_for(
            process.communicate(),
            timeout=timeout,
        )
        return (
            stdout.decode("utf-8", errors="replace").strip(),
            stderr.decode("utf-8", errors="replace").strip(),
            process.returncode or 0,
        )
    except FileNotFoundError:
        return "", f"Command not found: {cmd[0]}", 1
    except TimeoutError:
        return "", f"Command timed out after {timeout}s: {' '.join(cmd)}", 1


async def run_powershell_script(
    script_path: str,
    args: list[str] | None = None,
    timeout: float = 60.0,
) -> tuple[str, str, int]:
    """Run a PowerShell script on Windows."""
    cmd = [
        "powershell.exe",
        "-NoProfile",
        "-ExecutionPolicy",
        "Bypass",
        "-File",
        script_path,
    ]
    if args:
        cmd.extend(args)

    return await run_command(cmd, timeout=timeout)


async def get_git_email(cwd: str | None = None) -> str:
    """Get the configured git email for a directory."""
    stdout, stderr, rc = await run_command(["git", "config", "user.email"], cwd=cwd)
    if rc != 0:
        return f"error: {stderr}"
    return stdout


async def get_az_subscription_id() -> str | None:
    """Get the active Azure subscription ID."""
    stdout, stderr, rc = await run_command(
        ["az", "account", "show", "--query", "id", "-o", "tsv"],
    )
    if rc != 0:
        return None
    return stdout


async def get_gh_account() -> str | None:
    """Get the logged-in GitHub account name from gh auth status."""
    stdout, stderr, rc = await run_command(["gh", "auth", "status"])
    # gh auth status outputs to stderr
    output = stderr if stderr else stdout
    if rc != 0 and not output:
        return None

    # Parse "Logged in to github.com account <name>" pattern
    match = re.search(r"account\s+(\S+)", output)
    if match:
        return match.group(1)

    # Fallback: look for "Logged in to github.com as <name>"
    match = re.search(r"as\s+(\S+)", output)
    if match:
        return match.group(1)

    return None
