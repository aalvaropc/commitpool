import { useState } from 'react'
import { api, type OwnerView } from '../api'
import { Button, DemoTag, Notice } from './ui'

interface Props {
  token: string
  view: OwnerView
  onChange: () => Promise<unknown>
}

export default function DemoControls({ token, view, onChange }: Props) {
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  if (!view.demo.demo_mode || !view.proposal?.payments_open) return null

  async function closeNow() {
    setBusy(true)
    setResult(null)
    try {
      await api.demoCloseNow(token)
      setResult({ ok: true, text: 'Payments closed. The outcome appears above in a few seconds.' })
      await onChange()
    } catch (e) {
      setResult({ ok: false, text: e instanceof Error ? e.message : 'The demo control did not apply.' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <section aria-labelledby="demo-title" className="flex flex-col gap-3 rounded-sm border border-dashed border-edge p-4">
      <h2 id="demo-title" className="text-base font-bold">
        Demo controls
      </h2>
      <p className="text-sm text-ink-soft">This server runs in demo mode. Closing now ends the payment window as if the deadline had passed.</p>
      <div>
        <Button variant="demo" onClick={closeNow} busy={busy} busyLabel="Closing payments…">
          <DemoTag /> Close payments now
        </Button>
      </div>
      {result && (
        <Notice tone={result.ok ? 'neutral' : 'attention'} role={result.ok ? 'status' : 'alert'}>
          {result.text}
        </Notice>
      )}
    </section>
  )
}
