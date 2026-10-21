"""Machine-readable project config: the ```json tq-project block in CLAUDE.md.

Humans read the prose of the "Project config" section; tools (the build gate's
definition of done, the skills) read this block. Stdlib only: the gate hook
imports it.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

FENCE_RE = re.compile(r"^```json tq-project[ \t]*\n(.*?)^```", re.MULTILINE | re.DOTALL)
_LIST_KEYS = ("architecture_paths", "classified_paths")
_STR_KEYS = ("changelog",)


def load_project_config(root: Path) -> tuple[dict, str]:
    """Return (config, warning). Missing file or block -> ({}, ""). Invalid JSON ->
    ({}, warning). Wrong-typed or unknown keys are dropped and named in the warning."""
    path = Path(root) / "CLAUDE.md"
    try:
        text = path.read_text(encoding="utf-8-sig")
    except OSError:
        return {}, ""
    except UnicodeDecodeError:
        return {}, "CLAUDE.md is not valid UTF-8; save it as UTF-8 so the tq-project block can be read"
    m = FENCE_RE.search(text.replace("\r\n", "\n"))
    if not m:
        return {}, ""
    if not m.group(1).strip():
        return {}, "CLAUDE.md tq-project block is empty; add the JSON config or remove the block"
    try:
        raw = json.loads(m.group(1))
    except ValueError as e:
        return {}, f"CLAUDE.md tq-project block is invalid JSON ({e}); fix it so the definition of done can read the project commands"
    if not isinstance(raw, dict):
        return {}, "CLAUDE.md tq-project block is invalid: it must be a JSON object"
    cfg: dict = {}
    dropped: list[str] = []
    cmds = raw.get("commands")
    if isinstance(cmds, dict):
        kept = {k: v.strip() for k, v in cmds.items() if isinstance(k, str) and isinstance(v, str) and v.strip()}
        dropped += [f"commands.{k}" for k in cmds if k not in kept]
        if kept:
            cfg["commands"] = kept
    elif cmds is not None:
        dropped.append("commands")
    for key in _LIST_KEYS:
        v = raw.get(key)
        if isinstance(v, list) and all(isinstance(x, str) for x in v):
            cfg[key] = [x for x in v if x.strip()]
        elif v is not None:
            dropped.append(key)
    for key in _STR_KEYS:
        v = raw.get(key)
        if isinstance(v, str):
            cfg[key] = v.strip()
        elif v is not None:
            dropped.append(key)
    known = {"commands", *_LIST_KEYS, *_STR_KEYS}
    dropped += [k for k in raw if k not in known]
    warn = ""
    if dropped:
        warn = "CLAUDE.md tq-project block: ignored " + ", ".join(sorted(dropped)) + " (wrong type or unknown key)"
    return cfg, warn
