"""Dynamic path resolution for Tentaqles modules."""
import os
from pathlib import Path


def data_dir() -> Path:
    """Resolve the Tentaqles data directory."""
    d = os.environ.get("CLAUDE_PLUGIN_DATA") or os.environ.get("TENTAQLES_DATA_DIR") or str(Path.home() / ".tentaqles")
    p = Path(d)
    p.mkdir(parents=True, exist_ok=True)
    return p


def cache_dir() -> Path:
    return data_dir() / "cache"


def meta_db_path() -> Path:
    return data_dir() / "meta.db"
