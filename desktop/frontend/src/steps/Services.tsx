import {useEffect, useState} from 'react'
import * as api from '../api'
import {Badge, Button, Card, Field, Input, Select, StepHeader} from '../ui'
import {usePlan} from '../state'
import {CATEGORIES} from '../validate'

interface EnvRow {
  key: string
  value: string
}

export default function Services() {
  const {state, dispatch} = usePlan()
  const companies = state.plan.Companies

  const [providers, setProviders] = useState<api.Provider[]>([])
  const [loadError, setLoadError] = useState('')
  const [selected, setSelected] = useState(0)
  const [showCustom, setShowCustom] = useState(false)

  async function refresh() {
    try {
      setProviders((await api.providers()) ?? [])
      setLoadError('')
    } catch (e) {
      setLoadError(api.errorText(e))
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  if (companies.length === 0) {
    return (
      <div className="max-w-3xl">
        <StepHeader title="Services" />
        <Card>
          <p className="text-[var(--tq-muted)]">
            Add a company first — services are picked per company.
          </p>
        </Card>
      </div>
    )
  }

  const company = companies[Math.min(selected, companies.length - 1)]
  const chosen = company.Identities ?? []

  function toggle(id: string) {
    const next = chosen.includes(id) ? chosen.filter((x) => x !== id) : [...chosen, id]
    dispatch({type: 'setIdentities', name: company.Name, identities: next})
  }

  const known = new Set<string>(CATEGORIES)
  const grouped = CATEGORIES.map((cat) => ({
    cat,
    items: providers.filter((p) => {
      const c = p.Category || 'other'
      return c === cat || (cat === 'other' && !known.has(c))
    }),
  })).filter((g) => g.items.length > 0)

  return (
    <div className="max-w-3xl">
      <StepHeader
        title="Services"
        subtitle="Pick the services each company signs in to. tq keeps their credentials in that company's identity directory."
      />

      <div className="mb-4 flex flex-wrap gap-2">
        {companies.map((c, i) => (
          <button
            key={c.Name}
            onClick={() => setSelected(i)}
            className={`rounded-md border px-3 py-1.5 ${
              c.Name === company.Name
                ? 'border-[var(--tq-accent)] text-[var(--tq-accent)]'
                : 'border-[var(--tq-border)]'
            }`}
          >
            <span
              className="mr-2 inline-block h-2.5 w-2.5 rounded-full align-middle"
              style={{background: c.Color}}
            />
            {c.DisplayName || c.Name}
          </button>
        ))}
      </div>

      {loadError ? (
        <Card className="mb-4">
          <p className="text-[#e0432f]">Could not load providers: {loadError}</p>
        </Card>
      ) : null}

      {chosen.length === 0 ? (
        <p className="mb-3 text-sm text-[var(--tq-muted)]">
          No services selected — <code>claude</code> and <code>gh</code> will be used by default.
        </p>
      ) : null}

      <Card>
        <div className="space-y-5">
          {grouped.map((g) => (
            <section key={g.cat}>
              <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--tq-muted)]">
                {g.cat}
              </h3>
              <div className="grid grid-cols-2 gap-2">
                {g.items.map((p) => (
                  <label key={p.ID} className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={chosen.includes(p.ID)}
                      onChange={() => toggle(p.ID)}
                    />
                    <span>{p.Name}</span>
                    {!p.HasCLI ? <Badge>no CLI</Badge> : null}
                    {p.HasIdentity ? <Badge tone="accent">identity</Badge> : null}
                  </label>
                ))}
              </div>
            </section>
          ))}
        </div>
        <div className="mt-5">
          <Button onClick={() => setShowCustom((v) => !v)}>Other…</Button>
        </div>
      </Card>

      {showCustom ? (
        <CustomProviderForm
          onDone={async () => {
            setShowCustom(false)
            await refresh()
          }}
          onCancel={() => setShowCustom(false)}
        />
      ) : null}
    </div>
  )
}

function CustomProviderForm({onDone, onCancel}: {onDone: () => void; onCancel: () => void}) {
  const [id, setID] = useState('')
  const [name, setName] = useState('')
  const [category, setCategory] = useState('other')
  const [command, setCommand] = useState('')
  const [env, setEnv] = useState<EnvRow[]>([{key: '', value: ''}])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setError('')
    if (id.trim() === '' || name.trim() === '') {
      setError('Id and name are required')
      return
    }
    // This is the binary tq invokes, not a shell line — whitespace would mean
    // arguments, which belong in the provider's login/verify commands.
    if (/\s/.test(command.trim())) {
      setError('CLI command must be a single binary name, with no spaces or arguments')
      return
    }
    const envMap: Record<string, string> = {}
    for (const row of env) {
      if (row.key.trim() !== '') envMap[row.key.trim()] = row.value
    }
    setBusy(true)
    try {
      await api.addCustomProvider(id.trim(), name.trim(), category, command.trim(), envMap)
      onDone()
    } catch (e) {
      setError(api.errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card className="mt-4">
      <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--tq-muted)]">
        Custom service
      </h2>
      <div className="grid grid-cols-2 gap-4">
        <Field label="Id">
          <Input value={id} spellCheck={false} onChange={(e) => setID(e.target.value)} />
        </Field>
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Category">
          <Select value={category} onChange={(e) => setCategory(e.target.value)}>
            {CATEGORIES.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="CLI command (binary name)">
          <Input value={command} spellCheck={false} onChange={(e) => setCommand(e.target.value)} />
        </Field>
      </div>

      <div className="mt-4">
        <span className="text-xs uppercase tracking-wide text-[var(--tq-muted)]">
          Environment variables
        </span>
        <div className="mt-2 space-y-2">
          {env.map((row, i) => (
            <div key={i} className="flex gap-2">
              <Input
                placeholder="KEY"
                value={row.key}
                spellCheck={false}
                onChange={(e) =>
                  setEnv(env.map((r, j) => (j === i ? {...r, key: e.target.value} : r)))
                }
              />
              <Input
                placeholder="value"
                className="flex-1"
                value={row.value}
                spellCheck={false}
                onChange={(e) =>
                  setEnv(env.map((r, j) => (j === i ? {...r, value: e.target.value} : r)))
                }
              />
              <Button onClick={() => setEnv(env.filter((_, j) => j !== i))}>Remove</Button>
            </div>
          ))}
          <Button onClick={() => setEnv([...env, {key: '', value: ''}])}>Add variable</Button>
        </div>
      </div>

      {error ? <p className="mt-3 text-sm text-[#e0432f]">{error}</p> : null}

      <div className="mt-4 flex gap-2">
        <Button variant="primary" disabled={busy} onClick={submit}>
          Add service
        </Button>
        <Button onClick={onCancel}>Cancel</Button>
      </div>
    </Card>
  )
}
