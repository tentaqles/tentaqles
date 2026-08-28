import {useEffect, useState} from 'react'
import * as api from '../api'
import {usePlan} from '../state'
import {Button, Card, StepHeader} from '../ui'
import {countMissing, hintURL, isCommandHint, toolStatus, type ToolStatus} from '../tools'

const pill: Record<ToolStatus, string> = {
  ok: 'border-[#2fa84f] text-[#2fa84f]',
  missing: 'border-[#e0432f] text-[#e0432f]',
  'n/a': 'border-[var(--tq-border)] text-[var(--tq-muted)]',
}

function StatusPill({status}: {status: ToolStatus}) {
  return (
    <span className={`rounded-full border px-2 py-0.5 text-[10px] uppercase ${pill[status]}`}>
      {status}
    </span>
  )
}

function Hints({hints}: {hints: string[]}) {
  if (hints.length === 0) return <span className="text-[var(--tq-muted)]">—</span>
  return (
    <ul className="space-y-1">
      {hints.map((h, i) => {
        const url = hintURL(h)
        if (url) {
          return (
            <li key={i}>
              <button
                onClick={() => api.openURL(url)}
                className="text-[var(--tq-accent)] underline underline-offset-2"
              >
                {url}
              </button>
            </li>
          )
        }
        return (
          <li key={i} className="flex items-center gap-2">
            <code className="text-xs">{h}</code>
            {isCommandHint(h) ? (
              <Button className="!px-2 !py-0.5 text-xs" onClick={() => void api.openTerminal(h)}>
                Install
              </Button>
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}

export default function Tools() {
  const {state} = usePlan()
  const [results, setResults] = useState<Record<string, api.ToolResult[]>>({})
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function recheck() {
    setLoading(true)
    try {
      setResults((await api.toolCheck(state.plan)) ?? {})
      setError('')
    } catch (e) {
      setError(api.errorText(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void recheck()
    // Probing once per visit is enough; "Re-check" re-runs on demand.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const missing = countMissing(results)
  const companies = state.plan.Companies

  return (
    <div className="max-w-3xl">
      <StepHeader
        title="Tools"
        subtitle="tq checks whether each service's CLI is installed. Missing tools don't block setup — you can install them later."
      />

      <div className="mb-4 flex items-center gap-3">
        <Button onClick={() => void recheck()} disabled={loading}>
          {loading ? 'Checking…' : 'Re-check'}
        </Button>
        {missing > 0 ? (
          <span className="text-sm text-[#f28c28]">
            {missing} tool{missing === 1 ? '' : 's'} missing — setup can continue.
          </span>
        ) : null}
      </div>

      {error ? (
        <Card className="mb-4">
          <p className="text-sm text-[#e0432f]">{error}</p>
        </Card>
      ) : null}

      {companies.length === 0 ? (
        <Card>
          <p className="text-[var(--tq-muted)]">No companies to check yet.</p>
        </Card>
      ) : null}

      <div className="space-y-4">
        {companies.map((c) => {
          const rows = results[c.Name] ?? []
          return (
            <Card key={c.Name}>
              <h2 className="mb-3 font-semibold">{c.DisplayName || c.Name}</h2>
              {rows.length === 0 ? (
                <p className="text-sm text-[var(--tq-muted)]">
                  {loading ? 'Checking…' : 'No services selected for this company.'}
                </p>
              ) : (
                <table className="w-full text-left text-sm">
                  <thead className="text-xs uppercase tracking-wide text-[var(--tq-muted)]">
                    <tr>
                      <th className="py-1 pr-3">Service</th>
                      <th className="py-1 pr-3">Status</th>
                      <th className="py-1 pr-3">Version</th>
                      <th className="py-1 pr-3">Path / how to install</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((r) => {
                      const status = toolStatus(r)
                      return (
                        <tr key={r.ID} className="border-t border-[var(--tq-border)] align-top">
                          <td className="py-2 pr-3">{r.ID}</td>
                          <td className="py-2 pr-3">
                            <StatusPill status={status} />
                          </td>
                          <td className="py-2 pr-3 text-[var(--tq-muted)]">{r.Version || '—'}</td>
                          <td className="py-2 pr-3">
                            {status === 'missing' ? (
                              <Hints hints={r.Hints ?? []} />
                            ) : (
                              <span className="break-all text-[var(--tq-muted)]">
                                {r.Path || r.Err || '—'}
                              </span>
                            )}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              )}
            </Card>
          )
        })}
      </div>
    </div>
  )
}
