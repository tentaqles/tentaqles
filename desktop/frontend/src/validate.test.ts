import {describe, expect, it} from 'vitest'
import {validateCompany, validateEmail, validateName, localStepError, needsFullValidation, STEP_WELCOME, STEP_BASE, STEP_COMPANIES, normalizeGitProvider, OTHER_PENDING, GIT_PROVIDERS, NAMED_GIT_PROVIDERS} from './validate'

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

describe('step gating', () => {
  // The bug this exists to prevent: the wizard ran the whole-plan validator on
  // every step, so pressing Next on the Work folder step failed with "plan has
  // no companies" -- about a screen the user had not reached yet -- and there
  // was no way forward. The app was unusable past step 2.
  it('does not judge the whole plan before the Companies step', () => {
    expect(needsFullValidation(STEP_WELCOME)).toBe(false)
    expect(needsFullValidation(STEP_BASE)).toBe(false)
  })

  it('judges the whole plan from the Companies step onward', () => {
    expect(needsFullValidation(STEP_COMPANIES)).toBe(true)
    expect(needsFullValidation(STEP_COMPANIES + 1)).toBe(true)
    expect(needsFullValidation(7)).toBe(true)
  })

  it('still blocks an empty work folder on the step that asks for it', () => {
    expect(localStepError(STEP_BASE, {Base: ''})).toMatch(/work folder/i)
    expect(localStepError(STEP_BASE, {Base: '   '})).toMatch(/work folder/i)
    expect(localStepError(STEP_BASE, {Base: 'C:\repos'})).toBeNull()
  })

  it('asks nothing of the Welcome step, which collects nothing', () => {
    expect(localStepError(STEP_WELCOME, {Base: ''})).toBeNull()
  })
})

describe('git host selection', () => {
  it('treats "Other" with nothing typed as not applicable', () => {
    // Better an empty field than a placeholder that reads like a host name.
    expect(normalizeGitProvider(OTHER_PENDING)).toBe('')
  })

  it('keeps a typed host, trimmed', () => {
    expect(normalizeGitProvider('  gitea.internal  ')).toBe('gitea.internal')
    expect(normalizeGitProvider('github')).toBe('github')
  })

  it('keeps not-applicable empty', () => {
    expect(normalizeGitProvider('')).toBe('')
  })

  it('offers not-applicable and other alongside the named hosts', () => {
    const values = GIT_PROVIDERS.map((p) => p.value)
    expect(values).toContain('')
    expect(values).toContain('other')
    expect(NAMED_GIT_PROVIDERS).toContain('github')
    expect(NAMED_GIT_PROVIDERS).toContain('gitlab')
    expect(NAMED_GIT_PROVIDERS).toContain('azure-devops')
    // control values are not hosts
    expect(NAMED_GIT_PROVIDERS).not.toContain('')
    expect(NAMED_GIT_PROVIDERS).not.toContain('other')
  })
})
