import {useEffect, useState} from 'react'
import * as api from '../api'
import type {Company, FolderCandidate} from '../api'
import {Badge, Button, Card, ColorSwatches, Field, Input, Select, StepHeader} from '../ui'
import {newCompany, usePlan} from '../state'
import {COLORS, PERMISSION_MODES, validateCompany} from '../validate'

export default function Companies() {
  const {state, dispatch} = usePlan()
  const [form, setForm] = useState<Company | null>(null)
  const [editing, setEditing] = useState<string | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [confirming, setConfirming] = useState<string | null>(null)
  const [found, setFound] = useState<FolderCandidate[]>([])

  const companies = state.plan.Companies

  // Most people arrive with the folders already made -- personal, one per
  // client, each full of repositories -- and tq can adopt a folder as it
  // stands. Offering them beats asking for names that have to match the
  // directories character for character.
  useEffect(() => {
    let cancelled = false
    api
      .baseFolders(state.plan.Base)
      .then((f) => {
        if (!cancelled) setFound(f ?? [])
      })
      .catch(() => {
        if (!cancelled) setFound([])
      })
    return () => {
      cancelled = true
    }
  }, [state.plan.Base])

  // Adopt a folder: prefill from what its repositories already use, so the
  // identity carries over instead of being retyped.
  function adopt(f: FolderCandidate) {
    setEditing(null)
    setErrors({})
    setForm({
      ...newCompany(),
      Name: f.name,
      GitName: f.gitName,
      GitEmail: f.gitEmail,
    })
  }

  const takenNames = companies.map((c) => c.Name)
  const adoptable = found.filter((f) => !f.managed && !takenNames.includes(f.name))
  const alreadyManaged = found.filter((f) => f.managed)

  function startAdd() {
    setEditing(null)
    setErrors({})
    setForm(newCompany())
  }

  function startEdit(c: Company) {
    setEditing(c.Name)
    setErrors({})
    setForm({...c})
  }

  function save() {
    if (!form) return
    const taken = companies.filter((c) => c.Name !== editing).map((c) => c.Name)
    const errs = validateCompany(form, taken)
    setErrors(errs)
    if (Object.keys(errs).length > 0) return
    if (editing) dispatch({type: 'updateCompany', name: editing, company: form})
    else dispatch({type: 'addCompany', company: form})
    setForm(null)
    setEditing(null)
  }

  return (
    <div className="max-w-3xl">
      <StepHeader
        title="Companies"
        subtitle="One entry per client or employer. Each gets its own folder, git identity, and agent permission mode."
      />

      {adoptable.length > 0 || alreadyManaged.length > 0 ? (
        <Card>
          <div className="mb-2 text-xs uppercase text-[var(--tq-muted)]">
            Already in {state.plan.Base}
          </div>
          {adoptable.length === 0 ? (
            <p className="text-[var(--tq-muted)]">
              Every folder here is already set up with tq.
            </p>
          ) : (
            <table className="w-full text-left">
              <tbody>
                {adoptable.map((f) => (
                  <tr key={f.path} className="border-t border-[var(--tq-border)] first:border-0">
                    <td className="py-2">{f.name}</td>
                    <td className="py-2 text-sm text-[var(--tq-muted)]">
                      {f.repos > 0 ? `${f.repos} repo${f.repos === 1 ? '' : 's'}` : 'no repos'}
                    </td>
                    <td className="py-2 font-mono text-sm text-[var(--tq-muted)]">
                      {f.gitEmail || '—'}
                    </td>
                    <td className="py-2 text-right">
                      <Button onClick={() => adopt(f)}>Add as company</Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {alreadyManaged.length > 0 ? (
            <p className="mt-3 text-sm text-[var(--tq-muted)]">
              Already managed by tq: {alreadyManaged.map((f) => f.name).join(', ')}
            </p>
          ) : null}
        </Card>
      ) : null}

      <Card>
        {companies.length === 0 ? (
          <p className="text-[var(--tq-muted)]">No companies yet. Add your first one below.</p>
        ) : (
          <table className="w-full text-left">
            <thead className="text-xs uppercase text-[var(--tq-muted)]">
              <tr>
                <th className="pb-1">Name</th>
                <th className="pb-1">Git email</th>
                <th className="pb-1">Permissions</th>
                <th className="pb-1"></th>
              </tr>
            </thead>
            <tbody>
              {companies.map((c) => (
                <tr key={c.Name} className="border-t border-[var(--tq-border)]">
                  <td className="py-2">
                    <span
                      className="mr-2 inline-block h-3 w-3 rounded-full align-middle"
                      style={{background: c.Color}}
                    />
                    {c.Name}
                    {c.DisplayName ? <Badge>{c.DisplayName}</Badge> : null}
                  </td>
                  <td className="py-2 font-mono text-sm">{c.GitEmail}</td>
                  <td className="py-2">{c.PermissionMode}</td>
                  <td className="py-2 text-right">
                    {confirming === c.Name ? (
                      <span className="inline-flex items-center gap-2">
                        <span className="text-sm text-[var(--tq-muted)]">Remove?</span>
                        <Button
                          variant="danger"
                          onClick={() => {
                            dispatch({type: 'removeCompany', name: c.Name})
                            setConfirming(null)
                            if (editing === c.Name) {
                              setForm(null)
                              setEditing(null)
                            }
                          }}
                        >
                          Yes
                        </Button>
                        <Button onClick={() => setConfirming(null)}>No</Button>
                      </span>
                    ) : (
                      <span className="inline-flex gap-2">
                        <Button onClick={() => startEdit(c)}>Edit</Button>
                        <Button variant="danger" onClick={() => setConfirming(c.Name)}>
                          Remove
                        </Button>
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {form === null ? (
          <div className="mt-4">
            <Button variant="primary" onClick={startAdd}>
              Add company
            </Button>
          </div>
        ) : null}
      </Card>

      {form !== null ? (
        <Card className="mt-4">
          <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--tq-muted)]">
            {editing ? `Edit ${editing}` : 'New company'}
          </h2>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Name" error={errors.Name}>
              <Input
                value={form.Name}
                spellCheck={false}
                onChange={(e) => setForm({...form, Name: e.target.value})}
              />
            </Field>
            <Field label="Display name">
              <Input
                value={form.DisplayName}
                onChange={(e) => setForm({...form, DisplayName: e.target.value})}
              />
            </Field>
            <Field label="Git name" error={errors.GitName}>
              <Input
                value={form.GitName}
                onChange={(e) => setForm({...form, GitName: e.target.value})}
              />
            </Field>
            <Field label="Git email" error={errors.GitEmail}>
              <Input
                value={form.GitEmail}
                spellCheck={false}
                onChange={(e) => setForm({...form, GitEmail: e.target.value})}
              />
            </Field>
            <Field label="Git user (optional)">
              <Input
                value={form.GitUser}
                spellCheck={false}
                onChange={(e) => setForm({...form, GitUser: e.target.value})}
              />
            </Field>
            <Field label="Permission mode" error={errors.PermissionMode}>
              <Select
                value={form.PermissionMode}
                onChange={(e) => setForm({...form, PermissionMode: e.target.value})}
              >
                {PERMISSION_MODES.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Color">
              <ColorSwatches
                colors={COLORS}
                value={form.Color}
                onChange={(c) => setForm({...form, Color: c})}
              />
            </Field>
          </div>
          <div className="mt-4 flex gap-2">
            <Button variant="primary" onClick={save}>
              {editing ? 'Save' : 'Add'}
            </Button>
            <Button
              onClick={() => {
                setForm(null)
                setEditing(null)
                setErrors({})
              }}
            >
              Cancel
            </Button>
          </div>
        </Card>
      ) : null}
    </div>
  )
}
