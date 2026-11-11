import type { OwnerView } from '../api'
import { demoAction, demoControl } from '../lib/demo'
import { DEMO_ACTIONS, useDemoAction } from '../lib/demoActions'
import { Button, DemoTag, Notice } from './ui'

interface Props {
  token: string
  view: OwnerView
  onChange: () => Promise<unknown>
}

export default function DemoControls({ token, view, onChange }: Props) {
  const { busy, result, run } = useDemoAction(token, onChange)
  const action = demoControl(view)
  if (!action || demoAction(view)) return null
  const copy = DEMO_ACTIONS[action]

  return (
    <section aria-labelledby="demo-title" className="flex flex-col gap-3 rounded-control border border-dashed border-line-strong p-4">
      <h3 id="demo-title" className="text-base">
        Demo controls
      </h3>
      {action === 'close' ? (
        <p className="text-sm text-ink-soft">This server runs in demo mode. Closing now ends reservations as if the deadline had passed.</p>
      ) : (
        <p className="text-sm text-ink-soft">Demo clock: jumps ahead to the class start, so CommitPool pays you out now instead of when the class begins.</p>
      )}
      <div>
        <Button variant="demo" onClick={() => void run(action)} busy={busy} busyLabel={copy.busy}>
          <DemoTag /> {copy.label}
        </Button>
      </div>
      {result && result.action === action && (
        <Notice tone={result.ok ? 'neutral' : 'attention'} role={result.ok ? 'status' : 'alert'}>
          {result.text}
        </Notice>
      )}
    </section>
  )
}
