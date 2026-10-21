# Development Process Spine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the process spine of the tentaqles development process: the `prd`, `kickoff`, `feature`, `fix`, `direction` and `dod` skills, the shared templates, and the engine changes (recorded reviewer verdicts, a definition-of-done gate at Stop, process metrics in `tq insights`).

**Architecture:** The existing `/tentaqles:build` engine (`plugin/tentaqles/workflow/gate.py`, hooks installed per project) stays the single executor. Three new on-ramp skills (`kickoff`, `feature`, `fix`) produce a spec slice and a checkpoint plan tagged with a `workflow`, then hand off to `build`. The gate gains two kinds of script-recorded evidence the model cannot forge: **review records** (one signed line per reviewer verdict, tied to the tree hash; the review gate can only pass on a `build-critic` PASS at the current tree with no FAIL or BLOCKED beside it) and a **definition-of-done record** (`build-gate dod` runs the project's frozen test/lint/typecheck commands and checks ADR, migration-plan and changelog obligations; the Stop hook blocks once while a finished `feature`/`fix`/`kickoff` plan has no fresh DoD). Every gate outcome is appended as an aggregate-only event to `$TQ_HOME/build-gate/events.jsonl`, which `tq insights` reads for a new "Process" section. Machine-readable project settings live in a `json tq-project` fenced block inside the project's `CLAUDE.md` "Project config" section, scaffolded by `kickoff` and `add-project`. The engine is built **hub-ready** (user decision 2026-10-20, for the release-2 `tq hub` modelled on Paperclip's task-and-heartbeat core): every human stop is a signed decision record the orchestrator writes before ending its turn, approvals are records a human settles from the terminal or through a hub-minted actor token, and the whole run state is exported as JSON (`build-gate state`).

**Tech Stack:** Python 3.10+ stdlib only for `tentaqles/workflow/*` and `scripts/build-gate.py` (hook code must stay dependency-free), pytest; Go 1.23+ (`internal/insights`), `go test`; Markdown skills and agents with YAML frontmatter.

**Spec:** `C:\repos\tentaqles\docs\superpowers\specs\2026-10-20-development-process-design.md` (sections 4, 5, 6 rows prd/kickoff/feature/fix/direction/dod/add-project/ship/build, 7 rows build-critic/spec-critic/arbiter, 8.1, 8.3, 9, 10, 12). The security and privacy pack (threat-model, classify-data, privacy-review, supply-chain, llm-security-review, the four reviewer agents, guard rules 8.2, `tq redact`, the manifest `data:` block, triage questions) is split into the companion plans `2026-10-20-dev-process-security-privacy-tq.md` and `2026-10-20-dev-process-security-privacy-plugin.md`; `kickoff` names those skills and the companion plan ships them in the same release.

All paths below are relative to the monorepo root `C:\repos\tentaqles\tentaqles` unless they start with `C:\`. Python test commands assume `cd plugin` first and the interpreter `tq_env.sh` resolves (`source scripts/tq_env.sh && "$TENTAQLES_PY" -m pytest ...` when bare `python` is not the right one).

## Global Constraints

- Hook code (`plugin/tentaqles/workflow/*.py`, `plugin/scripts/build-gate.py`) is stdlib only, never touches the network, fails open on internal errors with a message, and finishes inside the 20 s hook timeout (spec §3.5, plugin `CLAUDE.md`).
- Evidence comes only from what the gate records itself: `verify`, `record-review`, `dod`. Text in a transcript never counts (spec §3.4).
- Signed state (`approved.json`, `evidence.jsonl`, `reviews.jsonl`, `dod.json`, `session.json`) uses the existing `sign()`/`verified()` HMAC helpers; an unsigned or tampered record counts as absent.
- Human-only commands (`approve`, `unlock-tests`, `finish`, new: `judge`) go through `human_confirm()` (TTY + typed code) and are refused to the agent by the hook's shell check, exactly like `approve` today.
- The Stop hook honours `stop_hook_active` and blocks at most `MAX_STOP_BLOCKS` (1) times per problem, then lets the next stop through (never trap the user).
- Skill bash blocks that call Python start with the identity-aware snippet pinned by `plugin/tests/test_skill_bootstrap_snippet.py` and use `"$TENTAQLES_PY"`, never bare `python`.
- Skill frontmatter: `name` equals the directory, `description` 40 to 1024 characters (pinned by `plugin/tests/test_skill_frontmatter.py`).
- Every skill ends with explicit-path staging. Never `git add -A`, `git add .`, `--no-verify`, `-c user.email=`. Never read or print `.env*` (use `tq dotenv run -- <cmd>`).
- Verdicts are `PASS`, `FAIL` or `BLOCKED` (the answer-key grammar, spec §3.7, §4.3); no scores anywhere.
- Templates are written fresh under the plugin's MIT licence; nothing is copied from `sources/` (spec §3.10).
- Conventional commits; git identity `reach@tentaqles.ai`; `go test ./...` and `pytest plugin/tests` stay green after every task.
- Plugin version bump to `0.12.0` happens at the end of the companion plan; this plan adds entries under `## [Unreleased]` in `plugin/CHANGELOG.md`.

## Review Focus

1. **A plan without a `workflow` field** (every plan written before this release) must behave as today at Stop: no DoD requirement. Pinned in Task 5 (`test_stop_without_workflow_is_unchanged`).
2. **A `CLAUDE.md` with a malformed `tq-project` block** (trailing comma, unknown key, non-string command) must not crash the hook: `load_project_config` returns `{}` plus a warning, and `dod` reports `project_config` as MISSING instead of raising. Pinned in Task 2 (`test_malformed_block_returns_empty_and_warning`) and Task 5 (`test_dod_with_invalid_project_config_is_missing_item`).
3. **A reviewer name or result the orchestrator misspells** (`build-critc`, `pass`, `Cannot Judge`) must be refused by `record-review`, not silently recorded as a new reviewer. Pinned in Task 4 (`test_record_review_rejects_unknown_reviewer_or_result`).
4. **A PRD whose section 10 heading is written differently** (`## 10 Open questions`, `## 10. Open Questions`, `## 10) ...`) must still be parsed; one with no section 10 at all is *not* planning-ready. Pinned in Task 2 (`test_prd_section_10_heading_variants`, `test_prd_without_section_10_is_not_ready`).
5. **A DoD override with an empty reason** must be refused (the reason is what `tq insights` and `LEARNINGS.md` record). Pinned in Task 5 (`test_dod_override_requires_reason`).

---

## File structure

| Path | Responsibility |
|---|---|
| `plugin/templates/process/prd.md` | PRD template, 11 numbered sections, FR shape, `NEEDS-DECISION` tag |
| `plugin/templates/process/adr-0000-template.md` | MADR-shaped ADR |
| `plugin/templates/process/spec-slice.md` | feature spec slice (`derived_from`, `data_classes_touched`, acceptance checks, migration plan) |
| `plugin/templates/process/fix-spec.md` | bug intake (observed, expected, environment, data classes, severity, hotfix) |
| `plugin/templates/process/LEARNINGS.md` | Rules + append-only Log with the `L<n>`/`D<n>`/`H<n>` row format |
| `plugin/templates/process/CLAUDE-project.md` | project `CLAUDE.md` scaffold: Reading path, Must not trade off, Project config with the `json tq-project` block |
| `plugin/templates/process/review-contract.md` | the answer-key verdict grammar every reviewer returns |
| `plugin/tentaqles/workflow/project.py` | `load_project_config(root)`: parse the `tq-project` block (stdlib) |
| `plugin/tentaqles/workflow/prd.py` | `prd_status(root)`: FR and `NEEDS-DECISION` counts, planning-ready |
| `plugin/scripts/prd-status.py` | CLI for skills: prints `prd_status` as JSON |
| `plugin/tentaqles/workflow/gate.py` | (modify) `workflow`/`hotfix` plan fields, `cmd_record_review`, `cmd_judge`, `cmd_dod`, `reviews_for`, DoD Stop rule, events log, approval snapshot of project config |
| `plugin/scripts/session-preamble.py` | (modify) reading path + open decision count when `docs/prd.md` exists |
| `plugin/agents/build-critic.md` | (modify) answer-key grammar, `BUILT-BY-ME` line |
| `plugin/agents/spec-critic.md` | new: compliance reviewer |
| `plugin/agents/arbiter.md` | new: one disputed finding, read-only |
| `plugin/agents/build-planner.md` | (modify) `workflow`, `hotfix`, `spec` top-level fields |
| `plugin/skills/build/SKILL.md`, `reference.md` | (modify) crew, record-review, judge, dod |
| `plugin/skills/{prd,kickoff,feature,fix,direction,dod}/SKILL.md` | new skills |
| `plugin/skills/add-project/SKILL.md`, `plugin/skills/ship/SKILL.md` | (modify) scaffold; DoD evidence + size advisory |
| `internal/insights/process.go`, `process_test.go` | `ReadProcessEvents`, `ProcessReport` |
| `internal/insights/report.go`, `render.go` | (modify) `Options.BuildGateDir`, `Report.Process`, text section |
| `plugin/tests/test_process_templates.py`, `test_project_config.py`, `test_prd_status.py`, `test_build_gate.py` (extend), `test_session_preamble.py` (extend), `test_agents.py`, `test_process_skills.py` | tests |
| `plugin/CHANGELOG.md`, `plugin/README.md`, `plugin/CLAUDE.md`, `docs/INSIGHTS.md` | docs |

---

### Task 1: Process templates

**Files:**
- Create: `plugin/templates/process/prd.md`, `adr-0000-template.md`, `spec-slice.md`, `fix-spec.md`, `LEARNINGS.md`, `CLAUDE-project.md`, `review-contract.md`
- Test: `plugin/tests/test_process_templates.py`

**Interfaces:**
- Produces: the template files, read by skills as `"$_tqe/templates/process/<name>"`. The PRD shape parsed by Task 2: `## <n>. <title>` section headings, FRs as `### FR-01 <title>` headings each followed by a `**Acceptance checks**` list, open questions as list items starting with `NEEDS-DECISION` under section 10. The `CLAUDE.md` scaffold's fenced block starts with the exact line ```` ```json tq-project ```` and is parsed by Task 2.

- [ ] **Step 1: Write the failing test**

```python
# plugin/tests/test_process_templates.py
"""The process templates ship with the plugin and keep the shapes the parsers rely on."""

from __future__ import annotations

import json
import re
from pathlib import Path

import pytest

TEMPLATES = Path(__file__).resolve().parents[1] / "templates" / "process"
NAMES = ["prd.md", "adr-0000-template.md", "spec-slice.md", "fix-spec.md",
         "LEARNINGS.md", "CLAUDE-project.md", "review-contract.md"]


@pytest.mark.parametrize("name", NAMES)
def test_template_exists_and_is_not_empty(name: str) -> None:
    path = TEMPLATES / name
    assert path.is_file(), path
    assert len(path.read_text(encoding="utf-8").strip()) > 200


def test_prd_has_eleven_numbered_sections_and_fr_shape() -> None:
    text = (TEMPLATES / "prd.md").read_text(encoding="utf-8")
    heads = re.findall(r"^## (\d+)\. ", text, flags=re.MULTILINE)
    assert heads == [str(i) for i in range(1, 12)], heads
    assert "### FR-01 " in text
    assert "**Acceptance checks**" in text
    assert "NEEDS-DECISION" in text
    assert "### 3.2 Non-goals" in text


def test_claude_scaffold_has_fixed_sections_and_valid_block() -> None:
    text = (TEMPLATES / "CLAUDE-project.md").read_text(encoding="utf-8")
    for h in ("## Reading path", "## Must not trade off", "## Project config"):
        assert h in text, h
    m = re.search(r"^```json tq-project\n(.*?)\n```", text, flags=re.MULTILINE | re.DOTALL)
    assert m, "scaffold must carry a ```json tq-project fenced block"
    cfg = json.loads(m.group(1))
    assert set(cfg) >= {"commands", "architecture_paths", "changelog"}
    assert "test" in cfg["commands"]


def test_learnings_has_rules_and_log_with_row_format() -> None:
    text = (TEMPLATES / "LEARNINGS.md").read_text(encoding="utf-8")
    assert "## Rules" in text and "## Log" in text
    assert "L<n> · <date> · <slug>#<cp> · <gate> ·" in text
    assert "D<n> · <date> · <slug> · dod-override ·" in text


def test_review_contract_defines_the_verdict_grammar() -> None:
    text = (TEMPLATES / "review-contract.md").read_text(encoding="utf-8")
    for token in ("RESULT: PASS", "RESULT: FAIL", "RESULT: BLOCKED", "CANNOT JUDGE", "BUILT-BY-ME:"):
        assert token in text, token
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd plugin && python -m pytest tests/test_process_templates.py -q`
Expected: FAIL, `AssertionError` on `path.is_file()` for `prd.md`.

- [ ] **Step 3: Write the templates**

`plugin/templates/process/prd.md`:

````markdown
# <Product or project name>: product requirements

Status: draft | planning-ready | superseded by <link>
Owner: <name>    Updated: <YYYY-MM-DD>

A PRD says what and why. How is decided in ADRs and spec slices. Every later
document cites a section number from here (`PRD §4 FR-03`).

Planning-ready means: section 10 is empty and every FR has at least one
acceptance check. `/tentaqles:kickoff` and `/tentaqles:feature` check this
mechanically (`prd-status.py`); a PRD that is not planning-ready blocks planning.

## 1. Problem and evidence
What hurts today, for whom, and the evidence (numbers, quotes, tickets). One
paragraph, then bullets.

## 2. Users and roles
| Role | Who they are | What they need from this | Can | Cannot |
|---|---|---|---|---|

## 3. Goals
Three to five outcomes, each measurable.
- G1:
- G2:

### 3.2 Non-goals
What this will not do, so nobody plans it by accident.
-

## 4. Functional requirements
One `### FR-nn` per requirement. Acceptance checks are binary, observable
statements someone can run or decide blind ("the second submit returns 409",
"a tester picks the new copy in a blind A/B"), never scores or adjectives.

### FR-01 <short imperative title>
Why: <one sentence tying it to a goal>
**Acceptance checks**
- <binary, observable statement>
- <binary, observable statement>

### FR-02 <title>
Why:
**Acceptance checks**
-

## 5. Data
Data classes handled (public, internal, pii, sensitive, financial, children,
secrets), where each comes from, where it lives, how long it is kept.
| Data | Class | Source | Store | Retention |
|---|---|---|---|---|

## 6. Security
Trust boundaries (who talks to what), the authentication model, roles and
permissions, and the abuse cases you expect (what a malicious user would try).
- Boundaries:
- Authentication:
- Abuse cases:

## 7. Non-functional requirements
Numbers only: load, latency, availability, data volume, cost ceiling, supported
browsers or platforms, accessibility level.
-

## 8. Data model sketch
Entities and their relations in prose or a small diagram. Not the schema.

## 9. Stack and constraints
Language, framework, database, hosting, must-use libraries, must-not-use
libraries, and why each constraint exists.
-

## 10. Open questions
Each item starts with `NEEDS-DECISION` and names how it could be settled (ask,
research, prototype) and who decides. Remove the item when decided and move the
answer into the section it belongs to. Empty section = ready to plan.
- NEEDS-DECISION: <question> (settle by: ask <who> | research | prototype)

## 11. Releases
Which FRs ship in release 1, 2, 3, and what "done" means for each release.
| Release | FRs | Done when |
|---|---|---|
````

`plugin/templates/process/adr-0000-template.md`:

````markdown
# ADR-NNNN: <decision in one line>

Status: proposed | accepted | superseded by ADR-NNNN
Date: <YYYY-MM-DD>
Cites: PRD §<n>, threat model boundary <name> (when relevant)

## Context
The forces at play: the requirement, the constraint, the problem that made a
decision necessary. Facts, not opinions.

## Decision
What we decided, in the active voice ("We use ...", "Every service ...").

## Consequences
What becomes easier, what becomes harder, what we now must do (a test, a rule
in CLAUDE.md "Must not trade off", a migration).

## Alternatives considered
| Option | Why not |
|---|---|

Records are never edited after acceptance. A changed mind is a new record that
supersedes this one.
````

`plugin/templates/process/spec-slice.md`:

````markdown
# <Feature name>

Status: draft | approved | implemented
derived_from: PRD §4 FR-nn (or "no PRD: legacy repository, see kickoff later")
data_classes_touched: [] # from the manifest data: block, e.g. [pii]
trust_boundaries_crossed: [] # from docs/threat-model.md, e.g. [browser->api]

## Problem
What is wrong or missing for whom, in two sentences.

## Behaviour
What the system does when this is done. Inputs, outputs, rules the code will
branch on (the grill-me interview answers go here as a table).

| ID | Rule | Example | Decided by |
|---|---|---|---|

## Acceptance checks
Binary, observable, each traceable to an FR acceptance check or a rule above.
- [ ]

## Out of scope
-

## Open questions
Each tagged NEEDS-DECISION. Planning is blocked while any remain.
-

## Migration plan
Only when schema files change: expand/contract steps, backfill, rollback. Else
write "none: no schema change".

## Assumptions
Decisions taken where the PRD and the user were silent, with the source.
-
````

`plugin/templates/process/fix-spec.md`:

````markdown
# fix: <slug>

Status: reproducing | reproduced | fixed
Severity: critical | high | medium | low
Hotfix: no | yes (reason: <production impact now, fix path obvious>)
data_classes_touched: [] # personal data exposed? then this bug is also an incident: start the clock (docs/incident-playbook.md)

## Report (verbatim)
Paste the report, stack trace or failing command exactly as received.

## Observed
What happens.

## Expected
What should happen, citing the PRD FR or rule when one exists.

## Environment
Version or commit, OS, browser or client, data set, flags.

## Reproduction
The locked regression test: `<path>::<test name>` and the command that runs it.
Written by the tester before any source edit and locked by `build-gate lock-tests`.

## Root cause
One paragraph after the fix: the actual cause, not the symptom.

## Lesson
The one sentence that would have prevented it (also appended to docs/LEARNINGS.md).

## Migration plan
Only when schema files change. Else "none".
````

`plugin/templates/process/LEARNINGS.md`:

````markdown
# Learnings

Rules are promoted lessons: a lesson seen twice, or a human rejection at stop 2,
becomes a rule here and (when it protects a property) a line under "Must not
trade off" in CLAUDE.md. The log is append-only.

## Rules
- R1 · <date> · <rule in one sentence> · from L<n>, L<m>

## Log
Format, one line each:
- `L<n> · <date> · <slug>#<cp> · <gate> · failure: <what> / root cause: <why> / fix: <what changed> / lesson: <one sentence>`
- `D<n> · <date> · <slug> · dod-override · <reason stated by the human>`
- `H<n> · <date> · <slug> · stop-2 · <what the human rejected and which gate should have caught it>`
````

`plugin/templates/process/CLAUDE-project.md`:

````markdown
# <Project name>

## Reading path
To work on this project read `docs/prd.md` and `docs/adr/` first, then the
spec slice you were given (`docs/specs/<slug>.md`). `docs/LEARNINGS.md` lists
the rules this repo learned the hard way.

## Must not trade off
One line per rule; each names the test that enforces it. The critic treats a
change that weakens one as a BLOCKER.
- <rule> · enforced by `<test file>::<test name>`

## Project config
Humans read the prose; tools read the fenced block. Keep both in step.

- Domain and roles: <who uses this and as what>
- Acting as a role in tests: <a seeded test user and a factory; never real credentials>
- URLs: local <http://127.0.0.1:PORT>, staging <...>

```json tq-project
{
  "commands": {
    "test": "<the command that runs the whole suite, e.g. python -m pytest -q>",
    "lint": "<optional, e.g. ruff check .>",
    "typecheck": "<optional, e.g. mypy src>",
    "build": "<optional>",
    "run": "<optional: starts the app on 127.0.0.1 for the breaker>"
  },
  "architecture_paths": ["src/core/**", "db/schema/**", "infra/**"],
  "classified_paths": [],
  "changelog": "CHANGELOG.md"
}
```

`architecture_paths` are globs whose changes require an ADR in the definition
of done. `classified_paths` mirrors the manifest `data:` block paths so the gate
can require `docs/privacy.md` to change with them. `changelog` is the file that
must change on every feature or fix (empty string: this project keeps none).

## Architecture rules
<layering, dependency direction, where things live>

## Shared helpers
<the helpers everyone should reuse instead of rewriting>
````

`plugin/templates/process/review-contract.md`:

````markdown
# Review contract (answer key)

Every reviewer returns this and nothing else. Verdicts are pre-registered and
binary; there are no scores.

```
RESULT: PASS | FAIL | BLOCKED
BUILT-BY-ME: no | yes — <how you were involved>
F1 BLOCKER <file:line> — <problem> → <concrete fix>
F2 MAJOR   <file:line> — <problem> → <concrete fix>
F3 MINOR   <file:line> — <problem> → <concrete fix>
U1 CANNOT JUDGE — <check> — <what a human must look at, and where>
```

- `FAIL` when any BLOCKER or MAJOR exists. MINOR alone is `PASS` (listed so it
  is logged).
- `BLOCKED` when a check you were asked for cannot be judged from the files
  (missing environment, a decision only a human can make). Every `U` row names
  the check and what the human must look at. A BLOCKED result goes to the human
  with the `U` rows; the human settles it in their own terminal with
  `build-gate judge <slug> <id> --reviewer <you> --result PASS|FAIL`.
- Every `F` row has a file and line and a concrete fix. No line, no finding.
- `BUILT-BY-ME: yes` when you had any part in producing the change (you wrote
  code, answered the planner, fixed a round). The orchestrator then discards the
  verdict and runs a fresh reviewer.
- In later rounds, rule on each earlier finding first:
  `F<n> resolved | upheld | withdrawn — <why>`, verifying a "fixed" claim by
  reading the cited lines. Then list new findings, numbered on.
````

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd plugin && python -m pytest tests/test_process_templates.py -q`
Expected: `11 passed`.

- [ ] **Step 5: Commit**

```bash
git add plugin/templates/process plugin/tests/test_process_templates.py
git commit -m "feat(plugin): process templates (PRD, ADR, spec slice, fix spec, LEARNINGS, CLAUDE scaffold, review contract)"
```

---

### Task 2: Project config block and PRD status parsers

**Files:**
- Create: `plugin/tentaqles/workflow/project.py`, `plugin/tentaqles/workflow/prd.py`, `plugin/scripts/prd-status.py`
- Test: `plugin/tests/test_project_config.py`, `plugin/tests/test_prd_status.py`

**Interfaces:**
- Produces: `load_project_config(root: Path) -> tuple[dict, str]` returning `(config, warning)`; `config` is `{}` when the block is absent or invalid. Keys: `commands` (dict str→str), `architecture_paths` (list[str]), `classified_paths` (list[str]), `changelog` (str). Unknown or wrong-typed keys are dropped and named in the warning.
- Produces: `prd_status(root: Path) -> dict` with keys `exists`, `path`, `frs`, `frs_without_checks`, `open_decisions`, `has_section_10`, `planning_ready`.
- Produces: CLI `prd-status.py [--cwd DIR]` printing that dict as JSON; exit 0 planning-ready, 3 not ready, 4 no PRD.

- [ ] **Step 1: Write the failing tests**

```python
# plugin/tests/test_project_config.py
"""The ```json tq-project block in CLAUDE.md is the machine-readable project config."""

from __future__ import annotations

from pathlib import Path

from tentaqles.workflow.project import load_project_config

BLOCK = '''# Demo

## Project config

```json tq-project
{"commands": {"test": "pytest -q", "lint": "ruff check ."},
 "architecture_paths": ["src/core/**"], "classified_paths": [], "changelog": "CHANGELOG.md"}
```
'''


def test_reads_commands_and_paths(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_text(BLOCK, encoding="utf-8")
    cfg, warn = load_project_config(tmp_path)
    assert warn == ""
    assert cfg["commands"]["test"] == "pytest -q"
    assert cfg["architecture_paths"] == ["src/core/**"]
    assert cfg["changelog"] == "CHANGELOG.md"


def test_missing_file_or_block_is_empty_without_warning(tmp_path: Path) -> None:
    assert load_project_config(tmp_path) == ({}, "")
    (tmp_path / "CLAUDE.md").write_text("# nothing here\n", encoding="utf-8")
    assert load_project_config(tmp_path) == ({}, "")


def test_malformed_block_returns_empty_and_warning(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_text("```json tq-project\n{\"commands\": {\"test\": 1,}}\n```\n", encoding="utf-8")
    cfg, warn = load_project_config(tmp_path)
    assert cfg == {}
    assert "tq-project" in warn and "invalid" in warn.lower()


def test_wrong_types_and_unknown_keys_are_dropped_with_warning(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_text(
        '```json tq-project\n{"commands": {"test": "pytest", "lint": 3}, "architecture_paths": "src/**", "colour": 1}\n```\n',
        encoding="utf-8")
    cfg, warn = load_project_config(tmp_path)
    assert cfg["commands"] == {"test": "pytest"}
    assert "architecture_paths" not in cfg
    assert "commands.lint" in warn and "architecture_paths" in warn and "colour" in warn


def test_crlf_and_bom_are_tolerated(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_bytes(b"\xef\xbb\xbf# x\r\n```json tq-project\r\n{\"commands\": {\"test\": \"go test ./...\"}}\r\n```\r\n")
    cfg, _ = load_project_config(tmp_path)
    assert cfg["commands"]["test"] == "go test ./..."
```

```python
# plugin/tests/test_prd_status.py
"""prd_status decides planning-ready mechanically from docs/prd.md."""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

from tentaqles.workflow.prd import prd_status

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "prd-status.py"

READY = """# X: product requirements

## 1. Problem and evidence
p
## 4. Functional requirements
### FR-01 Export CSV
Why: g1
**Acceptance checks**
- a download of 10 rows produces 11 lines
### FR-02 Delete account
**Acceptance checks**
- the row is gone after 24h
## 10. Open questions
Each item starts with `NEEDS-DECISION`. Empty section = ready to plan.
## 11. Releases
"""


def _write(tmp_path: Path, text: str) -> Path:
    (tmp_path / "docs").mkdir(exist_ok=True)
    (tmp_path / "docs" / "prd.md").write_text(text, encoding="utf-8")
    return tmp_path


def test_ready_prd(tmp_path: Path) -> None:
    st = prd_status(_write(tmp_path, READY))
    assert st["exists"] and st["frs"] == 2 and st["frs_without_checks"] == []
    assert st["open_decisions"] == 0 and st["has_section_10"] and st["planning_ready"]


def test_open_decision_blocks(tmp_path: Path) -> None:
    text = READY.replace("## 11. Releases", "- NEEDS-DECISION: which DB? (settle by: research)\n## 11. Releases")
    st = prd_status(_write(tmp_path, text))
    assert st["open_decisions"] == 1 and not st["planning_ready"]


def test_fr_without_checks_blocks(tmp_path: Path) -> None:
    text = READY.replace("**Acceptance checks**\n- the row is gone after 24h\n", "")
    st = prd_status(_write(tmp_path, text))
    assert st["frs_without_checks"] == ["FR-02"] and not st["planning_ready"]


def test_prd_section_10_heading_variants(tmp_path: Path) -> None:
    for head in ("## 10 Open questions", "## 10. Open Questions", "## 10) Open questions"):
        st = prd_status(_write(tmp_path, READY.replace("## 10. Open questions", head)))
        assert st["has_section_10"], head


def test_prd_without_section_10_is_not_ready(tmp_path: Path) -> None:
    text = READY.replace("## 10. Open questions\nEach item starts with `NEEDS-DECISION`. Empty section = ready to plan.\n", "")
    st = prd_status(_write(tmp_path, text))
    assert not st["has_section_10"] and not st["planning_ready"]


def test_tag_in_prose_does_not_count(tmp_path: Path) -> None:
    assert prd_status(_write(tmp_path, READY))["open_decisions"] == 0


def test_no_prd(tmp_path: Path) -> None:
    st = prd_status(tmp_path)
    assert st == {"exists": False, "path": "docs/prd.md", "frs": 0, "frs_without_checks": [],
                  "open_decisions": 0, "has_section_10": False, "planning_ready": False}


def test_cli_exit_codes(tmp_path: Path) -> None:
    def run(cwd: Path) -> tuple[int, dict]:
        p = subprocess.run([sys.executable, str(SCRIPT), "--cwd", str(cwd)], capture_output=True, text=True, timeout=60)
        return p.returncode, json.loads(p.stdout)
    code, out = run(tmp_path)
    assert code == 4 and not out["exists"]
    code, out = run(_write(tmp_path, READY))
    assert code == 0 and out["planning_ready"]
    code, _ = run(_write(tmp_path, READY.replace("## 11. Releases", "- NEEDS-DECISION: x\n## 11. Releases")))
    assert code == 3
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd plugin && python -m pytest tests/test_project_config.py tests/test_prd_status.py -q`
Expected: FAIL with `ModuleNotFoundError: No module named 'tentaqles.workflow.project'`.

- [ ] **Step 3: Implement**

`plugin/tentaqles/workflow/project.py`:

```python
"""Machine-readable project config: the ```json tq-project block in CLAUDE.md.

Humans read the prose of the "Project config" section; tools (the build gate's
definition of done, the skills) read this block. Stdlib only: the gate hook
imports it.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

FENCE_RE = re.compile(r"^```json tq-project[ \t]*\n(.*?)\n```", re.MULTILINE | re.DOTALL)
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
    m = FENCE_RE.search(text.replace("\r\n", "\n"))
    if not m:
        return {}, ""
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
```

`plugin/tentaqles/workflow/prd.py`:

```python
"""Mechanical PRD status: is docs/prd.md planning-ready?

Planning-ready (spec §4.2): section 10 exists and holds no NEEDS-DECISION item,
and every FR has at least one acceptance check. Stdlib only.
"""

from __future__ import annotations

import re
from pathlib import Path

PRD_REL = "docs/prd.md"
H2_RE = re.compile(r"^## (\d+)[.)]?\s+(.*)$")
FR_RE = re.compile(r"^### (FR-\d+)\b")
CHECKS_HEAD = "**Acceptance checks**"
ITEM_RE = re.compile(r"^\s*[-*]\s+(\[[ xX]\]\s*)?\S")
TAG_ITEM_RE = re.compile(r"^\s*[-*]\s+(\[[ xX]\]\s*)?NEEDS-DECISION\b")


def _sections(lines: list[str]) -> dict[int, list[str]]:
    out: dict[int, list[str]] = {}
    cur: int | None = None
    for ln in lines:
        m = H2_RE.match(ln)
        if m:
            cur = int(m.group(1))
            out[cur] = []
            continue
        if cur is not None:
            out[cur].append(ln)
    return out


def _frs(section: list[str]) -> tuple[list[str], list[str]]:
    frs: list[str] = []
    without: list[str] = []
    cur: str | None = None
    checks = 0
    in_checks = False
    for ln in section + ["### FR-END"]:
        m = FR_RE.match(ln)
        if m:
            if cur is not None and checks == 0:
                without.append(cur)
            if m.group(1) == "FR-END":
                break
            cur, checks, in_checks = m.group(1), 0, False
            frs.append(cur)
            continue
        if cur is None:
            continue
        if ln.strip().startswith(CHECKS_HEAD):
            in_checks = True
        elif in_checks and ITEM_RE.match(ln):
            checks += 1
        elif in_checks and ln.strip():
            in_checks = False
    return frs, without


def prd_status(root: Path) -> dict:
    base = {"exists": False, "path": PRD_REL, "frs": 0, "frs_without_checks": [],
            "open_decisions": 0, "has_section_10": False, "planning_ready": False}
    try:
        text = (Path(root) / PRD_REL).read_text(encoding="utf-8-sig")
    except OSError:
        return base
    secs = _sections(text.replace("\r\n", "\n").split("\n"))
    frs, without = _frs(secs.get(4, []))
    has10 = 10 in secs
    open_items = sum(1 for ln in secs.get(10, []) if TAG_ITEM_RE.match(ln)) if has10 else 0
    ready = has10 and open_items == 0 and bool(frs) and not without
    return {"exists": True, "path": PRD_REL, "frs": len(frs), "frs_without_checks": without,
            "open_decisions": open_items, "has_section_10": has10, "planning_ready": ready}
```

`plugin/scripts/prd-status.py`:

```python
#!/usr/bin/env python3
"""Print docs/prd.md status as JSON. Exit 0 planning-ready, 3 not ready, 4 no PRD.

Used by the kickoff, feature and prd skills, and by the SessionStart preamble.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import argparse
import json
from pathlib import Path

from tentaqles.workflow.prd import prd_status


def main() -> int:
    ap = argparse.ArgumentParser(prog="prd-status")
    ap.add_argument("--cwd", default=os.getcwd(), help="repository root holding docs/prd.md")
    a = ap.parse_args()
    st = prd_status(Path(a.cwd))
    print(json.dumps(st, ensure_ascii=False))
    if not st["exists"]:
        return 4
    return 0 if st["planning_ready"] else 3


if __name__ == "__main__":
    raise SystemExit(main())
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd plugin && python -m pytest tests/test_project_config.py tests/test_prd_status.py -q`
Expected: `13 passed`.

- [ ] **Step 5: Commit**

```bash
git add plugin/tentaqles/workflow/project.py plugin/tentaqles/workflow/prd.py plugin/scripts/prd-status.py plugin/tests/test_project_config.py plugin/tests/test_prd_status.py
git commit -m "feat(workflow): parse the CLAUDE.md tq-project block and the PRD planning-ready status"
```

---

### Task 3: SessionStart reading path and open decisions

**Files:**
- Modify: `plugin/scripts/session-preamble.py` (`main`)
- Test: `plugin/tests/test_session_preamble.py` (append)

**Interfaces:**
- Consumes: `tentaqles.workflow.prd.prd_status(root)` (Task 2), `tentaqles.workflow._common.find_git_root(start)`.
- Produces: one SessionStart line when `docs/prd.md` exists at the git root (or cwd): `Reading path: docs/prd.md, docs/adr/, then your spec slice · PRD: <frs> FRs, <open> open NEEDS-DECISION, planning-ready: yes|no`.

- [ ] **Step 1: Write the failing test**

Read `plugin/tests/test_session_preamble.py` first for the exact `_workspace(tmp_path)` and `_run_preamble(cwd, *args)` helper signatures, then append:

```python
def test_prd_reading_path_is_injected_when_prd_exists(tmp_path):
    ws = _workspace(tmp_path)
    (ws / "docs").mkdir()
    (ws / "docs" / "prd.md").write_text(
        "# X\n## 4. Functional requirements\n### FR-01 a\n**Acceptance checks**\n- ok\n"
        "## 10. Open questions\n- NEEDS-DECISION: db?\n", encoding="utf-8")
    out = _run_preamble(ws, "--memory-only")
    assert "Reading path: docs/prd.md, docs/adr/" in out
    assert "1 FRs, 1 open NEEDS-DECISION, planning-ready: no" in out


def test_no_prd_no_reading_path(tmp_path):
    out = _run_preamble(_workspace(tmp_path), "--memory-only")
    assert "Reading path" not in out
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd plugin && python -m pytest tests/test_session_preamble.py -q -k reading_path`
Expected: FAIL on the `Reading path` assertion.

- [ ] **Step 3: Implement**

Add above `main()` in `plugin/scripts/session-preamble.py`:

```python
def _prd_context(cwd: str) -> str:
    """One line pointing at the PRD when the repo keeps one (spec §8.3)."""
    try:
        from tentaqles.workflow._common import find_git_root
        from tentaqles.workflow.prd import prd_status

        st = prd_status(Path(find_git_root(cwd) or cwd))
        if not st["exists"]:
            return ""
        ready = "yes" if st["planning_ready"] else "no"
        return ("Reading path: docs/prd.md, docs/adr/, then your spec slice · "
                f"PRD: {st['frs']} FRs, {st['open_decisions']} open NEEDS-DECISION, planning-ready: {ready}")
    except Exception:
        return ""
```

In `main()`, the `--memory-only` branch becomes:

```python
        if memory_only:
            prd_line = _prd_context(cwd)
            if prd_line:
                print(prd_line)
            temporal = _get_temporal_context(cwd, context)
            if temporal:
                print(temporal)
            return
```

and after `summary = format_context_summary(context, checks)` add:

```python
        prd_line = _prd_context(cwd)
        if prd_line:
            summary += "\n\n" + prd_line
```

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_session_preamble.py -q`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/scripts/session-preamble.py plugin/tests/test_session_preamble.py
git commit -m "feat(hooks): SessionStart points at the PRD and counts open decisions"
```

---

### Task 4: Gate: workflow fields, review records, events log

**Files:**
- Modify: `plugin/tentaqles/workflow/gate.py`
- Modify: `plugin/tests/test_build_gate.py` (`test_full_pass_and_downgrade` and new tests)

**Interfaces:**
- Produces (plan JSON, top level): `"workflow": "build" | "feature" | "fix" | "kickoff"` (absent = `build`), `"hotfix": true|false`, `"spec": "docs/specs/<slug>.md"`.
- Produces: `REVIEWERS = ("build-critic", "spec-critic", "security-reviewer", "privacy-reviewer", "llm-security-reviewer", "breaker")`, `RESULTS = ("PASS", "FAIL", "BLOCKED")`.
- Produces: `cmd_record_review(root, slug, cid, reviewer, result, counts: dict) -> str` appending a signed line to `.claude/build/<slug>/reviews.jsonl` `{checkpoint, reviewer, result, blockers, majors, minors, unjudged, tree, ts, human: false}`; raises `SystemExit` on an unknown reviewer or result.
- Produces: `cmd_judge(root, slug, cid, reviewer, result, note) -> str` (human-only; same record with `human: true`, `result` PASS or FAIL only).
- Produces: `reviews_for(root, slug, cid, tree) -> dict[reviewer, record]` (latest verified record per reviewer at that tree).
- Produces: `log_event(root, kind, **fields)` appending to `key_dir()/events.jsonl` (`ts`, `kind`, `slug`, `checkpoint`, `workflow`, `project` = root basename, plus fields). Never file names or text.
- Changes: `check_plan` refuses `review: passed` unless `reviews_for` at the current tree has `build-critic` PASS and no FAIL/BLOCKED; refuses `review: n/a` unless the plan has `"hotfix": true`.
- CLI: `build-gate record-review <slug> <id> --reviewer R --result PASS|FAIL|BLOCKED [--blockers N] [--majors N] [--minors N] [--unjudged N]`; `build-gate judge <slug> <id> --reviewer R --result PASS|FAIL --note TEXT` (human-only).

- [ ] **Step 1: Write the failing tests**

In `plugin/tests/test_build_gate.py`, change `test_full_pass_and_downgrade` so the review pass is recorded first, and add the new tests. Keep the existing helpers (`repo`, `approve_and_lock`, `plan_file`, `with_gate`, `hook`, `write`, `bash`, `PLAN`).

```python
def test_full_pass_and_downgrade(repo):
    approve_and_lock(repo)
    p = plan_file(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    full = with_gate(PLAN, 2, behavior="passed", review="passed", status="passed")
    assert hook(repo, write(p, full))[0] == 0
    # reopening (moving toward pending) is always allowed
    assert hook(repo, write(p, PLAN))[0] == 0


def test_review_pass_needs_a_recorded_critic_pass(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed", review="passed")))
    assert code == 2 and "record-review" in err and "build-critic" in err


def test_review_pass_refused_while_any_reviewer_failed_or_blocked(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    gate.cmd_record_review(repo, "demo", 2, "spec-critic", "BLOCKED", {"unjudged": 1})
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed", review="passed")))
    assert code == 2 and "spec-critic" in err and "BLOCKED" in err
    # a human judgement settles it
    gate.cmd_judge(repo, "demo", 2, "spec-critic", "PASS", "checked the env by hand")
    assert hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed", review="passed")))[0] == 0


def test_review_records_are_tied_to_the_tree(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    (repo / "app.py").write_text("def f():\n    return 2\n", encoding="utf-8")
    gate.cmd_verify(repo, "demo", 2)  # fresh behaviour evidence, stale review
    code, _, err = hook(repo, write(plan_file(repo), with_gate(PLAN, 2, behavior="passed", review="passed")))
    assert code == 2 and "build-critic" in err


def test_record_review_rejects_unknown_reviewer_or_result(repo):
    approve_and_lock(repo)
    with pytest.raises(SystemExit):
        gate.cmd_record_review(repo, "demo", 2, "build-critc", "PASS", {})
    with pytest.raises(SystemExit):
        gate.cmd_record_review(repo, "demo", 2, "build-critic", "pass", {})
    with pytest.raises(SystemExit):
        gate.cmd_judge(repo, "demo", 2, "build-critic", "BLOCKED", "x")


def test_forged_review_record_is_ignored(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    path = repo / ".claude" / "build" / "demo" / "reviews.jsonl"
    tree = gate.tree_hash(repo)
    path.write_text(json.dumps({"checkpoint": 2, "reviewer": "build-critic", "result": "PASS", "tree": tree, "ts": "now"}) + "\n",
                    encoding="utf-8")
    assert gate.reviews_for(repo, "demo", 2, tree) == {}


def test_judge_and_record_review_are_human_only_for_judge(repo):
    code, _, err = hook(repo, bash("python build-gate.py judge demo 2 --reviewer spec-critic --result PASS --note ok"))
    assert code == 2 and "human" in err.lower()
    assert hook(repo, bash("python build-gate.py record-review demo 2 --reviewer build-critic --result PASS"))[0] == 0


def test_review_na_only_for_hotfix_plans(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    na = with_gate(PLAN, 2, behavior="passed", review="n/a", status="passed")
    code, _, err = hook(repo, write(plan_file(repo), na))
    assert code == 2 and "hotfix" in err
    save_plan(repo, {**PLAN, "workflow": "fix", "hotfix": True})
    gate.cmd_approve(repo, "demo")
    gate.cmd_verify(repo, "demo", 2)
    assert hook(repo, write(plan_file(repo), {**na, "workflow": "fix", "hotfix": True}))[0] == 0


def test_events_are_logged_aggregate_only(repo, tq_home):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "FAIL", {"blockers": 1, "majors": 0, "minors": 2})
    lines = [json.loads(l) for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]
    kinds = [l["kind"] for l in lines]
    assert kinds == ["verify", "review"]
    assert lines[0]["ok"] is True and lines[0]["slug"] == "demo" and lines[0]["checkpoint"] == 2
    assert lines[1]["reviewer"] == "build-critic" and lines[1]["result"] == "FAIL" and lines[1]["blockers"] == 1
    for l in lines:
        assert "app.py" not in json.dumps(l) and "tests/" not in json.dumps(l)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd plugin && python -m pytest tests/test_build_gate.py -q -k "review or events or hotfix or judge"`
Expected: FAIL with `AttributeError: module 'tentaqles.workflow.gate' has no attribute 'cmd_record_review'`.

- [ ] **Step 3: Implement in `gate.py`**

Constants, after `HEARTBEAT = ".hook-seen"`:

```python
REVIEWERS = ("build-critic", "spec-critic", "security-reviewer", "privacy-reviewer", "llm-security-reviewer", "breaker")
RESULTS = ("PASS", "FAIL", "BLOCKED")
WORKFLOWS = ("build", "feature", "fix", "kickoff")
DOD_WORKFLOWS = ("feature", "fix", "kickoff")
HUMAN_ONLY = ("approve", "unlock-tests", "finish", "judge")
```

Find the existing place where the shell check refuses human-only subcommands (`_gate_invocation` / `check_shell`; grep for `"approve"` in `gate.py`) and replace its literal tuple with `HUMAN_ONLY` so `judge` is refused to the agent.

Events log (after `save_session`):

```python
def events_path() -> Path:
    return key_dir() / "events.jsonl"


def log_event(root: Path, kind: str, slug: str, **fields) -> None:
    """Aggregate-only process event for `tq insights`: never a file name or text."""
    try:
        plan = load_plan(root, slug) or {}
        row = {"ts": now_iso(), "kind": kind, "slug": slug, "project": Path(root).name,
               "workflow": plan.get("workflow") or "build"}
        row.update({k: v for k, v in fields.items() if v is not None})
        append_jsonl(events_path(), row)
    except Exception:
        pass  # logging never blocks a gate
```

Review records (after `latest_evidence`):

```python
def reviews_path(root: Path, slug: str) -> Path:
    return state_dir(root, slug) / "reviews.jsonl"


def reviews_for(root: Path, slug: str, cid: int, tree: str | None) -> dict[str, dict]:
    """Latest verified record per reviewer for checkpoint cid at this tree hash."""
    out: dict[str, dict] = {}
    for line in read_text(reviews_path(root, slug)).splitlines():
        try:
            rec = json.loads(line)
        except ValueError:
            continue
        if not verified(rec) or rec.get("checkpoint") != cid or rec.get("tree") != tree:
            continue
        out[rec.get("reviewer", "")] = rec
    return out


def _record_review(root: Path, slug: str, cid: int, reviewer: str, result: str, counts: dict, human: bool, note: str = "") -> str:
    if reviewer not in REVIEWERS:
        raise SystemExit(f"unknown reviewer '{reviewer}'. Use one of: {', '.join(REVIEWERS)}.")
    if result not in RESULTS or (human and result == "BLOCKED"):
        allowed = ("PASS, FAIL" if human else ", ".join(RESULTS))
        raise SystemExit(f"result must be one of {allowed} (got '{result}').")
    plan = load_plan(root, slug)
    if plan is None or not any(c.get("id") == cid for c in plan["checkpoints"]):
        raise SystemExit(f"checkpoint #{cid} doesn't exist in {PLAN_DIR}/{slug}.json.")
    tree = tree_hash(root)
    rec = {"checkpoint": cid, "reviewer": reviewer, "result": result, "tree": tree, "ts": now_iso(), "human": human}
    for k in ("blockers", "majors", "minors", "unjudged"):
        rec[k] = int(counts.get(k) or 0)
    if note:
        rec["note"] = redact(note)[:400]
    append_jsonl(reviews_path(root, slug), sign(rec))
    log_event(root, "judge" if human else "review", slug, checkpoint=cid, reviewer=reviewer, result=result,
              blockers=rec["blockers"], majors=rec["majors"], minors=rec["minors"], unjudged=rec["unjudged"])
    return f"#{cid} {reviewer}: {result} recorded (tree {str(tree)[:12]}{', human' if human else ''})"


def cmd_record_review(root: Path, slug: str, cid: int, reviewer: str, result: str, counts: dict) -> str:
    return _record_review(root, slug, cid, reviewer, result, counts, human=False)


def cmd_judge(root: Path, slug: str, cid: int, reviewer: str, result: str, note: str) -> str:
    return _record_review(root, slug, cid, reviewer, result, {}, human=True, note=note)


def review_problems(root: Path, slug: str, cid: int, th: TreeHash) -> str:
    """'' when the review gate may pass at the current tree, else why not."""
    recs = reviews_for(root, slug, cid, th.get())
    bad = [f"{r} {rec['result']}" for r, rec in sorted(recs.items()) if rec.get("result") != "PASS"]
    if bad:
        return ("reviewer(s) not at PASS for the current code: " + ", ".join(bad)
                + ". Fix and re-review, or for BLOCKED have the human run `build-gate judge`.")
    if "build-critic" not in recs:
        return (f"no build-critic PASS recorded for the current code: run the critic and "
                f"`build-gate record-review {slug} {cid} --reviewer build-critic --result PASS`.")
    return ""
```

In `check_plan`, parse the whole document and add the two rules. Replace the top of the function:

```python
    try:
        doc = json.loads(new_text)
        new = doc["checkpoints"]
        if not isinstance(new, list):
            raise TypeError
    except (ValueError, KeyError, TypeError):
        return
    hotfix = bool(doc.get("hotfix")) if isinstance(doc, dict) else False
```

Inside the loop, right after `g = {name: gate(cp, name) for name in GATES}`:

```python
        if g["review"] == "n/a" and not hotfix:
            raise Block(f"#{cid}: review 'n/a' is only allowed when the plan has \"hotfix\": true (a hotfix skips the critic, never the test).")
```

and at the end of the loop, after the `evidence_fresh` check:

```python
        if "review" in rising:
            why = review_problems(root, slug, cid, th)
            if why:
                raise Block(f"#{cid}: can't mark review passed: {why}")
```

In `cmd_verify`, before the final `return`, add `log_event(root, "verify", slug, checkpoint=cid, ok=ok)`.

In `main()`, register the subcommands:

```python
    rr = sub.add_parser("record-review", help="record one reviewer's verdict for a checkpoint (tied to the tree hash)")
    rr.add_argument("slug"); rr.add_argument("id", type=int)
    rr.add_argument("--reviewer", required=True, choices=REVIEWERS)
    rr.add_argument("--result", required=True, choices=RESULTS)
    for k in ("blockers", "majors", "minors", "unjudged"):
        rr.add_argument(f"--{k}", type=int, default=0)
    jd = sub.add_parser("judge", help="human only: settle a BLOCKED (or disputed) reviewer verdict")
    jd.add_argument("slug"); jd.add_argument("id", type=int)
    jd.add_argument("--reviewer", required=True, choices=REVIEWERS)
    jd.add_argument("--result", required=True, choices=("PASS", "FAIL"))
    jd.add_argument("--note", required=True)
```

and dispatch:

```python
    elif a.cmd == "record-review":
        print(cmd_record_review(root, a.slug, a.id, a.reviewer, a.result,
                                {k: getattr(a, k) for k in ("blockers", "majors", "minors", "unjudged")}))
    elif a.cmd == "judge":
        if not human_confirm(f"record a human judgement for {a.reviewer} on '{a.slug}' #{a.id}: {a.result}"):
            return 2
        print(cmd_judge(root, a.slug, a.id, a.reviewer, a.result, a.note))
```

`HASH_EXCLUDES` already excludes `STATE_DIR`, so `reviews.jsonl` never changes the tree hash. `key_dir()` must exist before `append_jsonl`; `_key(create=True)` is called by `cmd_setup`, and `append_jsonl` creates parent directories (check `_common.append_jsonl`; if it does not, call `events_path().parent.mkdir(parents=True, exist_ok=True)` in `log_event`).

- [ ] **Step 4: Run the whole gate suite**

Run: `cd plugin && python -m pytest tests/test_build_gate.py -q`
Expected: all pass, including `test_full_pass_and_downgrade` with the recorded critic PASS and `test_review_bundle_has_diff_and_evidence` unchanged.

- [ ] **Step 5: Commit**

```bash
git add plugin/tentaqles/workflow/gate.py plugin/tests/test_build_gate.py
git commit -m "feat(build-gate): recorded reviewer verdicts gate the review; hotfix plans; process events log"
```

---

### Task 5: Gate: definition of done at the last checkpoint and at Stop

**Files:**
- Modify: `plugin/tentaqles/workflow/gate.py` (`cmd_approve`, new `cmd_dod`, `changed_files`, `check_stop`, CLI)
- Modify: `plugin/tests/test_build_gate.py`

**Interfaces:**
- Consumes: `tentaqles.workflow.project.load_project_config` (Task 2), `_common.glob_match(rel, patterns)`.
- Changes: `cmd_approve` stores `approved["project"] = load_project_config(root)[0]` (frozen with the plan) and `approved["project_warning"]`.
- Produces: `changed_files(root, base) -> list[str]`: repo-relative, slash-normalised paths from `git diff --name-only <base>` plus untracked files, minus `HASH_EXCLUDES`.
- Produces: `cmd_dod(root, slug, override: str | None = None) -> tuple[int, str]`; writes signed `.claude/build/<slug>/dod.json` `{ok, tree, ts, checks: [{id, ok, detail}], override}`; appends `D<n>` line to `docs/LEARNINGS.md` on override; logs event `dod` (ok) or `dod-override`.
- Check ids and rules: `project_config` (block present and valid), `tests` (frozen `commands.test` exit 0), `lint`, `typecheck` (when configured; `skip` otherwise), `locked_tests` (`tests_intact` empty), `checkpoints` (all `status: passed`), `reviews` (no checkpoint has a FAIL/BLOCKED record newer than its last PASS for the same reviewer), `adr` (changed files matching `architecture_paths` require a changed file under `docs/adr/`), `migration` (changed files under a migrations dir require a `## Migration plan` heading in the plan's `spec` file), `changelog` (configured changelog file must be among changed files), `privacy` (changed files matching `classified_paths` require `docs/privacy.md` changed; shared with the companion plan).
- Changes: `check_stop` blocks once when the owning session's plan has `workflow` in `DOD_WORKFLOWS`, every checkpoint is passed, the session is not finished, and `dod.json` is missing, not ok, or its `tree` differs from the current tree hash.
- CLI: `build-gate dod <slug> [--override REASON]`.

- [ ] **Step 1: Write the failing tests**

Append to `plugin/tests/test_build_gate.py`:

```python
PROJECT_BLOCK = ('# Demo\n\n## Project config\n\n```json tq-project\n'
                 '{"commands": {"test": %s}, "architecture_paths": ["core/**"], "classified_paths": ["data/**"], "changelog": "CHANGELOG.md"}\n'
                 '```\n')


def _project(repo, test_cmd=PASS_CMD):
    (repo / "CLAUDE.md").write_text(PROJECT_BLOCK % json.dumps(test_cmd), encoding="utf-8")
    (repo / "CHANGELOG.md").write_text("# Changelog\n", encoding="utf-8")
    _git(repo, "add", "CLAUDE.md", "CHANGELOG.md")
    _git(repo, "commit", "-q", "-m", "project config")


def _all_passed(repo, workflow="feature"):
    plan = {**PLAN, "workflow": workflow, "spec": "docs/specs/demo.md"}
    for cid in (2, 3):
        plan = with_gate(plan, cid, behavior="passed", review="passed", status="passed")
    save_plan(repo, plan)
    return plan


def _touch_changelog(repo):
    (repo / "CHANGELOG.md").write_text("# Changelog\n- demo\n", encoding="utf-8")


def _dod_checks(repo):
    rec = gate.read_json(repo / ".claude" / "build" / "demo" / "dod.json")
    assert gate.verified(rec)
    return {c["id"]: c for c in rec["checks"]}, rec


def test_dod_passes_when_everything_is_recorded(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 0 and out.startswith("DoD demo: PASS")
    checks, rec = _dod_checks(repo)
    assert rec["ok"] and checks["tests"]["ok"] and checks["adr"]["ok"] and checks["changelog"]["ok"]
    assert checks["lint"]["detail"].startswith("skip")


def test_dod_fails_on_failing_test_command(repo):
    _project(repo, FAIL_CMD)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING tests" in out


def test_dod_requires_adr_for_architecture_paths(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    (repo / "core").mkdir()
    (repo / "core" / "x.py").write_text("x = 1\n", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING adr" in out and "core/x.py" not in out  # no file names in the summary line
    (repo / "docs" / "adr").mkdir(parents=True)
    (repo / "docs" / "adr" / "0002-core.md").write_text("# ADR-0002\n", encoding="utf-8")
    assert gate.cmd_dod(repo, "demo")[0] == 0


def test_dod_requires_changelog_and_privacy_and_migration_plan(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING changelog" in out
    _touch_changelog(repo)
    (repo / "data").mkdir()
    (repo / "data" / "customers.sql").write_text("select 1;\n", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert "MISSING privacy" in out
    (repo / "docs" / "privacy.md").write_text("# privacy\n", encoding="utf-8")
    (repo / "migrations").mkdir()
    (repo / "migrations" / "001.sql").write_text("alter table x add y int;\n", encoding="utf-8")
    code, out = gate.cmd_dod(repo, "demo")
    assert "MISSING migration" in out
    (repo / "docs" / "specs").mkdir(parents=True)
    (repo / "docs" / "specs" / "demo.md").write_text("# demo\n\n## Migration plan\nexpand/contract\n", encoding="utf-8")
    assert gate.cmd_dod(repo, "demo")[0] == 0


def test_dod_with_invalid_project_config_is_missing_item(repo):
    (repo / "CLAUDE.md").write_text("```json tq-project\n{bad json}\n```\n", encoding="utf-8")
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    code, out = gate.cmd_dod(repo, "demo")
    assert code == 1 and "MISSING project_config" in out and "MISSING tests" in out


def test_dod_uses_the_frozen_test_command(repo):
    _project(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    _touch_changelog(repo)
    # the agent rewrites CLAUDE.md to a no-op test command after approval
    (repo / "CLAUDE.md").write_text(PROJECT_BLOCK % json.dumps(FAIL_CMD), encoding="utf-8")
    assert gate.cmd_dod(repo, "demo")[0] == 0  # frozen PASS_CMD ran, not the edited one


def test_dod_override_requires_reason_and_is_logged(repo, tq_home):
    _project(repo, FAIL_CMD)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    with pytest.raises(SystemExit):
        gate.cmd_dod(repo, "demo", override="   ")
    code, out = gate.cmd_dod(repo, "demo", override="release train leaves now; tests fixed in #42")
    assert code == 0 and "OVERRIDDEN" in out
    learn = (repo / "docs" / "LEARNINGS.md").read_text(encoding="utf-8")
    assert "· demo · dod-override · release train leaves now" in learn
    events = [json.loads(l) for l in (tq_home / "build-gate" / "events.jsonl").read_text(encoding="utf-8").splitlines()]
    assert events[-1]["kind"] == "dod-override" and "reason" not in events[-1]


def test_stop_requires_fresh_dod_for_feature_plans(repo):
    _project(repo)
    own(repo)
    _all_passed(repo)
    gate.cmd_approve(repo, "demo")
    code, _, err = stop(repo)
    assert code == 2 and "build-gate dod demo" in err
    assert stop(repo)[0] == 0  # blocked once, then through
    _touch_changelog(repo)
    assert gate.cmd_dod(repo, "demo")[0] == 0
    assert stop(repo)[0] == 0
    (repo / "app.py").write_text("def f():\n    return 3\n", encoding="utf-8")  # code changed: DoD stale
    assert stop(repo)[0] == 2


def test_stop_without_workflow_is_unchanged(repo):
    own(repo)
    done = with_gate(PLAN, 2, behavior="passed", review="passed", status="passed")
    done = with_gate(done, 3, behavior="passed", review="passed", status="passed")
    save_plan(repo, done)
    assert stop(repo)[0] == 0
```

Read the existing helpers `own(repo)` and `stop(repo, ...)` near the Stop tests before writing; reuse them as they are.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd plugin && python -m pytest tests/test_build_gate.py -q -k "dod or stop_without"`
Expected: FAIL, `AttributeError: ... 'cmd_dod'`.

- [ ] **Step 3: Implement**

Imports at the top of `gate.py`: add `from tentaqles.workflow.project import load_project_config` and `glob_match` to the `_common` import list.

In `cmd_approve`, after building `approved`:

```python
    project, pwarn = load_project_config(root)
    approved["project"] = project
    approved["project_warning"] = pwarn
```

Changed files and the DoD (place after `cmd_review_bundle`):

```python
MIGRATION_DIR_RE = re.compile(r"(^|/)(migrations|migrate|alembic/versions|supabase/migrations|prisma/migrations|db/migrate|flyway|sql/migrations)/")


def changed_files(root: Path, base: str | None) -> list[str]:
    files: set[str] = set()
    if base:
        code, out = git(root, "diff", "--name-only", base, "--", ".")
        if code == 0:
            files.update(out.splitlines())
    code, out = git(root, "diff", "--name-only", "HEAD", "--", ".")
    if code == 0:
        files.update(out.splitlines())
    code, out = git(root, "ls-files", "--others", "--exclude-standard")
    if code == 0:
        files.update(out.splitlines())
    out_files = []
    for f in files:
        f = norm(f).strip()
        if f and not any(f.startswith(p + "/") or f == p for p in HASH_EXCLUDES):
            out_files.append(f)
    return sorted(out_files)


def dod_path(root: Path, slug: str) -> Path:
    return state_dir(root, slug) / "dod.json"


def _next_learning_id(text: str, prefix: str) -> int:
    ids = [int(m) for m in re.findall(rf"^- `?{prefix}(\d+) ·", text, flags=re.MULTILINE)]
    return (max(ids) + 1) if ids else 1


def _append_learning(root: Path, line: str) -> None:
    path = root / "docs" / "LEARNINGS.md"
    text = read_text(path)
    if not text.strip():
        text = "# Learnings\n\n## Rules\n\n## Log\n"
    if "## Log" not in text:
        text = text.rstrip("\n") + "\n\n## Log\n"
    text = text.rstrip("\n") + "\n" + line + "\n"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def cmd_dod(root: Path, slug: str, override: str | None = None, timeout: float = 900) -> tuple[int, str]:
    """Definition of done (spec §8.1): run the frozen project commands and check the
    obligations the diff created. Records signed dod.json; the Stop hook reads it."""
    if override is not None and not override.strip():
        raise SystemExit("--override needs a reason; it is logged to docs/LEARNINGS.md and counted by tq insights.")
    plan = load_plan(root, slug)
    if plan is None:
        return 1, f"no plan at {PLAN_DIR}/{slug}.json"
    approved = load_approved(root, slug) or {}
    if not approved:
        return 1, f"plan '{slug}' isn't approved (build-gate approve {slug})."
    project = approved.get("project") or {}
    checks: list[dict] = []

    def add(cid: str, ok: bool | None, detail: str) -> None:
        checks.append({"id": cid, "ok": ok, "detail": detail})

    if project:
        add("project_config", True, "CLAUDE.md tq-project block frozen at approval")
    else:
        add("project_config", False, approved.get("project_warning") or "no ```json tq-project block in CLAUDE.md (add one with the test command)")
    cmds = project.get("commands") or {}
    before = tree_hash(root)
    for name in ("test", "lint", "typecheck"):
        c = cmds.get(name)
        cid = "tests" if name == "test" else name
        if not c:
            add(cid, None if name != "test" else False, "skip: not configured" if name != "test" else "no test command configured")
            continue
        code, out, err, timed_out = run(_shell_argv(c), cwd=root, timeout=timeout)
        add(cid, code == 0, f"exit {code}{' (timed out)' if timed_out else ''}: {c}" + ("" if code == 0 else "\n" + redact(tail(out + "\n" + err, 20))))
    tampered = tests_intact(root, approved)
    add("locked_tests", not tampered, "unchanged" if not tampered else f"{len(tampered)} locked test file(s) changed")
    cps = plan.get("checkpoints") or []
    unpassed = [c.get("id") for c in cps if c.get("status") != "passed"]
    add("checkpoints", not unpassed, f"{len(cps) - len(unpassed)}/{len(cps)} passed" + (f"; open: {unpassed}" if unpassed else ""))
    open_reviews = []
    for cp in cps:
        latest: dict[str, dict] = {}
        for line in read_text(reviews_path(root, slug)).splitlines():
            try:
                rec = json.loads(line)
            except ValueError:
                continue
            if verified(rec) and rec.get("checkpoint") == cp.get("id"):
                latest[rec["reviewer"]] = rec
        open_reviews += [f"#{cp.get('id')} {r} {rec['result']}" for r, rec in latest.items() if rec.get("result") != "PASS"]
    add("reviews", not open_reviews, "no open FAIL/BLOCKED" if not open_reviews else "open: " + ", ".join(open_reviews))
    files = changed_files(root, approved.get("base"))
    arch = [f for f in files if glob_match(f, project.get("architecture_paths") or [])]
    adr_changed = any(f.startswith("docs/adr/") for f in files)
    add("adr", (not arch) or adr_changed,
        "no architecture path changed" if not arch else (f"{len(arch)} architecture file(s) changed, ADR " + ("present" if adr_changed else "missing under docs/adr/")))
    classified = [f for f in files if glob_match(f, project.get("classified_paths") or [])]
    add("privacy", (not classified) or "docs/privacy.md" in files,
        "no classified path changed" if not classified else (f"{len(classified)} classified file(s) changed, docs/privacy.md " + ("updated" if "docs/privacy.md" in files else "not updated")))
    mig = [f for f in files if MIGRATION_DIR_RE.search(f)]
    spec_rel = plan.get("spec") or f"docs/specs/{slug}.md"
    has_plan = bool(re.search(r"^##+ .*migration plan", read_text(root / spec_rel), flags=re.IGNORECASE | re.MULTILINE))
    add("migration", (not mig) or has_plan,
        "no migration files changed" if not mig else (f"{len(mig)} migration file(s) changed, '## Migration plan' in {spec_rel} " + ("present" if has_plan else "missing")))
    changelog = project.get("changelog")
    if changelog and (root / changelog).exists():
        add("changelog", changelog in files, f"{changelog} " + ("changed" if changelog in files else "not changed"))
    else:
        add("changelog", None, "skip: project keeps no changelog")
    tree = tree_hash(root)
    if tree != before:
        add("stable_tree", False, "files changed while the DoD commands ran")
    ok = all(c["ok"] is not False for c in checks)
    rec = {"slug": slug, "ok": ok or override is not None, "tree": tree, "ts": now_iso(), "checks": checks,
           "approval_id": approved.get("approval_id"), "override": (override.strip() if override else None)}
    write_json(dod_path(root, slug), sign(rec))
    lines = []
    for c in checks:
        tag = "ok  " if c["ok"] else ("skip" if c["ok"] is None else "MISSING")
        lines.append(f"{tag} {c['id']}: {c['detail']}")
    if override is not None:
        n = _next_learning_id(read_text(root / "docs" / "LEARNINGS.md"), "D")
        _append_learning(root, f"- `D{n} · {now_iso()[:10]} · {slug} · dod-override · {redact(override.strip())}`")
        log_event(root, "dod-override", slug, ok=ok)
        head = f"DoD {slug}: OVERRIDDEN by the human (logged as D{n}); {sum(1 for c in checks if c['ok'] is False)} item(s) were missing"
        return 0, "\n".join([head, *lines])
    log_event(root, "dod", slug, ok=ok)
    head = f"DoD {slug}: {'PASS' if ok else 'FAIL'} (tree {str(tree)[:12]})"
    return (0 if ok else 1), "\n".join([head, *lines])


def dod_fresh(root: Path, slug: str, th: TreeHash) -> tuple[bool, str]:
    rec = read_json(dod_path(root, slug))
    if not verified(rec):
        return False, "no definition-of-done record"
    if not rec.get("ok"):
        return False, "the last definition-of-done run had missing items"
    if rec.get("tree") != th.get():
        return False, "code changed since the last definition-of-done run"
    return True, ""
```

In `stop_problems`, after the active-checkpoint block and before `return problems`:

```python
    if (plan.get("workflow") or "build") in DOD_WORKFLOWS and not sess.get("finished"):
        if all(c.get("status") == "passed" for c in plan.get("checkpoints") or []):
            ok, why = dod_fresh(root, slug, th)
            if not ok:
                problems.append(f"definition of done not proven ({why}): run `build-gate dod {slug}` and fix what it lists, "
                                f"or, when the human has said so, `build-gate dod {slug} --override \"<their reason>\"`")
    return problems
```

(`stop_problems` already loads `plan` and `sess`; make sure the new block uses the same `plan`/`sess` variables.)

CLI in `main()`:

```python
    dd = sub.add_parser("dod", help="definition of done: run the frozen project commands and check ADR/privacy/migration/changelog obligations")
    dd.add_argument("slug")
    dd.add_argument("--override", default=None, help="the human's reason for accepting missing items (logged)")
    dd.add_argument("--timeout", type=float, default=900)
```

```python
    elif a.cmd == "dod":
        code, msg = cmd_dod(root, a.slug, a.override, a.timeout)
        print(msg)
        return code
```

- [ ] **Step 4: Run the whole gate suite**

Run: `cd plugin && python -m pytest tests/test_build_gate.py -q`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/tentaqles/workflow/gate.py plugin/tests/test_build_gate.py
git commit -m "feat(build-gate): definition of done recorded by the gate and required at Stop for feature, fix and kickoff plans"
```

---

### Task 5b: Gate: decision records, hub actor tokens, run state export (hub-ready interfaces)

Runs after Task 5 and before Task 6. These are the three interfaces the user asked for on 2026-10-20 so that release 2's `tq hub` (a local server modelled on Paperclip's core: tasks, heartbeats, approvals) can drive the engine headless.

**Files:**
- Modify: `plugin/tentaqles/workflow/gate.py`
- Modify: `plugin/tests/test_build_gate.py`

**Interfaces:**
- **Decision records.** `.claude/build/<slug>/decisions.jsonl`, one signed line per record: `{id, kind, checkpoint, text, options, status: "pending"|"answered", asked_at, answered_at, choice, note, actor}`. Kinds: `plan-approval` (options `approve, edit, stop`), `result` (`approve, request-changes, stop`), `judge` (reviewer BLOCKED rows; options `PASS, FAIL`), `question` (free options). `DECISION_KINDS = ("plan-approval", "result", "judge", "question")`.
- `cmd_ask(root, slug, kind, text, options, cid=None, reviewer=None) -> str` (agent-callable): creates a pending record and prints its id; a pending record of the same kind, checkpoint and reviewer already open is returned instead of duplicated. `text` is redacted and capped at 4000 characters.
- `cmd_answer(root, slug, decision_id, choice, note, actor) -> str` (human-only): marks the record answered. `plan-approval` + `approve` also runs `cmd_approve`; `judge` + `PASS|FAIL` also records the reviewer judgement (`cmd_judge`) for the record's checkpoint and reviewer. Other kinds only record.
- `pending_decisions(root, slug) -> list[dict]`; `cmd_approve`, `cmd_finish` and `cmd_judge` mark the matching pending record answered with `actor: "terminal"` so a human who uses the terminal produces the same record as one who uses the hub.
- **Hub actor tokens.** `mint_hub_token(action, slug, actor) -> str` = HMAC-SHA256 over `hub:<action>:<slug>:<actor>` with the gate key; `hub_actor_ok(action, slug) -> str | None` reads `TQ_BUILD_ACTOR` and `TQ_BUILD_TOKEN` from the environment and returns the actor when the token verifies. `main()` accepts a valid token in place of `human_confirm` for `approve`, `unlock-tests`, `finish`, `judge`, `answer`. The agent cannot mint one (the hook refuses reads of the key and the key is outside every repository), and the hook's shell check still refuses the human-only subcommands to the agent regardless of tokens.
- **Run state export.** `cmd_state(root, slug) -> dict` and CLI `build-gate state <slug> --json`: `{slug, feature, workflow, hotfix, task, approved, halted, finished, checkpoints: [{id, title, status, gates: {behavior, review}, evidence: "fresh"|"stale"|"failing"|"-", reviews: {reviewer: result}}], dod: {ok, tree, override} | null, pending_decisions: [...], events_path}`. Plans may carry a top-level `"task": {"hub_id": "...", "external": {"provider": "...", "id": "...", "url": "..."}}` written by the on-ramps when launched from the hub; `cmd_state` echoes it.
- **Stop while waiting on a human.** `stop_problems` returns no problem for a run that has a pending decision (the orchestrator wrote the question and must end its turn so the hub can hand it to a human). The once-per-problem block still applies to runs with no pending decision.

- [ ] **Step 1: Write the failing tests**

Append to `plugin/tests/test_build_gate.py`:

```python
def _decisions(repo):
    path = repo / ".claude" / "build" / "demo" / "decisions.jsonl"
    return [json.loads(l) for l in path.read_text(encoding="utf-8").splitlines()] if path.exists() else []


def test_ask_creates_pending_and_is_idempotent(repo):
    out1 = gate.cmd_ask(repo, "demo", "plan-approval", "3 checkpoints, verify commands frozen on approve", ["approve", "edit", "stop"])
    out2 = gate.cmd_ask(repo, "demo", "plan-approval", "same question again", ["approve", "edit", "stop"])
    assert out1 == out2 and out1.startswith("decision ")
    pend = gate.pending_decisions(repo, "demo")
    assert len(pend) == 1 and pend[0]["kind"] == "plan-approval" and pend[0]["status"] == "pending"
    assert all(gate.verified(d) for d in _decisions(repo))


def test_answer_records_and_plan_approval_freezes_plan(repo):
    did = gate.cmd_ask(repo, "demo", "plan-approval", "approve?", ["approve", "edit", "stop"]).split()[1]
    gate.cmd_answer(repo, "demo", did, "approve", "looks right", actor="renato")
    assert gate.pending_decisions(repo, "demo") == []
    rec = [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]
    assert rec["choice"] == "approve" and rec["actor"] == "renato"
    assert gate.load_approved(repo, "demo") is not None  # approve ran


def test_terminal_approve_answers_the_pending_decision(repo):
    did = gate.cmd_ask(repo, "demo", "plan-approval", "approve?", ["approve", "edit", "stop"]).split()[1]
    gate.cmd_approve(repo, "demo")
    rec = [d for d in _decisions(repo) if d["id"] == did and d["status"] == "answered"][0]
    assert rec["actor"] == "terminal" and rec["choice"] == "approve"


def test_judge_decision_records_reviewer_judgement(repo):
    approve_and_lock(repo)
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "privacy-reviewer", "BLOCKED", {"unjudged": 1})
    did = gate.cmd_ask(repo, "demo", "judge", "U1 retention of the audit log?", ["PASS", "FAIL"], cid=2, reviewer="privacy-reviewer").split()[1]
    gate.cmd_answer(repo, "demo", did, "PASS", "24m", actor="renato")
    recs = gate.reviews_for(repo, "demo", 2, gate.tree_hash(repo))
    assert recs["privacy-reviewer"]["result"] == "PASS" and recs["privacy-reviewer"]["human"] is True


def test_hub_token_allows_non_interactive_human_actions(repo, monkeypatch):
    save_plan(repo, PLAN)
    monkeypatch.setenv("TQ_BUILD_ACTOR", "renato")
    monkeypatch.setenv("TQ_BUILD_TOKEN", gate.mint_hub_token("approve", "demo", "renato"))
    assert gate.hub_actor_ok("approve", "demo") == "renato"
    assert gate.hub_actor_ok("finish", "demo") is None   # a token is per action
    assert gate.hub_actor_ok("approve", "other") is None # and per slug
    code = gate.main(["--root", str(repo), "approve", "demo"])
    assert code == 0 and gate.load_approved(repo, "demo") is not None


def test_forged_hub_token_is_refused(repo, monkeypatch):
    import hashlib, hmac as _hmac
    monkeypatch.setenv("TQ_BUILD_ACTOR", "mallory")
    monkeypatch.setenv("TQ_BUILD_TOKEN", _hmac.new(b"guess", b"hub:approve:demo:mallory", hashlib.sha256).hexdigest())
    assert gate.hub_actor_ok("approve", "demo") is None
    code, _, err = hook(repo, bash("python build-gate.py answer demo d1 --choice approve --actor x"))
    assert code == 2 and "human" in err.lower()


def test_stop_allowed_while_a_decision_is_pending(repo):
    own(repo)
    save_plan(repo, with_gate(PLAN, 2, behavior="failed"))
    assert stop(repo)[0] == 2                      # unproven checkpoint blocks once
    gate.cmd_ask(repo, "demo", "question", "which DB?", ["postgres", "sqlite"], cid=2)
    assert stop(repo)[0] == 0                      # waiting on a human: the turn may end
    assert stop(repo)[0] == 0


def test_state_json_shape(repo):
    _project(repo)
    save_plan(repo, {**PLAN, "workflow": "feature", "task": {"hub_id": "t-42", "external": {"provider": "linear", "id": "ACME-7", "url": "https://linear.app/x"}}})
    gate.cmd_approve(repo, "demo")
    gate.cmd_verify(repo, "demo", 2)
    gate.cmd_record_review(repo, "demo", 2, "build-critic", "PASS", {})
    gate.cmd_ask(repo, "demo", "question", "ok?", ["yes", "no"], cid=2)
    st = gate.cmd_state(repo, "demo")
    assert st["slug"] == "demo" and st["workflow"] == "feature" and st["task"]["external"]["id"] == "ACME-7"
    assert st["approved"] is True and st["finished"] is False and st["dod"] is None
    cp2 = [c for c in st["checkpoints"] if c["id"] == 2][0]
    assert cp2["evidence"] == "fresh" and cp2["reviews"] == {"build-critic": "PASS"}
    assert st["pending_decisions"][0]["kind"] == "question"
    assert "app.py" not in json.dumps(st)  # names of checkpoints and reviewers only, no file paths or text
    out = gate.main(["--root", str(repo), "state", "demo", "--json"])
    assert out == 0
```

`gate.main` prints to stdout; the tests only check the exit code. `_project` is the helper from Task 5.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd plugin && python -m pytest tests/test_build_gate.py -q -k "ask or answer or hub_token or decision or state_json"`
Expected: FAIL with `AttributeError: ... 'cmd_ask'`.

- [ ] **Step 3: Implement in `gate.py`**

Constants:

```python
DECISION_KINDS = ("plan-approval", "result", "judge", "question")
HUMAN_ONLY = ("approve", "unlock-tests", "finish", "judge", "answer")  # replaces the Task 4 tuple
```

Decision records (after the review-record helpers):

```python
def decisions_path(root: Path, slug: str) -> Path:
    return state_dir(root, slug) / "decisions.jsonl"


def _decisions(root: Path, slug: str) -> list[dict]:
    out = []
    for line in read_text(decisions_path(root, slug)).splitlines():
        try:
            rec = json.loads(line)
        except ValueError:
            continue
        if verified(rec):
            out.append(rec)
    return out


def pending_decisions(root: Path, slug: str) -> list[dict]:
    latest: dict[str, dict] = {}
    for rec in _decisions(root, slug):
        latest[rec["id"]] = rec
    return [r for r in latest.values() if r.get("status") == "pending"]


def cmd_ask(root: Path, slug: str, kind: str, text: str, options: list[str], cid: int | None = None, reviewer: str | None = None) -> str:
    if kind not in DECISION_KINDS:
        raise SystemExit(f"unknown decision kind '{kind}'. Use one of: {', '.join(DECISION_KINDS)}.")
    if load_plan(root, slug) is None:
        raise SystemExit(f"no plan at {PLAN_DIR}/{slug}.json")
    for p in pending_decisions(root, slug):
        if p["kind"] == kind and p.get("checkpoint") == cid and p.get("reviewer") == reviewer:
            return f"decision {p['id']} (pending)"
    import secrets as _secrets
    rec = {"id": "d" + _secrets.token_hex(4), "kind": kind, "checkpoint": cid, "reviewer": reviewer,
           "text": redact(text)[:4000], "options": [o.strip() for o in options if o.strip()],
           "status": "pending", "asked_at": now_iso()}
    append_jsonl(decisions_path(root, slug), sign(rec))
    log_event(root, "ask", slug, checkpoint=cid, kind=kind)
    return f"decision {rec['id']} (pending)"


def _answer(root: Path, slug: str, rec: dict, choice: str, note: str, actor: str) -> None:
    done = {**rec, "status": "answered", "answered_at": now_iso(), "choice": choice, "note": redact(note or "")[:400], "actor": actor}
    append_jsonl(decisions_path(root, slug), sign(done))
    log_event(root, "answer", slug, checkpoint=rec.get("checkpoint"), kind=rec["kind"], actor=actor)


def _answer_pending(root: Path, slug: str, kind: str, choice: str, actor: str, cid: int | None = None, reviewer: str | None = None) -> None:
    for p in pending_decisions(root, slug):
        if p["kind"] == kind and (cid is None or p.get("checkpoint") == cid) and (reviewer is None or p.get("reviewer") == reviewer):
            _answer(root, slug, p, choice, "", actor)


def cmd_answer(root: Path, slug: str, decision_id: str, choice: str, note: str, actor: str) -> str:
    rec = next((p for p in pending_decisions(root, slug) if p["id"] == decision_id), None)
    if rec is None:
        raise SystemExit(f"no pending decision '{decision_id}' in '{slug}'.")
    if rec["options"] and choice not in rec["options"]:
        raise SystemExit(f"choice must be one of {', '.join(rec['options'])}.")
    if rec["kind"] == "plan-approval" and choice == "approve":
        cmd_approve(root, slug)           # also answers this record with actor "terminal"; re-answer below with the real actor
    if rec["kind"] == "judge" and choice in ("PASS", "FAIL") and rec.get("reviewer") and rec.get("checkpoint") is not None:
        cmd_judge(root, slug, int(rec["checkpoint"]), rec["reviewer"], choice, note or f"answered by {actor}")
    if rec["id"] in {p["id"] for p in pending_decisions(root, slug)}:
        _answer(root, slug, rec, choice, note, actor)
    else:  # already answered by the side effect above; record the actor on top
        _answer(root, slug, rec, choice, note, actor)
    return f"decision {decision_id}: {choice} by {actor}"
```

(Keep `_answer` idempotent: the latest record per id wins in `pending_decisions`, so a second answered line only updates the actor.)

In `cmd_approve` (end), `cmd_finish` (end) and `_record_review` when `human` is true: call `_answer_pending(root, slug, "plan-approval", "approve", "terminal")`, `_answer_pending(root, slug, "result", "approve", "terminal")` and `_answer_pending(root, slug, "judge", result, "terminal", cid, reviewer)` respectively.

Hub tokens (near `human_confirm`):

```python
def mint_hub_token(action: str, slug: str, actor: str) -> str:
    key = _key(create=True)
    return hmac.new(key, f"hub:{action}:{slug}:{actor}".encode("utf-8"), "sha256").hexdigest()


def hub_actor_ok(action: str, slug: str) -> str | None:
    actor = (os.environ.get("TQ_BUILD_ACTOR") or "").strip()
    tok = (os.environ.get("TQ_BUILD_TOKEN") or "").strip()
    key = _key()
    if not actor or not tok or key is None:
        return None
    want = hmac.new(key, f"hub:{action}:{slug}:{actor}".encode("utf-8"), "sha256").hexdigest()
    return actor if hmac.compare_digest(want, tok) else None
```

(`import hmac` at the top; `_key()` already exists and returns `None` when the key is missing.)

State export (after `cmd_status`):

```python
def cmd_state(root: Path, slug: str) -> dict:
    plan = load_plan(root, slug)
    if plan is None:
        raise SystemExit(f"no plan at {PLAN_DIR}/{slug}.json")
    approved = load_approved(root, slug)
    sess = load_session(root, slug)
    th = TreeHash(root)
    cps = []
    for cp in plan.get("checkpoints") or []:
        cid = cp.get("id")
        ev = latest_evidence(root, slug, cid)
        fresh = "-"
        if ev:
            ok, _ = evidence_fresh(root, slug, cid, th)
            fresh = "fresh" if ok else ("stale" if ev.get("ok") else "failing")
        cps.append({"id": cid, "title": cp.get("title", ""), "status": cp.get("status", "pending"),
                    "gates": {g: gate(cp, g) for g in GATES}, "evidence": fresh,
                    "reviews": {r: rec["result"] for r, rec in reviews_for(root, slug, cid, th.get()).items()}})
    dod = read_json(dod_path(root, slug))
    dod_out = {"ok": bool(dod.get("ok")), "tree": dod.get("tree"), "override": dod.get("override")} if verified(dod) else None
    return {"slug": slug, "feature": plan.get("feature", slug), "workflow": plan.get("workflow") or "build",
            "hotfix": bool(plan.get("hotfix")), "task": plan.get("task"), "approved": approved is not None,
            "halted": plan.get("halted"), "finished": bool(sess.get("finished")), "checkpoints": cps, "dod": dod_out,
            "pending_decisions": [{"id": p["id"], "kind": p["kind"], "checkpoint": p.get("checkpoint"),
                                   "reviewer": p.get("reviewer"), "text": p["text"], "options": p["options"], "asked_at": p["asked_at"]}
                                  for p in pending_decisions(root, slug)],
            "events_path": norm(events_path())}
```

Stop: at the top of `stop_problems`, after the `halted` check:

```python
    if pending_decisions(root, slug):
        return []  # waiting on a human: the orchestrator must be able to end its turn
```

CLI in `main()`:

```python
    ak = sub.add_parser("ask", help="record a decision the human must take (the orchestrator then ends its turn)")
    ak.add_argument("slug"); ak.add_argument("--kind", required=True, choices=DECISION_KINDS)
    ak.add_argument("--text", required=True); ak.add_argument("--options", default="", help="comma-separated")
    ak.add_argument("--checkpoint", type=int, default=None); ak.add_argument("--reviewer", default=None, choices=(None, *REVIEWERS))
    an = sub.add_parser("answer", help="human only: answer a pending decision")
    an.add_argument("slug"); an.add_argument("id"); an.add_argument("--choice", required=True)
    an.add_argument("--note", default=""); an.add_argument("--actor", default=None)
    stt = sub.add_parser("state", help="machine-readable run state for the hub")
    stt.add_argument("slug"); stt.add_argument("--json", action="store_true")
    tk = sub.add_parser("token", help="human only: mint a hub actor token for one action")
    tk.add_argument("action", choices=HUMAN_ONLY); tk.add_argument("slug"); tk.add_argument("--actor", required=True)
```

Dispatch: for `approve`, `finish`, `unlock-tests`, `judge`, `answer`, `token` first try `actor = hub_actor_ok(a.cmd, a.slug)`; when `None`, fall back to `human_confirm(...)` and use actor `"terminal"`. `answer` passes `a.actor or actor`. `state` prints `json.dumps(cmd_state(...), ensure_ascii=False, indent=2)` with `--json`, else a short text form. `token` prints `mint_hub_token(a.action, a.slug, a.actor)`.

Add `"token"` to `HUMAN_ONLY` so the agent's shell cannot mint tokens even if it found the key path.

- [ ] **Step 4: Run the whole gate suite**

Run: `cd plugin && python -m pytest tests/test_build_gate.py -q`
Expected: all pass, including the earlier Stop tests (no pending decision in those fixtures).

- [ ] **Step 5: Commit**

```bash
git add plugin/tentaqles/workflow/gate.py plugin/tests/test_build_gate.py
git commit -m "feat(build-gate): decision records, hub actor tokens and run state export so a hub can drive a run headless"
```

---

### Task 6: Reviewer agents: critic grammar, spec-critic, arbiter, planner fields

**Files:**
- Modify: `plugin/agents/build-critic.md` (Output section), `plugin/agents/build-planner.md` (Output JSON and rules)
- Create: `plugin/agents/spec-critic.md`, `plugin/agents/arbiter.md`
- Test: `plugin/tests/test_agents.py`

**Interfaces:**
- Produces: agents `spec-critic` and `arbiter` (read-only: `tools: Read, Glob, Grep`; `disallowedTools: Edit, Write, MultiEdit, NotebookEdit, Bash, PowerShell, Agent`). All reviewers return the review-contract grammar (`RESULT:`, `BUILT-BY-ME:`, `F`/`U` rows).
- Produces: planner writes top-level `workflow`, `hotfix`, `spec` when the orchestrator passes them.

- [ ] **Step 1: Write the failing test**

```python
# plugin/tests/test_agents.py
"""Agent definitions: frontmatter, tool lists and the review contract."""

from __future__ import annotations

import re
from pathlib import Path

import pytest
import yaml

AGENTS = Path(__file__).resolve().parents[1] / "agents"
READ_ONLY = ["build-critic", "spec-critic", "arbiter"]
WRITE_TOOLS = {"Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "PowerShell"}


def _fm(name: str) -> tuple[dict, str]:
    text = (AGENTS / f"{name}.md").read_text(encoding="utf-8")
    assert text.startswith("---\n")
    end = text.index("\n---\n", 4)
    return yaml.safe_load(text[4:end]), text[end + 5:]


@pytest.mark.parametrize("name", [p.stem for p in sorted(AGENTS.glob("*.md"))])
def test_every_agent_has_name_description_and_tools(name: str) -> None:
    fm, body = _fm(name)
    assert fm["name"] == name
    assert len(fm["description"]) >= 40
    assert fm.get("tools"), "agents list their tools explicitly"
    assert body.strip()


@pytest.mark.parametrize("name", READ_ONLY)
def test_reviewers_are_read_only(name: str) -> None:
    fm, _ = _fm(name)
    tools = {t.strip() for t in fm["tools"].split(",")}
    assert tools == {"Read", "Glob", "Grep"}
    disallowed = {t.strip() for t in fm["disallowedTools"].split(",")}
    assert WRITE_TOOLS <= disallowed and "Agent" in disallowed


@pytest.mark.parametrize("name", ["build-critic", "spec-critic"])
def test_reviewers_return_the_contract(name: str) -> None:
    _, body = _fm(name)
    for token in ("RESULT: PASS | FAIL | BLOCKED", "BUILT-BY-ME:", "CANNOT JUDGE", "file:line"):
        assert token in body, (name, token)
    assert "VERDICT:" not in body


def test_planner_knows_the_workflow_fields() -> None:
    _, body = _fm("build-planner")
    for token in ('"workflow"', '"hotfix"', '"spec"'):
        assert token in body
    assert re.search(r"hotfix.*n/a", body, flags=re.DOTALL)
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd plugin && python -m pytest tests/test_agents.py -q`
Expected: FAIL (`spec-critic.md` missing; `VERDICT:` present in build-critic).

- [ ] **Step 3: Edit and create the agents**

In `plugin/agents/build-critic.md` replace the `## Output` section with:

````markdown
## Output

Return exactly this, nothing else (the review contract in
`templates/process/review-contract.md`):

```
RESULT: PASS | FAIL | BLOCKED
BUILT-BY-ME: no | yes — <how>
F1 BLOCKER <file:line> — <problem> → <concrete fix>
F2 MAJOR   <file:line> — <problem> → <concrete fix>
F3 MINOR   <file:line> — <problem> → <concrete fix>
U1 CANNOT JUDGE — <check> — <what a human must look at>
```

- FAIL if there is any BLOCKER or MAJOR. MINOR alone still passes; list it so it is logged.
- BLOCKED when a check cannot be judged from the files (a missing environment, a product decision). Name it in a `U` row; a human settles it.
- `BUILT-BY-ME: yes` if you had any part in producing this change; the orchestrator then runs a fresh critic.
- Every finding has a file and line and a concrete fix. No line, no finding.
- In a later round, rule on each earlier finding first: `F<n> resolved|upheld|withdrawn — <why>`. Verify a "fixed" claim by reading the code at the cited lines; withdraw honestly when the fixer's evidence holds. Then list new findings, numbered on.
- Don't pad. A clean change is `RESULT: PASS` and `BUILT-BY-ME: no` with no findings.
````

Also add to the critic's "What to attack" list a sixth item:

```markdown
6. **Must not trade off**: `CLAUDE.md` lists rules with the test that enforces each. A change that weakens one, or edits its named test, is a BLOCKER.
```

Create `plugin/agents/spec-critic.md`:

````markdown
---
name: spec-critic
description: Read-only compliance reviewer for a /tentaqles:build checkpoint inside a feature, kickoff or fix run. Checks that the diff does what the spec slice and the PRD requirement say, nothing more and nothing less, that every acceptance check maps to a test, and that unplanned work is called out. Returns the answer-key verdict (RESULT PASS/FAIL/BLOCKED with file:line findings). Cannot edit files or run commands.
tools: Read, Glob, Grep
disallowedTools: Edit, Write, MultiEdit, NotebookEdit, Bash, PowerShell, Agent
---

You judge compliance, not quality: the quality critic runs beside you. You read; you never edit or run anything.

## Input

The caller gives you: the spec slice (`docs/specs/<slug>.md`), the PRD requirement it derives from (`docs/prd.md`, the `FR-nn` named in `derived_from`), the checkpoint slice from `docs/checkpoints/<slug>.json`, the review bundle path (`.claude/build/<slug>/review-<id>.md`: diff, new files, recorded verify evidence), and in later rounds the review thread. Read the project's `CLAUDE.md` and `docs/LEARNINGS.md` when present.

## Checks (rows of your answer key; keep the ids across rounds)

- C1 **Does what was asked.** Each acceptance check of the spec slice (and of the FR) that this checkpoint claims is observable in the diff or its tests. Missing: FAIL with the check quoted.
- C2 **Nothing more.** Changes outside the checkpoint's `files` and `tasks` are listed as unplanned work. Unplanned work that changes behaviour is MAJOR; a drive-by refactor that touches an `architecture_paths` glob is BLOCKER unless an ADR is in the diff.
- C3 **Tests prove the checks.** Every acceptance check has a test that would fail if the behaviour were removed. A test that mocks the thing under test, or asserts only that code ran, does not count.
- C4 **Spec kept in step.** If the implementation made a decision the spec left open, the spec slice records it under Assumptions (or Open questions became decided). Silent decisions are MAJOR.
- C5 **Fix scope** (fix runs only). The fix touches only the failing path; files outside it need a reason in the fix spec. The locked regression test is unchanged.
- C6 **Direction.** The change does not contradict a PRD non-goal (§3.2) or an accepted ADR. Contradiction is BLOCKER.

If you cannot decide a row from the files (for example the FR text is ambiguous and only the human can say), mark it `U<n> CANNOT JUDGE` and return BLOCKED.

## Output

Return exactly this, nothing else:

```
RESULT: PASS | FAIL | BLOCKED
BUILT-BY-ME: no | yes — <how>
F1 BLOCKER <file:line> — <problem> → <concrete fix>
F2 MAJOR   <file:line> — <problem> → <concrete fix>
F3 MINOR   <file:line> — <problem> → <concrete fix>
U1 CANNOT JUDGE — <check id and text> — <what the human must decide>
```

FAIL on any BLOCKER or MAJOR. Every finding has a file and line (for a missing test, cite the spec line and the test file that should hold it). In later rounds rule on earlier findings first (`F<n> resolved|upheld|withdrawn — <why>`). Say `BUILT-BY-ME: yes` if you took part in producing the change.
````

Create `plugin/agents/arbiter.md`:

````markdown
---
name: arbiter
description: Read-only arbiter for one disputed review finding in a /tentaqles:build run. Sees only that finding, the fixer's dispute, the cited files and CLAUDE.md, and rules upheld or withdrawn with a reason. Used when a critic upholds a finding twice after the fixer disputed it. Cannot edit files or run commands.
tools: Read, Glob, Grep
disallowedTools: Edit, Write, MultiEdit, NotebookEdit, Bash, PowerShell, Agent
---

You settle one disagreement. You get the finding text (`F<n> <severity> <file:line> — <problem> → <fix>`), the fixer's dispute with its evidence, the files cited by both, and the project's `CLAUDE.md`. Nothing else: do not go looking for other problems.

## How to rule

1. Read the cited lines in full context (the whole function, its callers via Grep).
2. Decide whether the problem the critic describes can actually happen with the code as it is now. A hypothetical that no caller can trigger is not a finding; a reachable one is, whatever the fixer says.
3. Check the dispute's evidence yourself: a cited test must exist and assert the behaviour; a cited `CLAUDE.md` rule must say what the fixer claims.
4. Severity stays as the critic set it unless the evidence shows a different blast radius; say so if you change it.

## Output

Return exactly this, nothing else:

```
RULING: upheld | withdrawn | upheld as <severity>
WHY: <two or three sentences citing file:line>
NEXT: <the concrete change to make, or "none">
BUILT-BY-ME: no | yes — <how>
```
````

In `plugin/agents/build-planner.md`, extend the Output JSON (after `"plain_summary"`) with:

```json
  "workflow": "build",
  "hotfix": false,
  "spec": "docs/specs/<slug>.md",
```

and add to the Rules paragraph:

```markdown
`workflow` is one of `build`, `feature`, `fix`, `kickoff`; the orchestrator tells you which, default `build`. `spec` is the spec slice path you planned from. `hotfix` is `true` only when the orchestrator says a fix is a hotfix: then every checkpoint's `gates.review.status` starts at `"n/a"` (the critic is skipped; the locked regression test never is). For `fix` plans write exactly one checkpoint whose first test is the locked regression test named in the fix spec.
```

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_agents.py tests/test_skill_frontmatter.py -q`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/agents/build-critic.md plugin/agents/build-planner.md plugin/agents/spec-critic.md plugin/agents/arbiter.md plugin/tests/test_agents.py
git commit -m "feat(agents): answer-key verdicts, spec-critic and arbiter, planner workflow fields"
```

---

### Task 7: Build skill: crew, recorded verdicts, DoD

**Files:**
- Modify: `plugin/skills/build/SKILL.md`, `plugin/skills/build/reference.md`
- Test: `plugin/tests/test_process_skills.py` (created here, extended by later tasks)

**Interfaces:**
- Consumes: gate CLI from Tasks 4 and 5 (`record-review`, `judge`, `dod`), agents from Task 6.
- Produces: the orchestration text other skills (`feature`, `fix`, `kickoff`) hand off to: "run Phase 2 to 4 of `/tentaqles:build` with `workflow=<x>`".

- [ ] **Step 1: Write the failing test**

```python
# plugin/tests/test_process_skills.py
"""Content checks for the development-process skills (spine)."""

from __future__ import annotations

import re
from pathlib import Path

import pytest

SKILLS = Path(__file__).resolve().parents[1] / "skills"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_build_runs_a_crew_and_records_every_verdict() -> None:
    body = _body("build")
    for token in ("record-review", "--reviewer build-critic --result", "spec-critic", "build-gate dod", "build-gate judge",
                  "BUILT-BY-ME", "workflow", "build-gate ask", "build-gate state", "## Headless mode", "TENTAQLES_HEADLESS"):
        assert token in body, token
    assert "git add -A" not in body and "git add ." not in body


def test_build_reference_lists_new_cli() -> None:
    ref = (SKILLS / "build" / "reference.md").read_text(encoding="utf-8")
    for token in ("build-gate record-review", "build-gate judge", "build-gate dod", "build-gate ask", "build-gate answer", "build-gate state", "reviews.jsonl", "dod.json", "decisions.jsonl", "events.jsonl", "TQ_BUILD_TOKEN"):
        assert token in ref, token
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q`
Expected: FAIL on `record-review`.

- [ ] **Step 3: Edit the build skill**

In `plugin/skills/build/SKILL.md`:

1. In the intro after the `$ARGUMENTS` line add:

```markdown
When another skill hands you a run, it passes `workflow=<feature|fix|kickoff>` and (for a fix) `hotfix=<yes|no>`; pass both to the planner so the plan carries `"workflow"` and `"hotfix"`. A plan without them is a plain `build`.
```

2. Replace **Phase 2 step d (Review gate)** with:

```markdown
**d. Review gate: a crew, every verdict recorded.**
1. `build-gate review-bundle <slug> <id>` prints the bundle path and `base=<commit>`.
2. Risk triage: `tq decide triage --range <base>..HEAD`. Exit 0 means **light**; exit 3, any error, or tq missing means **full** (when in doubt, review more). Note the flags it prints.
3. Pick the crew. Always `build-critic` (model `sonnet` for light, `opus` for full). Add `spec-critic` when the plan's `workflow` is `feature` or `kickoff`, or it is `fix` and `changed_files` include files outside the failing path named in the fix spec. Add `security-reviewer` on flags auth, secrets, api, state, destructive, or when the diff touches `.github/workflows/` or a dependency manifest; `privacy-reviewer` on flag `classified_data` or a diff touching `classified_paths`; `llm-security-reviewer` on flag `agentic`; `breaker` only when the checkpoint is marked `"runnable": true` in the plan and Project config has a `run` command. (The security and privacy reviewers ship with the security pack; if an agent is not installed, record nothing for it and tell the user which reviewer was skipped and why.) Run the crew members in parallel, each with the prompt marker `[checkpoint <slug>#<id> review]`, the checkpoint slice, the bundle path, the spec slice path and its checklist source.
4. Each reviewer returns the contract (`RESULT`, `BUILT-BY-ME`, `F` and `U` rows). If `BUILT-BY-ME: yes`, discard it and run a fresh instance. Record every verdict, you never skip one:
   ```
   build-gate record-review <slug> <id> --reviewer build-critic --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N
   ```
   The hook refuses `review = "passed"` until the current code has a recorded `build-critic` PASS and no FAIL or BLOCKED beside it.
5. **FAIL from any reviewer:** write all findings into `docs/reviews/<slug>-<id>.md` (one section per reviewer), run a fresh `build-fixer` (`[checkpoint <slug>#<id> fix]`) on the thread, re-run step c (code changed, so the behaviour gate reset), make a new bundle, and re-run only the reviewers that failed plus `build-critic`, giving them the thread. At most 3 rounds per checkpoint. A finding upheld twice after a dispute goes to the `arbiter` agent with only that finding, the dispute, the cited files and `CLAUDE.md`; its ruling is final for this run.
6. **BLOCKED from any reviewer:** the `U` rows are a human question. Use `AskUserQuestion` to show them (this does not count as one of the two stops: nothing proceeds without it anyway). The human settles each in their own terminal: `build-gate judge <slug> <id> --reviewer <name> --result PASS|FAIL --note "<why>"`. A FAIL ruling goes through step 5.
7. All PASS: Edit the plan: `review = "passed"`, `status = "passed"`. Commit the plan and thread (`gate(<slug>#<id>): passed`). Post one line to the user: `✓ 2/5 <title> (behaviour ✓ review ✓ light crew: critic, spec-critic · 0 retries)`.
```

3. In **Phase 3: Wrap-up** replace the first paragraph with:

```markdown
When the plan's `workflow` is `feature`, `fix` or `kickoff`, run the definition of done yourself: `build-gate dod <slug>`. It runs the project's test, lint and typecheck commands frozen at approval, checks the locked tests, every checkpoint and reviewer record, and the obligations the diff created (an ADR for `architecture_paths`, `docs/privacy.md` for `classified_paths`, a `## Migration plan` in the spec for migration files, the changelog). `MISSING` items are work: fix them (a fixer subagent for code, you for docs), then re-run. The Stop hook will not let the turn end while a finished plan has no fresh DoD. Only a human can waive an item: when they say so in their prompt, run `build-gate dod <slug> --override "<their words>"`; it is logged to `docs/LEARNINGS.md` and counted by `tq insights`. For a plain `build` plan, run one `general-purpose` subagent for the project's full test suite, lint and build as before.
```

4. In **Phase 4**, under **Request changes**, add: `Log it as an H<n> line in docs/LEARNINGS.md (what was rejected and which gate should have caught it).`

5. Add a section **## Headless mode** after "Non-negotiables":

```markdown
## Headless mode

The hub (release 2) and any `claude -p` caller set `TENTAQLES_HEADLESS=1`, or pass `--headless` in `$ARGUMENTS`. Then you never call `AskUserQuestion`. At every human stop you write the question as a decision record and end your turn; the Stop hook lets a turn end while a decision is pending:

```bash
build-gate ask <slug> --kind plan-approval --text "<the plain summary, one line per checkpoint, verify commands, assumptions>" --options approve,edit,stop
build-gate ask <slug> --kind result --text "<what to try, checkpoint summary, deferred MINORs>" --options approve,request-changes,stop
build-gate ask <slug> --kind judge --checkpoint <id> --reviewer <name> --text "<the U rows>" --options PASS,FAIL
build-gate ask <slug> --kind question --checkpoint <id> --text "<the question>" --options <a>,<b>
```

On resume (`/tentaqles:build resume <slug>`), read `build-gate state <slug> --json` first: `pending_decisions` empty and the matching record answered means continue from the answer (`approve` → the plan is already frozen; `edit` → re-run the planner with the note; `request-changes` → Phase 4's request-changes path with the note; `stop` → commit and end). A pending decision means the human has not answered: end the turn again without working.

In interactive mode you also write the `ask` record at each stop and then use `AskUserQuestion` as before; the human's terminal command (`approve`, `finish`, `judge`) answers it, so the record exists either way and the hub can show the history. The plan's top-level `task` block (hub id, external tracker id) is set by the on-ramp when the hub launched the run; never invent one.
```

6. In **Phase 1 step 4** and **Phase 4**, before the `AskUserQuestion`, add the matching `build-gate ask` line (kinds `plan-approval` and `result`); in step d.6 (BLOCKED) add the `judge` ask before showing the rows.

In `plugin/skills/build/reference.md`:

- Files table: add rows `.claude/build/<slug>/reviews.jsonl | gate | one signed line per reviewer verdict (reviewer, result, counts, tree hash)`, `.claude/build/<slug>/dod.json | gate | the last definition-of-done run (checks, tree hash, override)`, `$TQ_HOME/build-gate/events.jsonl | gate | aggregate process events (verify, review, judge, dod, dod-override, finish) read by tq insights; never file names or text`.
- Files table: also `.claude/build/<slug>/decisions.jsonl | gate | one signed line per decision asked of a human and its answer (kind, options, choice, actor)`.
- Gate CLI block: add
  ```
  build-gate ask <slug> --kind plan-approval|result|judge|question --text T --options a,b [--checkpoint N] [--reviewer R]
  build-gate answer <slug> <id> --choice C [--note N] [--actor A]       (human only, or a hub actor token)
  build-gate state <slug> --json                                          run state for the hub
  build-gate token <action> <slug> --actor A                              (human only) mint a hub actor token
  build-gate record-review <slug> <id> --reviewer R --result PASS|FAIL|BLOCKED [--blockers N --majors N --minors N --unjudged N]
  build-gate judge <slug> <id> --reviewer R --result PASS|FAIL --note TEXT      (human only)
  build-gate dod <slug> [--override REASON]      definition of done; records dod.json
  ```
- Gate rules, plan edits: add `review` becoming `passed` needs a verified `build-critic` PASS record at the current tree hash and no FAIL/BLOCKED record beside it; `review: n/a` only in a plan with `"hotfix": true`.
- Trust model: add `Human-only commands also accept a hub actor token: TQ_BUILD_ACTOR plus TQ_BUILD_TOKEN, an HMAC over the action, slug and actor made with the gate key. The hub holds the key as the user's own process; the agent cannot read it, and the hook refuses the human-only subcommands to the agent regardless.`
- Stop: add `A run with a pending decision may end its turn (the orchestrator wrote the question for a human).` and `For a plan whose workflow is feature, fix or kickoff, a turn also can't end once every checkpoint passed until dod.json is fresh (same tree hash) and ok, or carries a human override. Blocks once, like the checkpoint rule.`
- Trust model: add `Review and DoD records are signed like evidence; a record the agent writes by hand fails verification and counts as absent. The DoD runs the commands frozen in approved.json, not the ones in the working copy of CLAUDE.md.`

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/build/SKILL.md plugin/skills/build/reference.md plugin/tests/test_process_skills.py
git commit -m "feat(build): reviewer crew with recorded verdicts, human judge for BLOCKED, definition of done"
```

---

### Task 8: `dod` and `direction` skills

**Files:**
- Create: `plugin/skills/dod/SKILL.md`, `plugin/skills/direction/SKILL.md`
- Modify: `plugin/tests/test_process_skills.py`

**Interfaces:**
- Consumes: `build-gate dod` (Task 5), `prd-status.py` (Task 2).
- Produces: `/tentaqles:dod <slug>` and `/tentaqles:direction` invocable by `feature`, `fix`, `kickoff`, `ship`.

- [ ] **Step 1: Write the failing tests**

Append to `plugin/tests/test_process_skills.py`:

```python
def test_dod_skill_runs_the_gate_and_explains_override() -> None:
    body = _body("dod")
    assert SNIPPET in body and "build-gate.py" in body and "dod" in body
    assert "--override" in body and "LEARNINGS" in body
    assert "never" in body.lower() and "transcript" in body.lower()


def test_direction_is_read_only_and_uses_the_scorecard() -> None:
    body = _body("direction")
    for token in ("ON COURSE", "DRIFTING", "OFF COURSE", "Aligned", "Partial", "Missing", "Contradicted",
                  "Not verified", "Fix code", "Update PRD", "Decide"):
        assert token in body, token
    for forbidden in ("git commit", "Edit", "Write("):
        assert forbidden not in body, forbidden
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k "dod_skill or direction"`
Expected: FAIL, `FileNotFoundError` for `skills/dod/SKILL.md`.

- [ ] **Step 3: Write the skills**

`plugin/skills/dod/SKILL.md`:

````markdown
---
name: dod
description: Run the definition of done for a feature, fix or kickoff build and turn its MISSING items into work. The gate runs the project's frozen test, lint and typecheck commands, checks the locked tests, every checkpoint and reviewer record, and the obligations the diff created (ADR for architecture paths, docs/privacy.md for classified paths, a migration plan for migration files, the changelog). Use when the user says "/tentaqles:dod <slug>", "are we done", "definition of done", "what is missing before we ship", or when the Stop hook reports "definition of done not proven". Only a human may waive an item, and the waiver is logged.
argument-hint: <slug> [--override "<the human's reason>"]
---

# Definition of done

"Done" is a record the gate wrote, never a sentence in the transcript. This skill
runs the record and works through what it lists.

## 1. Run it

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" dod <slug>
```

Use the Bash tool (Git Bash on Windows). The output is one head line
(`DoD <slug>: PASS|FAIL (tree …)`) and one line per check: `ok`, `skip` or
`MISSING`. Nothing else is read from logs: the gate already recorded exit codes.

## 2. Work the MISSING items

| Item | What it means | Who fixes it |
|---|---|---|
| `project_config` | no valid ```` ```json tq-project ```` block in `CLAUDE.md` at approval time | you: add the block from `templates/process/CLAUDE-project.md`, then the human re-approves the plan (`build-gate approve <slug>`) so the commands are frozen |
| `tests`, `lint`, `typecheck` | the frozen command exited non-zero (last 20 lines are in the detail) | a fresh `build-fixer` subagent with the detail; never edit the locked tests |
| `locked_tests` | a locked test changed | restore it (`git checkout -- <file>` is fine for the human; you report it) |
| `checkpoints` | a checkpoint is not `passed` | finish it through `/tentaqles:build resume <slug>` |
| `reviews` | a reviewer's last record is FAIL or BLOCKED | FAIL: fixer round; BLOCKED: show the `U` rows to the human, they run `build-gate judge` |
| `adr` | an `architecture_paths` file changed without a new ADR | write `docs/adr/NNNN-<slug>.md` from `templates/process/adr-0000-template.md` |
| `privacy` | a `classified_paths` file changed and `docs/privacy.md` did not | run `/tentaqles:privacy-review`, update the record |
| `migration` | migration files changed and the spec has no `## Migration plan` | add the section (expand/contract, backfill, rollback) to the spec slice |
| `changelog` | the project keeps a changelog and it did not change | add the entry |

Re-run step 1 after each fix until the head line says PASS. Then the Stop hook
lets the turn end and `/tentaqles:ship` quotes the record in the PR body.

## 3. Override (human only)

A human may accept a missing item by saying so in their own words in their next
prompt. Then, and only then:

```bash
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" dod <slug> --override "<their words, verbatim>"
```

The gate records the override, appends a `D<n>` line to `docs/LEARNINGS.md` and
logs an event `tq insights` counts. Never run `--override` on your own
judgement, never paraphrase the reason, and never claim done in the transcript
when the record says FAIL.
````

`plugin/skills/direction/SKILL.md`:

````markdown
---
name: direction
description: Read-only drift audit of a repository against its PRD. Grades every functional requirement Aligned, Partial, Missing or Contradicted by reading the code and tests (never by trusting a summary), classifies work the PRD never asked for, and returns ON COURSE, DRIFTING or OFF COURSE with recommendations limited to Fix code, Update PRD or Decide. Use when the user asks "are we on track", "did we drift", "does the code match the PRD", at the end of /tentaqles:kickoff, before a release, or "/tentaqles:direction".
argument-hint: [FR-nn ...] (default: every FR)
---

# Direction

The PRD says what should exist. This audit says what does. It reads; it never
edits, commits or runs the app. Allowed tools: Read, Grep, Glob and read-only
git commands (`git log`, `git diff --stat`, `git ls-files`). If you did not
read the code that implements an FR, you do not grade it: it goes under "Not
verified".

## 1. Inputs

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
git -C "$root" log --oneline -30
```

`prd_exit=4` means there is no PRD: stop and point the user to
`/tentaqles:prd` ("from existing code" variant). Otherwise read `docs/prd.md`
(§3 goals, §3.2 non-goals, §4 FRs with their acceptance checks, §11 releases),
`docs/adr/*.md` (accepted decisions) and `docs/specs/*.md`.

## 2. Grade each FR (or the FRs named in `$ARGUMENTS`)

For every acceptance check of the FR, find the code and the test that prove it
(Grep for the nouns in the check; read the test; confirm it would fail if the
behaviour were removed). Then grade:

| Grade | Meaning |
|---|---|
| Aligned | every acceptance check has code and a test you read |
| Partial | some checks proven, some not; name the missing ones |
| Missing | no code for it |
| Contradicted | code does the opposite, or violates a §3.2 non-goal or an ADR |

An FR you could not trace within a reasonable search (say, ten Grep calls) is
"Not verified", not "Missing".

## 3. Unplanned work

List what the code does that no FR asked for: endpoints, tables, jobs, flags,
dependencies. Classify each: `harmless` (dev tooling), `should be an FR`
(users will rely on it), `violates a non-goal`, or `risk` (touches auth, money,
PII, or a trust boundary with no threat-model row).

## 4. Verdict

- **ON COURSE**: every FR of the current release (§11) Aligned or Partial with
  a plan, nothing Contradicted, no unplanned `risk`.
- **DRIFTING**: Missing FRs in the current release, or unplanned `should be an
  FR` work, or an unresolved `NEEDS-DECISION` that code already answered.
- **OFF COURSE**: any Contradicted FR, any non-goal violated, or unplanned
  `risk` work.

## 5. Output

```
DIRECTION: ON COURSE | DRIFTING | OFF COURSE

| FR | Grade | Evidence (file:line, test) | Gap |
|---|---|---|---|

Unplanned work:
- <item> · <class> · <file>

Not verified:
- <FR> · <why>

Recommendations (each one of exactly three kinds):
- Fix code: <what, to satisfy which check>
- Update PRD: <what the PRD must say to match a deliberate change; cite the ADR or decision>
- Decide: <the question only the human can answer, as a NEEDS-DECISION line for §10>
```

No other recommendation kinds. No scores. Do not propose edits to anything
yourself: hand the list to the user; `/tentaqles:feature` picks up "Fix code"
items, `/tentaqles:prd` the "Update PRD" ones.
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/dod plugin/skills/direction plugin/tests/test_process_skills.py
git commit -m "feat(skills): dod and direction"
```

---

### Task 9: `prd` skill

**Files:**
- Create: `plugin/skills/prd/SKILL.md`
- Modify: `plugin/tests/test_process_skills.py`

**Interfaces:**
- Consumes: `templates/process/prd.md`, `prd-status.py`.
- Produces: `docs/prd.md` that `prd-status.py` can grade; the skill refuses to call it planning-ready otherwise.

- [ ] **Step 1: Write the failing test**

```python
def test_prd_skill_interviews_and_checks_mechanically() -> None:
    body = _body("prd")
    for token in ("templates/process/prd.md", "prd-status.py", "NEEDS-DECISION", "you decide", "from existing code",
                  "planning-ready"):
        assert token in body, token
    assert SNIPPET in body
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k prd_skill`
Expected: FAIL, file missing.

- [ ] **Step 3: Write the skill**

````markdown
---
name: prd
description: Write or revise a product requirements document (docs/prd.md) through a short interview, from an idea, a brief, a ticket or an existing codebase. Captures problem, users, goals and non-goals, numbered functional requirements with binary acceptance checks, data classes, security boundaries, NFRs with numbers, stack constraints and open questions tagged NEEDS-DECISION, then grades planning-readiness mechanically. Use when the user says "write a PRD", "start a PRD", "requirements doc", "PRG", "spec the product", "reverse-engineer the requirements", or when /tentaqles:kickoff finds no planning-ready PRD.
argument-hint: <idea | brief path | ticket URL | "from existing code"> 
---

# PRD

What and why, in a shape machines can check. Every later artifact cites a
section number from this document, so section numbers never move.

## 0. Where it lives

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
echo "template=$_tqe/templates/process/prd.md"
```

`prd_exit=4`: start from the template. `0` or `3`: you are revising; read
`docs/prd.md` first and keep its numbering.

## 1. Pick the variant

- **From an idea** (one or two sentences): the full interview below.
- **From a brief or ticket** (a file path or URL in `$ARGUMENTS`): read it,
  fill every section you can with a citation to the brief, interview only
  the gaps.
- **From existing code** (`from existing code`, or a legacy repo with no PRD):
  read `README`, `CLAUDE.md`, routes or entry points, the schema and the tests;
  write §1 to §9 as what the code does today, each FR citing the file that
  implements it; interview only §1 (why), §3 (goals) and §10 (what is unclear).
  Mark the document `Status: draft (reverse-engineered)`.

## 2. Interview

Ask one question per message, 8 to 12 in total, multiple choice with a
recommended default where you can. The user may answer "you decide": record
your choice and the reason in the section, and add it to §10 only if the
choice has a cost they should see. Cover, in this order:

1. The problem and the evidence it is real (§1).
2. Who uses it and in which roles; who must never (§2).
3. Three to five measurable goals; what is explicitly out (§3, §3.2).
4. The three most important things a user does (§4 candidates).
5. For each: how would we know it works? Push until the answer is binary and
   observable ("the export has one row per order" not "exports work").
6. What personal, financial or sensitive data it touches; retention (§5).
7. Who talks to what; how people log in; what a malicious user would try (§6).
8. Numbers: users, requests, data volume, latency, availability, cost ceiling,
   platforms (§7).
9. Stack constraints and why (§9). Never assume a default stack.
10. What they are unsure about (§10).

Stop early when every section has an answer or an owner.

## 3. Write `docs/prd.md`

Copy the template (`cp "$_tqe/templates/process/prd.md" "$root/docs/prd.md"`
after `mkdir -p "$root/docs"`) and fill it. Rules:

- Headings stay exactly `## <n>. <title>`; FRs are `### FR-nn <title>` with a
  `**Acceptance checks**` list of binary statements.
- Open items are list lines starting with `NEEDS-DECISION:` under §10, each
  with how to settle it (ask <who>, research, prototype).
- No scores, no adjectives in acceptance checks. "Fast" becomes a number in §7.
- §11 names which FRs are release 1.

## 4. Grade it

```bash
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
```

- `0`: set `Status: planning-ready` in the header and say so.
- `3`: list what blocks it (the JSON names FRs without checks and the open
  decision count). Never write "planning-ready" while the exit code is 3; offer
  to settle each open item (ask, research, prototype) and loop.

## 5. Commit

```bash
git -C "$root" add docs/prd.md
git -C "$root" commit -m "docs(prd): <draft|planning-ready> product requirements"
```

Record the decisions the interview produced with `/tentaqles:session-wrap` so
the next session knows why each requirement exists.
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/prd plugin/tests/test_process_skills.py
git commit -m "feat(skills): prd (interview, variants, mechanical planning-ready check)"
```

---

### Task 10: `fix` skill

**Files:**
- Create: `plugin/skills/fix/SKILL.md`
- Modify: `plugin/tests/test_process_skills.py`

**Interfaces:**
- Consumes: `templates/process/fix-spec.md`, `/tentaqles:build` with `workflow=fix`, `hotfix=yes|no`.
- Produces: `docs/specs/fix-<slug>.md`, a one-checkpoint plan with the locked regression test, a `pending` memory item for hotfix debt.

- [ ] **Step 1: Write the failing test**

```python
def test_fix_skill_is_reproduce_first_and_bug_shaped() -> None:
    fm_text, body = _body("fix").split("\n---\n", 1)
    desc = fm_text.lower()
    for phrase in ("stack trace", "bug", "fails", "regression"):
        assert phrase in desc, phrase
    for token in ("lock-tests", "workflow=fix", "--hotfix", "templates/process/fix-spec.md", "systematic", "LEARNINGS",
                  "post-hotfix review", '"event": "pending"', "incident"):
        assert token in body, token
    assert "never" in body and "without a reproduction" in body
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k fix_skill`
Expected: FAIL, file missing.

- [ ] **Step 3: Write the skill**

````markdown
---
name: fix
description: Fix a bug reproduce-first. Captures the report verbatim, has a tester write the smallest failing test at the lowest layer that shows the bug, locks that regression test, then finds the root cause and makes the smallest change that turns it green, reviewed by risk, with the lesson appended to docs/LEARNINGS.md. Use when the user says "fix this bug", "this fails", "it crashes", "/tentaqles:fix", pastes a stack trace, an error message or a failing command, or reports behaviour that does not match the spec. --hotfix skips the critic for a production incident, never the regression test.
argument-hint: <report | pasted error | failing command> [--hotfix]
---

# Fix (reproduce first)

A bug is never fixed without a reproduction that stays in the suite. The
regression test is written and locked before any source file changes; the
implementer cannot edit it. Everything runs through the build engine with
`workflow=fix`, so the evidence, review and definition of done are recorded.

Human stops: (1) confirm the reproduction and the severity; (2) accept the
result. With `--hotfix`, stop 1 is skipped and the debt is recorded.

## 1. Intake

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
mkdir -p "$root/docs/specs"
echo "template=$_tqe/templates/process/fix-spec.md"
```

Slug: kebab-case from the symptom (`export-dup-rows`). Write
`docs/specs/fix-<slug>.md` from the template: the report **verbatim**,
observed, expected (cite the PRD FR or the spec rule when one exists),
environment, `data_classes_touched`. If the bug exposed personal data to
someone who should not see it, this is also an incident: open
`docs/incident-playbook.md` now, start its clock, and tell the user before
anything else.

Severity: `critical` (data loss or exposure, everyone blocked), `high` (a core
flow broken), `medium`, `low`. `--hotfix` applies only when production is
affected now and the fix path is obvious; write the reason in the spec header.

## 2. Reproduce first, then lock

Hand off to the engine: run `/tentaqles:build` Phase 0 with slug `fix-<slug>`,
then Phase 1 with the spec path, `workflow=fix` and `hotfix=yes|no`. The
planner writes exactly one checkpoint whose first test is the regression test.
Before human stop 1, run Phase 2a yourself for that checkpoint: a tester
subagent (`[checkpoint fix-<slug>#1 tests]`) writes the smallest failing test at
the lowest layer that shows the bug (unit before API before CLI before UI), runs
it, and reports the failure line. Then:

```bash
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" lock-tests fix-<slug> 1 <test file>
```

**No reproduction, no fix.** If the tester cannot make a test fail, do not
touch source code. Report what was tried, what to instrument (a log line, a
data sample, a flag), and stop at human stop 1 with the options: add logging
and wait for the next occurrence, provide data, or accept a reproduction at a
higher layer. Never write a fix "while we wait" without a reproduction.

**Human stop 1** (skipped for `--hotfix`): show the failing test and its
output, the severity, the one-checkpoint plan and its verify command. The
human approves in their terminal (`build-gate approve fix-<slug>`).

## 3. Root cause

The implementer subagent (`[checkpoint fix-<slug>#1 implement]`) follows the
systematic discipline: read the error completely, reproduce it with the locked
test, trace the data back to where the bad value is born, compare with a
working path, one hypothesis at a time, the smallest change that makes the
locked test pass without changing other behaviour. It may not edit the locked
test (the hook blocks it); if it believes the test is wrong, it reports
file:line evidence and the human decides.

## 4. Review by risk

Phase 2c and 2d of the engine. `tq decide triage` on the diff picks the depth:
low risk gets the light critic; high risk gets the full crew; files outside the
failing path add `spec-critic`. With `--hotfix` the plan carries
`"hotfix": true`, the review gate is `n/a` and the critic is skipped, **never
the behaviour gate**: the locked regression test still has to pass through
`build-gate verify`. Then record the debt so nobody forgets:

```bash
echo '{"cwd": "<root>", "event": "pending", "data": {"description": "post-hotfix review of fix-<slug>: run the full crew on the diff and close the LEARNINGS entry", "priority": "high", "node_ids": ["docs/specs/fix-<slug>.md"]}}' | "$TENTAQLES_PY" "$_tqe/scripts/memory-bridge.py"
```

`wrap` and the session preamble surface it until resolved.

## 5. Done

Fill `## Root cause` and `## Lesson` in the fix spec and append the `L<n>`
line to `docs/LEARNINGS.md` (format in `templates/process/LEARNINGS.md`). If
the lesson already appears there, promote it to a Rule. If the bug came from a
gap in the PRD, the threat model or the privacy record, change that document in
the same branch. Run the definition of done (`/tentaqles:dod fix-<slug>`), then
**human stop 2** (engine Phase 4), then `/tentaqles:ship`.

## Never

- fix without a locked failing reproduction, or edit the locked test
- skip the regression test for a hotfix (only the critic is skipped)
- `git add -A`, `git add .`, `--no-verify`, or reading `.env*` (`tq dotenv run -- <cmd>`)
- call it fixed in the transcript before `build-gate verify` and `build-gate dod` say so
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/fix plugin/tests/test_process_skills.py
git commit -m "feat(skills): fix (reproduce-first, locked regression test, hotfix debt)"
```

---

### Task 11: `feature` skill

**Files:**
- Create: `plugin/skills/feature/SKILL.md`
- Modify: `plugin/tests/test_process_skills.py`

**Interfaces:**
- Consumes: `prd-status.py`, `templates/process/spec-slice.md`, `grill-me`, `tq decide triage`, `/tentaqles:build` with `workflow=feature`, `/tentaqles:dod`.

- [ ] **Step 1: Write the failing test**

```python
def test_feature_skill_locates_the_fr_and_interviews_first() -> None:
    body = _body("feature")
    for token in ("prd-status.py", "FR-", "grill-me", "templates/process/spec-slice.md", "NEEDS-DECISION",
                  "tq decide triage", "workflow=feature", "/tentaqles:dod", "legacy", "docs/adr/"):
        assert token in body, token
    assert SNIPPET in body
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k feature_skill`
Expected: FAIL, file missing.

- [ ] **Step 3: Write the skill**

````markdown
---
name: feature
description: Add a feature from a PRD requirement through the gated build engine. Locates the FR (or adds one first), interviews the user on every business-rule branch with grill-me, writes a spec slice with binary acceptance checks and the data classes and trust boundaries it touches, pre-selects the reviewer crew by risk, plans checkpoints, runs the engine and the definition of done. Use when the user says "add feature", "implement FR-07", "build X", "new endpoint", "/tentaqles:feature", or describes behaviour the product should gain.
argument-hint: <description | FR-nn>
---

# Feature

Two human stops: approve the checkpoint plan; accept the result. Everything
else is decided and logged as an assumption.

## 1. Locate it in the PRD

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
grep -n "^### FR-" "$root/docs/prd.md" 2>/dev/null
```

- `prd_exit` 0 or 3 (a PRD exists): find the FR in `$ARGUMENTS` or by reading
  §4. **A feature with no FR is first a PRD change, never code**: add
  `### FR-nn` with acceptance checks to `docs/prd.md` on the feature branch,
  cite the goal it serves, and commit it (`docs(prd): FR-nn <title>`). If the
  PRD has open `NEEDS-DECISION` items that touch this feature, settle them
  first (ask the user, one at a time).
- `prd_exit=4` (legacy repository): continue; the spec slice says
  `derived_from: no PRD: legacy repository` and you mention once that
  `/tentaqles:kickoff` can backfill a PRD later. If `CLAUDE.md` has no
  ```` ```json tq-project ```` block, add one now from
  `templates/process/CLAUDE-project.md` with the real test command (read
  `package.json`, `pyproject.toml`, `go.mod`, the CI workflow; never guess) so
  the definition of done can run it.

## 2. Interview

Run the `grill-me` skill on this feature: business-rule branches, invalid
states, permissions, time and locale, money, concurrency, failure handling.
Answers become the rules table and the acceptance checks of the spec slice.
Anything the user cannot decide is a `NEEDS-DECISION` line in the slice and
**blocks planning** until settled (offer: ask someone, research, prototype).

## 3. Spec slice and pre-check

Write `docs/specs/<slug>.md` from `"$_tqe/templates/process/spec-slice.md"`:
`derived_from: PRD §4 FR-nn`, `data_classes_touched` (from the manifest `data:`
block; `tq doctor` warns when PII-looking columns have no block),
`trust_boundaries_crossed` (from `docs/threat-model.md`), behaviour, binary
acceptance checks, out of scope, migration plan when schema files will change.

Pre-select the reviewer crew before any code exists, so the plan already lists
who will review: list the files you expect to touch and run

```bash
tq decide triage; echo "triage_exit=$?"
```

on the current tree (the PRD and spec edits), then apply the crew rule from
the build skill: `spec-critic` always for a feature; `security-reviewer` on
auth, secrets, api, state, destructive flags, workflow or dependency changes;
`privacy-reviewer` when `data_classes_touched` is not empty or the files match
`classified_paths`; `llm-security-reviewer` when the feature adds LLM calls,
tools, MCP servers, hooks or skills; `breaker` when the plan marks a checkpoint
`runnable` and Project config has a `run` command. Write the expected crew
into the spec slice under Assumptions.

Commit: `git -C "$root" add docs/specs/<slug>.md docs/prd.md` then
`git -C "$root" commit -m "spec(<slug>): slice for FR-nn"`.

## 4. Plan and engine

Hand off to `/tentaqles:build` Phase 0 (worktree) and Phase 1 with the spec
path and `workflow=feature`. The planner reads the spec slice; every acceptance
check must appear in some checkpoint's `tests`. **Human stop 1** is the plan
approval in their terminal. Then Phases 2 and 3 as the build skill describes:
tests first and locked, implementer, behaviour gate, crew review with every
verdict recorded, fixer loop.

## 5. Definition of done and stop 2

`/tentaqles:dod <slug>` at the last checkpoint. The gate requires an ADR under
`docs/adr/` when files matching `architecture_paths` changed, `docs/privacy.md`
and the manifest when a data class or retention changed, a `## Migration plan`
in the spec when migration files changed, and the changelog entry. **Human
stop 2** (engine Phase 4), then `/tentaqles:ship`; the PR body quotes the DoD
record.

## Never

- write code for a feature that has no FR and no spec slice
- mark an acceptance check done without a test the gate ran
- `git add -A`, `git add .`, `--no-verify`, reading `.env*`
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/feature plugin/tests/test_process_skills.py
git commit -m "feat(skills): feature (FR-first, interview, pre-selected crew, engine, DoD)"
```

---

### Task 12: `kickoff` skill

**Files:**
- Create: `plugin/skills/kickoff/SKILL.md`
- Modify: `plugin/tests/test_process_skills.py`

**Interfaces:**
- Consumes: `prd`, `threat-model`, `classify-data`, `ci-pipeline`, `supply-chain` (the last four from the companion plan), `build` with `workflow=kickoff`, `direction`, templates `CLAUDE-project.md`, `adr-0000-template.md`, `LEARNINGS.md`.

- [ ] **Step 1: Write the failing test**

```python
def test_kickoff_requires_a_planning_ready_prd_and_scaffolds_everything() -> None:
    body = _body("kickoff")
    for token in ("prd-status.py", "prd_exit", "/tentaqles:prd", "docs/adr/0001-stack-and-layout.md",
                  "templates/process/CLAUDE-project.md", "/tentaqles:threat-model", "/tentaqles:classify-data",
                  "tq allow", "/tentaqles:ci-pipeline", "/tentaqles:supply-chain", "tq settings render",
                  ".gitignore", "workflow=kickoff", "/tentaqles:direction", "Must not trade off"):
        assert token in body, token
    assert SNIPPET in body
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k kickoff`
Expected: FAIL, file missing.

- [ ] **Step 3: Write the skill**

````markdown
---
name: kickoff
description: Start a new project the tentaqles way. Requires a planning-ready PRD (runs /tentaqles:prd otherwise), then writes the stack ADR and the project CLAUDE.md with its fixed sections, the threat model, the data classification and privacy record, the CI ratchet and supply-chain guardrails, plans the first slice from the release-1 FRs, runs the gated build engine and a direction audit. Use when the user says "new project", "start project X", "kick off", "greenfield", "bootstrap the repo", or "/tentaqles:kickoff".
argument-hint: <idea | path to docs/prd.md>
---

# Kickoff

Two human stops: (1) data classes, threat-model accepted risks and the first
checkpoint plan together; (2) the result. Everything in between is decided and
logged as an assumption.

## 1. PRD, planning-ready or nothing

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
```

- `prd_exit=4`: run `/tentaqles:prd` with `$ARGUMENTS`.
- `prd_exit=3`: stop here. List the open `NEEDS-DECISION` items and the FRs
  without acceptance checks, with how each could be settled (ask, research,
  prototype). Kickoff continues only at `prd_exit=0`.

If the repository does not exist yet, create it inside the client workspace
with `/tentaqles:add-project` first (it inherits the git identity from the
manifest; verify `git config user.email`).

## 2. Project shape

Read PRD §9 and the repository (`package.json`, `pyproject.toml`, `go.mod`,
lockfiles). Never assume a default stack; when §9 is silent, ask once.

- Write `docs/adr/0001-stack-and-layout.md` from
  `"$_tqe/templates/process/adr-0000-template.md"`: the stack, the layout,
  why, the alternatives. Status accepted.
- Write `CLAUDE.md` from `"$_tqe/templates/process/CLAUDE-project.md"`. The
  **Reading path** stays as the template says. **Must not trade off** starts
  from PRD §6 and §7: one line per property with the test that will enforce
  it, and every one of those tests is a case in the first checkpoint. **Project
  config**: fill the prose and the ```` ```json tq-project ```` block with the
  real commands (test, lint, typecheck, build, run), `architecture_paths`,
  `changelog`.
- Copy `"$_tqe/templates/process/LEARNINGS.md"` to `docs/LEARNINGS.md` and
  create `docs/specs/`, `docs/checkpoints/`, `docs/reviews/`.

## 3. Threat model

Run `/tentaqles:threat-model`: the four questions (what are we building, what
can go wrong, what will we do about it, did we do a good job) per trust
boundary from PRD §6 with STRIDE; it writes `docs/threat-model.md`. Any
unmitigated high-risk row becomes an FR (add it to the PRD with acceptance
checks) or a `NEEDS-DECISION` item for stop 1.

## 4. Data classification

Run `/tentaqles:classify-data` from PRD §5: it writes the manifest `data:`
block, mirrors its paths into `classified_paths` of the `tq-project` block,
and writes `docs/privacy.md`. If any class is `pii`, `sensitive`, `financial`
or `children`, the privacy record must name purpose, legal basis, retention
and the DSAR path before planning continues. Changing `.tentaqles.yaml` changes
its trust hash: the human runs `tq allow <workspace>` as part of stop 1.

## 5. Guardrails before code

- `/tentaqles:ci-pipeline`: the ratchet with a zero baseline (cheapest now).
- `/tentaqles:supply-chain`: actions pinned to SHAs, `permissions:` on every
  workflow, lockfiles committed, SBOM on release, secret scanning as a
  required check.
- `tq settings render <workspace>` for deny-first permissions.
- `.gitignore` entries: `.env*` (keep `.env.example`), `.claude/build/`,
  `.claude/settings.local.json`.

Commit explicit paths: `git -C "$root" add CLAUDE.md docs .github .gitignore`
then `git -C "$root" commit -m "chore(kickoff): project shape, threat model, privacy record, guardrails"`.
Stage `.tentaqles.yaml` only if this repository is where the manifest lives.

## 6. First slice and stop 1

Hand off to `/tentaqles:build` Phase 0 (worktree) and Phase 1 with the PRD
path as the spec, `workflow=kickoff`. Tell the planner to derive checkpoints
from the release-1 FRs of PRD §11, checkpoint 1 being the thinnest slice that
proves the stack end to end, and to include the "Must not trade off" tests in
it. **Human stop 1** shows, in one `AskUserQuestion`: the data classes and
the privacy record summary, the threat model's accepted risks, the checkpoint
plan with its verify commands, the assumptions. On approve, the human runs
`build-gate approve <slug>` and `tq allow <workspace>` in their terminal.

## 7. Engine

Phases 2 and 3 of the build skill. The crew for a kickoff always includes
`spec-critic`; `security-reviewer` and `privacy-reviewer` join by triage flags
and classified paths.

## 8. Direction, stop 2, ship

Run `/tentaqles:direction`: every release-1 FR graded, unplanned work listed,
`ON COURSE | DRIFTING | OFF COURSE`. Fix-code items go back into the engine as
new checkpoints; Update-PRD and Decide items go to the user at **stop 2**
(engine Phase 4) together with the DoD record (`/tentaqles:dod <slug>`). Then
`/tentaqles:ship`.

## Never

- plan from a PRD that is not planning-ready
- assume a stack, a cloud or a database the PRD and the repo do not name
- stage with `git add -A` or `git add .`; commit `.env*` or `.claude/build/`
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/kickoff plugin/tests/test_process_skills.py
git commit -m "feat(skills): kickoff (PRD-gated project start through the engine)"
```

---

### Task 13: `add-project` and `ship` changes

**Files:**
- Modify: `plugin/skills/add-project/SKILL.md` (section "2. Create CLAUDE.md", "6. Report")
- Modify: `plugin/skills/ship/SKILL.md` (steps 5 and 7)
- Modify: `plugin/tests/test_process_skills.py`

- [ ] **Step 1: Write the failing tests**

```python
def test_add_project_scaffolds_the_fixed_sections_and_points_to_kickoff() -> None:
    body = _body("add-project")
    for token in ("templates/process/CLAUDE-project.md", "## Reading path", "## Must not trade off", "## Project config",
                  "docs/LEARNINGS.md", "/tentaqles:kickoff"):
        assert token in body, token


def test_ship_quotes_the_dod_record_and_warns_on_large_diffs() -> None:
    body = _body("ship")
    for token in ("dod.json", "Definition of done", "400", "git diff --staged --shortstat"):
        assert token in body, token
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k "add_project or ship_quotes"`
Expected: FAIL.

- [ ] **Step 3: Edit the skills**

In `plugin/skills/add-project/SKILL.md`, replace section **"2. Create CLAUDE.md"** with:

````markdown
### 2. Create CLAUDE.md and the docs skeleton

Write `{client_root}/{project-slug}/CLAUDE.md` from the process scaffold
`"$_tqe/templates/process/CLAUDE-project.md"` (resolve `$_tqe` with the
snippet above). Keep its three fixed sections, `## Reading path`,
`## Must not trade off` and `## Project config`, and add the inherited client
context under a `## Client Context` heading:

```markdown
## Client Context
- Client: {client display_name}
- Cloud: {cloud provider from manifest}
- Database: {database provider + dialect from manifest}
- Git: {git provider} ({git email})
- Language: {language}
```

Fill the `tq-project` block's `commands.test` with the real test command when
the stack is known (read `package.json`, `pyproject.toml`, `go.mod`); leave the
template placeholder otherwise and say so in the report. Then create the docs
skeleton:

```bash
mkdir -p "{client_root}/{project-slug}/docs/adr" "{client_root}/{project-slug}/docs/specs"
cp "$_tqe/templates/process/LEARNINGS.md" "{client_root}/{project-slug}/docs/LEARNINGS.md"
```

The PRD is not written here: `/tentaqles:kickoff` interviews for it and
requires it before planning.
````

In section **"6. Report"**, replace the last bullet's text with: `Next steps: "Run /tentaqles:kickoff to write the PRD, the threat model and the data classification and plan the first slice; or /tentaqles:feature and /tentaqles:fix for work on an existing codebase." If the stack is Node/TypeScript, Python or Go, offer /tentaqles:ci-pipeline (kickoff runs it anyway). Do not add CI without a yes.`

In `plugin/skills/ship/SKILL.md`, at the end of **step 5 (Verify locally)** add:

````markdown
**Definition of done.** When this branch came from `/tentaqles:feature`,
`/tentaqles:fix` or `/tentaqles:kickoff`, the gate wrote
`.claude/build/<slug>/dod.json`. Read its `ok`, `tree`, `override` and
`checks` (`id`, `ok`, `detail`) and quote them in the PR body; if `ok` is false
and there is no override, stop: run `/tentaqles:dod <slug>` first. Confirm the
recorded `tree` matches the current tree hash (`build-gate status <slug>` shows
`evidence=fresh`); a stale record is not evidence.

**Size advisory** (not a block):

```bash
git diff --staged --shortstat
```

Over about 400 changed lines, the PR body needs a stated reason (a generated
file, a migration, a rename) under Notes; otherwise suggest splitting.
````

In **step 7**, extend the PR body shape with a section after `## Verification`:

```markdown
## Definition of done
<from dod.json: PASS at tree <12 chars> | OVERRIDDEN: <reason> | not applicable (plain build)>
- <check id>: <ok|skip|MISSING> — <detail>
Size: <N files, +A −D>; reason if over ~400 lines: <reason>
```

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass (`test_admin_merge_only_appears_as_forbidden` for `ship` still passes: do not add `--admin` anywhere).

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/add-project/SKILL.md plugin/skills/ship/SKILL.md plugin/tests/test_process_skills.py
git commit -m "feat(skills): add-project scaffolds the fixed CLAUDE.md sections; ship quotes the DoD record and flags large diffs"
```

---

### Task 14: `tq insights` process section

**Files:**
- Create: `internal/insights/process.go`, `internal/insights/process_test.go`
- Modify: `internal/insights/report.go` (`Options.BuildGateDir`, `Report.Process`, `Run`), `internal/insights/render.go`, `docs/INSIGHTS.md`

**Interfaces:**
- Consumes: `$TQ_HOME/build-gate/events.jsonl` rows `{ts, kind, slug, project, workflow, checkpoint?, ok?, reviewer?, result?, blockers?, majors?, minors?, unjudged?}` (Task 4/5).
- Produces:

```go
type ReviewerStats struct {
    Pass, Fail, Blocked      int `json:"pass" ...`
    Blockers, Majors, Minors int
    Unjudged                 int
    HumanJudgements          int `json:"human_judgements"`
}
type ProcessReport struct {
    VerifyPass, VerifyFail int
    Reviewers    map[string]ReviewerStats
    DoDPass, DoDFail, DoDOverrides int
    Finished     int
    ByWorkflow   map[string]int // runs seen per workflow (distinct slug)
    CannotJudgeRate float64     // BLOCKED / (PASS+FAIL+BLOCKED)
}
func ReadProcessEvents(path string, since time.Time) (ProcessReport, error)
```

- [ ] **Step 1: Write the failing test**

```go
// internal/insights/process_test.go
package insights

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadProcessEvents(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	lines := []string{
		`{"ts":"2026-10-20T10:00:00Z","kind":"verify","slug":"a","project":"x","workflow":"feature","checkpoint":1,"ok":true}`,
		`{"ts":"2026-10-20T10:01:00Z","kind":"verify","slug":"a","project":"x","workflow":"feature","checkpoint":1,"ok":false}`,
		`{"ts":"2026-10-20T10:02:00Z","kind":"review","slug":"a","project":"x","workflow":"feature","checkpoint":1,"reviewer":"build-critic","result":"FAIL","blockers":1,"majors":0,"minors":2,"unjudged":0}`,
		`{"ts":"2026-10-20T10:03:00Z","kind":"review","slug":"a","project":"x","workflow":"feature","checkpoint":1,"reviewer":"spec-critic","result":"BLOCKED","unjudged":1}`,
		`{"ts":"2026-10-20T10:04:00Z","kind":"judge","slug":"a","project":"x","workflow":"feature","checkpoint":1,"reviewer":"spec-critic","result":"PASS"}`,
		`{"ts":"2026-10-20T10:05:00Z","kind":"dod","slug":"a","project":"x","workflow":"feature","ok":true}`,
		`{"ts":"2026-10-20T10:06:00Z","kind":"dod-override","slug":"b","project":"x","workflow":"fix","ok":false}`,
		`{"ts":"2026-10-20T10:07:00Z","kind":"finish","slug":"a","project":"x","workflow":"feature"}`,
		`{"ts":"2026-01-01T00:00:00Z","kind":"verify","slug":"old","project":"x","workflow":"build","ok":true}`,
		`not json`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	r, err := ReadProcessEvents(p, since)
	if err != nil {
		t.Fatal(err)
	}
	if r.VerifyPass != 1 || r.VerifyFail != 1 {
		t.Fatalf("verify %+v", r)
	}
	bc := r.Reviewers["build-critic"]
	if bc.Fail != 1 || bc.Blockers != 1 || bc.Minors != 2 {
		t.Fatalf("critic %+v", bc)
	}
	sc := r.Reviewers["spec-critic"]
	if sc.Blocked != 1 || sc.Unjudged != 1 || sc.HumanJudgements != 1 || sc.Pass != 0 {
		t.Fatalf("spec-critic %+v (a human judgement is counted separately, not as a reviewer PASS)", sc)
	}
	if r.DoDPass != 1 || r.DoDFail != 0 || r.DoDOverrides != 1 || r.Finished != 1 {
		t.Fatalf("dod %+v", r)
	}
	if r.ByWorkflow["feature"] != 1 || r.ByWorkflow["fix"] != 1 || r.ByWorkflow["build"] != 0 {
		t.Fatalf("by workflow %v (events before --since are skipped)", r.ByWorkflow)
	}
	if got := r.CannotJudgeRate; got < 0.49 || got > 0.51 {
		t.Fatalf("cannot-judge rate %v want 0.5 (1 BLOCKED of 2 verdicts)", got)
	}
}

func TestReadProcessEvents_MissingFileIsEmpty(t *testing.T) {
	r, err := ReadProcessEvents(filepath.Join(t.TempDir(), "none.jsonl"), time.Time{})
	if err != nil || r.VerifyPass != 0 || r.Reviewers == nil {
		t.Fatalf("%+v %v", r, err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/insights/ -run TestReadProcessEvents -v`
Expected: compile error, `undefined: ReadProcessEvents`.

- [ ] **Step 3: Implement**

`internal/insights/process.go`:

```go
package insights

import (
	"encoding/json"
	"time"
)

// Process events are written by the plugin's build gate
// ($TQ_HOME/build-gate/events.jsonl): one aggregate-only line per verify run,
// reviewer verdict, human judgement, definition-of-done run, override and
// finish. They carry slugs, reviewer names, results and counts; never file
// names or text.

// ReviewerStats aggregates one reviewer's verdicts.
type ReviewerStats struct {
	Pass            int `json:"pass"`
	Fail            int `json:"fail"`
	Blocked         int `json:"blocked"`
	Blockers        int `json:"blockers"`
	Majors          int `json:"majors"`
	Minors          int `json:"minors"`
	Unjudged        int `json:"unjudged"`
	HumanJudgements int `json:"human_judgements"`
}

// ProcessReport is the "Process" section of the report (spec §12).
type ProcessReport struct {
	VerifyPass      int                      `json:"verify_pass"`
	VerifyFail      int                      `json:"verify_fail"`
	Reviewers       map[string]ReviewerStats `json:"reviewers"`
	DoDPass         int                      `json:"dod_pass"`
	DoDFail         int                      `json:"dod_fail"`
	DoDOverrides    int                      `json:"dod_overrides"`
	Finished        int                      `json:"finished"`
	ByWorkflow      map[string]int           `json:"by_workflow"`
	CannotJudgeRate float64                  `json:"cannot_judge_rate"`
}

type processEvent struct {
	TS         string `json:"ts"`
	Kind       string `json:"kind"`
	Slug       string `json:"slug"`
	Workflow   string `json:"workflow"`
	Checkpoint int    `json:"checkpoint"`
	OK         *bool  `json:"ok"`
	Reviewer   string `json:"reviewer"`
	Result     string `json:"result"`
	Blockers   int    `json:"blockers"`
	Majors     int    `json:"majors"`
	Minors     int    `json:"minors"`
	Unjudged   int    `json:"unjudged"`
}

func parseEventTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000000+00:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ReadProcessEvents aggregates the events at or after since. A missing file
// yields an empty report and no error.
func ReadProcessEvents(path string, since time.Time) (ProcessReport, error) {
	r := ProcessReport{Reviewers: map[string]ReviewerStats{}, ByWorkflow: map[string]int{}}
	runs := map[string]string{} // slug -> workflow
	verdicts, blocked := 0, 0
	err := eachLine(path, func(line []byte) {
		var e processEvent
		if json.Unmarshal(line, &e) != nil {
			return
		}
		t, ok := parseEventTime(e.TS)
		if !ok || t.Before(since) {
			return
		}
		if e.Slug != "" {
			wf := e.Workflow
			if wf == "" {
				wf = "build"
			}
			runs[e.Slug] = wf
		}
		switch e.Kind {
		case "verify":
			if e.OK != nil && *e.OK {
				r.VerifyPass++
			} else {
				r.VerifyFail++
			}
		case "review":
			s := r.Reviewers[e.Reviewer]
			switch e.Result {
			case "PASS":
				s.Pass++
			case "FAIL":
				s.Fail++
			case "BLOCKED":
				s.Blocked++
				blocked++
			}
			verdicts++
			s.Blockers += e.Blockers
			s.Majors += e.Majors
			s.Minors += e.Minors
			s.Unjudged += e.Unjudged
			r.Reviewers[e.Reviewer] = s
		case "judge":
			s := r.Reviewers[e.Reviewer]
			s.HumanJudgements++
			r.Reviewers[e.Reviewer] = s
		case "dod":
			if e.OK != nil && *e.OK {
				r.DoDPass++
			} else {
				r.DoDFail++
			}
		case "dod-override":
			r.DoDOverrides++
		case "finish":
			r.Finished++
		}
	})
	for _, wf := range runs {
		r.ByWorkflow[wf]++
	}
	if verdicts > 0 {
		r.CannotJudgeRate = float64(blocked) / float64(verdicts)
	}
	return r, err
}
```

`eachLine` (in `jev.go`) returns nil for a missing file; keep that behaviour.

In `report.go`: add `BuildGateDir string // "" means $TQ_HOME/build-gate` to `Options`, `Process ProcessReport \`json:"process"\`` to `Report` (after `Jev`), default it in `Run` (`o.BuildGateDir = filepath.Join(paths.Home(), "build-gate")`) and read it next to the Jev logs:

```go
	rep.Process, _ = ReadProcessEvents(filepath.Join(o.BuildGateDir, "events.jsonl"), o.Since)
```

In `render.go`, after the Jev block and before "Background review sessions":

```go
	pr := r.Process
	fmt.Fprintln(w, "\n== Process (build gate)")
	fmt.Fprintf(w, "verify: pass %d, fail %d   dod: pass %d, fail %d, overrides %d   finished runs %d   by workflow: %s\n",
		pr.VerifyPass, pr.VerifyFail, pr.DoDPass, pr.DoDFail, pr.DoDOverrides, pr.Finished, countList(pr.ByWorkflow))
	if len(pr.Reviewers) > 0 {
		tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "reviewer\tpass\tfail\tblocked\tblockers\tmajors\tminors\tunjudged\thuman judgements")
		for _, name := range sortedKeys(pr.Reviewers) {
			s := pr.Reviewers[name]
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", name, s.Pass, s.Fail, s.Blocked, s.Blockers, s.Majors, s.Minors, s.Unjudged, s.HumanJudgements)
		}
		tw.Flush()
		fmt.Fprintf(w, "cannot-judge rate: %s\n", pct(pr.CannotJudgeRate))
	}
```

In `docs/INSIGHTS.md`, add a section `### Process (build gate)` describing the events file, the fields, and that time-from-PRD-to-merge and hotfix-debt counts are not yet measured (they need PR data and memory.db; follow-up).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/insights/ ./internal/cmd/ -run 'TestReadProcessEvents|TestInsights' -v`
Expected: pass; `TestInsightsCmdJSON` still passes (the JSON gains a `process` key).

- [ ] **Step 5: Commit**

```bash
git add internal/insights/process.go internal/insights/process_test.go internal/insights/report.go internal/insights/render.go docs/INSIGHTS.md
git commit -m "feat(insights): process section from the build gate's events log"
```

---

### Task 15: Docs, changelog, final verification

**Files:**
- Modify: `plugin/CHANGELOG.md`, `plugin/README.md`, `plugin/CLAUDE.md`, `docs/CLAUDE-SETTINGS.md` (no change needed unless the doctor table moved), `C:\repos\tentaqles\docs\superpowers\plans\2026-10-20-dev-process-spine.md` (status line)

- [ ] **Step 1: Changelog**

Add at the top of `plugin/CHANGELOG.md`, above `## [0.11.1]`:

```markdown
## [Unreleased]

### Added

- **Development process spine.** New skills `prd`, `kickoff`, `feature`, `fix`,
  `direction`, `dod`; agents `spec-critic` and `arbiter`; process templates
  under `templates/process/` (PRD, ADR, spec slice, fix spec, LEARNINGS,
  project CLAUDE.md scaffold, review contract). The build engine records every
  reviewer verdict (`build-gate record-review`, human `judge`), refuses a
  review pass without a `build-critic` PASS at the current tree, and runs a
  definition of done (`build-gate dod`) that the Stop hook requires for
  `feature`, `fix` and `kickoff` plans. `tq insights` gains a Process section
  from `$TQ_HOME/build-gate/events.jsonl`. SessionStart points at the PRD and
  counts open `NEEDS-DECISION` items. `add-project` scaffolds the fixed
  `CLAUDE.md` sections; `ship` quotes the DoD record and flags diffs over ~400
  lines.

### Changed

- `build-critic` returns the answer-key grammar (`RESULT`, `BUILT-BY-ME`,
  `F`/`U` rows) instead of `VERDICT`. Plans gain top-level `workflow`,
  `hotfix` and `spec` fields; `review: n/a` is allowed only for hotfix plans.
```

- [ ] **Step 2: README and plugin CLAUDE.md**

In `plugin/README.md` skills table, add rows for `/tentaqles:prd`, `/tentaqles:kickoff`, `/tentaqles:feature`, `/tentaqles:fix`, `/tentaqles:direction`, `/tentaqles:dod` (one line each, matching the style of existing rows), and extend the `/tentaqles:build` row with "reviewer crew with recorded verdicts, definition of done". In `plugin/CLAUDE.md` Architecture block add `templates/process/` and `tentaqles/workflow/project.py`, `prd.py` lines; in the `agents/` line list `spec-critic`, `arbiter`.

- [ ] **Step 3: Full verification**

Run:
```bash
cd plugin && python -m pytest tests -q
cd .. && go test ./... 
```
Expected: both green. Paste the pass counts into the commit message body.

- [ ] **Step 4: Commit**

```bash
git add plugin/CHANGELOG.md plugin/README.md plugin/CLAUDE.md
git commit -m "docs(plugin): development process spine"
```

Then update the status line at the top of this plan file (workspace docs, not git-tracked) to `Status: implemented on branch <name>`.

---

## Self-review notes

- Spec coverage: §4.1 layout (Tasks 1, 12, 13), §4.2 PRD template and mechanical check (Tasks 1, 2, 9), §4.3 templates (Task 1; threat model, privacy record and incident playbook templates are in the companion plan), §5.1 (Task 12), §5.2 (Task 11), §5.3 (Task 10), §5.4 crew and verdicts and DoD (Tasks 4, 5, 7), §6 rows prd/kickoff/feature/fix/direction/dod/add-project/ship/build (Tasks 7 to 13), §7 build-critic/spec-critic/arbiter (Task 6), §8.1 (Task 5), §8.3 SessionStart and PR size (Tasks 3, 13; the PostToolUse command audit already exists as fix atoms in 0.11.1), §9 skill picker (the `fix` description carries the bug-shaped triggers; Jev picks among installed skills without code changes), §9 Stop check (the DoD is enforced by the gate's own Stop hook; the Jev completion check is unchanged), §12 (Task 14; PRD-to-merge time and hotfix debt deferred and documented).
- Hub-ready interfaces (user decision 2026-10-20): decision records, hub actor tokens and `build-gate state` (Task 5b); the build skill's headless mode (Task 7).
- Open point resolution: `/fix` with Jev disabled reviews **full** (the build skill already treats a missing `tq decide triage` as full). `breaker` runs only when a checkpoint is marked `"runnable": true` (Task 7 text).
- Deviation from spec §8.1 wording: "the 8-block cap as today" is implemented as the gate's existing once-per-problem block (`MAX_STOP_BLOCKS = 1`), which is what the engine does today.
