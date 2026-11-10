import { useState } from 'react'
import { api, type Enrollment, type OwnerView, type VerifyResult } from '../api'
import { moneySummary, verifyDifferences, verifyHeadline, verifySkipped } from '../lib/payments'
import { authorizationPending, enrollmentTone, reservedSeatNote, seatHeld } from '../lib/status'
import { earlier, formatClock } from '../lib/time'
import PayPalRecord from './PayPalRecord'
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

const atPayPal = (e: Enrollment) => Boolean(e.payment && (e.payment.order_id || e.payment.authorization_id || e.payment.capture_id || e.payment.refund_id))

function VerifyOutcome({ result, timeZone }: { result: VerifyResult; timeZone: string }) {
  const differences = verifyDifferences(result)
  const skipped = verifySkipped(result)
  const { records, matching } = result.summary
  const tone = records === 0 ? 'neutral' : matching === records ? 'paid' : 'attention'
  return (
    <Notice tone={tone} role={tone === 'attention' ? 'alert' : 'status'}>
      <p>{verifyHeadline(result, timeZone)}</p>
      {skipped && <p className="mt-1 text-sm text-ink">{skipped}</p>}
      {differences.length > 0 && (
        <ul className="mt-2 flex flex-col gap-1.5 text-sm text-ink">
          {differences.map((d) => (
            <li key={d.key} className="[overflow-wrap:anywhere]">
              {d.text}
              {d.debugId && (
                <>
                  {' '}
                  PayPal debug ID <code className="font-mono">{d.debugId}</code>.
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </Notice>
  )
}

export default function EnrollmentList({ token, view, onChange }: Props) {
  const [busy, setBusy] = useState<string | null>(null)
  const [result, setResult] = useState<{ tone: 'paid' | 'attention'; text: string; enrollment: string; status: string } | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [verified, setVerified] = useState<VerifyResult | null>(null)
  const [verifyError, setVerifyError] = useState<string | null>(null)
  const tz = view.workshop.timezone
  const mocks = view.demo.sandbox_mocks
  const summary = view.proposal ? moneySummary(view.enrollments, view.proposal.price) : null
  const verifiable = view.enrollments.some(atPayPal)

  async function verify() {
    setVerifying(true)
    setVerifyError(null)
    try {
      setVerified(await api.verifyWithPayPal(token))
    } catch (err) {
      setVerifyError(err instanceof Error ? err.message : 'PayPal could not be checked right now.')
    } finally {
      setVerifying(false)
    }
  }

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
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="enrollments-title" className="text-2xl">
          Payments
        </h2>
        {verifiable && (
          <Button variant="secondary" size="sm" onClick={() => void verify()} busy={verifying} busyLabel="Verifying…">
            Verify with PayPal
          </Button>
        )}
      </div>
      {summary && <p className="font-semibold tabular-nums">{summary}</p>}
      {verifiable && <p className="text-sm text-ink-soft">Verify with PayPal reads each spot back from PayPal and compares it with these records. It changes nothing.</p>}
      {verifyError && (
        <Notice tone="attention" role="alert">
          {verifyError}
        </Notice>
      )}
      {verified && <VerifyOutcome result={verified} timeZone={tz} />}
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
                      {e.payment && <PayPalRecord payment={e.payment} who={e.display_name ?? 'this spot'} />}
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
