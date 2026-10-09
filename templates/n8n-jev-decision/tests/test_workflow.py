"""Static and behavioural checks for the Jev decision sub-workflow.

Stdlib only. Run with `python -m pytest templates/n8n-jev-decision/tests`
or `python templates/n8n-jev-decision/tests/test_workflow.py`.
The Code-node tests need `node` on PATH and are skipped without it.
"""

from __future__ import annotations

import json
import re
import shutil
import subprocess
import unittest
from pathlib import Path

WORKFLOW = Path(__file__).resolve().parent.parent / "jev-decision.workflow.json"
PINNED_MODEL = "jev-1.13.0"
TYPESAFE_URL = "https://api.typesafe.ai/v1/systemone"

# Secret-shaped strings that must never appear in an importable workflow.
SECRET_RES = [
    re.compile(r"\bBearer\s+[A-Za-z0-9._\-+/=]{16,}", re.I),
    re.compile(r"\bsk-[A-Za-z0-9_\-]{20,}"),
    re.compile(r"\b(?:ghp|gho|ghs|github_pat)_[A-Za-z0-9_]{20,}"),
    re.compile(r"\beyJ[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]{5,}"),
    re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
]
NON_FLOW_TYPES = {"n8n-nodes-base.stickyNote"}


def load() -> dict:
    return json.loads(WORKFLOW.read_text(encoding="utf-8"))


def node(wf: dict, name: str) -> dict:
    return next(n for n in wf["nodes"] if n["name"] == name)


def edges(wf: dict) -> list[tuple[str, int, str]]:
    out = []
    for src, kinds in wf["connections"].items():
        for outputs in kinds.values():
            for idx, targets in enumerate(outputs):
                for t in targets or []:
                    out.append((src, idx, t["node"]))
    return out


class WorkflowStructure(unittest.TestCase):
    def setUp(self) -> None:
        self.raw = WORKFLOW.read_text(encoding="utf-8")
        self.wf = load()

    def test_parses_and_has_nodes(self) -> None:
        self.assertIsInstance(self.wf["nodes"], list)
        names = [n["name"] for n in self.wf["nodes"]]
        self.assertEqual(len(names), len(set(names)), "node names must be unique")
        ids = [n["id"] for n in self.wf["nodes"]]
        self.assertEqual(len(ids), len(set(ids)), "node ids must be unique")

    def test_execute_workflow_trigger_with_inputs(self) -> None:
        trig = [n for n in self.wf["nodes"] if n["type"] == "n8n-nodes-base.executeWorkflowTrigger"]
        self.assertEqual(len(trig), 1)
        inputs = {v["name"] for v in trig[0]["parameters"]["workflowInputs"]["values"]}
        self.assertTrue({"state", "questions"} <= inputs)

    def test_no_inline_secrets(self) -> None:
        for rx in SECRET_RES:
            self.assertIsNone(rx.search(self.raw), f"secret-shaped string matched {rx.pattern}")
        http = node(self.wf, "TypeSafe Jev")
        params = json.dumps(http["parameters"]).lower()
        self.assertNotIn("authorization", params, "auth header must come from the credential")
        self.assertNotIn("headerparameters", params)

    def test_credential_reference_used(self) -> None:
        http = node(self.wf, "TypeSafe Jev")
        self.assertEqual(http["type"], "n8n-nodes-base.httpRequest")
        self.assertEqual(http["parameters"]["url"], TYPESAFE_URL)
        self.assertEqual(http["parameters"]["method"], "POST")
        self.assertEqual(http["parameters"]["authentication"], "genericCredentialType")
        self.assertEqual(http["parameters"]["genericAuthType"], "httpHeaderAuth")
        cred = http["credentials"]["httpHeaderAuth"]
        self.assertEqual(cred["name"], "TypeSafe API")
        self.assertIn("id", cred)

    def test_pinned_model(self) -> None:
        build = node(self.wf, "Build Jev Request")["parameters"]["jsCode"]
        self.assertIn(f"const MODEL = '{PINNED_MODEL}';", build)
        self.assertNotIn("jev-latest", self.raw)

    def test_every_node_connected(self) -> None:
        flow = {n["name"] for n in self.wf["nodes"] if n["type"] not in NON_FLOW_TYPES}
        e = edges(self.wf)
        for src, _, dst in e:
            self.assertIn(src, flow, f"connection from unknown node {src}")
            self.assertIn(dst, flow, f"connection to unknown node {dst}")
        touched = {s for s, _, _ in e} | {d for _, _, d in e}
        self.assertEqual(flow - touched, set(), "orphan nodes")
        # Every node is reachable from the trigger.
        start = "When Executed by Another Workflow"
        seen, stack = {start}, [start]
        while stack:
            cur = stack.pop()
            for s, _, d in e:
                if s == cur and d not in seen:
                    seen.add(d)
                    stack.append(d)
        self.assertEqual(flow - seen, set(), "nodes unreachable from the trigger")

    def test_error_outputs_default_to_human_review(self) -> None:
        e = edges(self.wf)
        for name in ("Build Jev Request", "TypeSafe Jev", "Score Confidence"):
            n = node(self.wf, name)
            self.assertEqual(n.get("onError"), "continueErrorOutput", name)
            self.assertIn((name, 1, "Mark Error"), e, f"{name} error output must go to review")
        self.assertIn(("Mark Error", 0, "Queue for Review (replace me)"), e)
        self.assertIn(("Queue for Review (replace me)", 0, "Human Review (Wait)"), e)

    def test_confidence_routing(self) -> None:
        e = edges(self.wf)
        self.assertIn(("Confident Enough?", 0, "Return Decision"), e)
        self.assertIn(("Confident Enough?", 1, "Mark Low Confidence"), e)
        cond = node(self.wf, "Confident Enough?")["parameters"]["conditions"]["conditions"][0]
        self.assertEqual(cond["operator"]["operation"], "gte")
        self.assertIn("min_confidence", cond["leftValue"])
        self.assertIn("threshold", cond["rightValue"])


NODE = shutil.which("node")

HARNESS = r"""
const fs = require('fs');
const [code, json, refs] = JSON.parse(fs.readFileSync(0, 'utf8'));
const $ = (name) => ({ item: { json: refs[name] } });
const fn = new Function('$json', '$', code);
try {
  process.stdout.write(JSON.stringify({ ok: fn(json, $) }));
} catch (e) {
  process.stdout.write(JSON.stringify({ error: String(e.message) }));
}
"""


@unittest.skipUnless(NODE, "node not on PATH")
class CodeNodes(unittest.TestCase):
    def run_code(self, name: str, json_in: dict, refs: dict | None = None) -> dict:
        code = node(load(), name)["parameters"]["jsCode"]
        proc = subprocess.run(
            [NODE, "-e", HARNESS],
            input=json.dumps([code, json_in, refs or {}]),
            capture_output=True,
            text=True,
            timeout=30,
            check=True,
        )
        return json.loads(proc.stdout)

    def test_build_request_frames_redacts_and_pins(self) -> None:
        fake_key = "sk-" + "a1b2c3d4" * 4  # built at runtime, not a real key
        out = self.run_code(
            "Build Jev Request",
            {
                "state": {"ticket": f"refund please, my key is {fake_key}"},
                "questions": {"refund": {"type": "noul", "instructions": "Is this a refund request?"}},
            },
        )["ok"]["json"]
        body = out["body"]
        self.assertEqual(body["model"], PINNED_MODEL)
        self.assertIn("untrusted", body["state"]["notice"])
        self.assertNotIn(fake_key, json.dumps(body))
        self.assertIn("[REDACTED]", body["state"]["data"]["ticket"])
        self.assertEqual(out["threshold"], 0.8)
        self.assertEqual(out["question_ids"], ["refund"])

    def test_build_request_rejects_bad_questions(self) -> None:
        out = self.run_code("Build Jev Request", {"state": "x", "questions": {"q": {"type": "essay"}}})
        self.assertIn("noul|choice|score", out["error"])

    def test_score_confidence(self) -> None:
        req = {"threshold": 0.8, "question_ids": ["a", "b", "c"], "body": {"model": PINNED_MODEL}}
        resp = {
            "answers": {
                "a": {"noul": 0.95},
                "b": {"choice": "refund", "confidence": 0.7, "probabilities": {"refund": 0.7}},
            },
            "usage": {"input_tokens": 120},
        }
        out = self.run_code("Score Confidence", resp, {"Build Jev Request": req})["ok"]["json"]
        self.assertAlmostEqual(out["confidence"]["a"], 0.95)
        self.assertAlmostEqual(out["confidence"]["b"], 0.7)
        self.assertEqual(out["confidence"]["c"], 0)  # missing answer -> review
        self.assertEqual(out["min_confidence"], 0)
        self.assertEqual(out["low_confidence"], ["b", "c"])


if __name__ == "__main__":
    unittest.main()
