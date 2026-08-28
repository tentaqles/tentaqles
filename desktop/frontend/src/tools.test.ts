import {describe, expect, it} from 'vitest'
import type {Change, ToolResult} from './api'
import {countMissing, hintURL, isCommandHint, summarizeChanges, toolStatus} from './tools'

function result(over: Partial<ToolResult> = {}): ToolResult {
  return {
    ID: 'aws',
    Command: 'aws',
    Path: '',
    Version: '',
    Err: '',
    Installed: false,
    Hints: [],
    ...over,
  }
}

describe('isCommandHint', () => {
  it('accepts package-manager commands', () => {
    for (const h of [
      'winget install Amazon.AWSCLI',
      'scoop install gh',
      'brew install awscli',
      'sudo apt install gh',
      'pip install snowflake-cli',
      'npm install -g vercel',
    ]) {
      expect(isCommandHint(h)).toBe(true)
    }
  })

  it('rejects urls, notes and blanks', () => {
    expect(isCommandHint('see https://example.test/install')).toBe(false)
    expect(isCommandHint('no CLI to install')).toBe(false)
    expect(isCommandHint('   ')).toBe(false)
    expect(isCommandHint('rm -rf /')).toBe(false)
  })
})

describe('hintURL', () => {
  it('extracts http(s) urls from "see" hints', () => {
    expect(hintURL('see https://example.test/x')).toBe('https://example.test/x')
    expect(hintURL('see ftp://example.test')).toBe('')
    expect(hintURL('winget install X')).toBe('')
  })
})

describe('toolStatus', () => {
  it('classifies installed, missing and n/a', () => {
    expect(toolStatus(result({Installed: true, Version: '2.1'}))).toBe('ok')
    expect(toolStatus(result({Err: 'no CLI'}))).toBe('n/a')
    expect(toolStatus(result({Hints: ['no CLI to install']}))).toBe('n/a')
    expect(toolStatus(result({Hints: ['winget install X']}))).toBe('missing')
  })
})

describe('countMissing', () => {
  it('counts only actionable misses across companies', () => {
    const results: Record<string, ToolResult[]> = {
      acme: [result({Installed: true}), result({Hints: ['brew install gh']})],
      globex: [result({Err: 'no CLI'}), result({})],
    }
    expect(countMissing(results)).toBe(2)
    expect(countMissing({})).toBe(0)
  })
})

describe('summarizeChanges', () => {
  it('counts by kind in first-seen order', () => {
    const changes: Change[] = [
      {Kind: 'create', Target: 'a', Detail: ''},
      {Kind: 'skip', Target: 'b', Detail: ''},
      {Kind: 'create', Target: 'c', Detail: ''},
    ] as Change[]
    expect(summarizeChanges(changes)).toEqual([
      {kind: 'create', count: 2},
      {kind: 'skip', count: 1},
    ])
  })

  it('handles an empty list', () => {
    expect(summarizeChanges([])).toEqual([])
  })
})
