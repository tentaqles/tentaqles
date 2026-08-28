// Thin typed wrappers over the generated Wails bindings. Every call the UI
// makes goes through here so the screens never import wailsjs directly.
import * as App from '../wailsjs/go/main/App'
import {setupapi} from '../wailsjs/go/models'
import {BrowserOpenURL} from '../wailsjs/runtime'

// The generated models are classes (they carry a convertValues helper), which
// makes them awkward to build and spread in a reducer. The UI works with these
// plain structural equivalents and converts at the binding boundary.
export interface Company {
  Name: string
  DisplayName: string
  Color: string
  GitName: string
  GitEmail: string
  GitUser: string
  Identities: string[]
  PermissionMode: string
}

export interface Plan {
  Base: string
  Companies: Company[]
  Hooks: string[]
  Trust: boolean
}

const asPlan = (p: Plan): setupapi.Plan => p as unknown as setupapi.Plan

export type Provider = setupapi.Provider
export type Change = setupapi.Change
export type Report = setupapi.Report
// ToolResult is declared here rather than imported: the Wails binding
// generator omits it from models.ts (it only appears as a map value type).
export interface ToolResult {
  ID: string
  Command: string
  Path: string
  Version: string
  Err: string
  Installed: boolean
  Hints: string[]
}
export type HookStatus = setupapi.HookStatus
export type Workspace = setupapi.Workspace
export type Finding = setupapi.Finding

export const defaultBase = (): Promise<string> => App.DefaultBase()
export const detectShells = (): Promise<string[]> => App.DetectShells()
export const hooksStatus = (): Promise<HookStatus[]> => App.HooksStatus()
export const existingWorkspaces = (): Promise<Workspace[]> => App.ExistingWorkspaces()
export const providers = (): Promise<Provider[]> => App.Providers()
export const pickFolder = (): Promise<string> => App.PickFolder()
export const tqVersion = (): Promise<string> => App.TQVersion()
export const bundledTQPath = (): Promise<string> => App.BundledTQPath()
export const installTQ = (dest: string): Promise<string> => App.InstallTQ(dest)
export const validatePlan = (plan: Plan): Promise<void> => App.ValidatePlan(asPlan(plan))
export const preview = (plan: Plan): Promise<Change[]> => App.Preview(asPlan(plan))
export const apply = (plan: Plan): Promise<Report> => App.Apply(asPlan(plan))
export const doctor = (): Promise<Finding[]> => App.Doctor()
export const openTerminal = (command: string): Promise<void> => App.OpenTerminal(command)
export const loginCommand = (workspace: string, provider: string): Promise<string> =>
  App.LoginCommand(workspace, provider)
export const toolCheck = (plan: Plan): Promise<Record<string, ToolResult[]>> =>
  App.ToolCheck(asPlan(plan)) as unknown as Promise<Record<string, ToolResult[]>>

export const addCustomProvider = (
  id: string,
  name: string,
  category: string,
  command: string,
  env: Record<string, string>,
): Promise<string> => App.AddCustomProvider(id, name, category, command, env)

// openURL hands a link to the user's default browser.
export const openURL = (url: string): void => BrowserOpenURL(url)

// errorText normalises whatever a rejected binding throws into a string.
export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message
  if (typeof e === 'string') return e
  return String(e)
}
