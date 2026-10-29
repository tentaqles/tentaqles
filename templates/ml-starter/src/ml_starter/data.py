"""Pinned data snapshots (packs.ml.data_manifest = data/snapshots.yaml).

Training reads data only through `snapshot_path`, which refuses a name that is not
in the manifest and a file whose sha256 differs from the pinned one. A moving
reference ("latest", an unbounded query) is never a snapshot.
"""

from __future__ import annotations

import hashlib
from pathlib import Path

import pandas as pd
import yaml

MANIFEST = Path("data") / "snapshots.yaml"


class SnapshotMismatch(RuntimeError):
    """The file on disk is not the pinned snapshot."""


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with Path(path).open("rb") as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def load_manifest(root: Path) -> dict[str, dict]:
    data = yaml.safe_load((Path(root) / MANIFEST).read_text(encoding="utf-8")) or {}
    if not isinstance(data, dict):
        raise ValueError(f"{MANIFEST}: expected a mapping of snapshot name -> {{uri, sha256}}")
    return data


def snapshot_path(root: Path, name: str) -> Path:
    """The local path of snapshot `name`, after checking its sha256 against the manifest."""
    entries = load_manifest(root)
    if name not in entries:
        raise KeyError(f"snapshot {name!r} is not pinned in {MANIFEST}")
    entry = entries[name]
    uri = str(entry["uri"])
    if "://" in uri or Path(uri).is_absolute() or ".." in Path(uri).parts:
        raise ValueError(f"snapshot {name!r}: uri must be a repo-relative path, got {uri!r}")
    path = Path(root) / uri
    got = sha256_file(path)
    if got != entry["sha256"]:
        raise SnapshotMismatch(
            f"snapshot {name!r} at {uri}: sha256 {got[:12]}… != pinned {str(entry['sha256'])[:12]}…; "
            "regenerate the data or pin the new snapshot (and update the model card)")
    return path


def load_frame(path: Path) -> pd.DataFrame:
    """Read a CSV or parquet file (parquet needs the `parquet` extra: pyarrow)."""
    path = Path(path)
    if path.suffix == ".parquet":
        return pd.read_parquet(path)
    return pd.read_csv(path)
