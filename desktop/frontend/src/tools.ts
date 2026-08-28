// Pure helpers for the Tools and Preview screens. Kept free of React and of
// the Wails bindings so they can be unit-tested directly.
import type {Change, ToolResult} from './api'

export type ToolStatus = 'ok' | 'missing' | 'n/a'

// Package managers whose hints are runnable commands. Anything else (a
// "see <url>" pointer or a free-form note) is shown as text, not a button.
const COMMAND_PREFIXES = [
  'winget ',
  'scoop ',
  'brew ',
  'apt ',
  'sudo apt ',
  'pip ',
  'pip3 ',
  'npm ',
]

// isCommandHint reports whether a hint line can be handed to a terminal.
export function isCommandHint(hint: string): boolean {
  const h = hint.trim()
  if (h === '') return false
  return COMMAND_PREFIXES.some((p) => h.startsWith(p))
}

// hintURL extracts the URL from a "see <url>" hint, or "" for other hints.
export function hintURL(hint: string): string {
  const h = hint.trim()
  if (!h.startsWith('see ')) return ''
  const url = h.slice(4).trim()
  return /^https?:\/\//.test(url) ? url : ''
}

// toolStatus classifies one probe result: providers with no CLI at all are
// "n/a" rather than a failure the user has to act on.
export function toolStatus(r: ToolResult): ToolStatus {
  if (r.Installed) return 'ok'
  if (r.Err === 'no CLI') return 'n/a'
  if ((r.Hints ?? []).includes('no CLI to install')) return 'n/a'
  return 'missing'
}

// countMissing returns how many results across every company need action.
export function countMissing(results: Record<string, ToolResult[]>): number {
  return Object.values(results ?? {}).reduce(
    (n, list) => n + (list ?? []).filter((r) => toolStatus(r) === 'missing').length,
    0,
  )
}

// summarizeChanges counts changes by Kind, preserving first-seen order.
export function summarizeChanges(changes: Change[]): {kind: string; count: number}[] {
  const order: string[] = []
  const counts = new Map<string, number>()
  for (const c of changes ?? []) {
    const kind = c.Kind || 'change'
    if (!counts.has(kind)) order.push(kind)
    counts.set(kind, (counts.get(kind) ?? 0) + 1)
  }
  return order.map((kind) => ({kind, count: counts.get(kind) ?? 0}))
}
