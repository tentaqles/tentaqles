// Pure helpers for the Board tab.
import type {Hub, Workspace} from './api'

const LOOPBACK = new Set(['127.0.0.1', 'localhost', '[::1]'])

// isLoopbackHubURL guards the window navigation: the board is only ever a plain-http hub on
// this machine (a non-loopback hub needs a token, which this app never handles).
export function isLoopbackHubURL(raw: string): boolean {
  let u: URL
  try {
    u = new URL(raw)
  } catch {
    return false
  }
  return u.protocol === 'http:' && LOOPBACK.has(u.hostname)
}

export interface BoardRow {
  name: string
  trusted: boolean
  servedAt: string // the hub's base URL, '' when no running hub serves it
}

export function boardRows(workspaces: Workspace[] | null, hubs: Hub[] | null): BoardRow[] {
  const live = hubs ?? []
  return (workspaces ?? []).map((w) => ({
    name: w.Name,
    trusted: w.Trusted,
    servedAt: live.find((h) => (h.Workspaces ?? []).includes(w.Name))?.URL ?? '',
  }))
}
