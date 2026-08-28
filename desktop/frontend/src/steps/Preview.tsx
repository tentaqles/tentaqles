import {useEffect, useState} from 'react'
import * as api from '../api'
import {usePlan} from '../state'
import {Button, Card, StepHeader} from '../ui'
import {summarizeChanges} from '../tools'

export default function Preview() {
  const {state, dispatch} = usePlan()
  const [changes, setChanges] = useState<api.Change[]>([])
  const [hooks, setHooks] = useState<api.HookStatus[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [applying, setApplying] = useState(false)

  async function refresh(cancelled?: () => boolean) {
    setLoading(true)
    try {
      const c = (await api.preview(state.plan)) ?? []
      if (!cancelled?.()) {
        setChanges(c)
        setError('')
      }
    } catch (e) {
      if (!cancelled?.()) setError(api.errorText(e))
    } finally {
      if (!cancelled?.()) setLoading(false)
    }
    try {
      const h = (await api.hooksStatus()) ?? []
      if (!cancelled?.()) setHooks(h)
    } catch {
      if (!cancelled?.()) setHooks([])
    }
  }

  useEffect(() => {
    let cancelled = false
    void refresh(() => cancelled)
    // eslint-disable-next-line react-hooks/exhaustive-deps
    return () => {
      cancelled = true
    }
  }, [])

  async function runApply() {
    setApplying(true)
    try {
      const report = await api.apply(state.plan)
      dispatch({type: 'setReport', report})
      setError('')
      setConfirming(false)
      dispatch({type: 'next'})
    } catch (e) {
      setError(api.errorText(e))
      setConfirming(false)
    } finally {
      setApplying(false)
    }
  }

  // Escape closes the confirm dialog, but never while an apply is in flight.
  useEffect(() => {
    if (!confirming) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && !applying) setConfirming(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [confirming, applying])

  const summary = summarizeChanges(changes)

  return (
    <div className="max-w-3xl">
      <StepHeader
        title="Preview"
        subtitle="Everything tq would write. Nothing has been changed yet."
      />

      {error ? (
        <Card className="mb-4">
          <p className="text-sm text-[#e0432f]">{error}</p>
        </Card>
      ) : null}

      <Card className="mb-4">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="font-semibold">Changes</h2>
          <span className="text-xs text-[var(--tq-muted)]">
            {summary.map((s) => `${s.count} ${s.kind}`).join(' · ') || (loading ? 'Loading…' : 'none')}
          </span>
        </div>
        {changes.length === 0 ? (
          <p className="text-sm text-[var(--tq-muted)]">
            {loading ? 'Building the preview…' : 'Nothing to do.'}
          </p>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wide text-[var(--tq-muted)]">
              <tr>
                <th className="py-1 pr-3">Kind</th>
                <th className="py-1 pr-3">Target</th>
                <th className="py-1 pr-3">Detail</th>
              </tr>
            </thead>
            <tbody>
              {changes.map((c, i) => (
                <tr key={i} className="border-t border-[var(--tq-border)] align-top">
                  <td className="py-1.5 pr-3">{c.Kind}</td>
                  <td className="py-1.5 pr-3 break-all">{c.Target}</td>
                  <td className="py-1.5 pr-3 text-[var(--tq-muted)]">{c.Detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      <Card className="mb-4">
        <h2 className="mb-2 font-semibold">Shell hooks</h2>
        {hooks.length === 0 ? (
          <p className="text-sm text-[var(--tq-muted)]">No shells detected.</p>
        ) : (
          <ul className="space-y-1 text-sm">
            {hooks.map((h) => (
              <li key={h.Shell + h.Profile}>
                <span className="font-medium">{h.Shell}</span>
                <span className="ml-2 text-[var(--tq-muted)]">{h.State}</span>
                <span className="ml-2 break-all text-xs text-[var(--tq-muted)]">{h.Profile}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <div className="flex gap-2">
        <Button onClick={() => void refresh()} disabled={loading || applying}>
          Refresh
        </Button>
        <Button variant="primary" onClick={() => setConfirming(true)} disabled={applying}>
          Apply
        </Button>
      </div>

      {confirming ? (
        <div className="fixed inset-0 z-10 flex items-center justify-center bg-black/40 p-6">
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="apply-dialog-title"
            className="w-full max-w-md rounded-lg border border-[var(--tq-border)] bg-[var(--tq-panel)] p-5"
          >
            <h3 id="apply-dialog-title" className="text-lg font-semibold">
              Apply this plan?
            </h3>
            <p className="mt-2 text-sm text-[var(--tq-muted)]">
              tq will write to {state.plan.Base} and to your shell profiles.
            </p>
            <ul className="mt-3 space-y-1 text-sm">
              {summary.length === 0 ? (
                <li className="text-[var(--tq-muted)]">No changes.</li>
              ) : (
                summary.map((s) => (
                  <li key={s.kind}>
                    {s.count} × {s.kind}
                  </li>
                ))
              )}
            </ul>
            <div className="mt-5 flex justify-end gap-2">
              <Button onClick={() => setConfirming(false)} disabled={applying}>
                Cancel
              </Button>
              <Button variant="primary" onClick={() => void runApply()} disabled={applying}>
                {applying ? 'Applying…' : 'Apply'}
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
