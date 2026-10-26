"""Stand-in for `claude -p --output-format stream-json --verbose` in the hub tests.

Each call plays the next round of the JSON script named by $FAKE_CLAUDE_SCRIPT and
prints a stream shaped like Claude Code's stream-json output, one event per line: a
system init event, the round's "events", then the result event:

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
                 "events": [{"type": "assistant", ...}],  # printed (flushed) after the init event
                 "pause": 0,                      # seconds to sleep after the events
                 "stdout": ["plain line"],        # non-JSON lines printed after the events
                 "no_result": false,              # true: the stream ends without a result event
                 "single": false,                 # true: the old single pretty-printed result document
                 "raw": null}]}                   # a string: printed instead of the JSON

Rounds past the end repeat the last one. Every call appends what it saw (argv, cwd and the
hub environment, the stdin prompt, the --settings allow list and its pid) to argv_log. Standard library only, platform-neutral.
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
        "gh_config_dir": os.environ.get("GH_CONFIG_DIR"),
        "claude_config_dir": os.environ.get("CLAUDE_CONFIG_DIR"),
        "prompt": prompt,
        "allowed_tools": allowed,
        "pid": os.getpid(),
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


def emit(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()


session = act.get("session_id", "sess-%d" % n)
if act.get("raw") is not None:
    print(act["raw"])
else:
    if not act.get("single"):
        init = {"type": "system", "subtype": "init", "session_id": session, "tools": ["Bash", "Edit"]}
        if act.get("model"):
            init["model"] = act["model"]
        emit(init)
        for ev in act.get("events") or []:
            emit(ev)
        for line in act.get("stdout") or []:
            sys.stdout.write(line + "\n")
            sys.stdout.flush()
        if act.get("pause"):
            time.sleep(act["pause"])
    subtype = act.get("subtype", "success")
    out = {"type": "result", "subtype": subtype, "is_error": subtype != "success",
           "num_turns": act.get("num_turns", 1), "result": act.get("result", ""),
           "session_id": session}
    if act.get("cost") is not None:
        out["total_cost_usd"] = act["cost"]
    if act.get("usage") is not None:
        out["usage"] = act["usage"]
    if act.get("model"):
        out["modelUsage"] = {act["model"]: {"inputTokens": 1, "outputTokens": 1}}
    if act.get("permission_denials"):
        out["permission_denials"] = act["permission_denials"]
    if act.get("single"):
        print(json.dumps(out, indent=2))
    elif not act.get("no_result"):
        emit(out)
sys.exit(int(act.get("exit", 0)))
