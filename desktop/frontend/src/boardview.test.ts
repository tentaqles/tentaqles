import {describe, expect, it} from 'vitest'
import {boardRows, isLoopbackHubURL} from './boardview'

describe('isLoopbackHubURL', () => {
  it('accepts only plain http on loopback', () => {
    expect(isLoopbackHubURL('http://127.0.0.1:7470/?ws=acme')).toBe(true)
    expect(isLoopbackHubURL('http://localhost:7470/?ws=acme')).toBe(true)
    expect(isLoopbackHubURL('http://[::1]:7470/')).toBe(true)
    expect(isLoopbackHubURL('https://127.0.0.1:7470/')).toBe(false)
    expect(isLoopbackHubURL('http://acme.test:7470/')).toBe(false)
    expect(isLoopbackHubURL('javascript:alert(1)')).toBe(false)
    expect(isLoopbackHubURL('not a url')).toBe(false)
  })
})

describe('boardRows', () => {
  const ws = (Name: string, Trusted = true) => ({Name, Root: '/w/' + Name, Email: '', Trusted, Identities: []})
  const hub = (Port: number, Workspaces: string[]) => ({
    URL: `http://127.0.0.1:${Port}`,
    Port,
    PID: 1,
    Mode: 'single',
    Workspaces,
  })

  it('marks the workspaces a running hub serves', () => {
    const rows = boardRows([ws('acme'), ws('beta'), ws('gamma', false)], [hub(7470, ['acme'])])
    expect(rows).toEqual([
      {name: 'acme', trusted: true, servedAt: 'http://127.0.0.1:7470'},
      {name: 'beta', trusted: true, servedAt: ''},
      {name: 'gamma', trusted: false, servedAt: ''},
    ])
  })

  it('tolerates null lists from the bindings', () => {
    expect(boardRows(null, null)).toEqual([])
  })
})
