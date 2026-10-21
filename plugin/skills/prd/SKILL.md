---
name: prd
description: Write or revise a product requirements document (docs/prd.md) through a short interview, from an idea, a brief, a ticket or an existing codebase. Captures problem, users, goals and non-goals, numbered functional requirements with binary acceptance checks, data classes, security boundaries, NFRs with numbers, stack constraints and open questions tagged NEEDS-DECISION, then grades planning-readiness mechanically. Use when the user says "write a PRD", "start a PRD", "requirements doc", "spec the product", "reverse-engineer the requirements", or when /tentaqles:kickoff finds no planning-ready PRD.
argument-hint: '<idea | brief path | ticket URL | "from existing code"> [--headless]'
---

# PRD

What and why, in a shape machines can check. Every later artifact cites a
section number from this document, so section numbers never move.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. The prd skill has no build run
of its own, so there is no slug to write a decision record under: at a human
stop (an interview question you cannot answer from the brief or the code, an
open `NEEDS-DECISION`) report what it needs as its final output and end the turn. Fill what the inputs support, write the draft, run the grader, and list
the unanswered questions in the report. Use
`build-gate ask <slug> --kind question --text "<the question>" --options <a>,<b>`
only when the caller passed the slug of an active build run; the `build-gate`
prefix is in `skills/build/SKILL.md`.

## 0. Where it lives

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
echo "template=$_tqe/templates/process/prd.md"
```

`prd_exit=4`: no PRD, start from `templates/process/prd.md`. `0` or `3`: you
are revising; read `docs/prd.md` first and keep its numbering.

## 1. Pick the variant

- **From an idea** (one or two sentences): the full interview below.
- **From a brief or ticket** (a file path or URL in `$ARGUMENTS`): read it,
  fill every section you can with a citation to the brief, interview only
  the gaps.
- **From existing code** (`from existing code`, or a legacy repo with no PRD):
  read `README`, `CLAUDE.md`, routes or entry points, the schema and the tests;
  write sections 1 to 9 as what the code does today, each FR citing the file
  that implements it; interview only section 1 (why), section 3 (goals) and
  section 10 (what is unclear). Mark the document
  `Status: draft (reverse-engineered)`.

## 2. Interview

Ask one question per message, 8 to 12 in total, multiple choice with a
recommended default where you can. The user may answer "you decide": record
your choice and the reason in the section, and add it to section 10 only if
the choice has a cost they should see. Cover, in this order:

1. The problem and the evidence it is real (section 1).
2. Who uses it and in which roles; who must never (section 2).
3. Three to five measurable goals; what is explicitly out (sections 3, 3.2).
4. The three most important things a user does (section 4 candidates).
5. For each: how would we know it works? Push until the answer is binary and
   observable ("the export has one row per order" not "exports work").
6. What personal, financial or sensitive data it touches; retention (section 5).
7. Who talks to what; how people log in; what a malicious user would try
   (section 6).
8. Numbers: users, requests, data volume, latency, availability, cost ceiling,
   platforms (section 7).
9. Stack constraints and why (section 9). Never assume a default stack.
10. What they are unsure about (section 10).

Stop early when every section has an answer or an owner.

## 3. Write `docs/prd.md`

```bash
mkdir -p "$root/docs"; [ -f "$root/docs/prd.md" ] || cp "$_tqe/templates/process/prd.md" "$root/docs/prd.md"
```

Fill it. The grader reads exactly this, so:

- Headings stay exactly `## <n>. <title>`; section 4 holds the FRs as
  `### FR-nn <title>`, each with a `**Acceptance checks**` heading followed by
  list items, each a binary statement. An FR with no list item under that
  heading counts as having no checks.
- Open items are list lines starting with `NEEDS-DECISION:` under section 10,
  each with how to settle it (ask <who>, research, prototype). Delete the
  template's placeholder line; decided items leave section 10 and their answer
  moves into the section it belongs to.
- No scores, no adjectives in acceptance checks. "Fast" becomes a number in
  section 7.
- Section 11 names which FRs are release 1.

## 4. Grade it

```bash
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
```

Note: the grader's exit code decides; you never declare a PRD planning-ready on your
own judgement. Quote its JSON output in your reply.

- `0` (planning-ready): set `Status: planning-ready` in the header and say so.
- `3` (not ready): list what blocks it from the JSON (`frs_without_checks`,
  `open_decisions`, `has_section_10`, `frs`). Never write "planning-ready"
  while the exit code is 3. Offer to settle each open item (ask, research,
  prototype), edit the PRD, and grade again. Headless: report the blockers as
  your final output and end the turn.
- `4` (no PRD): the write step did not produce `docs/prd.md`; fix that.

## 5. Commit

```bash
git -C "$root" add docs/prd.md
git -C "$root" commit -m "docs(prd): <draft|planning-ready> product requirements"
```

Use `draft` unless the last grade was exit 0. Record the decisions the
interview produced with `/tentaqles:session-wrap` so the next session knows why
each requirement exists.
