// Pure helpers for the Services step's company tools (spec B §6): the tracker the hub mirrors,
// and the chat and email connectors the agent uses over MCP. The manifest holds names only, so
// nothing here ever asks for a token.
import type {Company, Tracker} from './api'

export const TRACKER_LABELS: Record<string, string> = {
  github: 'GitHub',
  'azure-devops': 'Azure DevOps',
  jira: 'Jira',
  linear: 'Linear',
  asana: 'Asana',
}

export const CHAT_LABELS: Record<string, string> = {slack: 'Slack', teams: 'Microsoft Teams', none: 'None'}
export const EMAIL_LABELS: Record<string, string> = {gmail: 'Gmail', outlook: 'Outlook', none: 'None'}

export const AUTH_LABELS: Record<string, string> = {
  cli: "The tracker's own CLI login (gh, az, acli)",
  keychain: 'A token in the OS keychain (tq secret set)',
  env: 'A token through tq dotenv (name the variable)',
  mcp: "The agent's MCP connector (no background sync)",
}

// The fallbacks when the catalog binding is unavailable; the backend validates either way.
export const DEFAULT_CATALOG = {
  Trackers: Object.keys(TRACKER_LABELS),
  TrackerAuths: Object.keys(AUTH_LABELS),
  Assignees: ['me', 'anyone'],
  Chat: Object.keys(CHAT_LABELS),
  Email: Object.keys(EMAIL_LABELS),
}

// The hub's lanes a tracker status can map to.
export const HUB_LANES = ['backlog', 'todo', 'active', 'review', 'done']

// renameLane changes a lane override's status key in place, keeping the row order.
export function renameLane(lanes: Record<string, string>, from: string, to: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(lanes)) out[k === from ? to : k] = v
  return out
}

export const label = (labels: Record<string, string>, id: string): string => labels[id] ?? id

export interface ScopeField {
  key: string
  label: string
  placeholder: string
  required: boolean
}

// scopeFields are the tracker.scope keys each provider reads (spec B §6).
export function scopeFields(provider: string): ScopeField[] {
  const f = (key: string, label: string, placeholder: string, required: boolean): ScopeField => ({
    key,
    label,
    placeholder,
    required,
  })
  switch (provider) {
    case 'azure-devops':
      return [
        f('org', 'Organization', 'acme', true),
        f('project', 'Project', 'Platform', true),
        f('board', 'Team (optional)', 'Platform Team', false),
      ]
    case 'jira':
      return [
        f('org', 'Site', 'acme.atlassian.net', true),
        f('project', 'Project key', 'PLAT', true),
        f('board', 'Board (optional)', '', false),
      ]
    case 'github':
      return [
        f('org', 'Owner (user or organization)', 'acme', true),
        f('project', 'Repository (optional)', 'platform', false),
        f('board', 'Project number (optional)', '3', false),
      ]
    case 'linear':
      return [f('org', 'Team', 'core', true), f('project', 'Project (optional)', '', false)]
    case 'asana':
      return [f('org', 'Workspace', 'acme', true), f('board', 'Project', 'Platform', true)]
    default:
      return []
  }
}

export function emptyTracker(provider: string): Tracker {
  return {Provider: provider, Scope: {}, AssignedTo: 'me', Auth: 'cli', Env: '', Lanes: {}}
}

const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/

// validateTracker mirrors the manifest loader's checks that the form can judge on its own.
export function validateTracker(t: Tracker): string[] {
  const errs: string[] = []
  if (!t.Provider) return ['Pick a tracker, or skip it']
  for (const f of scopeFields(t.Provider)) {
    if (f.required && !(t.Scope?.[f.key] ?? '').trim()) errs.push(`${f.label} is required`)
  }
  if (t.Auth === 'env') {
    const v = t.Env.trim()
    if (!v || !ENV_NAME.test(v)) errs.push('Name the environment variable tq dotenv resolves (letters, digits, _)')
  }
  for (const [status, lane] of Object.entries(t.Lanes ?? {})) {
    if (!status.trim() || !lane.trim()) errs.push('Each lane override needs both a tracker status and a hub lane')
  }
  return errs
}

// servicesStepError is the Services step's own check: the first incomplete tracker, by company.
export function servicesStepError(companies: Company[]): string | null {
  for (const c of companies) {
    if (!c.Tracker) continue
    const errs = validateTracker(c.Tracker)
    if (errs.length > 0) return `${c.Name}: ${errs[0]}`
  }
  return null
}

// servicesSummary is the one-line view of what the blocks will write.
export function servicesSummary(c: Company): string {
  const parts: string[] = []
  if (c.Tracker?.Provider) {
    const scope = scopeFields(c.Tracker.Provider)
      .map((f) => (c.Tracker?.Scope?.[f.key] ?? '').trim())
      .filter(Boolean)
      .join('/')
    parts.push(label(TRACKER_LABELS, c.Tracker.Provider) + (scope ? ` (${scope})` : ''))
  }
  if (c.Chat) parts.push(c.Chat === 'none' ? 'no chat' : `chat ${label(CHAT_LABELS, c.Chat)}`)
  if (c.Email) parts.push(c.Email === 'none' ? 'no email' : `email ${label(EMAIL_LABELS, c.Email)}`)
  return parts.join(' · ')
}
