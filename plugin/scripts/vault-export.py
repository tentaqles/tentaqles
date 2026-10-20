#!/usr/bin/env python3
"""Export this workspace's memory to the Obsidian vault and ingest configured folders.

Usage:
    python scripts/vault-export.py [--cwd DIR] [--no-ingest]

The SessionEnd worker does the same automatically; this is the on-demand
refresh behind the /tentaqles:export-vault skill. Settings come from
tentaqles.knowledge (manifest ``knowledge:`` section, knowledge.yaml files,
TENTAQLES_VAULT_DIR). Always exits 0 and says what it did or why it did nothing.
"""

from __future__ import annotations

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths

setup_paths()


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--cwd", default=os.getcwd(), help="a directory inside the workspace (default: cwd)")
    ap.add_argument("--no-ingest", action="store_true", help="skip ingesting configured folders")
    args = ap.parse_args(argv)

    from tentaqles.knowledge import config_files, load_knowledge_config
    from tentaqles.manifest.loader import load_manifest

    manifest = load_manifest(args.cwd)
    client_root = manifest.get("_client_root", args.cwd) if manifest else args.cwd
    client = manifest.get("client", "unknown") if manifest else "unknown"
    cfg = load_knowledge_config(manifest)
    if not cfg.enabled:
        files = config_files()
        print(
            "Vault export is not configured. Set `vault_dir: <folder>` (and optionally "
            "`ingest: {<client>: [<folders>]}`) in " + str(files[-1]) + " (shared by all identities)"
            + (" or " + str(files[0]) + " (this identity only)" if len(files) > 1 else "")
            + ", or add a `knowledge:` section to .tentaqles.yaml, or set TENTAQLES_VAULT_DIR."
        )
        return 0

    from tentaqles.memory.store import MemoryStore

    store = MemoryStore(client_root)
    try:
        if cfg.ingest_dirs and not args.no_ingest:
            from tentaqles.memory.ingest import ingest_markdown_dirs

            r = ingest_markdown_dirs(store, cfg.ingest_dirs)
            print(
                f"Ingested {r['added']} new, {r['updated']} updated, {r['skipped']} unchanged "
                f"from {len(cfg.ingest_dirs)} folder(s): " + ", ".join(str(d) for d in cfg.ingest_dirs)
            )
        if cfg.vault_dir is not None:
            from tentaqles.memory.vault_export import export_workspace, safe_name

            written = export_workspace(store, manifest or {"client": client}, cfg)
            print(f"Exported {len(written)} note(s) for {client} to {os.path.join(str(cfg.vault_dir), safe_name(client))}")
            for p in written:
                print(f"  {p}")
    finally:
        store.close()
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:  # never fail a skill on a vault problem
        print(f"vault export failed: {exc}")
        sys.exit(0)
