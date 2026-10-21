"""Stand-in for `claude -p <prompt> --output-format json` in the hub tests.

Each call plays the next round of the JSON script named by $FAKE_CLAUDE_SCRIPT and
prints a result document shaped like Claude Code's JSON output:

    {"counter": "<file>", "argv_log": "<file>",
     "rounds": [{"subtype": "success",            # success | error_max_turns | error_during_execution
                 "result": "final text",
                 "session_id": "s1",
                 "cost": 0.25,                    # missing or null: no total_cost_usd field
                 "usage": {"input_tokens": 10, "output_tokens": 5,
                           "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0},  # null: no usage
                 "model": "claude-opus-5-5",       # names the modelUsage key; null: no modelUsage
                 "num_turns": 3, "exit": 0,
                 "permission_denials": [{"tool_name": "Bash", "tool_input": {"command": "npm test"}}],
                 "write": {"<absolute path>": "<text>"},
                 "write_json": {"<absolute path>": {"any": "json"}},   # gate state fixtures
                 "sleep": 0,
                 "raw": null}]}                   # a string: printed instead of the JSON

Rounds past the end repeat the last one. Every call appends what it saw (argv, cwd and the
hub environment, the stdin prompt and the --settings allow list) to argv_log. Standard library only, platform-neutral.
"""

import json
import os
import sys
import time

with open(os.environ["FAKE_CLAUDE_SCRIPT"], encoding="utf-8") as fh:
    script = json.load(fh)
counter = script["counter"]
n = 0
if os.path.exists(counter):
    with open(counter, encoding="utf-8") as fh:
        n = int(fh.read() or "0")
with open(counter, "w", encoding="utf-8") as fh:
    fh.write(str(n + 1))
prompt = sys.stdin.read()
allowed = None
if "--settings" in sys.argv:
    with open(sys.argv[sys.argv.index("--settings") + 1], encoding="utf-8") as fh:
        allowed = json.load(fh).get("permissions", {}).get("allow")
with open(script["argv_log"], "a", encoding="utf-8") as fh:
    fh.write(json.dumps({
        "round": n,
        "argv": sys.argv[1:],
        "cwd": os.getcwd(),
        "headless": os.environ.get("TENTAQLES_HEADLESS"),
        "headless_child": "TENTAQLES_HEADLESS_CHILD" in os.environ,
        "build_actor": "TQ_BUILD_ACTOR" in os.environ,
        "build_token": "TQ_BUILD_TOKEN" in os.environ,
        "hub_task": os.environ.get("TQ_HUB_TASK"),
        "ws": os.environ.get("TQ_WS"),
        "prompt": prompt,
        "allowed_tools": allowed,
    }) + "\n")
rounds = script["rounds"]
act = rounds[n] if n < len(rounds) else rounds[-1]
if act.get("sleep"):
    time.sleep(act["sleep"])
for path, text in (act.get("write") or {}).items():
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write(text)
for path, obj in (act.get("write_json") or {}).items():
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w", encoding="utf-8", newline="\n") as fh:
        json.dump(obj, fh, indent=2)
sys.stderr.write("fake claude round %d\n" % n)
if act.get("raw") is not None:
    print(act["raw"])
else:
    subtype = act.get("subtype", "success")
    out = {"type": "result", "subtype": subtype, "is_error": subtype != "success",
           "num_turns": act.get("num_turns", 1), "result": act.get("result", ""),
           "session_id": act.get("session_id", "sess-%d" % n)}
    if act.get("cost") is not None:
        out["total_cost_usd"] = act["cost"]
    if act.get("usage") is not None:
        out["usage"] = act["usage"]
    if act.get("model"):
        out["modelUsage"] = {act["model"]: {"inputTokens": 1, "outputTokens": 1}}
    if act.get("permission_denials"):
        out["permission_denials"] = act["permission_denials"]
    print(json.dumps(out))
sys.exit(int(act.get("exit", 0)))
