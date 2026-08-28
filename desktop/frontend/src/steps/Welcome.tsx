import {useEffect, useState} from 'react'
import * as api from '../api'
import {Button, Card, StepHeader} from '../ui'
import {usePlan} from '../state'

const CHANGES = [
  'Your work folder is registered with tq as a workspace base.',
  'Git safety is turned on: no commit uses the wrong identity again.',
  'Each company gets a manifest plus its own identity directory.',
  'Your shells get a small hook so the right identity loads per folder.',
]

export default function Welcome() {
  const {dispatch} = usePlan()
  const [version, setVersion] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    api
      .tqVersion()
      .then((v) => live && setVersion(v))
      .catch(() => live && setVersion(''))
    return () => {
      live = false
    }
  }, [])

  return (
    <div className="max-w-2xl">
      <StepHeader
        title="Welcome to tq"
        subtitle="tq keeps every client you work for in its own lane — git identity, cloud logins, and agent permissions switch with the folder you are in."
      />
      <Card>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-[var(--tq-muted)]">
          What this setup will change
        </h2>
        <ul className="mt-3 list-disc space-y-1.5 pl-5">
          {CHANGES.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
        <p className="mt-4 text-sm text-[var(--tq-muted)]">
          Nothing is written until you review the preview at the end.
        </p>
      </Card>

      <Card className="mt-4">
        {version === null ? (
          <p className="text-[var(--tq-muted)]">Looking for the tq CLI…</p>
        ) : version === '' ? (
          <div>
            <p className="font-medium">tq is not installed yet.</p>
            <p className="mt-1 text-sm text-[var(--tq-muted)]">
              This app bundles a copy of the CLI and can install it for you on the Tools step.
            </p>
          </div>
        ) : (
          <p>
            tq CLI detected: <span className="font-mono">{version}</span>
          </p>
        )}
      </Card>

      <div className="mt-5">
        <Button variant="primary" onClick={() => dispatch({type: 'next'})}>
          Continue
        </Button>
      </div>
    </div>
  )
}
