import { useState } from 'react'
import { api, ApiError, type ParticipantView } from '../api'
import { enrollErrorMessage, isSafeRedirect, REFRESH_AFTER } from '../lib/enroll'
import { formatMoney } from '../lib/money'
import { count } from '../lib/plural'
import { authorizationPending, canStartCheckout, enrollmentTone, seatHeld } from '../lib/status'
import { lockedTerms } from '../lib/terms'
import { durationLabel, earlier, formatInZone, formatTimeInZone, windowLabel } from '../lib/time'
import Deadline from './Deadline'
import SeatMeter from './SeatMeter'
import { TermsList } from './Terms'
import { Button, Notice, Tag } from './ui'

interface Props {
  token: string
  view: ParticipantView
  onChange: () => Promise<unknown>
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/**
 * The terms a participant commits to once the organizer fixes a date: when,
 * how much, the minimum and the deadline, the exact consent text and the
 * button that places the hold at PayPal.
 */
export default function CommitmentCard({ token, view, onChange }: Props) {
  const { proposal, workshop, enrollment, consent, tally } = view
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  if (!proposal) return null
  const tz = workshop.timezone
  const pending = authorizationPending(enrollment)
  const canAuthorize = canStartCheckout(proposal.payments_open, enrollment?.status, pending)
  const held = seatHeld(enrollment, proposal.payments_open, Date.now())
  const date = view.dates.find((d) => d.id === proposal.date_id)

  async function authorize() {
    if (!proposal) return
    setBusy(true)
    setError(null)
    let leaving = false
    try {
      let res
      try {
        res = await api.enroll(token, consent.version, proposal.id)
      } catch (e) {
        if (!(e instanceof ApiError) || e.code !== 'checkout_in_progress') throw e
        await sleep(2000)
        res = await api.enroll(token, consent.version, proposal.id)
      }
      if (res.approve_url && isSafeRedirect(res.approve_url)) {
        leaving = true
        window.location.assign(res.approve_url)
        return
      }
      await onChange()
    } catch (e) {
      setError(enrollErrorMessage(e))
      if (e instanceof ApiError && REFRESH_AFTER.has(e.code)) await onChange()
    } finally {
      if (!leaving) setBusy(false)
    }
  }

  return (
    <section aria-labelledby="terms-title" className="rounded-sm border border-rule border-t-4 border-t-ink bg-sheet">
      <div className="flex flex-col gap-6 p-5 sm:p-7">
        <h2 id="terms-title" className="text-2xl font-bold">
          Payment terms
        </h2>

        <div className="grid gap-5 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
          <div className="flex flex-col gap-0.5">
            <p className="text-sm text-ink-soft">Workshop date</p>
            <p className="wide text-xl font-semibold tabular-nums sm:text-2xl">
              <time dateTime={proposal.starts_at}>{date?.label ?? formatInZone(proposal.starts_at, tz)}</time>
            </p>
            <p className="text-ink-soft">{durationLabel(workshop.duration_min)}</p>
          </div>
          <div className="flex flex-col gap-0.5 sm:text-right">
            <p className="text-sm text-ink-soft">Price per seat</p>
            <p className="wide text-4xl font-bold tabular-nums">{formatMoney(proposal.price)}</p>
          </div>
        </div>

        <div className="flex flex-col gap-2 border-t border-rule pt-5">
          <Deadline closesAt={proposal.closes_at} timeZone={tz} open={proposal.payments_open} size="lg" />
          {workshop.payment_window_min < 1440 && (
            <p>
              <Tag tone="hold">{windowLabel(workshop.payment_window_min)}</Tag>
            </p>
          )}
        </div>

        <TermsList
          className="grid-cols-2 sm:max-w-md"
          items={lockedTerms({ timeZone: tz, minimum: proposal.minimum, capacity: proposal.capacity })}
        />

        <div className="flex flex-col gap-2">
          <h3 className="text-base font-bold">Group progress</h3>
          <SeatMeter capacity={tally.capacity} minimum={tally.minimum} paid={tally.paid} authorized={tally.authorized} label="Group progress" />
          <p className="text-sm text-ink-soft">
            {count(tally.interested, 'person', 'people')} interested.
          </p>
        </div>

        <div className="flex flex-col gap-1 border-t border-rule pt-5">
          <h3 className="text-base font-bold">Your status</h3>
          {enrollment ? (
            <>
              <p>
                <Tag tone={enrollmentTone(enrollment.status, { paymentsOpen: proposal.payments_open, seatHeld: held, authPending: pending })}>
                  {enrollment.status_label}
                </Tag>
              </p>
              {enrollment.status === 'SEAT_RESERVED' &&
                (pending ? (
                  <p className="text-sm text-ink-soft">PayPal is reviewing your authorization. Your seat is kept until PayPal decides.</p>
                ) : held ? (
                  <p className="text-sm text-ink-soft">
                    Your seat is held until {formatTimeInZone(earlier(enrollment.seat_expires_at, proposal.closes_at), tz)}.{' '}
                    {enrollment.approval_cancelled_at
                      ? 'You cancelled at PayPal; authorize again to keep it.'
                      : 'Authorize the hold at PayPal to keep it.'}
                  </p>
                ) : (
                  <p className="text-sm text-ink-soft">
                    {proposal.payments_open
                      ? 'Your seat reservation expired. Authorize again to take a seat if one is still free.'
                      : 'Your seat reservation expired. Nothing was charged.'}
                  </p>
                ))}
            </>
          ) : (
            <p className="text-ink-soft">You have not authorized a hold yet.</p>
          )}
        </div>

        <div className="flex flex-col gap-4 border-t border-rule pt-5">
          <h3 className="text-base font-bold">{canAuthorize ? 'What you agree to' : enrollment ? 'What you agreed to' : 'The terms'}</h3>
          <blockquote id="consent-text" className="border-l-2 border-ink pl-4 text-ink">
            <p className="max-w-prose">{consent.text}</p>
          </blockquote>
          {canAuthorize ? (
            <div className="flex flex-col items-start gap-3">
              <p className="text-sm text-ink-soft">Selecting Authorize with PayPal accepts these terms and opens PayPal in this tab.</p>
              <Button onClick={authorize} busy={busy} busyLabel="Opening PayPal…" aria-describedby="consent-text" className="px-6 py-3 text-lg">
                Authorize with PayPal
              </Button>
            </div>
          ) : (
            !proposal.payments_open && <p className="text-ink-soft">Payments are closed for this workshop.</p>
          )}
          {error && (
            <Notice tone="attention" role="alert">
              {error}
            </Notice>
          )}
        </div>
      </div>
    </section>
  )
}
