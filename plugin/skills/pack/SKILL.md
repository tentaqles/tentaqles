---
name: pack
description: Run and explain the tq pack checks (data, ml, cloud, db) of this workspace. Lists the enabled packs, runs tq pack check, reads each result and its findings, picks the pack reviewers with tq pack crew, and fixes a declared tool that is not installed. Use when the user says "pack check", "which packs are on", "run the data checks", "why did pack:data/... fail", "declared in packs ... but not installed", when the definition of done shows pack:config, pack:run or pack:<pack>/<check> items, or when an on-ramp's Packs step sends you here.
argument-hint: "[check [--pack <name>] | list | doctor | crew --flags <a,b>]"
---

# Packs

A pack is a set of checks, guard rules, reviewers and on-ramp notes for one kind of
work: `data` and `ml` (release 3), `cloud` and `db` (release 4). The trusted layer is
the manifest `packs:` block (pinned by `tq allow`). A repo may add a `tq-packs.yaml`
at its root, which can only add or tighten, never loosen. tq's `docs/PACKS.md` has
the config keys, the merge rules and the links to each pack's own page.

You run the commands and explain what they print. Never edit `tq-packs.yaml`, a
baseline file or the manifest to make a check pass: the guard asks the human before
an agent edits them, and the build gate freezes the config hash at plan approval.

## 1. Which packs are on

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 10"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 10"
$_to tq pack list --enabled --json; echo "packs_exit=$?"
```

- `packs_exit=0`: the enabled packs, their merged config, the `config_hash` and each
  check with its kind (built-in or external), tool and `required` bit.
- `tq: unknown command "pack"`: this tq predates packs. Say so and stop; packs are off.
- `packs_exit=124`: tq did not answer in 10 s. Report it; do not retry in a loop.
- `packs_exit=2`: a config error. The message names the file and key. Explain it; the
  human fixes the manifest, an agent may propose the `tq-packs.yaml` change.

## 2. Run the checks

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
tq pack check --budget 600s; echo "check_exit=$?"
```

- Add `--pack <name>` to run one pack, `--staged` for the index, `--base <ref>` for a
  branch, `--all` for every tracked file, `--builtin-only` to skip the external tools.
  `--json` prints schema `tq.packs/v1`. The budget caps the whole run (max 1800 s).
- A check that needs warehouse or cloud credentials runs only when the human wraps it:
  `tq dotenv run -- tq pack check`. Never read `.env` files yourself.
- `check_exit=0`: no required check failed. `1`: at least one did. `2`: a usage or
  config error.

Each line is one result: `ok`, `fail` or `skip`, the check id `<pack>/<check>` and a
detail, then up to 20 findings as `file:line level rule message`. Only an `error`
finding fails a check; `warn` findings are listed and never fail.

| Result | Meaning | What you do |
|---|---|---|
| `ok` | the check passed | nothing |
| `fail`, required | a built-in check of an enabled pack, or an external check whose tool is declared in `tools:` | fix the code at the `file:line` findings; the pack's page in `docs/packs/` explains the rule |
| `fail`, not required | **advisory**: the tool is installed but not declared | report it; it never fails the definition of done. The human may declare the tool to make it required |
| `skip` | the check does not apply, or an undeclared tool is not installed | nothing |
| `fail`: `declared in packs.<p>.tools but not installed; install it or remove it from tools` | a required tool is missing | step 4 |
| `fail`: `timed out` | a built-in check passed 10 s, or the run passed its budget | report it with the check id; a larger repo may need a larger `pack_budget` |

A finding that a baseline file (`packs.<pack>.baseline`) already lists is downgraded to
`warn`. Writing or refreshing a baseline is human only: `tq pack baseline [--pack X]`.
You never run it, and you never add an entry to make a finding go away.

## 3. Reviewers

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 10"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 10"
$_to tq pack crew --flags <the flags tq decide triage printed, comma-separated> --json; echo "crew_exit=$?"
```

It prints the pack reviewers the flags map to, de-duplicated (for example
`pipeline-reviewer` for `pipeline`, `ml-reviewer` for `ml`). Add each one to the crew.
The gate's `record-review` accepts any `<name>-reviewer` that has an agent file in the
plugin. If an agent is not installed, record nothing for it and tell the user which
reviewer was skipped and why.

## 4. A declared tool is not installed

`tq pack doctor` lists each tool: declared or not, installed or not, path, version and
the pack's minimum version.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
tq pack doctor; echo "doctor_exit=$?"
```

tq looks in `tool_paths.<tool>` (manifest only), then the project's `.venv`, then
`PATH`. Two fixes, both the human's choice: install the tool (into the project's
`.venv` for Python tools, with the install hint the doctor prints), or remove it from
`tools:` in the manifest (`tq allow` again) so the check becomes advisory. Never
remove a tool from `tools:` yourself, and never add a `tool_paths` entry to
`tq-packs.yaml` (the repo layer ignores it).

## 5. In the definition of done

`build-gate dod <slug>` (the `dod` skill) runs `tq pack check --json` against the
build's base and records:

| Item | Meaning | Who fixes it |
|---|---|---|
| `pack` (skip) | no packs were frozen at approval (none enabled, or tq missing then) | nobody |
| `pack:config` | the merged pack config hash differs from the one frozen at approval (the manifest or `tq-packs.yaml` changed) | the human re-approves the plan so the new config is frozen |
| `pack:run` | tq is missing, crashed, timed out or printed output that is not `tq.packs/v1`, while packs were frozen: the gate fails closed | install or repair tq (`tq doctor`), then re-run the DoD; never re-approve to skip it |
| `pack:<pack>/<check>` | one per result: `ok`, `MISSING` for a required fail, `skip` for an advisory fail or a skip; the detail carries the first 5 findings | fix the code; a declared-missing tool goes through step 4 |

The budget is 600 s unless the project's `tq-project` block sets `pack_budget`
(seconds, at most 1800), frozen at approval with the rest of the block.

## On-ramp step

The `build`, `feature`, `fix`, `kickoff`, `dod` and `ship` skills each run this step
(their `## Packs` section):

1. Run step 1 (`tq pack list --enabled --json`, 10 s). A tq without the `pack`
   command: skip the rest. No pack enabled: skip the rest, except in `kickoff` (2).
2. For each enabled pack, read every `"$_tqe/skills/pack/references/onramps/<pack>-*.md"`
   and follow its `## <on-ramp>` section (for example `## feature`). A file without
   that section adds nothing for this on-ramp. `kickoff` reads the `## kickoff`
   section of every pack, enabled or not, because it proposes which packs to enable
   (as a proposal for the human; it never edits the manifest).
3. Wherever the on-ramp picks reviewers, run step 3 with the flags triage printed
   (or, before any diff exists, the flags the on-ramp files say the work will raise)
   and add every reviewer it prints.
