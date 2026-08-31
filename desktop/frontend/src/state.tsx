import React, {createContext, useContext, useEffect, useMemo, useReducer} from 'react'
import type {Company, Plan, Report} from './api'
import * as api from './api'

export const STEPS = [
  'Welcome',
  'Work folder',
  'Companies',
  'Services',
  'Tools',
  'Bundles',
  'Preview',
  'Log in',
] as const

export interface State {
  plan: Plan
  step: number
  errors: string[]
  report?: Report
}

export type Action =
  | {type: 'setBase'; base: string}
  | {type: 'setHooks'; hooks: string[]}
  | {type: 'addCompany'; company: Company}
  | {type: 'updateCompany'; name: string; company: Company}
  | {type: 'removeCompany'; name: string}
  | {type: 'setIdentities'; name: string; identities: string[]}
  | {type: 'setPermission'; name: string; mode: string}
  | {type: 'next'}
  | {type: 'back'}
  | {type: 'goto'; step: number}
  | {type: 'setErrors'; errors: string[]}
  | {type: 'setReport'; report: Report}

export function emptyPlan(): Plan {
  return {Base: '', Companies: [], Hooks: [], Trust: true}
}

export function newCompany(name = ''): Company {
  return {
    Name: name,
    DisplayName: '',
    Color: '#2a8fd6',
    GitName: '',
    GitEmail: '',
    GitUser: '',
    GitProvider: '',
    Identities: [],
    PermissionMode: 'default',
  }
}

export const initialState: State = {plan: emptyPlan(), step: 0, errors: []}

function mapCompany(
  plan: Plan,
  name: string,
  fn: (c: Company) => Company,
): Plan {
  return {...plan, Companies: plan.Companies.map((c) => (c.Name === name ? fn(c) : c))}
}

export function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'setBase':
      return {...state, plan: {...state.plan, Base: action.base}}
    case 'setHooks':
      return {...state, plan: {...state.plan, Hooks: action.hooks}}
    case 'addCompany':
      if (state.plan.Companies.some((c) => c.Name === action.company.Name)) return state
      return {
        ...state,
        plan: {...state.plan, Companies: [...state.plan.Companies, action.company]},
      }
    case 'updateCompany':
      return {...state, plan: mapCompany(state.plan, action.name, () => action.company)}
    case 'removeCompany':
      return {
        ...state,
        plan: {
          ...state.plan,
          Companies: state.plan.Companies.filter((c) => c.Name !== action.name),
        },
      }
    case 'setIdentities':
      return {
        ...state,
        plan: mapCompany(state.plan, action.name, (c) => ({...c, Identities: action.identities})),
      }
    case 'setPermission':
      return {
        ...state,
        plan: mapCompany(state.plan, action.name, (c) => ({...c, PermissionMode: action.mode})),
      }
    case 'next':
      return {...state, step: Math.min(state.step + 1, STEPS.length - 1), errors: []}
    case 'back':
      return {...state, step: Math.max(state.step - 1, 0), errors: []}
    case 'goto':
      return {...state, step: Math.min(Math.max(action.step, 0), STEPS.length - 1), errors: []}
    case 'setErrors':
      return {...state, errors: action.errors}
    case 'setReport':
      return {...state, report: action.report}
    default:
      return state
  }
}

interface Ctx {
  state: State
  dispatch: React.Dispatch<Action>
}

const StateContext = createContext<Ctx | null>(null)

export function StateProvider({children}: {children: React.ReactNode}) {
  const [state, dispatch] = useReducer(reducer, initialState)

  // Seed the plan from the backend: default base + detected shells.
  useEffect(() => {
    let live = true
    api
      .defaultBase()
      .then((base) => live && base && dispatch({type: 'setBase', base}))
      .catch(() => undefined)
    api
      .detectShells()
      .then((shells) => live && shells && dispatch({type: 'setHooks', hooks: shells}))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [])

  const value = useMemo(() => ({state, dispatch}), [state])
  return <StateContext.Provider value={value}>{children}</StateContext.Provider>
}

export function usePlan(): Ctx {
  const ctx = useContext(StateContext)
  if (!ctx) throw new Error('usePlan must be used inside <StateProvider>')
  return ctx
}
