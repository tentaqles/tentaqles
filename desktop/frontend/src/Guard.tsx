import {useEffect, useState} from 'react'
import {errorText} from './api'
import * as guard from './guardview'
import {Button, Card, Select, StepHeader} from './ui'

function LockIcon({title}: {title: string}) {
  return (
    <svg viewBox="0 0 16 16" width="12" height="12" aria-label={title} role="img" className="inline-block">
      <title>{title}</title>
      <path
        fill="currentColor"
        d="M5 7V5a3 3 0 0 1 6 0v2h.5A1.5 1.5 0 0 1 13 8.5v5a1.5 1.5 0 0 1-1.5 1.5h-7A1.5 1.5 0 0 1 3 13.5v-5A1.5 1.5 0 0 1 4.5 7H5Zm1.5 0h3V5a1.5 1.5 0 0 0-3 0v2Z"
      />
    </svg>
  )
}

function Segmented<T extends string>({
  options,
  value,
  onChange,
  disabled,
  label,
}: {
  options: readonly T[]
  value: string
  onChange: (v: T) => void
  disabled?: boolean
  label: string
}) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex overflow-hidden rounded-md border border-[var(--tq-border)]">
      {options.map((o) => (
        <button
          key={o}
          role="radio"
          aria-checked={value === o}
          disabled={disabled}
          onClick={() => onChange(o)}
          className={`px-2.5 py-1 text-xs disabled:cursor-not-allowed ${
            value === o ? 'bg-[var(--tq-accent)]/15 font-semibold text-[var(--tq-accent)]' : 'hover:bg-black/5 dark:hover:bg-white/5'
          }`}
        >
          {o}
        </button>
      ))}
    </div>
  )
}

// Guard is the desktop Guard tab (spec E §10.2): the resolved mode of each
// workspace, a preset control, a four-way toggle per category (the locked
// groups greyed), the ceiling banner with the grant, and the 24h counts.
// Writes go through the same functions as `tq mode` and re-trust the manifest.
export default function Guard() {
  const [views, setViews] = useState<guard.GuardView[]>([])
  const [ws, setWs] = useState('')
  const [scope, setScope] = useState('workspace')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [confirmGrant, setConfirmGrant] = useState(false)
  const [draft, setDraft] = useState<guard.GuardView | null>(null)

  async function refresh(keep?: string) {
    setLoading(true)
    try {
      const vs = await guard.guardWorkspaces()
      setViews(vs)
      const name = keep && vs.some((v) => v.workspace === keep) ? keep : (vs[0]?.workspace ?? '')
      setWs(name)
      setDraft(null)
      setError('')
    } catch (e) {
      setError(errorText(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  const current = views.find((v) => v.workspace === ws)
  const view = draft ?? current

  async function write(optimistic: guard.GuardView, run: () => Promise<guard.GuardChange>) {
    setBusy(true)
    setDraft(optimistic)
    setNotice('')
    try {
      const ch = await run()
      setNotice(guard.changeNotice(ch))
      await refresh(ws)
    } catch (e) {
      setDraft(null)
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function grant() {
    if (!ws) return
    setBusy(true)
    try {
      const mode = await guard.grantMode(ws)
      setNotice(`granted ${mode} for ${ws}`)
      setConfirmGrant(false)
      await refresh(ws)
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function revoke() {
    if (!ws) return
    setBusy(true)
    try {
      await guard.revokeGrant(ws)
      setNotice(`grant removed for ${ws}`)
      await refresh(ws)
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  const banner = view ? guard.ceilingBanner(view) : null

  return (
    <div className="max-w-3xl">
      <StepHeader
        title="Guard"
        subtitle="What tq does with each rule that matches a tool call. identity, secrets, self-protection and privacy are locked: enforced in every mode. A mode looser than default needs a one-time grant."
      />
      {error ? (
        <Card className="mb-4">
          <p className="text-[#e0432f]">{error}</p>
        </Card>
      ) : null}
      {loading && !view ? (
        <Card>
          <p className="text-[var(--tq-muted)]">Reading the guard mode of each workspace…</p>
        </Card>
      ) : !view ? (
        <Card>
          <p className="text-[var(--tq-muted)]">No trusted workspace yet. Set one up in the Setup tab.</p>
        </Card>
      ) : (
        <div className="space-y-4">
          <Card>
            <div className="flex flex-wrap items-center gap-3">
              <label className="flex items-center gap-2 text-sm">
                <span className="text-[var(--tq-muted)]">Workspace</span>
                <Select value={ws} onChange={(e) => {
                    setWs(e.target.value)
                    setScope('workspace')
                    setDraft(null)
                    setConfirmGrant(false)
                  }}
                >
                  {views.map((v) => (
                    <option key={v.workspace} value={v.workspace}>
                      {v.workspace}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="flex items-center gap-2 text-sm">
                <span className="text-[var(--tq-muted)]">Layer</span>
                <Select value={scope} onChange={(e) => setScope(e.target.value)}>
                  {guard.scopeChoices(view).map((s) => (
                    <option key={s.value} value={s.value}>
                      {s.label}
                    </option>
                  ))}
                </Select>
              </label>
            </div>
            <div className="mt-4 flex flex-wrap items-center gap-3">
              <Segmented
                label="Preset"
                options={guard.MODES}
                value={view.mode}
                disabled={busy}
                onChange={(m) => {
                  const next = guard.applyPreset(view, m)
                  void write(next, () => guard.setMode(ws, scope, m, m === 'custom' ? (next.base ?? '') : ''))
                }}
              />
              <span className="text-sm">
                <b>{view.label}</b>
                {view.requested && view.requested !== view.mode ? (
                  <span className="text-[var(--tq-muted)]"> · {view.requested} requested</span>
                ) : null}
              </span>
            </div>
            <p className="mt-3 text-sm text-[var(--tq-muted)]">
              Ceiling: {guard.ceilingText(view)} · {guard.countsText(view)}
            </p>
            {notice ? <p className="mt-2 text-sm text-[var(--tq-accent)]">{notice}</p> : null}
          </Card>

          {banner ? (
            <Card className="border-[#f28c28]">
              <div className="flex flex-wrap items-center gap-3">
                <span className="text-sm text-[#f28c28]">{banner.text}</span>
                {confirmGrant ? (
                  <>
                    <span className="text-sm">
                      Grant {banner.grant} for {ws}? Its sessions then skip or lower the rules shown below.
                    </span>
                    <Button variant="primary" disabled={busy} onClick={() => void grant()}>
                      Confirm grant
                    </Button>
                    <Button disabled={busy} onClick={() => setConfirmGrant(false)}>
                      Cancel
                    </Button>
                  </>
                ) : (
                  <Button variant="primary" disabled={busy} onClick={() => setConfirmGrant(true)}>
                    Grant {banner.grant} for {ws}
                  </Button>
                )}
              </div>
            </Card>
          ) : view.granted ? (
            <div className="flex justify-end">
              <Button variant="danger" disabled={busy} onClick={() => void revoke()}>
                Remove the {view.ceiling} grant
              </Button>
            </div>
          ) : null}

          <Card>
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wide text-[var(--tq-muted)]">
                  <th className="pb-2">Category</th>
                  <th className="pb-2">Value</th>
                  <th className="pb-2">Set by</th>
                </tr>
              </thead>
              <tbody>
                {view.categories.map((c) => {
                  const locked = guard.isLocked(c)
                  return (
                    <tr key={c.name} className={locked ? 'opacity-50' : ''} title={locked ? guard.lockedTooltip(c.name) : undefined}>
                      <td className="py-1.5 pr-3">
                        {c.name} {locked ? <LockIcon title={guard.lockedTooltip(c.name)} /> : null}
                      </td>
                      <td className="py-1.5 pr-3">
                        <Segmented
                          label={c.name}
                          options={guard.VALUES}
                          value={c.value}
                          disabled={locked || busy}
                          onChange={(v) => void write(guard.applyToggle(view, c.name, v), () => guard.toggleCategory(ws, scope, c.name, v))}
                        />
                      </td>
                      <td className="py-1.5 text-[var(--tq-muted)]">{locked ? 'locked' : c.source}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
            <p className="mt-3 text-xs text-[var(--tq-muted)]">
              rule is the rule's own action, skip lets the call through, ask confirms, deny blocks.
            </p>
          </Card>

          {view.overrides.length > 0 ? (
            <Card>
              <h2 className="mb-2 text-sm font-semibold">Rule overrides</h2>
              <ul className="space-y-1 text-sm">
                {view.overrides.map((o) => (
                  <li key={o.id} className="flex items-center gap-2">
                    <code>{o.id}</code>
                    <span>{o.value}</span>
                    {o.source ? <span className="text-[var(--tq-muted)]">({o.source})</span> : null}
                    <Button
                      className="ml-auto text-xs"
                      disabled={busy}
                      onClick={() => void write(view, () => guard.setOverride(ws, scope, o.id, 'clear'))}
                    >
                      Clear
                    </Button>
                  </li>
                ))}
              </ul>
            </Card>
          ) : null}
        </div>
      )}
    </div>
  )
}
