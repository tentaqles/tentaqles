"""Session history service — discovers and manages past Claude conversations."""

from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path


@dataclass
class SessionEntry:
    """A historical Claude session."""

    session_id: str
    project_path: str = ""
    created_at: str = ""
    updated_at: str = ""
    model: str = ""
    first_message: str = ""
    message_count: int = 0
    cost_usd: float = 0.0

    @property
    def display_title(self) -> str:
        """Generate a display title from the first message."""
        if self.first_message:
            text = self.first_message.strip()
            if len(text) > 80:
                return text[:77] + "..."
            return text
        return f"Session {self.session_id[:8]}..."

    @property
    def age_label(self) -> str:
        """Generate a human-readable age label."""
        if not self.updated_at:
            return ""
        try:
            dt = datetime.fromisoformat(self.updated_at.replace("Z", "+00:00"))
            now = datetime.now(timezone.utc)
            delta = now - dt
            if delta.days == 0:
                hours = delta.seconds // 3600
                if hours == 0:
                    return "Just now"
                return f"{hours}h ago"
            if delta.days == 1:
                return "Yesterday"
            if delta.days < 7:
                return f"{delta.days}d ago"
            return dt.strftime("%b %d")
        except Exception:
            return ""


def discover_sessions(workspace_path: str, limit: int = 30) -> list[SessionEntry]:
    """Discover past Claude sessions for a given workspace.

    Scans ~/.claude/projects/<encoded-path>/ for session JSONL files.
    Falls back to a basic directory listing if the structure differs.
    """
    sessions: list[SessionEntry] = []

    # The Claude CLI stores sessions under ~/.claude/projects/
    claude_dir = Path.home() / ".claude" / "projects"
    if not claude_dir.is_dir():
        return sessions

    # Normalize workspace path to match Claude's encoding
    norm_path = Path(workspace_path).resolve()

    # Claude encodes the project path in the folder name
    # e.g., "d-repos-booster" or similar encoding
    # Try to find the matching project folder
    target_folder = _find_project_folder(claude_dir, norm_path)
    if target_folder is None:
        return sessions

    # Scan for .jsonl session files
    session_files = sorted(
        target_folder.glob("*.jsonl"),
        key=lambda p: p.stat().st_mtime if p.exists() else 0,
        reverse=True,
    )

    for sf in session_files[:limit]:
        entry = _parse_session_file(sf, workspace_path)
        if entry:
            sessions.append(entry)

    return sessions


def load_session_messages(workspace_path: str, session_id: str) -> list[dict]:
    """Load messages from a session JSONL file for display in the UI."""
    claude_dir = Path.home() / ".claude" / "projects"
    if not claude_dir.is_dir():
        return []

    norm_path = Path(workspace_path).resolve()
    target_folder = _find_project_folder(claude_dir, norm_path)
    if target_folder is None:
        return []

    session_file = target_folder / f"{session_id}.jsonl"
    if not session_file.exists():
        return []

    messages: list[dict] = []
    try:
        with open(session_file, "r", encoding="utf-8", errors="replace") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue

                event_type = event.get("type", "")
                if event_type not in ("user", "human", "assistant"):
                    continue

                msg = event.get("message", {})
                role = "user" if event_type in ("user", "human") else "assistant"
                content = msg.get("content", "")
                timestamp = event.get("timestamp", "")

                # Normalize content to list of content blocks
                blocks: list[dict] = []
                if isinstance(content, str):
                    if content.strip():
                        blocks.append({"type": "text", "text": content})
                elif isinstance(content, list):
                    for block in content:
                        if isinstance(block, dict):
                            if block.get("type") == "text":
                                text = block.get("text", "")
                                if text and not text.startswith("<ide_"):
                                    blocks.append({"type": "text", "text": text})
                            elif block.get("type") == "tool_use":
                                blocks.append({
                                    "type": "tool_use",
                                    "text": f"Used tool: {block.get('name', 'unknown')}",
                                    "tool_name": block.get("name"),
                                    "tool_id": block.get("id"),
                                })
                            elif block.get("type") == "tool_result":
                                blocks.append({
                                    "type": "tool_result",
                                    "text": str(block.get("content", ""))[:500],
                                    "tool_id": block.get("tool_use_id"),
                                })

                if blocks:
                    messages.append({
                        "role": role,
                        "content": blocks,
                        "model": msg.get("model", ""),
                        "timestamp": timestamp,
                    })
    except Exception:
        pass

    return messages


def delete_session(workspace_path: str, session_id: str) -> bool:
    """Delete a session JSONL file from ~/.claude/projects/.

    Returns True if the file was deleted, False if not found.
    """
    claude_dir = Path.home() / ".claude" / "projects"
    if not claude_dir.is_dir():
        return False

    norm_path = Path(workspace_path).resolve()
    target_folder = _find_project_folder(claude_dir, norm_path)
    if target_folder is None:
        return False

    session_file = target_folder / f"{session_id}.jsonl"
    if session_file.exists():
        session_file.unlink()
        return True

    return False


def _encode_path_to_folder_name(workspace_path: Path) -> str:
    """Encode a workspace path into Claude CLI's folder name format.

    Claude encodes paths like:
      D:\\repos\\dirtybird\\database  →  D--repos-dirtybird-database
      C:\\Users\\renat               →  C--Users-renat
    """
    p = str(workspace_path)
    # Replace :\ or :/ with --
    p = p.replace(":\\", "--").replace(":/", "--")
    # Replace remaining separators with -
    p = p.replace("\\", "-").replace("/", "-")
    return p


def _find_project_folder(claude_dir: Path, workspace_path: Path) -> Path | None:
    """Find the Claude project folder matching a workspace path."""
    encoded = _encode_path_to_folder_name(workspace_path)

    # Normalize for comparison: lowercase, underscores→hyphens
    def normalize(s: str) -> str:
        return s.lower().replace("_", "-")

    encoded_norm = normalize(encoded)

    # Exact match (case-insensitive, underscore-insensitive)
    for folder in claude_dir.iterdir():
        if not folder.is_dir():
            continue
        if normalize(folder.name) == encoded_norm:
            return folder

    # Partial match: folder name is a prefix or contains the key parts
    # This handles cases where the path was registered at a parent level
    wp_parts = normalize(str(workspace_path).replace("\\", "-").replace("/", "-").replace(":", "-"))
    for folder in claude_dir.iterdir():
        if not folder.is_dir():
            continue
        fn = normalize(folder.name)
        if fn in wp_parts or wp_parts in fn:
            return folder

    # Fallback: check each folder for a marker file with the path
    wp_str = str(workspace_path).replace("\\", "/").lower()
    for folder in claude_dir.iterdir():
        if not folder.is_dir():
            continue
        marker = folder / "project.json"
        if marker.exists():
            try:
                data = json.loads(marker.read_text(encoding="utf-8"))
                if str(data.get("path", "")).replace("\\", "/").lower() == wp_str:
                    return folder
            except Exception:
                continue

    return None


def _parse_session_file(path: Path, workspace_path: str) -> SessionEntry | None:
    """Parse a session JSONL file to extract metadata."""
    try:
        stat = path.stat()
        session_id = path.stem  # filename without extension

        entry = SessionEntry(
            session_id=session_id,
            project_path=workspace_path,
            updated_at=datetime.fromtimestamp(stat.st_mtime, tz=timezone.utc).isoformat(),
            created_at=datetime.fromtimestamp(stat.st_ctime, tz=timezone.utc).isoformat(),
        )

        # Read the first few lines to extract metadata
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            msg_count = 0
            for i, line in enumerate(f):
                if i > 100:  # Don't read entire large files
                    break
                line = line.strip()
                if not line:
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue

                event_type = event.get("type", "")

                # Extract first user message (Claude CLI uses "user", older versions use "human")
                if event_type in ("user", "human") and not entry.first_message:
                    content = event.get("message", {}).get("content", "")
                    if isinstance(content, str):
                        entry.first_message = content
                    elif isinstance(content, list):
                        for block in content:
                            if isinstance(block, dict) and block.get("type") == "text":
                                text = block.get("text", "")
                                # Skip IDE context blocks (e.g., "<ide_opened_file>")
                                if text and not text.startswith("<ide_"):
                                    entry.first_message = text
                                    break

                # Count messages
                if event_type in ("user", "human", "assistant"):
                    msg_count += 1

                # Extract model
                if event_type == "assistant":
                    entry.model = event.get("message", {}).get("model", entry.model)

            entry.message_count = msg_count

        return entry

    except Exception:
        return None
