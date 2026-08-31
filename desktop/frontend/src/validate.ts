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

// GIT_PROVIDERS are the hosts tq knows how to check an expected_user against.
// The value is what lands in the manifest's git.provider.
//
// '' means not applicable -- plenty of workspaces have a git identity without
// a hosting account to verify -- and 'other' reveals a free-text field, so an
// answer that is not on the list is recorded rather than forced into the
// nearest wrong one.
export const GIT_PROVIDERS: {value: string; label: string}[] = [
  {value: '', label: 'Not applicable'},
  {value: 'github', label: 'GitHub'},
  {value: 'gitlab', label: 'GitLab'},
  {value: 'azure-devops', label: 'Azure DevOps'},
  {value: 'bitbucket', label: 'Bitbucket'},
  {value: 'gitea', label: 'Gitea / Forgejo'},
  {value: 'other', label: 'Other…'},
]

// The values above that are a real, named host rather than a control option.
export const NAMED_GIT_PROVIDERS = GIT_PROVIDERS.map((p) => p.value).filter(
  (v) => v !== '' && v !== 'other',
)

// OTHER_PENDING marks "Other…" chosen but not yet typed. It has to be
// distinguishable from both a real host and from "not applicable", or picking
// Other would snap the select straight back to Not applicable and the text box
// would never appear.
export const OTHER_PENDING = 'other:'

// normalizeGitProvider turns the form's working value into what belongs in the
// manifest. "Other…" chosen but never filled in is the same as not applicable:
// better an empty field than a placeholder that looks like a host name.
export function normalizeGitProvider(v: string): string {
  const t = v.trim()
  if (t === OTHER_PENDING) return ''
  return t
}
