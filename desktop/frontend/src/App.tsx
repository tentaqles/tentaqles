import * as api from './api'
import {STEPS, StateProvider, usePlan} from './state'
import {Button, ErrorList, Placeholder} from './ui'
import Welcome from './steps/Welcome'
import Base from './steps/Base'
import Companies from './steps/Companies'
import Services from './steps/Services'

function StepBody({step}: {step: number}) {
  switch (step) {
    case 0:
      return <Welcome />
    case 1:
      return <Base />
    case 2:
      return <Companies />
    case 3:
      return <Services />
    default:
      return <Placeholder title={STEPS[step]} />
  }
}

function Shell() {
  const {state, dispatch} = usePlan()

  async function next() {
    try {
      await api.validatePlan(state.plan)
      dispatch({type: 'next'})
    } catch (e) {
      dispatch({type: 'setErrors', errors: [api.errorText(e)]})
    }
  }

  return (
    <div className="flex h-full">
      <nav className="w-56 shrink-0 border-r border-[var(--tq-border)] bg-[var(--tq-panel)] p-4">
        <div className="mb-5 text-sm font-semibold tracking-wide">tq setup</div>
        <ol className="space-y-1">
          {STEPS.map((label, i) => {
            const active = i === state.step
            const done = i < state.step
            return (
              <li key={label}>
                <button
                  onClick={() => dispatch({type: 'goto', step: i})}
                  disabled={i > state.step}
                  className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left disabled:cursor-default disabled:opacity-40 ${
                    active
                      ? 'bg-[var(--tq-accent)]/15 font-medium text-[var(--tq-accent)]'
                      : 'hover:bg-black/5 dark:hover:bg-white/5'
                  }`}
                >
                  <span
                    className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-full border text-[11px] ${
                      active || done
                        ? 'border-[var(--tq-accent)] text-[var(--tq-accent)]'
                        : 'border-[var(--tq-border)] text-[var(--tq-muted)]'
                    }`}
                  >
                    {done ? '✓' : i + 1}
                  </span>
                  {label}
                </button>
              </li>
            )
          })}
        </ol>
      </nav>

      <div className="flex min-w-0 flex-1 flex-col">
        <main className="flex-1 overflow-auto p-8">
          <StepBody step={state.step} />
        </main>
        <footer className="border-t border-[var(--tq-border)] bg-[var(--tq-panel)] px-8 py-4">
          <ErrorList errors={state.errors} />
          <div className="flex justify-between">
            <Button disabled={state.step === 0} onClick={() => dispatch({type: 'back'})}>
              Back
            </Button>
            <Button
              variant="primary"
              disabled={state.step === STEPS.length - 1}
              onClick={next}
            >
              Next
            </Button>
          </div>
        </footer>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <StateProvider>
      <Shell />
    </StateProvider>
  )
}
