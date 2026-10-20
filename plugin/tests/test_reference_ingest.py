"""Vault folders as a connector: Markdown notes indexed as reference items.

Each file is a watermark (path + mtime): new files are added, changed files
re-indexed, untouched files skipped, so a scheduled run costs one stat per
file. The note's own date (``published``, then ``ingested_date``) is what
recency weighting sees, and the text is redacted like everything else.
"""

from __future__ import annotations

import os
import time
from pathlib import Path

import pytest

from tentaqles.memory.ingest import ingest_markdown_dirs
from tentaqles.memory.store import MemoryStore

CLIPPING = """---
type: "source"
title: "Build Your Personal Knowledge Base With AI"
source_type: "clipping"
url: "https://www.youtube.com/watch?v=LP7bfT4vOlo"
published: 2026-10-18
ingested_date: "2026-10-20T14:15:59-03:00"
description: "Quick walkthrough of a personal knowledge base system"
tags:
  - "clippings"
---
![](https://www.youtube.com/watch?v=LP7bfT4vOlo)

Recency weighting stamps every item with a date and boosts the newer ones.
BM25 full-text search turned out to beat the vector database.
"""


@pytest.fixture
def store(tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: None)

    def _no_backend(self):
        raise RuntimeError("fastembed is not installed")

    monkeypatch.setattr(MemoryStore, "_get_emb", _no_backend)
    s = MemoryStore(tmp_path / "ws")
    yield s
    s.close()


@pytest.fixture
def clippings(tmp_path) -> Path:
    d = tmp_path / "Clippings"
    d.mkdir()
    (d / "kb.md").write_text(CLIPPING, encoding="utf-8")
    (d / "notes" ).mkdir()
    (d / "notes" / "plain.md").write_text("# Plain note\n\nNo frontmatter here, just text about pgbouncer.\n", encoding="utf-8")
    (d / "image.png").write_bytes(b"\x89PNG")
    return d


def _refs(store):
    return store._conn.execute("SELECT path, title, url, created_at, text FROM reference_docs ORDER BY path").fetchall()


def test_first_run_indexes_every_markdown_file(store, clippings):
    result = ingest_markdown_dirs(store, [clippings])

    assert result == {"added": 2, "updated": 0, "skipped": 0}
    rows = _refs(store)
    assert len(rows) == 2
    kb = next(r for r in rows if r[0].endswith("kb.md"))
    assert kb[1] == "Build Your Personal Knowledge Base With AI"
    assert kb[2] == "https://www.youtube.com/watch?v=LP7bfT4vOlo"
    assert kb[3].startswith("2026-10-18"), "published date is the item's date"
    assert "BM25 full-text search" in kb[4]
    assert "---\ntype:" not in kb[4], "frontmatter is not part of the text"
    plain = next(r for r in rows if r[0].endswith("plain.md"))
    assert plain[1] == "Plain note"


def test_second_run_skips_unchanged_and_reindexes_changed(store, clippings):
    ingest_markdown_dirs(store, [clippings])
    f = clippings / "notes" / "plain.md"
    f.write_text("# Plain note\n\nNow it also mentions pgcat.\n", encoding="utf-8")
    os.utime(f, (time.time() + 5, time.time() + 5))

    result = ingest_markdown_dirs(store, [clippings])

    assert result == {"added": 0, "updated": 1, "skipped": 1}
    rows = _refs(store)
    assert len(rows) == 2
    assert "pgcat" in next(r for r in rows if r[0].endswith("plain.md"))[4]


def test_references_are_searchable_with_title_first(store, clippings):
    ingest_markdown_dirs(store, [clippings])

    results = store.search_memory("bm25 vector database")

    assert results and results[0]["type"] == "reference"
    assert results[0]["text"].startswith("Build Your Personal Knowledge Base With AI")
    assert len(results[0]["text"]) <= 400, "search returns a snippet, not the whole note"
    assert results[0]["date"].startswith("2026-10-18")


def test_ingest_redacts_secrets_and_caps_size(store, tmp_path):
    d = tmp_path / "clips"
    d.mkdir()
    fake = "api_key=sk_abcdef1234567890XYZ"  # tq:allow-secret (fixture, same fake as test_store.py)
    (d / "leak.md").write_text(fake + "\n" + ("word " * 30000), encoding="utf-8")

    ingest_markdown_dirs(store, [d])

    (row,) = _refs(store)
    assert "sk_abcdef1234567890XYZ" not in row[4] and "[REDACTED" in row[4]
    assert len(row[4]) <= 60_000


def test_symlinks_out_of_the_folder_are_not_followed(store, tmp_path):
    d = tmp_path / "clips"
    d.mkdir()
    secret = tmp_path / "outside" / "id_rsa.md"
    secret.parent.mkdir()
    secret.write_text("private material", encoding="utf-8")
    (d / "inside.md").write_text("# Inside\n\nfine\n", encoding="utf-8")
    try:
        os.symlink(secret, d / "link.md")
        os.symlink(secret.parent, d / "linked-dir", target_is_directory=True)
    except (OSError, NotImplementedError):
        pytest.skip("symlinks not permitted on this host")

    ingest_markdown_dirs(store, [d])

    paths = [r[0] for r in _refs(store)]
    assert len(paths) == 1 and paths[0].endswith("inside.md"), paths


def test_missing_directory_is_reported_not_raised(store, tmp_path):
    result = ingest_markdown_dirs(store, [tmp_path / "nope"])
    assert result == {"added": 0, "updated": 0, "skipped": 0}


def test_no_dirs_touches_nothing(store):
    assert ingest_markdown_dirs(store, []) == {"added": 0, "updated": 0, "skipped": 0}
    assert _refs(store) == []
