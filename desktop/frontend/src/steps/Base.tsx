import {useEffect, useState} from 'react'
import * as api from '../api'
import {Button, Card, Field, Input, StepHeader} from '../ui'
import {usePlan} from '../state'

export default function Base() {
  const {state, dispatch} = usePlan()
  const [workspaces, setWorkspaces] = useState<api.Workspace[]>([])
  const [pickError, setPickError] = useState('')

  useEffect(() => {
    let live = true
    api
      .existingWorkspaces()
      .then((ws) => live && setWorkspaces(ws ?? []))
      .catch(() => live && setWorkspaces([]))
    return () => {
      live = false
    }
  }, [])

  const base = state.plan.Base
  const norm = (s: string) =>
    s.split('\\').join('/').replace(/\/+$/, '').toLowerCase()
  const under = base
    ? workspaces.filter((w) => norm(w.Root).startsWith(norm(base) + '/'))
    : []

  async function pick() {
    setPickError('')
    try {
      const dir = await api.pickFolder()
      if (dir) dispatch({type: 'setBase', base: dir})
    } catch (e) {
      setPickError(api.errorText(e))
    }
  }

  return (
    <div className="max-w-2xl">
      <StepHeader
        title="Work folder"
        subtitle="Every company gets a folder under this base. tq watches it to decide which identity is active."
      />
      <Card>
        <Field label="Base folder" error={pickError}>
          <div className="flex gap-2">
            <Input
              className="flex-1 font-mono"
              value={base}
              spellCheck={false}
              onChange={(e) => dispatch({type: 'setBase', base: e.target.value})}
            />
            <Button onClick={pick}>Pick folder…</Button>
          </div>
        </Field>
      </Card>

      {under.length > 0 ? (
        <Card className="mt-4">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-[var(--tq-muted)]">
            Workspaces already here
          </h2>
          <table className="mt-3 w-full text-left">
            <thead className="text-xs uppercase text-[var(--tq-muted)]">
              <tr>
                <th className="pb-1">Name</th>
                <th className="pb-1">Email</th>
                <th className="pb-1">Trusted</th>
              </tr>
            </thead>
            <tbody>
              {under.map((w) => (
                <tr key={w.Root} className="border-t border-[var(--tq-border)]">
                  <td className="py-1.5">{w.Name}</td>
                  <td className="py-1.5 font-mono text-sm">{w.Email}</td>
                  <td className="py-1.5">{w.Trusted ? 'yes' : 'no'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      ) : null}
    </div>
  )
}
