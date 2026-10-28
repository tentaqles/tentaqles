"""A fake `tq` for the plan-cards tests: records every call, answers `tq hub add --json`.

Environment:
  FAKE_TQ_LOG      JSON-lines file; one object per call: {"argv": [...], "body": "<body-file text>"}
  FAKE_TQ_FAIL_AT  1-based number of the `hub add` call that fails (exit 1, message on stderr)
Card ids are c-fake00000001, c-fake00000002, ... counted from the log.
"""

from __future__ import annotations

import json
import os
import sys


def main() -> int:
    argv = sys.argv[1:]
    log = os.environ["FAKE_TQ_LOG"]
    rec: dict = {"argv": argv}
    if "--body-file" in argv:
        with open(argv[argv.index("--body-file") + 1], encoding="utf-8") as f:
            rec["body"] = f.read()
    adds = 0
    if os.path.exists(log):
        with open(log, encoding="utf-8") as f:
            adds = sum(1 for ln in f if json.loads(ln)["argv"][:2] == ["hub", "add"])
    with open(log, "a", encoding="utf-8") as f:
        f.write(json.dumps(rec) + "\n")
    if argv[:2] != ["hub", "add"]:
        print("fake tq: unsupported " + " ".join(argv[:2]), file=sys.stderr)
        return 1
    n = adds + 1
    if os.environ.get("FAKE_TQ_FAIL_AT") == str(n):
        print("Error: tq hub is not reachable", file=sys.stderr)
        return 1
    flag = lambda k: argv[argv.index(k) + 1] if k in argv else ""  # noqa: E731
    card = {"id": "c-fake%08d" % n, "title": argv[2], "body": rec.get("body", ""), "lane": "backlog",
            "kind": flag("--kind"), "tags": [], "parent_id": flag("--parent"), "project_dir": flag("--project")}
    print(json.dumps(card, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
