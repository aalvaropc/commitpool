import { useState } from 'react'
import { api, type OwnerView } from '../api'
import { canSkipToLastHours } from '../lib/extend'
import { Button, DemoTag, Notice } from './ui'

interface Props {
  token: string
  view: OwnerView
  onChange: () => Promise<unknown>
}

export default function DemoClock({ token, view, onChange }: Props) {
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  const p = view.proposal
  if (!view.demo.demo_mode || view.workshop.status !== 'FUNDING' || !p?.payments_open) return null
  const canSkip = canSkipToLastHours(p.closes_at, Date.now())
  if (!canSkip && !result) return null

  async function skip() {
    setBusy(true)
    setResult(null)
    try {
      await api.demoSkipToLastHours(token)
      setResult({ ok: true, text: 'Reservations now close in 2 hours. If the class is still short, Claude suggests a rescue above.' })
      await onChange()
    } catch (e) {
      setResult({ ok: false, text: e instanceof Error ? e.message : 'The demo clock did not move.' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <section aria-labelledby="demo-clock-title" className="flex flex-col gap-3 rounded-control border border-dashed border-line-strong p-4">
      <h3 id="demo-clock-title" className="text-base">
        Demo clock
      </h3>
      <p className="text-sm text-ink-soft">Accelerated clock: moves the deadline to 2 hours from now, as if the class had stayed short until its last hours.</p>
      {canSkip && (
        <div>
          <Button variant="demo" onClick={() => void skip()} busy={busy} busyLabel="Moving the clock…">
            <DemoTag /> Skip to the last 2 hours
          </Button>
        </div>
      )}
      {result && (
        <Notice tone={result.ok ? 'neutral' : 'attention'} role={result.ok ? 'status' : 'alert'}>
          {result.text}
        </Notice>
      )}
    </section>
  )
}
