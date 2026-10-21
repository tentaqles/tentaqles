"""Regenerate hub_token_vectors.json from the plugin's own token code (gate._hub_mac).

From the monorepo root:
    python internal/hub/gate/testdata/gen_hub_token_vectors.py > internal/hub/gate/testdata/hub_token_vectors.json
"""

import json
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "..", "..", "plugin"))
from tentaqles.workflow.gate import _hub_mac  # noqa: E402

KEY = bytes(range(32))
CASES = [
    ("answer", "demo", "hub:renato"),
    ("answer", "fix-login", "hub:Jos\u00e9"),
    ("answer", "demo", 'hub:a"b\\c'),
    ("answer", "demo", "hub:<x>&y"),
    ("answer", "demo", "hub:\U0001F600"),
    ("answer", "demo", "hub:tab\there\x7f"),
    ("approve", "demo", "hub:renato"),
]
out = {"key_hex": KEY.hex(), "vectors": [
    {"action": a, "slug": s, "actor": u,
     "message": json.dumps(["hub", a, s, u], ensure_ascii=True, separators=(",", ":")),
     "mac": _hub_mac(KEY, a, s, u)} for a, s, u in CASES]}
print(json.dumps(out, indent=2, ensure_ascii=True))
