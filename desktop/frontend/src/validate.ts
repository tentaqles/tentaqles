// Pure form-validation helpers, shared by the Companies screen and its tests.

export const NAME_RE = /^[a-z0-9][a-z0-9._-]*$/

export const COLORS = [
  '#e0432f',
  '#f28c28',
  '#e5c229',
  '#2fa84f',
  '#2a8fd6',
  '#7a4fd0',
  '#888',
]

export const PERMISSION_MODES = ['default', 'acceptEdits', 'plan', 'bypass']

export const CATEGORIES = ['cloud', 'vcs', 'data', 'deploy', 'pm', 'agent', 'other']

export function validateName(name: string, taken: string[]): string | null {
  if (name.trim() === '') return 'Name is required'
  if (!NAME_RE.test(name)) {
    return 'Use lowercase letters, digits, and . _ - (must start with a letter or digit)'
  }
  if (taken.includes(name)) return `A company named "${name}" already exists`
  return null
}

export function validateEmail(email: string): string | null {
  if (email.trim() === '') return 'Git email is required'
  if (!email.includes('@')) return 'Git email must contain "@"'
  return null
}

export interface CompanyForm {
  Name: string
  DisplayName: string
  Color: string
  GitName: string
  GitEmail: string
  GitUser: string
  PermissionMode: string
}

// validateCompany returns a field -> message map; empty means valid.
// takenNames must exclude the company being edited.
export function validateCompany(
  form: CompanyForm,
  takenNames: string[],
): Record<string, string> {
  const errors: Record<string, string> = {}
  const nameErr = validateName(form.Name, takenNames)
  if (nameErr) errors.Name = nameErr
  const emailErr = validateEmail(form.GitEmail)
  if (emailErr) errors.GitEmail = emailErr
  if (form.GitName.trim() === '') errors.GitName = 'Git name is required'
  if (!PERMISSION_MODES.includes(form.PermissionMode)) {
    errors.PermissionMode = 'Unknown permission mode'
  }
  return errors
}

// Step indices, mirroring STEPS in state.tsx.
export const STEP_WELCOME = 0
export const STEP_BASE = 1
export const STEP_COMPANIES = 2

// localStepError checks only what the CURRENT step is responsible for.
//
// A wizard must never refuse to advance because of something a LATER step
// collects. The full plan validator quite correctly rejects a plan with no
// companies -- but on the Work folder step there are no companies yet, because
// the Companies step comes next. Running it there blocks the user on step 2
// with "plan has no companies" and no way to reach the screen that would add
// one.
export function localStepError(
  step: number,
  plan: {Base: string},
): string | null {
  if (step === STEP_BASE && plan.Base.trim() === '') {
    return 'Choose a work folder to continue'
  }
  return null
}

// needsFullValidation reports whether the whole plan can be judged yet. From
// the Companies step onward it can: "no companies" is a real error to show
// someone standing on the Companies screen.
export function needsFullValidation(step: number): boolean {
  return step >= STEP_COMPANIES
}
