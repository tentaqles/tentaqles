---
name: classify-data
description: "Classify the data a project handles and record how it is protected. Builds the data dictionary from the PRD and the schema, writes the manifest data block (classes, regions, repository paths and database tables per class, retention, incident authority and contact), mirrors the paths into the CLAUDE.md tq-project block, writes docs/privacy.md with purpose, legal basis, retention and the DSAR path, and offers a Faker-based synthetic fixture generator. Use during /tentaqles:kickoff, when the user asks \"what data do we handle\", \"classify the data\", \"privacy record\", when tq doctor reports data-block-missing, or when a schema change adds PII-looking columns."
argument-hint: '[update] [--headless]'
---

# Classify data

The manifest `data:` block is what makes the guard deterministic. Personal
data may be read, queried and processed locally; the guards are at the exits:
commits, pushes, PR and issue text, memory, fixtures, and model calls outside
`prompts.allow`. The declared classes decide what `tq` refuses to record at
those exits, what the plugin masks before memory, the vault or a model call,
which tables and paths may never be dumped into fixtures, which diffs triage
flags, and when the definition of done requires `docs/privacy.md` to move with
the data. None of that works on a block without paths or tables, so this skill
never writes one. Nothing here stops you from opening a data file to classify it.

Never commit on main or master: if `git branch --show-current` says `main` or
`master`, stop and ask for a branch first (headless: write the files, do not
commit, and say so in the report).

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. Where a default exists
(recommended retention, the authority for the regions), take it and log the
assumption in the `## Open decisions` section of `docs/privacy.md`. Where none
exists, write a `NEEDS-DECISION` line there and list it in the final report.
Use `build-gate ask <slug> --kind question --text "<the question>" --options <a>,<b>`
only when the caller passed the slug of an active build run; the `build-gate`
prefix is in `skills/build/SKILL.md`.

Headless never edits `.tentaqles.yaml`. The manifest is the human's: every
change moves its trust hash and blocks the workspace until `tq allow`, which
only a human runs. Headless writes the proposed block to
`docs/privacy-manifest-proposal.yaml` instead (section 3).

## 1. Inputs

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" -c "
import os
from tentaqles.manifest.loader import load_manifest
m = load_manifest(os.getcwd())
print('manifest=' + (os.path.join(m['_client_root'], '.tentaqles.yaml') if m else 'NONE'))
print('data=' + repr(m.get('data')) if m else 'data=None')
"
command -v tq >/dev/null 2>&1 && tq doctor 2>&1 | grep -i 'data-block' || true
echo "dictionary=$_tqe/skills/classify-data/references/data-dictionary.md"
echo "privacy_template=$_tqe/templates/process/privacy.md"
echo "playbook_template=$_tqe/templates/process/incident-playbook.md"
```

`manifest=NONE`: stop; the workspace has no manifest (`/tentaqles:add-client`).
A `data-block-missing` line from `tq doctor` means this skill has work to do.

## 2. Data dictionary

Read PRD §5 and §2, the schema (SQL, Prisma, migrations, models), exports and
log statements. Grep the column-name signals from
`references/data-dictionary.md`. Produce the table: data, class, source, store
(repository path or `schema.table`), retention, deletion job. Ask the user one
question at a time only where the class is unclear (is the phone a business
line or a person's?) or where retention is a business decision; offer a
recommended default and record the answer. Also ask, one question each:

- "Which countries are the data subjects in?" This fills `data.regions`.
  Default: infer from the PRD's users and markets and log the assumption.
- "Who receives an incident notice (name or role, and the channel)?" This
  fills `incident.contact`. Headless: never invent a contact; write a
  `NEEDS-DECISION` line in `## Open decisions` and do not write the block
  with a made-up address.

Headless: see the paragraph at the top.

## 3. Manifest `data:` block

Headless (see the top): do not edit `.tentaqles.yaml`, with any tool. Write
the block below, as a complete `data:` mapping, to
`docs/privacy-manifest-proposal.yaml` (a comment line at the top says it is a
proposal and that regions and retention are assumptions), then record one human
decision: with an active build slug,
`build-gate ask <slug> --kind question --text "Review docs/privacy-manifest-proposal.yaml, apply it to .tentaqles.yaml, then run tq allow <workspace>" --options applied,stop`
and end the turn; without a slug, a `NEEDS-DECISION` line in `## Open decisions`
of `docs/privacy.md` with the same text, listed in the final report. Carry on
with sections 4 to 6 (`classified_paths`, the privacy record); they do not
touch the manifest.

Interactive: edit `.tentaqles.yaml` with the Edit tool (never from the shell, never with
credentials: the contact is an address, not a login):

```yaml
data:
  classes: [pii, financial]            # from: public, internal, pii, sensitive, financial, children, secrets
  regions: [BR]                        # ISO country codes of the data subjects
  prompts:
    # Governs what may be sent to a MODEL CALL unmasked.
    # The plugin never STORES personal data unmasked (memory, vault), regardless of this list.
    allow: [public, internal]
  paths:                               # at least one path or table per declared class
    - path: db/schema/customers.sql
      classes: [pii]
    - path: data/exports/**
      classes: [pii, financial]
    - table: public.payments
      classes: [financial]
  retention_default: 24m               # <n>d | <n>m | <n>y
  incident:
    authority: ANPD                    # ANPD (3 business days) | GDPR (72 hours) | none
    contact: dpo@example.test          # an address, never a credential
```

Rules:

- Every declared class has **at least one** `path` or `table` entry. If none
  can be found, ask for them; headless: write a `NEEDS-DECISION` line, do not
  write the block, and list it in the report.
- Memory, the vault and hook model calls always mask declared
  pii, sensitive, financial, children and secrets classes; `prompts.allow` does not apply to them.
- `incident.authority` is a single value. The clocks come from `data.regions`
  (BR: ANPD, 3 business days; an EU or EEA country: GDPR, 72 hours). When both
  apply, set `authority` to GDPR, the shorter clock, which governs the first
  notice; `docs/privacy.md` lists both. Regions outside BR and the EU/EEA
  get `incident.authority: none` and a line in `## Open decisions` asking
  which authority applies.
- `tq` validates the block on load and refuses unknown classes, a `path` and a
  `table` on the same entry, or a malformed retention.
- Changing the manifest changes its trust hash: tell the user to run
  `tq allow <workspace>` and wait for it before relying on the guard rules.
  Headless never changes it; the human applies the proposal and runs
  `tq allow <workspace>` themselves.

Mirror the `path` entries into `CLAUDE.md`'s `tq-project` block (a JSON
block) as `classified_paths`, a JSON array of the same globs; create the key when it is missing so the build gate's
definition of done can require `docs/privacy.md` to change with them. For each
`table:` entry, also add the schema or migration files that define that table
to `classified_paths`; when you cannot find them, ask (headless: write a
`NEEDS-DECISION` line in `## Open decisions`). Never add
`docs/privacy.md`, or anything under `docs/`, to `classified_paths`: a
privacy-record edit would then trigger the privacy review that asked for it.

## 4. Privacy record

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
mkdir -p "$root/docs"
[ -f "$root/docs/privacy.md" ] || cp "$_tqe/templates/process/privacy.md" "$root/docs/privacy.md"
```

Write or update `docs/privacy.md` from `templates/process/privacy.md`: the
data dictionary, one processing activity per purpose with its legal basis
(LGPD Art. 7 or 11; GDPR Art. 6 or 9), minimisation note, retention and
deletion job, access controls, encryption, processors and transfers, masked
logging; the DSAR path (LGPD 15 days, GDPR one month); the DPIA or RIPD
trigger; the incident clock for the authorities of `data.regions`. If any class
is `pii`, `sensitive`, `financial` or `children`, purpose, legal basis,
retention and the DSAR path must be filled before `/tentaqles:kickoff`
continues, and also write `docs/incident-playbook.md` from
`templates/process/incident-playbook.md` (skip it if it exists; update the
contact and clocks). Put every open question for the DPO or the user in the
`## Open decisions` section of `docs/privacy.md`, one `NEEDS-DECISION` line
each, and say so in the report.

## 5. Synthetic fixtures

Offer to write `scripts/make_fixtures.py`: Faker (`pt_BR` where CPF and
phone shapes matter) generating one file per classified table into
`tests/fixtures/`, deterministic seed, no network. Production rows into
fixtures are denied by `tq/pii-fixture-dump`.

## 6. Commit

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" branch --show-current
git -C "$root" status --short
```

On `main` or `master`, stop (see the top). Otherwise stage only the files this
run changed, named explicitly: `docs/privacy.md`, `docs/incident-playbook.md`
and `scripts/make_fixtures.py` if written, and `CLAUDE.md` if
`classified_paths` changed. Add `.tentaqles.yaml` only when the manifest lives
in this repository (interactive only; headless stages
`docs/privacy-manifest-proposal.yaml` instead).

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" add -- docs/privacy.md CLAUDE.md   # list only the files this run changed
git -C "$root" commit -m "docs(privacy): data classification and record of processing"
```

Report the classes, the paths and tables, the authority and clock, the open
`NEEDS-DECISION` items, and remind the user: `tq allow <workspace>`. Headless: the
reminder is the manifest decision itself (review the proposal, apply it, then
`tq allow <workspace>`).
