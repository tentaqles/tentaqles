import {useState} from 'react'
import * as api from '../api'
import {usePlan} from '../state'
import {Button, Card, StepHeader} from '../ui'

const levelTone: Record<string, string> = {
  ok: 'border-[#2fa84f] text-[#2fa84f]',
  warn: 'border-[#f28c28] text-[#f28c28]',
  error: 'border-[#e0432f] text-[#e0432f]',
}

export default function Logins() {
  const {state} = usePlan()
  const report = state.report
  const [findings, setFindings] = useState<api.Finding[] | null>(null)
  const [error, setError] = useState('')
  const [running, setRunning] = useState(false)
  const [done, setDone] = useState(false)

  async function runDoctor() {
    setRunning(true)
    try {
      setFindings((await api.doctor()) ?? [])
      setError('')
    } catch (e) {
      setError(api.errorText(e))
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="max-w-3xl">
      <StepHeader
        title="Log in"
        subtitle="Setup is applied. Sign each company into its services, then check everything with doctor."
      />

      <p className="mb-4 text-sm text-[var(--tq-muted)]">
        Note: the new terminal must have <code>tq</code> on PATH (open a fresh terminal after installing).
      </p>

      {report?.Warnings?.length ? (
        <Card className="mb-4">
          <h2 className="mb-2 text-sm font-semibold text-[#f28c28]">Warnings</h2>
          <ul className="space-y-1 text-sm text-[#f28c28]">
            {report.Warnings.map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>
        </Card>
      ) : null}

      <Card className="mb-4">
        <h2 className="mb-3 font-semibold">Logins</h2>
        {!report ? (
          <p className="text-sm text-[var(--tq-muted)]">
            Apply the plan on the Preview step to get the login commands.
          </p>
        ) : (report.Logins ?? []).length === 0 ? (
          <p className="text-sm text-[var(--tq-muted)]">
            No services need an interactive login.
          </p>
        ) : (
          <ul className="space-y-2">
            {report.Logins.map((cmd) => (
              <li key={cmd} className="flex items-center justify-between gap-3">
                <code className="break-all text-sm">{cmd}</code>
                <Button className="shrink-0" onClick={() => void api.openTerminal(cmd)}>
                  Open terminal
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <Card className="mb-4">
        <div className="mb-3 flex items-center gap-3">
          <h2 className="font-semibold">Doctor</h2>
          <Button onClick={() => void runDoctor()} disabled={running}>
            {running ? 'Running…' : 'Run doctor'}
          </Button>
        </div>
        {error ? <p className="text-sm text-[#e0432f]">{error}</p> : null}
        {findings === null ? (
          <p className="text-sm text-[var(--tq-muted)]">
            Doctor checks each workspace's identity, git config, and trust state.
          </p>
        ) : findings.length === 0 ? (
          <p className="text-sm text-[#2fa84f]">No problems found.</p>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wide text-[var(--tq-muted)]">
              <tr>
                <th className="py-1 pr-3">Level</th>
                <th className="py-1 pr-3">Workspace</th>
                <th className="py-1 pr-3">Message</th>
                <th className="py-1 pr-3">Fix</th>
              </tr>
            </thead>
            <tbody>
              {findings.map((f, i) => (
                <tr key={i} className="border-t border-[var(--tq-border)] align-top">
                  <td className="py-1.5 pr-3">
                    <span
                      className={`rounded-full border px-2 py-0.5 text-[10px] uppercase ${
                        levelTone[f.Level] ?? levelTone.warn
                      }`}
                    >
                      {f.Level}
                    </span>
                  </td>
                  <td className="py-1.5 pr-3">{f.Workspace || '—'}</td>
                  <td className="py-1.5 pr-3">{f.Msg}</td>
                  <td className="py-1.5 pr-3 text-[var(--tq-muted)]">{f.Fix || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      <div className="flex items-center gap-3">
        <Button variant="primary" onClick={() => setDone(true)}>
          Finish
        </Button>
        {done ? (
          <span className="text-sm text-[#2fa84f]">
            All done. Open a new terminal to activate the hooks.
          </span>
        ) : null}
      </div>
    </div>
  )
}
