"""Diff service — git diff parsing for the ChangesPanel."""

from __future__ import annotations

import logging
import subprocess
from dataclasses import dataclass

logger = logging.getLogger(__name__)


@dataclass
class DiffLine:
    type: str  # "added" | "removed" | "context"
    content: str
    old_number: int | None
    new_number: int | None


@dataclass
class DiffHunk:
    old_start: int
    old_count: int
    new_start: int
    new_count: int
    header: str
    lines: list[DiffLine]


@dataclass
class FileDiff:
    path: str
    status: str  # "modified" | "added" | "deleted" | "renamed"
    old_path: str | None
    hunks: list[DiffHunk]
    additions: int
    deletions: int


def _run_git(workspace_path: str, *args: str) -> str:
    """Run a git command and return stdout."""
    try:
        result = subprocess.run(
            ["git", *args],
            cwd=workspace_path,
            capture_output=True,
            text=True,
            timeout=15,
        )
        return result.stdout
    except (subprocess.TimeoutExpired, FileNotFoundError) as e:
        logger.warning("git command failed in %s: %s", workspace_path, e)
        return ""


def get_diff(workspace_path: str, staged: bool = False, ref: str | None = None) -> list[dict]:
    """Get parsed git diff for a workspace."""
    args = ["diff", "--unified=3"]
    if staged:
        args.append("--cached")
    if ref:
        args.append(ref)

    raw = _run_git(workspace_path, *args)
    if not raw.strip():
        return []

    return [_file_diff_to_dict(fd) for fd in _parse_diff(raw)]


def get_status(workspace_path: str) -> list[dict]:
    """Get git status --porcelain for a workspace."""
    raw = _run_git(workspace_path, "status", "--porcelain", "-u")
    files = []
    for line in raw.splitlines():
        if len(line) < 4:
            continue
        xy = line[:2]
        path = line[3:]
        status = "modified"
        if "A" in xy or "?" in xy:
            status = "added"
        elif "D" in xy:
            status = "deleted"
        elif "R" in xy:
            status = "renamed"
        files.append({"path": path, "status": status, "staged": xy[0] != " " and xy[0] != "?"})
    return files


def _parse_diff(raw: str) -> list[FileDiff]:
    """Parse unified diff output into structured FileDiff objects."""
    files: list[FileDiff] = []
    current_file: FileDiff | None = None
    current_hunk: DiffHunk | None = None
    old_line = 0
    new_line = 0

    for line in raw.split("\n"):
        if line.startswith("diff --git"):
            if current_file:
                files.append(current_file)
            # Extract path from "diff --git a/path b/path"
            parts = line.split(" b/", 1)
            path = parts[1] if len(parts) > 1 else ""
            current_file = FileDiff(
                path=path,
                status="modified",
                old_path=None,
                hunks=[],
                additions=0,
                deletions=0,
            )
            current_hunk = None

        elif current_file and line.startswith("new file"):
            current_file.status = "added"

        elif current_file and line.startswith("deleted file"):
            current_file.status = "deleted"

        elif current_file and line.startswith("rename from"):
            current_file.old_path = line[len("rename from "):]
            current_file.status = "renamed"

        elif current_file and line.startswith("@@"):
            # Parse hunk header: @@ -old_start,old_count +new_start,new_count @@
            try:
                header = line
                parts = line.split("@@")[1].strip().split()
                old_parts = parts[0].lstrip("-").split(",")
                new_parts = parts[1].lstrip("+").split(",")
                old_start = int(old_parts[0])
                old_count = int(old_parts[1]) if len(old_parts) > 1 else 1
                new_start = int(new_parts[0])
                new_count = int(new_parts[1]) if len(new_parts) > 1 else 1

                current_hunk = DiffHunk(
                    old_start=old_start,
                    old_count=old_count,
                    new_start=new_start,
                    new_count=new_count,
                    header=header,
                    lines=[],
                )
                current_file.hunks.append(current_hunk)
                old_line = old_start
                new_line = new_start
            except (IndexError, ValueError):
                pass

        elif current_hunk and current_file:
            if line.startswith("+"):
                current_hunk.lines.append(
                    DiffLine(type="added", content=line[1:], old_number=None, new_number=new_line)
                )
                current_file.additions += 1
                new_line += 1
            elif line.startswith("-"):
                current_hunk.lines.append(
                    DiffLine(type="removed", content=line[1:], old_number=old_line, new_number=None)
                )
                current_file.deletions += 1
                old_line += 1
            elif line.startswith(" ") or line == "":
                content = line[1:] if line.startswith(" ") else ""
                current_hunk.lines.append(
                    DiffLine(type="context", content=content, old_number=old_line, new_number=new_line)
                )
                old_line += 1
                new_line += 1

    if current_file:
        files.append(current_file)

    return files


def _file_diff_to_dict(fd: FileDiff) -> dict:
    """Convert a FileDiff dataclass to a serializable dict."""
    return {
        "path": fd.path,
        "status": fd.status,
        "old_path": fd.old_path,
        "additions": fd.additions,
        "deletions": fd.deletions,
        "hunks": [
            {
                "old_start": h.old_start,
                "old_count": h.old_count,
                "new_start": h.new_start,
                "new_count": h.new_count,
                "header": h.header,
                "lines": [
                    {
                        "type": ln.type,
                        "content": ln.content,
                        "old_number": ln.old_number,
                        "new_number": ln.new_number,
                    }
                    for ln in h.lines
                ],
            }
            for h in fd.hunks
        ],
    }
