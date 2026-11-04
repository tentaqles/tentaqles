---
name: classify-data
description: "Classify the data a project handles and record how it is protected. Builds the data dictionary from the PRD and the schema, writes docs/privacy.md with the classes, where each lives (repository paths and database tables), the regions of the data subjects, purpose, legal basis, retention, the incident authority and contact, and the DSAR path, and offers a Faker-based synthetic fixture generator. Use during /tentaqles:kickoff, when the user asks \"what data do we handle\", \"classify the data\", \"privacy record\", or when a schema change adds PII-looking columns."
argument-hint: '[update]'
---

# Classify data

Personal data may be read, queried and processed locally; the care is at the
exits: commits, pushes, PR and issue text, memory, fixtures, and model calls.
`docs/privacy.md` is the record of what the project handles and how: the
privacy reviewer reads it, the definition of done requires it to change with
the data, and the spec slice names its classes. None of that works on a
record without paths or tables, so this skill never writes one. Nothing here
stops you from opening a data file to classify it.

Never commit on main or master: if `git branch --show-current` says `main` or
`master`, stop and ask for a branch first.

## 1. Inputs

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
ls "$root/docs/privacy.md" 2>/dev/null || echo "no privacy record yet"
echo "dictionary=$_tqe/skills/classify-data/references/data-dictionary.md"
echo "privacy_template=$_tqe/templates/process/privacy.md"
echo "playbook_template=$_tqe/templates/process/incident-playbook.md"
```

An existing record means `update`: re-read the PRD and the schema and revise
it; a missing one is written from the template.

## 2. Data dictionary

Read PRD §5 and §2, the schema (SQL, Prisma, migrations, models), exports and
log statements. Grep the column-name signals from
`references/data-dictionary.md`. Produce the table: data, class (`public`,
`internal`, `pii`, `sensitive`, `financial`, `children`, `secrets`), source,
store (repository path or `schema.table`), retention, deletion job. Ask the
user one question at a time only where the class is unclear (is the phone a
business line or a person's?) or where retention is a business decision; offer
a recommended default and record the answer. Also ask, one question each:

- "Which countries are the data subjects in?" This fills the regions line of
  the record. Default: infer from the PRD's users and markets and log the
  assumption under `## Open decisions`.
- "Who receives an incident notice (name or role, and the channel)?" This
  fills the owner line. Never invent a contact: without an answer, write a
  `NEEDS-DECISION` line under `## Open decisions` and leave the owner blank.

## 3. Where the data lives

Every declared class has **at least one** `path` or `table` in the data
dictionary's Store column (a repository path such as `db/schema/customers.sql`
or `data/exports/**`, or a table such as `public.payments`). If none can be
found for a class, ask for it; do not write the class without one. For each
table, also list the schema or migration files that define it; when you cannot
find them, ask. Never list `docs/privacy.md` itself, or anything under
`docs/`, as a store: a privacy-record edit would then look like a data change.

The incident authority is a single value. The clocks come from the regions
(BR: ANPD, 3 business days; an EU or EEA country: GDPR, 72 hours). When both
apply, set the authority to GDPR, the shorter clock, which governs the first
notice; the record lists both. Regions outside BR and the EU/EEA get
`authority: none` and a line in `## Open decisions` asking which authority
applies. The contact is an address or a role, never a credential.

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
trigger; the incident clock for the authorities of the regions. If any class
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
`tests/fixtures/`, deterministic seed, no network. Production rows never go
into fixtures, seeds or tests.

## 6. Commit

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" branch --show-current
git -C "$root" status --short
```

On `main` or `master`, stop (see the top). Otherwise stage only the files this
run changed, named explicitly: `docs/privacy.md`, `docs/incident-playbook.md`
and `scripts/make_fixtures.py` if written.

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" add -- docs/privacy.md   # list only the files this run changed
git -C "$root" commit -m "docs(privacy): data classification and record of processing"
```

Report the classes, the paths and tables, the authority and clock, and the
open `NEEDS-DECISION` items.
