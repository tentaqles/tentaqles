"""Persistent disk cache for embeddings — keyed by text hash, survives sessions."""

from __future__ import annotations

import hashlib
import json
import numpy as np
from pathlib import Path


class EmbeddingCache:
    """File-based embedding cache. Each text gets a hash key, vector stored as .npy."""

    def __init__(self, cache_dir: Path):
        self._dir = cache_dir
        self._dir.mkdir(parents=True, exist_ok=True)
        # In-memory LRU for hot path
        self._mem: dict[str, np.ndarray] = {}
        self._max_mem = 10_000

    @staticmethod
    def _hash(text: str) -> str:
        return hashlib.sha256(text.encode("utf-8")).hexdigest()[:16]

    def _path(self, key: str) -> Path:
        # Two-level directory to avoid flat folder with thousands of files
        return self._dir / key[:2] / f"{key}.npy"

    def get(self, text: str) -> np.ndarray | None:
        key = self._hash(text)

        # Check memory first
        if key in self._mem:
            return self._mem[key]

        # Check disk
        path = self._path(key)
        if path.exists():
            vec = np.load(path)
            if len(self._mem) < self._max_mem:
                self._mem[key] = vec
            return vec

        return None

    def put(self, text: str, embedding: np.ndarray) -> None:
        key = self._hash(text)

        # Write to memory
        if len(self._mem) < self._max_mem:
            self._mem[key] = embedding

        # Write to disk
        path = self._path(key)
        path.parent.mkdir(parents=True, exist_ok=True)
        np.save(path, embedding)

    def stats(self) -> dict:
        """Return cache statistics."""
        total_files = sum(1 for _ in self._dir.rglob("*.npy"))
        total_bytes = sum(f.stat().st_size for f in self._dir.rglob("*.npy"))
        return {
            "cached_embeddings": total_files,
            "disk_bytes": total_bytes,
            "memory_entries": len(self._mem),
        }
