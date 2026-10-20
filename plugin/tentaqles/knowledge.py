"""Knowledge-base settings: the Obsidian mirror and the folders it ingests.

Resolution order, first definition wins:

1. the workspace manifest's ``knowledge:`` section
   (``vault_dir``, ``export_sessions``, ``export_fixes``, ``ingest_dirs``);
2. ``$CLAUDE_PLUGIN_DATA/knowledge.yaml`` (one per identity);
3. ``$TQ_HOME/knowledge.yaml`` (``~/.tentaqles/knowledge.yaml``, shared by
   every identity on the machine); its ``ingest:`` mapping is per client;
4. ``TENTAQLES_VAULT_DIR`` for the vault dir alone.

Ingest dirs accumulate across layers (manifest first) and are deduplicated.
Nothing configured means the feature is off: no folder is created anywhere.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field
from pathlib import Path

import yaml

SHARED_FILE = "knowledge.yaml"


@dataclass
class KnowledgeConfig:
    vault_dir: Path | None = None
    export_sessions: bool = True
    export_fixes: bool = True
    ingest_dirs: list[Path] = field(default_factory=list)

    @property
    def enabled(self) -> bool:
        return self.vault_dir is not None or bool(self.ingest_dirs)


def _read_yaml(path: Path) -> dict:
    try:
        data = yaml.safe_load(path.read_text(encoding="utf-8"))
    except (OSError, yaml.YAMLError):
        return {}
    return data if isinstance(data, dict) else {}


def tq_home() -> Path:
    return Path(os.environ.get("TQ_HOME") or (Path.home() / ".tentaqles"))


def config_files() -> list[Path]:
    """Candidate settings files, highest precedence first."""
    out: list[Path] = []
    plugin_data = os.environ.get("CLAUDE_PLUGIN_DATA") or os.environ.get("TENTAQLES_DATA_DIR")
    if plugin_data:
        out.append(Path(plugin_data) / SHARED_FILE)
    out.append(tq_home() / SHARED_FILE)
    return out


def _as_paths(value) -> list[Path]:
    if isinstance(value, str):
        return [Path(value)] if value else []
    if isinstance(value, (list, tuple)):
        return [Path(str(v)) for v in value if v]
    return []


def _norm(p) -> str:
    return os.path.normcase(os.path.normpath(os.path.abspath(str(p))))


def _within(child, parent) -> bool:
    """True when child is parent or lies under it (lexically, case-folded on Windows)."""
    c, p = _norm(child), _norm(parent)
    return c == p or c.startswith(p.rstrip(os.sep) + os.sep)


def load_knowledge_config(manifest: dict | None) -> KnowledgeConfig:
    """Resolve the settings for one workspace.

    The manifest travels with the repository, so a cloned project could point
    the plugin at arbitrary paths. Its flags are honoured as the first layer,
    but its paths are confined: ``vault_dir`` must lie inside the vault the
    user configured (knowledge.yaml or TENTAQLES_VAULT_DIR), and
    ``ingest_dirs`` must lie inside the workspace itself. Anything else from
    the manifest is dropped.
    """
    manifest = manifest or {}
    client = str(manifest.get("client") or "")
    client_root = manifest.get("_client_root")
    section = manifest.get("knowledge")
    manifest_layer: dict = section if isinstance(section, dict) else {}
    user_layers: list[dict] = [_read_yaml(f) for f in config_files() if f.is_file()]
    layers = [manifest_layer] + user_layers

    def first(key: str, where: list[dict]):
        for layer in where:
            if layer.get(key) is not None:
                return layer[key]
        return None

    vault = first("vault_dir", user_layers)
    if vault is None:
        vault = os.environ.get("TENTAQLES_VAULT_DIR") or None
    own_vault = manifest_layer.get("vault_dir")
    if vault and own_vault and _within(own_vault, vault):
        vault = own_vault
    sessions = first("export_sessions", layers)
    fixes = first("export_fixes", layers)

    dirs: list[Path] = []
    if client_root:
        dirs += [d for d in _as_paths(manifest_layer.get("ingest_dirs")) if _within(d, client_root)]
    for layer in user_layers:
        dirs += _as_paths(layer.get("ingest_dirs"))
        mapping = layer.get("ingest")
        if isinstance(mapping, dict) and client:
            dirs += _as_paths(mapping.get(client))
    seen: set[str] = set()
    unique: list[Path] = []
    for d in dirs:
        key = str(d).replace("\\", "/").lower()
        if key not in seen:
            seen.add(key)
            unique.append(d)

    return KnowledgeConfig(
        vault_dir=Path(str(vault)) if vault else None,
        export_sessions=True if sessions is None else bool(sessions),
        export_fixes=True if fixes is None else bool(fixes),
        ingest_dirs=unique,
    )
