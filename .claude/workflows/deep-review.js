export const meta = {
  name: 'deep-review',
  description: 'Read-only audit of a branch diff: scan by lens twice, dedupe in code, skeptics vote',
  whenToUse: 'Whole-diff review of a High-risk ticket before landing, or any branch worth a deep look.',
  phases: [
    { title: 'Scope', detail: 'list the changed files' },
    { title: 'Scan', detail: 'each lens reads every file batch, two rounds' },
    { title: 'Verify', detail: 'skeptics try to refute each finding' },
  ],
}

// A fixed-script, read-only audit of a branch. The script is the plan, so you can read it in
// advance and its cost doesn't depend on what a model decides to do. Args: a base ref as a
// string, or { base, dir, skeptics }. dir is the worktree to audit: a ticket's branch isn't the
// session's checkout, so pass it for a ticket. skeptics: 1 by default; pass 3 on High risk
// (2 of 3 must agree). It can't ask you anything mid-run. It returns confirmed findings in the
// review loop's format; the router hands them to the ticket-lead as a fix round.
const opts = typeof args === 'string' ? { base: args } : (args ?? {})
const BATCH = 15
const SKEPTICS = opts.skeptics === 3 ? 3 : 1
const NEEDED = SKEPTICS === 3 ? 2 : 1
const WHERE = opts.dir ? `Everything you audit is in the worktree ${opts.dir}: read files under that path, and run git as \`git -C "${opts.dir}" ...\`. ` : ''
const RULES = `${WHERE}Read CLAUDE.md and REVIEW.md first; REVIEW.md defines severity, what to ignore and "no rule, no finding". You are read-only: use Read, Grep, Glob and read-only git only.`

const FINDINGS = {
  type: 'object',
  required: ['findings'],
  properties: {
    filesRead: { type: 'array', items: { type: 'string' } },
    findings: {
      type: 'array',
      items: {
        type: 'object',
        required: ['file', 'line', 'severity', 'class', 'title', 'rule', 'why'],
        properties: {
          file: { type: 'string' }, line: { type: 'number' },
          severity: { enum: ['P0', 'P1', 'P2', 'P3'] },
          class: { type: 'string', description: 'kebab-case kind of mistake' },
          title: { type: 'string' }, rule: { type: 'string', description: 'the rule or requirement it breaks' },
          why: { type: 'string', description: 'concrete input and wrong result' },
        },
      },
    },
  },
}
const VERDICT = {
  type: 'object', required: ['real', 'reason'],
  properties: { real: { type: 'boolean' }, reason: { type: 'string' } },
}
const SCOPE = {
  type: 'object', required: ['base', 'files'],
  properties: { base: { type: 'string' }, files: { type: 'array', items: { type: 'string' } } },
}

const LENSES = [
  ['correctness', 'Logic that gives a wrong result, on the inputs this code will really meet. Trace the actual path; don\'t pattern-match.'],
  ['failure-paths', 'What happens when a step fails: swallowed errors, partial writes, wrong exit codes, a hang, cleanup that is skipped.'],
  ['tests-and-priorities', 'The project\'s non-negotiables in CLAUDE.md and REVIEW.md, and whether each test really tests what it claims. A green test that can\'t fail is a finding.'],
]

phase('Scope')
const scope = await agent(
  `${WHERE}Find the merge base and list changed files. Base ref: ${opts.base ?? 'the mainBranch in .claude/project.json'}. Use git merge-base and git diff --name-only <merge-base>...HEAD. Return the merge-base sha and every changed file except generated files and lockfiles.`,
  { label: 'scope', phase: 'Scope', schema: SCOPE, effort: 'low' },
)
// "Nothing to review" must never read as "clean": it usually means the wrong checkout.
if (!scope?.files?.length) return { confirmed: [], clean: false, note: `No changed files against ${scope?.base ?? 'the base'}${opts.dir ? ` in ${opts.dir}` : ' in this checkout: a ticket needs its worktree passed as dir'}. Nothing was audited.` }

const batches = []
for (let i = 0; i < scope.files.length; i += BATCH) batches.push(scope.files.slice(i, i + BATCH))
log(`${scope.files.length} files in ${batches.length} batches, ${LENSES.length} lenses, 2 scan rounds, ${SKEPTICS} skeptic(s) per finding`)

// One place, one finding. Plain code, so the merge is deterministic.
const key = (f) => `${f.file.replace(/\\/g, '/')}#${Math.round(f.line / 5)}`
const seen = new Map()
const reached = new Set()

const scan = async (round) => {
  const known = [...seen.values()].map((f) => `${f.file}:${f.line} ${f.title}`).join('\n') || 'none'
  const jobs = []
  for (const [lens, brief] of LENSES) {
    batches.forEach((files, b) => jobs.push(() => agent(
      `${RULES}\n\nReview the diff of these files against base ${scope.base}, through one lens: ${brief}\nRead each whole file for context, and report only problems the change introduced or makes reachable. Every finding needs a file, a line, the rule it breaks, and a concrete input with its wrong result. If you find nothing, return an empty list: that's a fine answer.\n\nFiles:\n${files.join('\n')}\n\nAlready reported, don't repeat:\n${known}\n\nReturn filesRead with the files you actually read.`,
      { label: `${lens} r${round} b${b + 1}`, phase: 'Scan', schema: FINDINGS },
    )))
  }
  const fresh = []
  for (const r of (await parallel(jobs)).filter(Boolean)) {
    for (const f of r.filesRead ?? []) reached.add(f.replace(/\\/g, '/'))
    for (const f of r.findings ?? []) {
      const k = key(f)
      if (!seen.has(k)) { seen.set(k, f); fresh.push(f) }
    }
  }
  log(`round ${round}: ${fresh.length} new findings`)
  return fresh
}

const verify = (findings) => parallel(findings.map((f) => async () => {
  const votes = (await parallel(Array.from({ length: SKEPTICS }, (_, i) => () => agent(
    `${RULES}\n\nMost reports are false alarms. Try to refute this one by reading the code at ${f.file}:${f.line} on this branch (base ${scope.base}). Answer real: false if it's wrong, unreachable, already handled, not caused by this change, on REVIEW.md's ignore list, or if you aren't sure.\n\n${f.severity} ${f.class}: ${f.title}\nRule: ${f.rule}\nClaim: ${f.why}`,
    { label: `skeptic ${f.file}:${f.line}${SKEPTICS > 1 ? ` #${i + 1}` : ''}`, phase: 'Verify', schema: VERDICT },
  )))).filter(Boolean)
  const yes = votes.filter((v) => v.real).length
  return { ...f, confirmed: yes >= NEEDED, votes: votes.map((v) => `${v.real ? 'real' : 'refuted'}: ${v.reason}`) }
}))

phase('Scan')
const first = await scan(1)
// A second round finds what the first missed, knowing what's already been reported.
const second = await scan(2)

phase('Verify')
const judged = (await verify([...first, ...second])).filter(Boolean)
const confirmed = judged.filter((f) => f.confirmed)
const refuted = judged.filter((f) => !f.confirmed).map((f) => `${f.file}:${f.line} ${f.title}`)

// No silent caps: say which files no agent reports having read.
const notReached = scope.files.filter((f) => !reached.has(f.replace(/\\/g, '/')))
if (notReached.length) log(`Not reached by any agent: ${notReached.join(', ')}`)

return { base: scope.base, confirmed, refuted, notReached, order: 'P0 first', skeptics: SKEPTICS }
