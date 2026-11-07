import { useState } from 'react'
import { api, ApiError, type Enrollment, type OwnerView } from '../api'
import { operatorActions, type OperatorAction } from '../lib/actions'
import { Button, Notice } from './ui'

interface Props {
  token: string
  view: OwnerView
  onChange: () => Promise<unknown>
}

const LABELS: Record<OperatorAction, { idle: string; busy: string }> = {
  reconcile: { idle: 'Check with PayPal', busy: 'Checking…' },
  retry_refund: { idle: 'Retry refund', busy: 'Asking PayPal…' },
  clear: { idle: 'Mark resolved', busy: 'Clearing…' },
}

export default function OperatorPanel({ token, view, onChange }: Props) {
  const [busy, setBusy] = useState<string | null>(null)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  const seats = view.enrollments.map((e) => ({ e, actions: operatorActions(e) })).filter((x) => x.actions.length > 0)
  const workshopFlagged = view.workshop.resolution === 'NEEDS_ATTENTION'
  if (seats.length === 0 && !workshopFlagged) return null

  async function run(key: string, action: () => Promise<unknown>, done: string) {
    setBusy(key)
    setResult(null)
    try {
      await action()
      setResult({ ok: true, text: done })
      await onChange()
    } catch (e) {
      const text =
        e instanceof ApiError && e.code === 'still_unresolved'
          ? 'Something is still in flight or unresolved. Check with PayPal first, then mark it resolved.'
          : e instanceof Error
            ? e.message
            : 'The action did not apply.'
      setResult({ ok: false, text })
    } finally {
      setBusy(null)
    }
  }

  function act(e: Enrollment, a: OperatorAction) {
    const who = e.display_name ?? 'this seat'
    switch (a) {
      case 'reconcile':
        return run(e.id + a, () => api.operatorReconcile(token, e.id), `Read ${who}'s payment back from PayPal.`)
      case 'retry_refund':
        return run(e.id + a, () => api.operatorRetryRefund(token, e.id), `Asked PayPal again to refund ${who}.`)
      case 'clear':
        return run(e.id + a, () => api.operatorClearFlag(token, e.id), `${who} no longer needs a check.`)
    }
  }

  return (
    <section aria-labelledby="operator-title" className="flex flex-col gap-4 rounded-tile bg-attention-wash p-5 ring-1 ring-attention/30 sm:p-6">
      <h2 id="operator-title" className="text-2xl text-attention">
        Needs your check
      </h2>
      <p className="max-w-prose">
        PayPal did not settle these on its own. Check with PayPal reads the payment back; Retry refund asks PayPal again with a new request;
        Mark resolved clears the flag once nothing is in flight.
      </p>
      {seats.length > 0 && (
        <ul className="flex flex-col">
          {seats.map(({ e, actions }) => (
            <li key={e.id} className="flex flex-col gap-2 border-t border-line py-3 sm:flex-row sm:items-center sm:gap-4">
              <p className="font-semibold sm:w-40 sm:shrink-0">{e.display_name}</p>
              <p className="flex-1 text-sm">{e.status_label}</p>
              <div className="flex flex-wrap gap-2">
                {actions.map((a) => (
                  <Button
                    key={a}
                    variant="secondary"
                    onClick={() => act(e, a)}
                    busy={busy === e.id + a}
                    busyLabel={LABELS[a].busy}
                    disabled={busy !== null && busy !== e.id + a}
                    size="sm"
                  >
                    {LABELS[a].idle}
                  </Button>
                ))}
              </div>
            </li>
          ))}
        </ul>
      )}
      {workshopFlagged && (
        <div className="flex flex-col gap-2 border-t border-line pt-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-sm">The workshop itself is flagged for a check of one of its payment actions.</p>
          <Button
            variant="secondary"
            onClick={() => run('workshop', () => api.operatorClearFlag(token), 'The workshop no longer needs a check.')}
            busy={busy === 'workshop'}
            busyLabel={LABELS.clear.busy}
            disabled={busy !== null && busy !== 'workshop'}
            className="self-start px-3 py-1.5 text-sm"
          >
            Mark workshop resolved
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
