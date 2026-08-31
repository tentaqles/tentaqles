import {useEffect, useState} from 'react'
import * as api from './api'
import {STEPS, StateProvider, usePlan} from './state'
import {Button, ErrorList, Placeholder} from './ui'
import Welcome from './steps/Welcome'
import Base from './steps/Base'
import Companies from './steps/Companies'
import Services from './steps/Services'
import Tools from './steps/Tools'
import Bundles from './steps/Bundles'
import Preview from './steps/Preview'
import Logins from './steps/Logins'

const CURL = 'curl -fsSL https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.sh | sh'
const IRM = 'irm https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.ps1 | iex'

// InstallBanner shows on first run when no tq binary is on PATH. It installs
// the binary bundled next to the app when there is one, and otherwise falls
// back to the published one-liners.
function InstallBanner() {
  const [version, setVersion] = useState<string | null>(null)
  const [bundled, setBundled] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [probeError, setProbeError] = useState('')

  async function check(cancelled: () => boolean) {
    try {
      const v = await api.tqVersion()
      if (!cancelled()) {
        setVersion(v)
        setProbeError('')
      }
    } catch (e) {
      // tq was found and would not run. Reporting that as "not installed"
      // sends someone off to install what they already have, so the banner
      // says what actually went wrong instead.
      if (!cancelled()) {
        setVersion('')
        setProbeError(api.errorText(e))
      }
    }
    try {
      const b = await api.bundledTQPath()
      if (!cancelled()) setBundled(b)
    } catch {
      if (!cancelled()) setBundled('')
    }
  }

  useEffect(() => {
    let cancelled = false
    void check(() => cancelled)
    return () => {
      cancelled = true
    }
  }, [])

  async function install() {
    setBusy(true)
    try {
      await api.installTQ(bundled)
      setVersion(await api.tqVersion())
      setError('')
    } catch (e) {
      setError(api.errorText(e))
    } finally {
      setBusy(false)
    }
  }

  if (version === null || version !== '') return null

  return (
    <div className="border-b border-[#f28c28] bg-[#f28c28]/10 px-8 py-3 text-sm">
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-medium text-[#f28c28]">
          {probeError ? 'tq could not be run' : 'tq is not installed'}
        </span>
        {probeError ? (
          <span className="text-[var(--tq-muted)]">{probeError}</span>
        ) : bundled ? (
          <Button onClick={() => void install()} disabled={busy}>
            {busy ? 'Installing…' : 'Install tq'}
          </Button>
        ) : (
          <span className="text-[var(--tq-muted)]">
            bundled binary not found — install via{' '}
            <code className="select-all">{CURL}</code> or{' '}
            <code className="select-all">{IRM}</code>
          </span>
        )}
      </div>
      {error ? <p className="mt-2 text-[#e0432f]">{error}</p> : null}
    </div>
  )
}

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
    case 4:
      return <Tools />
    case 5:
      return <Bundles />
    case 6:
      return <Preview />
    case 7:
      return <Logins />
    default:
      return <Placeholder title={STEPS[step]} />
  }
}

function Shell() {
  const {state, dispatch} = usePlan()

  async function next() {
    // The Welcome step collects nothing, so there is nothing to validate yet —
    // validating there would surface errors about fields the user hasn't seen.
    if (state.step === 0) {
      dispatch({type: 'setErrors', errors: []})
      dispatch({type: 'next'})
      return
    }
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
                  aria-current={active ? 'step' : undefined}
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
        <InstallBanner />
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
