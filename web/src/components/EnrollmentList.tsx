import { useState } from 'react'
import { api, type Enrollment, type OwnerView } from '../api'
import { authorizationPending, enrollmentTone, reservedSeatNote, seatHeld } from '../lib/status'
import { earlier, formatClock } from '../lib/time'
import { Avatar, Button, DemoTag, Notice, Pill, Tile } from './ui'

interface Props {
  token: string
  view: OwnerView
  onChange: () => Promise<unknown>
}

const human = (s: string) => s.charAt(0) + s.slice(1).toLowerCase().replaceAll('_', ' ')

function paymentLine(e: Enrollment): string | null {
  const p = e.payment
  if (!p) return null
  const parts: string[] = []
  if (p.order_id) parts.push(`PayPal order ${p.order_id}${p.order_status ? ` (${human(p.order_status)})` : ''}`)
  if (p.authorization_status) parts.push(`hold ${human(p.authorization_status)}`)
  if (p.capture_status) parts.push(`charge ${human(p.capture_status)}`)
  if (p.refund_status) parts.push(`refund ${human(p.refund_status)}`)
  return parts.length ? parts.join(', ') : null
}

export default function EnrollmentList({ token, view, onChange }: Props) {
  const [busy, setBusy] = useState<string | null>(null)
  const [result, setResult] = useState<{ tone: 'paid' | 'attention'; text: string; enrollment: string; status: string } | null>(null)
  const tz = view.workshop.timezone
  const mocks = view.demo.sandbox_mocks

  async function failNext(e: Enrollment) {
    setBusy(e.id)
    setResult(null)
    try {
      await api.demoFailNextCapture(token, e.id)
      setResult({ tone: 'paid', text: `The next charge for ${e.display_name ?? 'this seat'} will fail.`, enrollment: e.id, status: e.status })
      await onChange()
    } catch (err) {
      setResult({ tone: 'attention', text: err instanceof Error ? err.message : 'The demo control did not apply.', enrollment: e.id, status: e.status })
    } finally {
      setBusy(null)
    }
  }

  return (
    <Tile as="section" aria-labelledby="enrollments-title" className="flex flex-col gap-4 sm:p-6">
      <h2 id="enrollments-title" className="text-2xl">
        Payments
      </h2>
      {view.enrollments.length === 0 ? (
        <p className="text-ink-soft">No one has reserved a spot yet.</p>
      ) : (
        <ul className="flex flex-col">
          {view.enrollments.map((e) => {
            const line = paymentLine(e)
            const open = view.proposal?.payments_open ?? false
            const held = seatHeld(e, open, Date.now())
            const pending = authorizationPending(e)
            const canFail = mocks && (e.status === 'SEAT_RESERVED' || e.status === 'AUTHORIZED') && e.payment && !e.payment.capture_status
            return (
              <li key={e.id} className="flex flex-col gap-2 border-t border-line py-3.5 first:border-t-0 sm:flex-row sm:items-start sm:gap-4">
                <p className="flex min-w-0 items-center gap-2.5 font-semibold sm:w-44 sm:shrink-0">
                  <Avatar name={e.display_name ?? '?'} size="sm" />
                  <span className="min-w-0 [overflow-wrap:anywhere]">{e.display_name}</span>
                </p>
                <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                  <p className="flex flex-wrap items-center gap-2">
                    <Pill tone={enrollmentTone(e.status, { paymentsOpen: open, seatHeld: held, authPending: pending })}>{e.status_label}</Pill>
                  </p>
                  {line && (
                    <details className="text-sm text-ink-soft">
                      <summary className="-my-2 w-fit cursor-pointer rounded-sm py-3 font-medium text-ink-soft hover:text-ink">PayPal details</summary>
                      <p className="mt-1 break-words">
                        {line} · {human(e.status)}
                      </p>
                    </details>
                  )}
                  {e.payment?.capture_issue && <p className="text-sm font-medium text-attention">PayPal answered {e.payment.capture_issue}.</p>}
                  {e.status === 'SEAT_RESERVED' && (
                    <p className="text-sm text-ink-soft">
                      {reservedSeatNote({
                        authPending: pending,
                        held,
                        until: formatClock(view.proposal ? earlier(e.seat_expires_at, view.proposal.closes_at) : e.seat_expires_at, tz),
                      })}
                      {e.approval_cancelled_at ? '; the participant cancelled at PayPal once.' : '.'}
                    </p>
                  )}
                </div>
                {canFail && (
                  <Button variant="demo" size="sm" onClick={() => failNext(e)} busy={busy === e.id} busyLabel="Applying…" className="self-start">
                    <DemoTag /> Fail next charge
                  </Button>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {result && view.enrollments.some((e) => e.id === result.enrollment && e.status === result.status) && (
        <Notice tone={result.tone} role={result.tone === 'attention' ? 'alert' : 'status'}>
          {result.text}
        </Notice>
      )}
    </Tile>
  )
}
