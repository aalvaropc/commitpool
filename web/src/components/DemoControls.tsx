import { useState } from 'react'
import { api, type OwnerView } from '../api'
import { canStartClass } from '../lib/payout'
import { Button, DemoTag, Notice } from './ui'

interface Props {
  token: string
  view: OwnerView
  onChange: () => Promise<unknown>
}

export default function DemoControls({ token, view, onChange }: Props) {
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string; closing: boolean } | null>(null)
  const closing = view.demo.demo_mode && Boolean(view.proposal?.payments_open)
  const starting = canStartClass(view)
  if (!closing && !starting) return null

  async function run(action: () => Promise<unknown>, done: string) {
    setBusy(true)
    setResult(null)
    try {
      await action()
      setResult({ ok: true, text: done, closing })
      await onChange()
    } catch (e) {
      setResult({ ok: false, text: e instanceof Error ? e.message : 'The demo control did not apply.', closing })
    } finally {
      setBusy(false)
    }
  }

  return (
    <section aria-labelledby="demo-title" className="flex flex-col gap-3 rounded-control border border-dashed border-line-strong p-4">
      <h3 id="demo-title" className="text-base">
        Demo controls
      </h3>
      {closing ? (
        <>
          <p className="text-sm text-ink-soft">This server runs in demo mode. Closing now ends reservations as if the deadline had passed.</p>
          <div>
            <Button variant="demo" onClick={() => void run(() => api.demoCloseNow(token), 'Reservations closed. The outcome appears above in a few seconds.')} busy={busy} busyLabel="Closing reservations…">
              <DemoTag /> Close reservations now
            </Button>
          </div>
        </>
      ) : (
        <>
          <p className="text-sm text-ink-soft">Demo clock: jumps ahead to the class start, so CommitPool pays you out now instead of when the class begins.</p>
          <div>
            <Button variant="demo" onClick={() => void run(() => api.demoStartClass(token), 'The class has started. Your payout is on its way.')} busy={busy} busyLabel="Starting the class…">
              <DemoTag /> Start the class now
            </Button>
          </div>
        </>
      )}
      {result && result.closing === closing && (
        <Notice tone={result.ok ? 'neutral' : 'attention'} role={result.ok ? 'status' : 'alert'}>
          {result.text}
        </Notice>
      )}
    </section>
  )
}
