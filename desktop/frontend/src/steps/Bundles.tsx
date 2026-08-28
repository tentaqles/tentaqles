import * as api from '../api'
import {usePlan} from '../state'
import {Card, StepHeader} from '../ui'

const DOCS = 'https://github.com/tentaqles/tentaqles#bundles'

const COMMANDS: {cmd: string; what: string}[] = [
  {cmd: 'tq bundle capture <company>', what: 'snapshot the plugins/skills a company currently uses'},
  {cmd: 'tq bundle sync <company>', what: 'apply that snapshot to the company workspace'},
  {cmd: 'tq bundle diff <company>', what: 'show what drifted since the last capture'},
]

export default function Bundles() {
  const {state} = usePlan()

  return (
    <div className="max-w-3xl">
      <StepHeader
        title="Bundles"
        subtitle="Claude Code plugins and skills, kept per company."
      />

      <Card className="mb-4">
        <p className="text-sm">
          Each company can carry its own bundle — the set of Claude Code plugins and skills that
          workspace uses. Setup doesn't write bundles: you capture them from a workspace once it
          looks right, then sync that bundle whenever you set the company up again.
        </p>
        <p className="mt-3 text-sm text-[var(--tq-muted)]">
          Run these from a terminal after setup finishes:
        </p>
        <ul className="mt-2 space-y-2">
          {COMMANDS.map((c) => (
            <li key={c.cmd} className="text-sm">
              <code className="rounded bg-black/5 px-1.5 py-0.5 dark:bg-white/10">{c.cmd}</code>
              <span className="ml-2 text-[var(--tq-muted)]">— {c.what}</span>
            </li>
          ))}
        </ul>
        <p className="mt-4 text-sm">
          <button
            onClick={() => api.openURL(DOCS)}
            className="text-[var(--tq-accent)] underline underline-offset-2"
          >
            Read the bundle docs
          </button>
        </p>
      </Card>

      {state.plan.Companies.length > 0 ? (
        <Card>
          <h2 className="mb-2 text-sm font-semibold">Companies in this plan</h2>
          <ul className="space-y-1 text-sm text-[var(--tq-muted)]">
            {state.plan.Companies.map((c) => (
              <li key={c.Name}>
                <code>tq bundle capture {c.Name}</code>
              </li>
            ))}
          </ul>
        </Card>
      ) : null}
    </div>
  )
}
