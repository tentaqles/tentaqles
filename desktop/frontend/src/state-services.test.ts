import {describe, expect, it} from 'vitest'
import {initialState, newCompany, reducer} from './state'
import {emptyTracker} from './services'

describe('reducer: company services', () => {
  const s0 = ['acme', 'beta'].reduce(
    (s, n) => reducer(s, {type: 'addCompany', company: newCompany(n)}),
    initialState,
  )

  it('starts every company with no service blocks', () => {
    expect(s0.plan.Companies[0].Tracker).toBeUndefined()
    expect(s0.plan.Companies[0].Chat).toBe('')
    expect(s0.plan.Companies[0].Email).toBe('')
  })

  it('sets and clears a tracker on one company', () => {
    const t = {...emptyTracker('jira'), Scope: {org: 'acme.atlassian.net', project: 'PLAT'}}
    const s1 = reducer(s0, {type: 'setTracker', name: 'acme', tracker: t})
    expect(s1.plan.Companies[0].Tracker).toEqual(t)
    expect(s1.plan.Companies[1].Tracker).toBeUndefined()
    const s2 = reducer(s1, {type: 'setTracker', name: 'acme', tracker: undefined})
    expect(s2.plan.Companies[0].Tracker).toBeUndefined()
  })

  it('sets chat and email per company', () => {
    let s = reducer(s0, {type: 'setChat', name: 'beta', provider: 'teams'})
    s = reducer(s, {type: 'setEmail', name: 'beta', provider: 'none'})
    expect(s.plan.Companies[1].Chat).toBe('teams')
    expect(s.plan.Companies[1].Email).toBe('none')
    expect(s.plan.Companies[0].Chat).toBe('')
  })
})
