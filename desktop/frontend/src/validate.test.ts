import {describe, expect, it} from 'vitest'
import {validateCompany, validateEmail, validateName} from './validate'

describe('validateName', () => {
  it('accepts lowercase slugs', () => {
    for (const n of ['acme', 'acme-inc', 'acme_inc', 'acme.inc', 'a1', '9lives']) {
      expect(validateName(n, [])).toBeNull()
    }
  })

  it('rejects bad shapes', () => {
    for (const n of ['', 'Acme', 'acme inc', '-acme', '.acme', 'acme!', 'ACME']) {
      expect(validateName(n, [])).not.toBeNull()
    }
  })

  it('rejects duplicates', () => {
    expect(validateName('acme', ['globex', 'acme'])).toMatch(/already exists/)
    expect(validateName('acme', ['globex'])).toBeNull()
  })
})

describe('validateEmail', () => {
  it('requires an @', () => {
    expect(validateEmail('me@acme.test')).toBeNull()
    expect(validateEmail('me-at-acme')).not.toBeNull()
    expect(validateEmail('')).not.toBeNull()
  })
})

describe('validateCompany', () => {
  const ok = {
    Name: 'acme',
    DisplayName: 'Acme',
    Color: '#2a8fd6',
    GitName: 'Renato',
    GitEmail: 'me@acme.test',
    GitUser: '',
    PermissionMode: 'default',
  }

  it('passes a complete form', () => {
    expect(validateCompany(ok, ['globex'])).toEqual({})
  })

  it('reports every bad field at once', () => {
    const errs = validateCompany(
      {...ok, Name: 'Acme Inc', GitEmail: 'nope', GitName: ' ', PermissionMode: 'yolo'},
      [],
    )
    expect(Object.keys(errs).sort()).toEqual([
      'GitEmail',
      'GitName',
      'Name',
      'PermissionMode',
    ])
  })

  it('lets a company keep its own name while editing', () => {
    expect(validateCompany(ok, ['globex'])).toEqual({})
    expect(validateCompany(ok, ['globex', 'acme']).Name).toMatch(/already exists/)
  })
})
