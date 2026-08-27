"""Promotion service — bottom-up config push from project to client (or client to global).

Promotes individual config items upstream so they can be propagated to all
sibling projects.
"""

from __future__ import annotations

import json
import shutil
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class PromotionItem:
    """A single item that can be promoted upstream."""

    config_type: str  # "rule", "mcp", "claude-md", "copilot", "hook"
    name: str
    source_path: str
    dest_path: str
    exists_at_dest: bool = False
    content_differs: bool = False


@dataclass
class PromotionPreview:
    """Preview of what promotion would do."""

    source_level: str  # "project" or "client"
    source_path: str
    dest_level: str  # "client" or "global"
    dest_path: str
    items: list[PromotionItem] = field(default_factory=list)

    @property
    def new_items(self) -> list[PromotionItem]:
        return [i for i in self.items if not i.exists_at_dest]

    @property
    def updated_items(self) -> list[PromotionItem]:
        return [i for i in self.items if i.exists_at_dest and i.content_differs]

    @property
    def unchanged_items(self) -> list[PromotionItem]:
        return [i for i in self.items if i.exists_at_dest and not i.content_differs]


def preview_promote_rule(
    source_path: str,
    dest_path: str,
    filename: str,
    source_level: str = "project",
    dest_level: str = "client",
) -> PromotionPreview:
    """Preview promoting a single rule file upstream."""
    src_file = Path(source_path) / ".claude" / "rules" / filename
    dst_file = Path(dest_path) / ".claude" / "rules" / filename

    preview = PromotionPreview(
        source_level=source_level,
        source_path=source_path,
        dest_level=dest_level,
        dest_path=dest_path,
    )

    if not src_file.exists():
        return preview

    exists = dst_file.exists()
    differs = False
    if exists:
        src_content = src_file.read_text(encoding="utf-8")
        dst_content = dst_file.read_text(encoding="utf-8")
        differs = src_content != dst_content

    preview.items.append(
        PromotionItem(
            config_type="rule",
            name=filename,
            source_path=str(src_file),
            dest_path=str(dst_file),
            exists_at_dest=exists,
            content_differs=differs,
        )
    )
    return preview


def preview_promote_mcp(
    source_path: str,
    dest_path: str,
    server_name: str,
    source_level: str = "project",
    dest_level: str = "client",
) -> PromotionPreview:
    """Preview promoting a single MCP server upstream."""
    preview = PromotionPreview(
        source_level=source_level,
        source_path=source_path,
        dest_level=dest_level,
        dest_path=dest_path,
    )

    # Find source MCP config
    src_config = _load_mcp_config(source_path)
    if not src_config:
        return preview

    src_servers = src_config.get("mcpServers", {})
    if server_name not in src_servers:
        return preview

    # Check destination
    dst_config = _load_mcp_config(dest_path)
    exists = server_name in dst_config.get("mcpServers", {}) if dst_config else False
    differs = False
    if exists and dst_config:
        differs = src_servers[server_name] != dst_config["mcpServers"][server_name]

    preview.items.append(
        PromotionItem(
            config_type="mcp",
            name=server_name,
            source_path=source_path,
            dest_path=dest_path,
            exists_at_dest=exists,
            content_differs=differs,
        )
    )
    return preview


def preview_promote_claude_md(
    source_path: str,
    dest_path: str,
    source_level: str = "project",
    dest_level: str = "client",
) -> PromotionPreview:
    """Preview promoting CLAUDE.md upstream."""
    src_file = Path(source_path) / "CLAUDE.md"
    dst_file = Path(dest_path) / "CLAUDE.md"

    preview = PromotionPreview(
        source_level=source_level,
        source_path=source_path,
        dest_level=dest_level,
        dest_path=dest_path,
    )

    if not src_file.exists():
        return preview

    exists = dst_file.exists()
    differs = False
    if exists:
        differs = src_file.read_text(encoding="utf-8") != dst_file.read_text(encoding="utf-8")

    preview.items.append(
        PromotionItem(
            config_type="claude-md",
            name="CLAUDE.md",
            source_path=str(src_file),
            dest_path=str(dst_file),
            exists_at_dest=exists,
            content_differs=differs,
        )
    )
    return preview


def promote_rule(source_path: str, dest_path: str, filename: str) -> bool:
    """Promote a rule file from source to dest level. Returns True on success."""
    src_file = Path(source_path) / ".claude" / "rules" / filename
    if not src_file.exists():
        return False

    dst_dir = Path(dest_path) / ".claude" / "rules"
    dst_dir.mkdir(parents=True, exist_ok=True)
    shutil.copy2(str(src_file), str(dst_dir / filename))
    return True


def promote_mcp(
    source_path: str,
    dest_path: str,
    server_name: str,
    target_files: list[str] | None = None,
) -> bool:
    """Promote an MCP server from source to dest level.

    Args:
        source_path: Path containing source MCP config.
        dest_path: Path to write the promoted MCP to.
        server_name: Name of the MCP server to promote.
        target_files: Which MCP config files to update at dest.
                      Defaults to [".mcp.json"].
    """
    if target_files is None:
        target_files = [".mcp.json"]

    src_config = _load_mcp_config(source_path)
    if not src_config:
        return False

    src_servers = src_config.get("mcpServers", {})
    if server_name not in src_servers:
        return False

    server_config = src_servers[server_name]

    for target in target_files:
        dst_path = Path(dest_path) / target
        if dst_path.exists():
            dst_content = dst_path.read_text(encoding="utf-8")
            try:
                dst_config = json.loads(dst_content)
            except json.JSONDecodeError:
                dst_config = {"mcpServers": {}}
        else:
            dst_config = {"mcpServers": {}}

        dst_config.setdefault("mcpServers", {})[server_name] = server_config
        dst_path.parent.mkdir(parents=True, exist_ok=True)
        dst_path.write_text(
            json.dumps(dst_config, indent=2, ensure_ascii=False) + "\n",
            encoding="utf-8",
        )

    return True


def promote_claude_md(source_path: str, dest_path: str) -> bool:
    """Promote CLAUDE.md from source to dest level. Returns True on success."""
    src_file = Path(source_path) / "CLAUDE.md"
    if not src_file.exists():
        return False

    dst_file = Path(dest_path) / "CLAUDE.md"
    shutil.copy2(str(src_file), str(dst_file))
    return True


def promote_all_rules(source_path: str, dest_path: str) -> list[str]:
    """Promote all rule files from source to dest. Returns list of promoted filenames."""
    src_rules = Path(source_path) / ".claude" / "rules"
    if not src_rules.is_dir():
        return []

    promoted: list[str] = []
    for rule_file in sorted(src_rules.glob("*.md")):
        if promote_rule(source_path, dest_path, rule_file.name):
            promoted.append(rule_file.name)
    return promoted


def promote_all_mcps(
    source_path: str,
    dest_path: str,
    target_files: list[str] | None = None,
) -> list[str]:
    """Promote all MCP servers from source to dest. Returns list of promoted names."""
    src_config = _load_mcp_config(source_path)
    if not src_config:
        return []

    promoted: list[str] = []
    for server_name in src_config.get("mcpServers", {}):
        if promote_mcp(source_path, dest_path, server_name, target_files):
            promoted.append(server_name)
    return promoted


def _load_mcp_config(workspace_path: str) -> dict | None:
    """Load MCP config from .mcp.json."""
    path = Path(workspace_path) / ".mcp.json"
    if path.exists():
        try:
            return json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            pass
    return None
