import {describe, expect, it} from 'vitest'
import {initialState, newCompany, reducer, STEPS, type State} from './state'

function withCompanies(names: string[]): State {
  return names.reduce(
    (s, n) => reducer(s, {type: 'addCompany', company: newCompany(n)}),
    initialState,
  )
}

describe('reducer', () => {
  it('sets the base', () => {
    const s = reducer(initialState, {type: 'setBase', base: 'C:/work'})
    expect(s.plan.Base).toBe('C:/work')
  })

  it('sets hooks', () => {
    const s = reducer(initialState, {type: 'setHooks', hooks: ['bash', 'pwsh']})
    expect(s.plan.Hooks).toEqual(['bash', 'pwsh'])
  })

  it('adds companies and ignores duplicates', () => {
    const s = withCompanies(['acme', 'globex', 'acme'])
    expect(s.plan.Companies.map((c) => c.Name)).toEqual(['acme', 'globex'])
  })

  it('updates a company by name', () => {
    const s = withCompanies(['acme', 'globex'])
    const updated = {...s.plan.Companies[0], GitEmail: 'me@acme.test'}
    const next = reducer(s, {type: 'updateCompany', name: 'acme', company: updated})
    expect(next.plan.Companies[0].GitEmail).toBe('me@acme.test')
    expect(next.plan.Companies[1].GitEmail).toBe('')
    // original state is untouched
    expect(s.plan.Companies[0].GitEmail).toBe('')
  })

  it('renames through updateCompany without reordering', () => {
    const s = withCompanies(['acme', 'globex'])
    const renamed = {...s.plan.Companies[0], Name: 'acme-inc'}
    const next = reducer(s, {type: 'updateCompany', name: 'acme', company: renamed})
    expect(next.plan.Companies.map((c) => c.Name)).toEqual(['acme-inc', 'globex'])
  })

  it('removes a company', () => {
    const s = withCompanies(['acme', 'globex'])
    const next = reducer(s, {type: 'removeCompany', name: 'acme'})
    expect(next.plan.Companies.map((c) => c.Name)).toEqual(['globex'])
  })

  it('removing an unknown company is a no-op', () => {
    const s = withCompanies(['acme'])
    expect(reducer(s, {type: 'removeCompany', name: 'nope'}).plan.Companies).toHaveLength(1)
  })

  it('sets identities on one company only', () => {
    const s = withCompanies(['acme', 'globex'])
    const next = reducer(s, {type: 'setIdentities', name: 'globex', identities: ['aws', 'gh']})
    expect(next.plan.Companies[1].Identities).toEqual(['aws', 'gh'])
    expect(next.plan.Companies[0].Identities).toEqual([])
  })

  it('sets the permission mode', () => {
    const s = withCompanies(['acme'])
    const next = reducer(s, {type: 'setPermission', name: 'acme', mode: 'plan'})
    expect(next.plan.Companies[0].PermissionMode).toBe('plan')
  })

  it('clamps navigation and clears errors', () => {
    let s = reducer(initialState, {type: 'setErrors', errors: ['boom']})
    expect(s.errors).toEqual(['boom'])
    s = reducer(s, {type: 'back'})
    expect(s.step).toBe(0)
    expect(s.errors).toEqual([])

    let last = s
    for (let i = 0; i < STEPS.length + 3; i++) last = reducer(last, {type: 'next'})
    expect(last.step).toBe(STEPS.length - 1)

    expect(reducer(s, {type: 'goto', step: 99}).step).toBe(STEPS.length - 1)
    expect(reducer(s, {type: 'goto', step: -5}).step).toBe(0)
  })
})
