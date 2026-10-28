import {describe, expect, it} from 'vitest'
import {
  emptyTracker,
  renameLane,
  scopeFields,
  servicesSummary,
  servicesStepError,
  validateTracker,
} from './services'
import {newCompany} from './state'

describe('scopeFields', () => {
  it('labels the scope keys per tracker', () => {
    expect(scopeFields('azure-devops').map((f) => f.key)).toEqual(['org', 'project', 'board'])
    expect(scopeFields('jira')[0].label).toMatch(/site/i)
    expect(scopeFields('github')[0].label).toMatch(/owner/i)
    // asana keeps its project under board, as the manifest does
    expect(scopeFields('asana').map((f) => f.key)).toEqual(['org', 'board'])
    expect(scopeFields('linear')[0].label).toMatch(/team/i)
    expect(scopeFields('')).toEqual([])
  })
})

describe('validateTracker', () => {
  it('accepts a complete tracker', () => {
    const t = {...emptyTracker('azure-devops'), Scope: {org: 'acme', project: 'Platform'}}
    expect(validateTracker(t)).toEqual([])
  })

  it('needs the required scope fields', () => {
    const errs = validateTracker(emptyTracker('azure-devops'))
    expect(errs.join(' ')).toMatch(/Organization/)
    expect(errs.join(' ')).toMatch(/Project/)
  })

  it('needs a variable name for env auth, never a value', () => {
    const base = {...emptyTracker('jira'), Scope: {org: 'acme.atlassian.net', project: 'PLAT'}}
    expect(validateTracker({...base, Auth: 'env', Env: ''}).join(' ')).toMatch(/variable/)
    expect(validateTracker({...base, Auth: 'env', Env: 'not a name'}).join(' ')).toMatch(/variable/)
    expect(validateTracker({...base, Auth: 'env', Env: 'ACME_JIRA_TOKEN'})).toEqual([])
  })

  it('rejects half-filled lane rows', () => {
    const t = {
      ...emptyTracker('github'),
      Scope: {org: 'acme'},
      Lanes: {QA: ''},
    }
    expect(validateTracker(t).join(' ')).toMatch(/lane/i)
  })
})

describe('servicesStepError', () => {
  it('names the company whose tracker is incomplete', () => {
    const ok = newCompany('acme')
    const bad = {...newCompany('beta'), Tracker: emptyTracker('jira')}
    expect(servicesStepError([ok])).toBeNull()
    expect(servicesStepError([ok, bad])).toMatch(/^beta: /)
  })
})

describe('renameLane', () => {
  it('renames a status key and keeps the row order', () => {
    const out = renameLane({QA: 'review', '': 'done', Shipped: 'done'}, '', 'Closed')
    expect(Object.entries(out)).toEqual([
      ['QA', 'review'],
      ['Closed', 'done'],
      ['Shipped', 'done'],
    ])
  })
})

describe('servicesSummary', () => {
  it('lists what each block will write', () => {
    const c = {
      ...newCompany('acme'),
      Tracker: {...emptyTracker('linear'), Scope: {org: 'core'}},
      Chat: 'slack',
      Email: 'none',
    }
    expect(servicesSummary(c)).toBe('Linear (core) · chat Slack · no email')
    expect(servicesSummary(newCompany('x'))).toBe('')
  })
})
