"""Stand-in for `claude -p` in the loop-runner tests.

Reads the prompt on stdin, performs the scripted action for this round, and prints
a JSON result like `claude -p --output-format json`. The script lives in the
JSON file named by $FAKE_CLAUDE_PLAN:

    {"rounds": [{"write": {"app.py": "..."}, "cost": 0.5, "tokens": 100,
                 "idea": "...", "sleep": 0, "raw": null}],
     "counter": "<file>", "argv_log": "<file>"}

`raw` (a string) is printed instead of the JSON, to simulate unparseable output.
"""

import json
import os
import sys
import time

plan = json.load(open(os.environ["FAKE_CLAUDE_PLAN"], encoding="utf-8"))
prompt = sys.stdin.read()
counter = plan["counter"]
n = int(open(counter).read()) if os.path.exists(counter) else 0
open(counter, "w").write(str(n + 1))
with open(plan["argv_log"], "a", encoding="utf-8") as f:
    f.write(json.dumps({
        "argv": sys.argv[1:],
        "env_guard": os.environ.get("TQ_LOOP_GUARD"),
        "headless": os.environ.get("TENTAQLES_HEADLESS_CHILD"),
        "prompt_has_rules": "Do not run git" in prompt,
    }) + "\n")
rounds = plan["rounds"]
act = rounds[n] if n < len(rounds) else rounds[-1]
if act.get("sleep"):
    time.sleep(act["sleep"])
for rel, content in (act.get("write") or {}).items():
    path = os.path.join(os.getcwd(), rel)
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(content)
if act.get("tamper_guard"):
    # a misbehaving agent rewriting its own guard config (absolute path outside the worktree)
    with open(os.environ["TQ_LOOP_GUARD"], "w", encoding="utf-8") as f:
        f.write('{"root": "/", "locked": [], "allow_network": true}')
if act.get("git_commit"):
    # a misbehaving agent committing behind the runner's back (moves HEAD)
    import subprocess
    subprocess.run(["git", "add", "--", *act.get("write", {}).keys()], check=True, capture_output=True)
    subprocess.run(["git", "commit", "-q", "-m", "sneaky"], check=True, capture_output=True)
if act.get("raw") is not None:
    print(act["raw"])
else:
    print(json.dumps({
        "type": "result",
        "total_cost_usd": act.get("cost", 0.1),
        "usage": {"input_tokens": act.get("tokens", 10), "output_tokens": 0,
                  "cache_creation_input_tokens": 0, "cache_read_input_tokens": 999999},
        "result": "made a change\nIDEA: " + act.get("idea", "try something"),
    }))
