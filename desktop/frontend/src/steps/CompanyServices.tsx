import {useEffect, useState} from 'react'
import * as api from '../api'
import {Button, Card, Field, Input, Select} from '../ui'
import {usePlan} from '../state'
import {
  AUTH_LABELS,
  CHAT_LABELS,
  DEFAULT_CATALOG,
  EMAIL_LABELS,
  HUB_LANES,
  TRACKER_LABELS,
  emptyTracker,
  label,
  renameLane,
  scopeFields,
  servicesSummary,
  validateTracker,
} from '../services'

// CompanyServices is the Services step's second half: the company board the hub mirrors, and
// the chat and email connectors the agent uses over MCP. Every block is optional and holds
// names only; tokens never pass through this screen.
export default function CompanyServices({company}: {company: api.Company}) {
  const {dispatch} = usePlan()
  const [catalog, setCatalog] = useState<api.Services>(DEFAULT_CATALOG)

  useEffect(() => {
    let live = true
    api
      .serviceCatalog()
      .then((c) => live && c && setCatalog(c))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [])

  const t = company.Tracker
  const setTracker = (tracker: api.Tracker | undefined) =>
    dispatch({type: 'setTracker', name: company.Name, tracker})
  const patch = (p: Partial<api.Tracker>) => t && setTracker({...t, ...p})
  const errors = t ? validateTracker(t) : []
  const lanes = Object.entries(t?.Lanes ?? {})
  const summary = servicesSummary(company)

  return (
    <Card className="mt-4">
      <h3 className="mb-1 font-semibold">Company board, chat and email</h3>
      <p className="mb-4 text-sm text-[var(--tq-muted)]">
        Written into this company's <code>.tentaqles.yaml</code>. Each one can be skipped. Logins and
        tokens are set up afterwards with <code>tq login</code>, <code>tq secret set</code> or the
        agent's <code>/mcp</code>, never here.
      </p>

      <div className="grid grid-cols-2 gap-3">
        <Field label="Tracker">
          <Select
            value={t?.Provider ?? ''}
            onChange={(e) => setTracker(e.target.value ? emptyTracker(e.target.value) : undefined)}
          >
            <option value="">Skip (no board sync)</option>
            {catalog.Trackers.map((p) => (
              <option key={p} value={p}>
                {label(TRACKER_LABELS, p)}
              </option>
            ))}
          </Select>
        </Field>
        {t ? (
          <Field label="Mirror items assigned to">
            <Select value={t.AssignedTo || 'me'} onChange={(e) => patch({AssignedTo: e.target.value})}>
              {catalog.Assignees.map((a) => (
                <option key={a} value={a}>
                  {a === 'me' ? 'Me (plus children of mirrored cards)' : 'Anyone in the scope'}
                </option>
              ))}
            </Select>
          </Field>
        ) : null}

        {t
          ? scopeFields(t.Provider).map((f) => (
              <Field key={f.key} label={f.label}>
                <Input
                  value={t.Scope?.[f.key] ?? ''}
                  placeholder={f.placeholder}
                  onChange={(e) => patch({Scope: {...t.Scope, [f.key]: e.target.value}})}
                />
              </Field>
            ))
          : null}

        {t ? (
          <Field label="Sign in with">
            <Select value={t.Auth || 'cli'} onChange={(e) => patch({Auth: e.target.value})}>
              {catalog.TrackerAuths.map((a) => (
                <option key={a} value={a}>
                  {label(AUTH_LABELS, a)}
                </option>
              ))}
            </Select>
          </Field>
        ) : null}
        {t?.Auth === 'env' ? (
          <Field label="Environment variable (its name, not the token)">
            <Input
              value={t.Env}
              placeholder="ACME_JIRA_TOKEN"
              onChange={(e) => patch({Env: e.target.value})}
            />
          </Field>
        ) : null}
      </div>

      {t ? (
        <div className="mt-4">
          <div className="mb-2 text-xs uppercase tracking-wide text-[var(--tq-muted)]">
            Lane overrides (optional; statuses map by category otherwise)
          </div>
          {lanes.map(([status, lane], i) => (
            <div key={i} className="mb-2 flex items-center gap-2">
              <Input
                value={status}
                placeholder="In Review"
                onChange={(e) => patch({Lanes: renameLane(t.Lanes ?? {}, status, e.target.value)})}
              />
              <span className="text-[var(--tq-muted)]">→</span>
              <Select
                value={lane}
                onChange={(e) => patch({Lanes: {...t.Lanes, [status]: e.target.value}})}
              >
                {HUB_LANES.map((l) => (
                  <option key={l} value={l}>
                    {l}
                  </option>
                ))}
              </Select>
              <Button
                variant="danger"
                onClick={() => {
                  const next = {...t.Lanes}
                  delete next[status]
                  patch({Lanes: next})
                }}
              >
                Remove
              </Button>
            </div>
          ))}
          <Button
            disabled={'' in (t.Lanes ?? {})}
            onClick={() => patch({Lanes: {...t.Lanes, '': 'review'}})}
          >
            Add lane override
          </Button>
        </div>
      ) : null}

      <div className="mt-4 grid grid-cols-2 gap-3">
        <Field label="Chat (agent MCP connector)">
          <Select
            value={company.Chat}
            onChange={(e) => dispatch({type: 'setChat', name: company.Name, provider: e.target.value})}
          >
            <option value="">Skip</option>
            {catalog.Chat.map((p) => (
              <option key={p} value={p}>
                {p === 'none' ? 'None (this company has no chat)' : label(CHAT_LABELS, p)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Email (agent MCP connector)">
          <Select
            value={company.Email}
            onChange={(e) => dispatch({type: 'setEmail', name: company.Name, provider: e.target.value})}
          >
            <option value="">Skip</option>
            {catalog.Email.map((p) => (
              <option key={p} value={p}>
                {p === 'none' ? 'None (this company has no email)' : label(EMAIL_LABELS, p)}
              </option>
            ))}
          </Select>
        </Field>
      </div>

      {errors.length > 0 ? (
        <ul className="mt-3 text-sm text-[#e0432f]">
          {errors.map((e) => (
            <li key={e}>{e}</li>
          ))}
        </ul>
      ) : summary ? (
        <p className="mt-3 text-sm text-[var(--tq-muted)]">Will write: {summary}</p>
      ) : null}
    </Card>
  )
}
