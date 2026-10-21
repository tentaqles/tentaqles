"""Stand-in for the plugin's build-gate.py in the hub tests.

State lives in $FAKE_GATE_DIR/<slug>.json (the document `state --json` prints; a `records`
key is printed only with --records). `answer` checks TQ_BUILD_ACTOR/TQ_BUILD_TOKEN exactly as
gate.hub_actor_ok does (HMAC-SHA256 over the compact ASCII JSON ["hub","answer",slug,actor] with
the key at $TQ_HOME/build-gate/hmac.key), then moves the decision from pending_decisions to
decisions. Every call is appended to $FAKE_GATE_DIR/calls.jsonl. Standard library only.
"""

import hashlib
import hmac
import json
import os
import sys


def mac(key, action, slug, actor):
    msg = json.dumps(["hub", action, slug, actor], ensure_ascii=True, separators=(",", ":"))
    return hmac.new(key, msg.encode("ascii"), hashlib.sha256).hexdigest()


def main(argv):
    d = os.environ["FAKE_GATE_DIR"]
    root = os.getcwd()
    if argv[:1] == ["--root"]:
        root, argv = argv[1], argv[2:]
    with open(os.path.join(d, "calls.jsonl"), "a", encoding="utf-8") as fh:
        fh.write(json.dumps({"argv": argv, "root": root, "cwd": os.getcwd(),
                             "actor": os.environ.get("TQ_BUILD_ACTOR"),
                             "has_token": bool(os.environ.get("TQ_BUILD_TOKEN"))}) + "\n")
    cmd, slug = argv[0], argv[1]
    path = os.path.join(d, slug + ".json")
    if not os.path.exists(path):
        print(f"no plan at docs/checkpoints/{slug}.json", file=sys.stderr)
        return 1
    with open(path, encoding="utf-8") as fh:
        st = json.load(fh)
    if cmd == "state":
        if "--records" not in argv:
            st.pop("records", None)
        print(json.dumps(st, ensure_ascii=False, indent=2))
        return 0
    if cmd == "answer":
        did = argv[2]
        choice = argv[argv.index("--choice") + 1]
        note = argv[argv.index("--note") + 1] if "--note" in argv else ""
        actor = (os.environ.get("TQ_BUILD_ACTOR") or "").strip()
        tok = (os.environ.get("TQ_BUILD_TOKEN") or "").strip()
        home = os.environ.get("TQ_HOME") or os.path.join(os.path.expanduser("~"), ".tentaqles")
        try:
            with open(os.path.join(home, "build-gate", "hmac.key"), "rb") as fh:
                key = fh.read()
        except OSError:
            key = None
        if not actor or not tok or key is None or not hmac.compare_digest(mac(key, "answer", slug, actor), tok):
            print("build-gate: this step needs a human at an interactive terminal. Refusing.", file=sys.stderr)
            return 2
        if st.get("rolled_back"):
            print("build state was rolled back or altered; a human must re-approve", file=sys.stderr)
            return 2
        pend = [p for p in st.get("pending_decisions", []) if p["id"] == did]
        if not pend:
            print(f"no pending decision '{did}' in '{slug}'.", file=sys.stderr)
            return 1
        rec = pend[0]
        if rec.get("options") and choice not in rec["options"]:
            print(f"choice must be one of {', '.join(rec['options'])}.", file=sys.stderr)
            return 1
        st["pending_decisions"] = [p for p in st["pending_decisions"] if p["id"] != did]
        done = {k: v for k, v in rec.items() if k != "text"}
        done.update({"status": "answered", "choice": choice, "note": note, "actor": actor, "answered_at": "2026-10-21T12:00:00+00:00"})
        st["decisions"] = [x for x in st.get("decisions", []) if x["id"] != did] + [done]
        if rec["kind"] == "result" and choice == "approve":
            st["finished"] = True
        if rec["kind"] == "plan-approval" and choice == "approve":
            st["approved"] = True
        with open(path, "w", encoding="utf-8") as fh:
            json.dump(st, fh, indent=2)
        print(f"decision {did}: {choice} by {actor}")
        return 0
    print(f"fake gate: unsupported command {cmd}", file=sys.stderr)
    return 64


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
