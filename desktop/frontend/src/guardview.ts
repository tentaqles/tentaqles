// Guard tab logic (spec E §10.2): plain types for the guard view, the toggle
// state the screen shows before the write comes back, and thin wrappers over
// the Wails bindings. The screen (Guard.tsx) imports only this module.
import * as App from '../wailsjs/go/main/App'

export type Value = 'skip' | 'ask' | 'deny' | 'rule'
export const VALUES: Value[] = ['skip', 'ask', 'deny', 'rule']
export const PRESETS = ['default', 'relaxed', 'strict'] as const
export const MODES = ['default', 'relaxed', 'strict', 'custom'] as const
export type Mode = (typeof MODES)[number]

export interface GuardCategory {
  name: string
  value: string
  source: string
  locked: boolean
}

export interface GuardOverride {
  id: string
  value: string
  source?: string
}

export interface GuardView {
  workspace?: string
  mode: string
  label: string
  base?: string
  requested?: string
  mode_source?: string
  categories: GuardCategory[]
  overrides: GuardOverride[]
  ceiling: string
  granted: boolean
  granted_at?: string
  looser_than_default: boolean
  needs_grant: boolean
  proposal?: string
  clamped: string[]
  ignored: string[]
  skipped_confirms_24h: number
  skipped_blocks_24h: number
  locked: string[]
  projects: string[]
}

export interface GuardChange {
  Workspace: string
  Scope: string
  Looser: boolean
  Proposal: string
  Carried: boolean
  Dropped: string
  Warnings: string[]
}

// The four locked groups: enforced in every mode, never toggled.
export const LOCKED = ['identity', 'secrets', 'self-protection', 'privacy']

export function isLocked(c: GuardCategory): boolean {
  return c.locked || LOCKED.includes(c.name)
}

export function lockedTooltip(name: string): string {
  return `Locked: ${name} is enforced in every mode tq is on in, and cannot be toggled.`
}

// The relaxed and strict presets (spec E §4), for the toggle state before the
// write comes back. Missing categories are rule.
const PRESET_TABLES: Record<string, Record<string, Value>> = {
  default: {},
  relaxed: {
    'git-history': 'ask',
    'prod-changes': 'skip',
    'cloud-destructive': 'skip',
    'db-destructive': 'skip',
    system: 'ask',
    'external-writes': 'skip',
    'ci-supply-chain': 'skip',
    hygiene: 'skip',
    packs: 'skip',
  },
  strict: {
    'git-history': 'deny',
    'prod-changes': 'deny',
    'cloud-destructive': 'deny',
    'external-writes': 'ask',
  },
}

export function presetValue(preset: string, category: string): Value {
  return PRESET_TABLES[preset]?.[category] ?? 'rule'
}

// customLabel is "custom (relaxed + 2)": the categories that differ from the base.
export function customLabel(base: string, categories: GuardCategory[]): string {
  const n = categories.filter((c) => !isLocked(c) && c.value !== presetValue(base, c.name)).length
  return n === 0 ? `custom (${base})` : `custom (${base} + ${n})`
}

// applyToggle is the view after setting one category, as tq does it: a preset
// becomes custom with that preset as its base. A locked row never changes.
export function applyToggle(view: GuardView, name: string, value: Value): GuardView {
  const row = view.categories.find((c) => c.name === name)
  if (!row || isLocked(row) || row.value === value) return view
  const base = view.mode === 'custom' ? view.base || 'default' : view.mode
  const categories = view.categories.map((c) => (c.name === name ? {...c, value} : c))
  return {...view, mode: 'custom', base, categories, label: customLabel(base, categories)}
}

// applyPreset is the view after choosing a preset (or custom on top of the
// current preset).
export function applyPreset(view: GuardView, mode: Mode, base?: string): GuardView {
  if (mode === 'custom') {
    const b = base || (view.mode === 'custom' ? view.base || 'default' : view.mode)
    return {...view, mode, base: b, label: customLabel(b, view.categories)}
  }
  const categories = view.categories.map((c) => (isLocked(c) ? c : {...c, value: presetValue(mode, c.name)}))
  return {...view, mode, base: '', categories, label: mode}
}

// Scope choices for a workspace: the workspace, each project, and the global file.
export function scopeChoices(view: GuardView): {value: string; label: string}[] {
  return [
    {value: 'workspace', label: `${view.workspace ?? 'workspace'} (workspace)`},
    ...view.projects.map((p) => ({value: `project:${p}`, label: `project ${p}`})),
    {value: 'global', label: 'every workspace (global)'},
  ]
}

// ceilingBanner is the line above the grid when the layers ask for more than
// the grant allows; null when nothing needs a grant.
export function ceilingBanner(view: GuardView): {text: string; grant: string} | null {
  if (!view.needs_grant) return null
  // Name only what GrantMode will grant (the proposal); never guess relaxed.
  const grant = view.proposal || 'the current layers'
  const ws = view.workspace ?? 'this workspace'
  return {text: `Looser than your grant: the session runs at ${view.ceiling} until you grant ${grant} for ${ws}.`, grant}
}

function plural(n: number, noun: string): string {
  return n === 1 ? `1 ${noun}` : `${n} ${noun}s`
}

export function countsText(view: GuardView): string {
  return `${plural(view.skipped_confirms_24h, 'confirm')} and ${plural(view.skipped_blocks_24h, 'block')} skipped in 24h`
}

export function ceilingText(view: GuardView): string {
  return view.granted ? `${view.ceiling} grant${view.granted_at ? ` (granted ${view.granted_at})` : ''}` : 'default (no grant)'
}

// changeNotice turns a write's result into one line for the screen.
export function changeNotice(ch: GuardChange): string {
  const parts: string[] = []
  if (ch.Carried) parts.push('grant carried')
  if (ch.Dropped) parts.push(`${ch.Dropped} grant dropped: the change loosens past it`)
  if (ch.Looser && ch.Proposal) parts.push(`needs a ${ch.Proposal} grant to take effect`)
  parts.push(...(ch.Warnings ?? []))
  return parts.join(' · ')
}

// ---- bindings -----------------------------------------------------------

function plain<T>(x: unknown): T {
  return JSON.parse(JSON.stringify(x)) as T
}

export async function guardWorkspaces(): Promise<GuardView[]> {
  return plain<GuardView[]>((await App.GuardWorkspaces()) ?? [])
}

export async function setMode(ws: string, scope: string, mode: string, base: string): Promise<GuardChange> {
  return plain<GuardChange>(await App.SetMode(ws, scope, mode, base))
}

export async function toggleCategory(ws: string, scope: string, category: string, value: string): Promise<GuardChange> {
  return plain<GuardChange>(await App.ToggleCategory(ws, scope, category, value))
}

export async function setOverride(ws: string, scope: string, id: string, value: string): Promise<GuardChange> {
  return plain<GuardChange>(await App.SetOverride(ws, scope, id, value))
}

export async function grantMode(ws: string): Promise<string> {
  return App.GrantMode(ws)
}

export async function revokeGrant(ws: string): Promise<void> {
  return App.RevokeGrant(ws)
}
