import {describe, expect, it} from 'vitest'
import {
  applyPreset,
  applyToggle,
  ceilingBanner,
  changeNotice,
  countsText,
  customLabel,
  isLocked,
  presetValue,
  scopeChoices,
  type GuardView,
} from './guardview'

const CATS = [
  'identity',
  'secrets',
  'self-protection',
  'privacy',
  'git-history',
  'prod-changes',
  'cloud-destructive',
  'db-destructive',
  'system',
  'external-writes',
  'ci-supply-chain',
  'hygiene',
  'packs',
]

function view(mode: string, over: Partial<GuardView> = {}): GuardView {
  const preset = mode === 'custom' ? over.base || 'default' : mode
  return {
    workspace: 'acme',
    mode,
    label: mode,
    categories: CATS.map((name, i) => ({name, value: presetValue(preset, name), source: 'acme', locked: i < 4})),
    overrides: [],
    ceiling: 'default',
    granted: false,
    looser_than_default: false,
    needs_grant: false,
    clamped: [],
    ignored: [],
    skipped_confirms_24h: 0,
    skipped_blocks_24h: 0,
    locked: CATS.slice(0, 4),
    projects: [],
    ...over,
  }
}

describe('presets', () => {
  it('relaxed asks on git-history and system and skips the rest', () => {
    expect(presetValue('relaxed', 'git-history')).toBe('ask')
    expect(presetValue('relaxed', 'system')).toBe('ask')
    expect(presetValue('relaxed', 'prod-changes')).toBe('skip')
    expect(presetValue('relaxed', 'privacy')).toBe('rule')
  })
  it('strict denies prod, cloud and git-history and asks on external writes', () => {
    expect(presetValue('strict', 'git-history')).toBe('deny')
    expect(presetValue('strict', 'external-writes')).toBe('ask')
    expect(presetValue('strict', 'db-destructive')).toBe('rule')
  })
})

describe('applyToggle', () => {
  it('turns a preset into custom with that preset as base', () => {
    const v = applyToggle(view('relaxed'), 'db-destructive', 'ask')
    expect(v.mode).toBe('custom')
    expect(v.base).toBe('relaxed')
    expect(v.categories.find((c) => c.name === 'db-destructive')?.value).toBe('ask')
    expect(v.label).toBe('custom (relaxed + 1)')
  })

  it('keeps the base of a custom table', () => {
    const v = applyToggle(applyToggle(view('strict'), 'hygiene', 'skip'), 'system', 'deny')
    expect(v.base).toBe('strict')
    expect(v.label).toBe('custom (strict + 2)')
  })

  it('never changes a locked row, privacy included', () => {
    const before = view('relaxed')
    for (const name of ['identity', 'secrets', 'self-protection', 'privacy']) {
      expect(applyToggle(before, name, 'skip')).toBe(before)
    }
  })

  it('is a no-op for the same value or an unknown category', () => {
    const before = view('default')
    expect(applyToggle(before, 'system', 'rule')).toBe(before)
    expect(applyToggle(before, 'nope', 'skip')).toBe(before)
  })
})

describe('applyPreset', () => {
  it('resets the table to the preset and leaves locked rows alone', () => {
    const v = applyPreset(applyToggle(view('relaxed'), 'system', 'deny'), 'strict')
    expect(v.mode).toBe('strict')
    expect(v.categories.find((c) => c.name === 'system')?.value).toBe('rule')
    expect(v.categories.filter(isLocked).every((c) => c.value === 'rule')).toBe(true)
  })
  it('custom starts from the current preset', () => {
    expect(applyPreset(view('relaxed'), 'custom').base).toBe('relaxed')
    expect(customLabel('relaxed', view('relaxed').categories)).toBe('custom (relaxed)')
  })
})

describe('ceilingBanner', () => {
  it('names the grant a looser layer needs', () => {
    expect(ceilingBanner(view('default'))).toBeNull()
    const b = ceilingBanner(view('default', {needs_grant: true, requested: 'relaxed', proposal: 'relaxed'}))
    expect(b?.grant).toBe('relaxed')
    expect(b?.text).toMatch(/Looser than your grant/)
    expect(b?.text).toMatch(/relaxed for acme/)
  })
  it('never falls back to relaxed when no proposal is known', () => {
    const b = ceilingBanner(view('default', {needs_grant: true, clamped: ['db-destructive'], proposal: '', requested: ''}))
    expect(b?.grant).toBe('the current layers')
    expect(b?.text).not.toMatch(/relaxed/)
  })
})

describe('text', () => {
  it('counts and notices', () => {
    expect(countsText(view('relaxed', {skipped_confirms_24h: 3, skipped_blocks_24h: 1}))).toBe(
      '3 confirms and 1 block skipped in 24h',
    )
    expect(
      changeNotice({Workspace: 'acme', Scope: 'workspace', Looser: true, Proposal: 'custom', Carried: false, Dropped: 'relaxed', Warnings: []}),
    ).toBe('relaxed grant dropped: the change loosens past it · needs a custom grant to take effect')
  })
  it('lists the scopes', () => {
    expect(scopeChoices(view('default', {projects: ['demo-todo']})).map((s) => s.value)).toEqual([
      'workspace',
      'project:demo-todo',
      'global',
    ])
  })
})
