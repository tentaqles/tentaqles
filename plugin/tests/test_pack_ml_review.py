# plugin/tests/test_pack_ml_review.py
"""ML pack, workstream M3: the ml-reviewer agent, the ml-review skill and its
checklist, the ml-review on-ramp notes, docs/packs/ml-review.md and the
templates/ml-starter layout (spec C §4.7, §5.1, §5.2, §5.5, §6)."""

from __future__ import annotations

import re
from pathlib import Path

import pytest
import yaml

PLUGIN = Path(__file__).resolve().parents[1]
REPO = PLUGIN.parent
AGENT = PLUGIN / "agents" / "ml-reviewer.md"
SKILL = PLUGIN / "skills" / "ml-review" / "SKILL.md"
CHECKLIST = PLUGIN / "skills" / "ml-review" / "references" / "checklist.md"
ONRAMP = PLUGIN / "skills" / "pack" / "references" / "onramps" / "ml-review.md"
DOC = REPO / "docs" / "packs" / "ml-review.md"
TEMPLATE = REPO / "templates" / "ml-starter"

SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'
WRITE_TOOLS = {"Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "PowerShell"}
ROWS = {
    "M1": "Leakage", "M2": "Split strategy", "M3": "Metrics", "M4": "Reproducibility",
    "M5": "Training/serving skew", "M6": "Personal data as features", "M7": "Model card",
    "M8": "Serialization and supply chain", "M9": "Promotion path", "M10": "Cost and resources",
    "M11": "PyTorch specifics",
}
ONRAMPS = {"build", "feature", "fix", "kickoff", "dod", "ship"}
HUMAN_ONLY = ("tq pack baseline", "tq pack ml promote apply", "tq hub answer", "build-gate approve")
CARD_HEADINGS = [
    "Model details", "Intended use / Out of scope", "Training data", "Evaluation data", "Metrics",
    "Fairness and slices", "Limitations and risks", "Reproducibility", "Monitoring and drift",
    "Promotion history",
]
ML_KEYS = {"enabled", "tools", "timeouts", "baseline", "paths", "seed_helper", "data_manifest", "models",
           "promotion", "drift"}


def _read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def _fm(p: Path) -> tuple[dict, str]:
    text = _read(p)
    assert text.startswith("---\n"), p
    end = text.index("\n---\n", 4)
    return yaml.safe_load(text[4:end]), text[end + 5:]


def _bash_blocks(text: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", text, re.S)


def _sections(text: str) -> dict[str, str]:
    out: dict[str, str] = {}
    for part in re.split(r"^## ", text, flags=re.M)[1:]:
        head, _, body = part.partition("\n")
        out[head.strip()] = body
    return out


def _tq_calls_have_timeouts(block: str) -> None:
    for line in block.splitlines():
        s = line.strip()
        if re.match(r"^(\$_to |timeout \d+ |gtimeout \d+ )?tq ", s) or s.startswith("tq "):
            assert s.startswith(("$_to ", "timeout ", "gtimeout ")), f"tq call without a timeout: {s}"


# ---- agent -------------------------------------------------------------------------------


def test_agent_is_read_only_and_named_for_the_gate() -> None:
    fm, _ = _fm(AGENT)
    assert fm["name"] == "ml-reviewer"
    assert re.match(r"^[a-z][a-z0-9-]*-reviewer$", fm["name"])  # gate.py pack-reviewer acceptance
    assert len(fm["description"]) >= 40
    assert {t.strip() for t in fm["tools"].split(",")} == {"Read", "Glob", "Grep"}
    disallowed = {t.strip() for t in fm["disallowedTools"].split(",")}
    assert WRITE_TOOLS <= disallowed and "Agent" in disallowed


def test_agent_returns_the_answer_key_contract() -> None:
    _, body = _fm(AGENT)
    for token in ("RESULT: PASS | FAIL | BLOCKED", "BUILT-BY-ME:", "CANNOT JUDGE", "file:line"):
        assert token in body, token
    assert "VERDICT:" not in body
    assert "skills/ml-review/references/checklist.md" in body
    for row in ROWS:
        assert re.search(rf"\b{row}\b", body), row
    assert "M1" in body and "M11" in body and "none is skipped silently" in body


def test_agent_reads_the_pack_results_and_never_promotes() -> None:
    _, body = _fm(AGENT)
    for token in ("ml/leakage", "ml/seeds", "ml/env-lock", "ml/data-snapshot", "ml/model-card",
                  "ml/unsafe-deserialize", "ml/promotion-in-code", "MODEL_CARD.md", "docs/privacy.md",
                  "`ml`", "`model_promotion`", "data_manifest"):
        assert token in body, token
    assert "Never copy" in body  # no personal-data values in findings
    assert "tq pack ml promote apply" not in body or "never" in body.lower()


# ---- checklist -----------------------------------------------------------------------------


def test_checklist_has_every_row_of_the_spec() -> None:
    text = _read(CHECKLIST)
    for row, title in ROWS.items():
        assert re.search(rf"^\| {row} \| {re.escape(title)} \|", text, re.M), (row, title)
    for token in ("L1", "L8", "eval_set=", "safetensors", "skops", "weights_only=True", "tq pack ml promote",
                  "calibration", "baseline", "confidence interval", "worker_init_fn", "no_grad"):
        assert token in text, token


# ---- skill ---------------------------------------------------------------------------------


def test_skill_frontmatter_and_triggers() -> None:
    fm, _ = _fm(SKILL)
    assert fm["name"] == "ml-review"
    desc = fm["description"]
    for phrase in ("review the training code", "leakage", "ml-reviewer", "model_promotion"):
        assert phrase in desc, phrase
    assert len(desc) <= 1024


def test_skill_runs_checks_quotes_results_and_records() -> None:
    _, body = _fm(SKILL)
    for token in ("ml-reviewer", "references/checklist.md", "tq pack check", "--pack ml", "--json",
                  "ml/leakage", "ml/seeds", "ml/env-lock", "ml/data-snapshot", "tq decide triage",
                  "tq pack crew", "[checkpoint <slug>#<id> review]", "BUILT-BY-ME",
                  "--reviewer ml-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N"):
        assert token in body, token
    assert "TENTAQLES_HEADLESS" in body and "AskUserQuestion" in body and "build-gate ask" in body
    assert "promote apply" in body and "never" in body.lower()
    assert ".env" in body  # never read


def test_skill_bash_blocks_bootstrap_and_time_out_tq() -> None:
    _, body = _fm(SKILL)
    blocks = _bash_blocks(body)
    assert blocks
    for block in blocks:
        if "$TENTAQLES_PY" in block or "$_tqe" in block or "tq " in block:
            assert block.lstrip().startswith(SNIPPET), block[:80]
        _tq_calls_have_timeouts(block)
        assert "promote apply" not in block
        assert " python " not in f" {block} ".replace("$TENTAQLES_PY", "")
        assert "git commit" not in block


# ---- on-ramp notes -------------------------------------------------------------------------


def test_onramp_file_follows_the_convention() -> None:
    assert re.match(r"^ml-[a-z0-9-]+\.md$", ONRAMP.name)
    text = _read(ONRAMP)
    assert text.startswith("# ml: review\n")
    secs = _sections(text)
    assert set(secs) <= ONRAMPS, set(secs) - ONRAMPS
    assert {"feature", "fix", "kickoff", "build", "ship"} <= set(secs)
    first = next(line for line in secs["kickoff"].splitlines() if line.strip())
    assert first.startswith("Only when the stack"), first
    assert "ml-reviewer" not in secs["feature"] or "tq pack crew" in secs["feature"]
    assert "`ml`" in secs["feature"]


def test_onramp_never_tells_the_agent_to_run_a_human_only_command() -> None:
    text = _read(ONRAMP)
    for line in text.splitlines():
        for cmd in HUMAN_ONLY:
            if cmd in line:
                assert re.search(r"\b(never|not|human)\b", line, re.I), line
    assert ".env" not in text or "never" in text.lower()
    for block in _bash_blocks(text):
        assert block.lstrip().startswith(SNIPPET)
        _tq_calls_have_timeouts(block)


# ---- docs ----------------------------------------------------------------------------------


def test_docs_page_covers_agent_skill_and_template() -> None:
    text = _read(DOC)
    for token in ("ml-reviewer", "ml-review", "ml-starter", "`ml`", "`model_promotion`",
                  "tq pack crew", "drift_profile.py", "scripts/promote.py", "MLFLOW_TRACKING_URI",
                  "tq.drift-profile/v1"):
        assert token in text, token
    for row in ROWS:
        assert re.search(rf"\b{row}\b", text), row


# ---- template layout -----------------------------------------------------------------------


def test_template_files_exist() -> None:
    for rel in ("README.md", "pyproject.toml", "uv.lock", "tq-packs.yaml", "data/snapshots.yaml",
                "data/profiles/churn-train.json", "models/churn/MODEL_CARD.md", "reports/churn/metrics.json",
                "src/drift_profile.py", "src/ml_starter/seed.py", "src/ml_starter/train.py",
                "scripts/promote.py", "tests"):
        assert (TEMPLATE / rel).exists(), rel


def test_template_pack_config_uses_only_spec_keys() -> None:
    cfg = yaml.safe_load(_read(TEMPLATE / "tq-packs.yaml"))
    ml = cfg["packs"]["ml"]
    assert set(ml) <= ML_KEYS, set(ml) - ML_KEYS
    assert ml["seed_helper"].split(":")[0] and (TEMPLATE / ml["seed_helper"].split(":")[0]).is_file()
    assert (TEMPLATE / ml["data_manifest"]).is_file()
    for m in ml["models"]:
        assert (TEMPLATE / m["card"]).is_file() and (TEMPLATE / m["metrics"]).is_file()
    assert "{model}" in ml["promotion"]["command"] and "{target}" in ml["promotion"]["command"]
    assert (TEMPLATE / ml["drift"]["reference"]).is_file()
    assert ml["drift"]["metric"] in ("psi", "js")
    assert "tool_paths" not in ml  # trusted layer only


def test_template_model_card_has_the_required_headings_and_pinned_snapshot() -> None:
    card = _read(TEMPLATE / "models" / "churn" / "MODEL_CARD.md")
    secs = _sections(card)
    for h in CARD_HEADINGS:
        assert h in secs, h
        assert secs[h].strip() and secs[h].strip() != "TODO", h
    snaps = yaml.safe_load(_read(TEMPLATE / "data" / "snapshots.yaml"))
    assert snaps
    for entry in snaps.values():
        assert (TEMPLATE / entry["uri"]).parent.is_dir() or entry["uri"].startswith("data/")
        assert re.fullmatch(r"[0-9a-f]{64}", entry["sha256"])
        assert entry["sha256"] in secs["Training data"]
    assert "@" not in card.replace("tester@acme.test", "")


def test_template_readme_row() -> None:
    text = _read(REPO / "templates" / "README.md")
    assert "| [`ml-starter/`](ml-starter/) |" in text


def test_template_reference_profile_schema() -> None:
    import json
    prof = json.loads(_read(TEMPLATE / "data" / "profiles" / "churn-train.json"))
    assert prof["schema"] == "tq.drift-profile/v1"
    assert {"rows", "source", "created_at", "features"} <= set(prof)
    for name, f in prof["features"].items():
        assert f["type"] in ("numeric", "categorical"), name
        if f["type"] == "numeric":
            assert len(f["counts"]) == len(f["edges"]) - 1
            assert f["edges"] == sorted(f["edges"])
        else:
            assert isinstance(f["counts"], dict) and isinstance(f["hashed"], bool)