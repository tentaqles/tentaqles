import {useEffect, useState} from 'react'
import * as api from './api'
import {boardRows, isLoopbackHubURL, type BoardRow} from './boardview'
import {Badge, Button, Card, StepHeader} from './ui'

// Board opens a workspace's hub board in this window: a running hub is reused, otherwise the
// app starts `tq hub serve` for that workspace. The window itself navigates to the board (the
// hub refuses to be framed); View > Setup (Ctrl/Cmd+1) brings the wizard back.
export default function Board() {
  const [rows, setRows] = useState<BoardRow[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  async function refresh() {
    setLoading(true)
    try {
      const [wss, live] = await Promise.all([api.existingWorkspaces(), api.hubs()])
      setRows(boardRows(wss, live))
      setError('')
    } catch (e) {
      setError(api.errorText(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  async function open(ws: string) {
    setBusy(ws)
    setError('')
    try {
      const target = await api.openBoard(ws)
      if (!isLoopbackHubURL(target.URL)) {
        throw new Error(`refusing to open ${target.URL}: the board must be a hub on this machine`)
      }
      window.location.assign(target.URL)
    } catch (e) {
      setError(api.errorText(e))
      setBusy('')
    }
  }

  return (
    <div className="max-w-3xl">
      <StepHeader
        title="Board"
        subtitle="Open a workspace's board. A running hub is reused; otherwise this app starts one and stops it when it closes. Use View > Setup (Ctrl/Cmd+1) to come back here."
      />
      {error ? (
        <Card className="mb-4">
          <p className="text-[#e0432f]">{error}</p>
        </Card>
      ) : null}
      <Card>
        {loading ? (
          <p className="text-[var(--tq-muted)]">Looking for workspaces and running hubs…</p>
        ) : rows.length === 0 ? (
          <p className="text-[var(--tq-muted)]">
            No workspaces yet. Run the Setup tab first, then come back.
          </p>
        ) : (
          <ul className="divide-y divide-[var(--tq-border)]">
            {rows.map((r) => (
              <li key={r.name} className="flex items-center justify-between py-2">
                <span>
                  {r.name}
                  {r.servedAt ? <Badge tone="accent">running</Badge> : null}
                  {!r.trusted ? <Badge>not trusted</Badge> : null}
                </span>
                <Button
                  variant="primary"
                  disabled={!r.trusted || busy !== ''}
                  title={r.trusted ? '' : `run tq allow ${r.name} in a terminal first`}
                  onClick={() => void open(r.name)}
                >
                  {busy === r.name ? (r.servedAt ? 'Opening…' : 'Starting hub…') : 'Open board'}
                </Button>
              </li>
            ))}
          </ul>
        )}
        <div className="mt-4">
          <Button onClick={() => void refresh()} disabled={loading}>
            Refresh
          </Button>
        </div>
      </Card>
    </div>
  )
}
