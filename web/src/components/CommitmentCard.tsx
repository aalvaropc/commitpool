import { useState } from 'react'
import { api, ApiError, type ParticipantView } from '../api'
import { withdrawKind, type WithdrawKind } from '../lib/actions'
import { enrollErrorMessage, isSafeRedirect, REFRESH_AFTER } from '../lib/enroll'
import { formatMoney } from '../lib/money'
import { authorizationPending, canStartCheckout, enrollmentTone, seatHeld } from '../lib/status'
import { durationLabel, earlier, formatClock, formatDateRange, formatMoment, windowLabel, zoneName } from '../lib/time'
import { offersCalendar } from '../lib/event'
import { latestDeadline } from '../lib/extend'
import AddToCalendar from './AddToCalendar'
import Deadline from './Deadline'
import { Button, ConfirmDialog, Icon, Notice, Pill, Tile, TileLabel } from './ui'

interface Props {
  token: string
  view: ParticipantView
  onChange: () => Promise<unknown>
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

export default function CommitmentCard({ token, view, onChange }: Props) {
  const { proposal, workshop, enrollment, consent } = view
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [leaving, setLeaving] = useState<WithdrawKind | null>(null)
  const [withdrawn, setWithdrawn] = useState<string | null>(null)
  if (!proposal) return null
  const tz = workshop.timezone
  const pending = authorizationPending(enrollment)
  const canAuthorize = canStartCheckout(proposal.payments_open, enrollment?.status, pending)
  const held = seatHeld(enrollment, proposal.payments_open, Date.now())
  const date = view.dates.find((d) => d.id === proposal.date_id)
  const kind = withdrawKind(enrollment, workshop.status)
  const canWithdraw = kind === 'void' || (kind === 'release' && held)
  const latest = latestDeadline(proposal, workshop.status)

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

  async function withdraw() {
    const was = leaving
    setBusy(true)
    setError(null)
    try {
      await api.withdraw(token)
      setLeaving(null)
      setWithdrawn(was === 'void' ? 'You left. PayPal releases your hold; you are not charged.' : 'You gave up your spot. Nothing was held at PayPal.')
      await onChange()
    } catch (e) {
      setLeaving(null)
      setError(e instanceof Error ? e.message : 'You could not leave right now.')
      if (e instanceof ApiError) await onChange()
    } finally {
      setBusy(false)
    }
  }

  return (
    <Tile as="section" aria-labelledby="terms-title" className="flex flex-col gap-5 sm:p-7">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <h2 id="terms-title" className="text-2xl">
          Your reservation
        </h2>
        {enrollment && (
          <Pill tone={enrollmentTone(enrollment.status, { paymentsOpen: proposal.payments_open, seatHeld: held, authPending: pending })}>
            {enrollment.status_label}
          </Pill>
        )}
      </div>

      <div className="grid gap-4 rounded-control bg-surface-muted p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
        <div className="flex flex-col gap-1">
          <TileLabel>Date</TileLabel>
          <p className="font-display text-xl font-bold tabular-nums sm:text-2xl">
            <time dateTime={proposal.starts_at}>{date ? formatDateRange(date.starts_at, date.ends_at, tz) : formatMoment(proposal.starts_at, tz)}</time>
          </p>
          <p className="text-sm text-ink-soft">
            {durationLabel(workshop.duration_min)} · times in {zoneName(tz)}
          </p>
          <div className="mt-2">
            {offersCalendar(workshop.status, enrollment) && <AddToCalendar path={`/api/v1/p/${encodeURIComponent(token)}/calendar.ics`} />}
          </div>
        </div>
        <div className="flex flex-col gap-1 sm:text-right">
          <TileLabel>Price</TileLabel>
          <p className="font-display text-4xl font-bold tracking-tight tabular-nums">{formatMoney(proposal.price)}</p>
        </div>
      </div>

      <div className="flex flex-wrap items-end justify-between gap-3">
        <Deadline closesAt={proposal.closes_at} timeZone={tz} open={proposal.payments_open} size="lg" />
        {workshop.payment_window_min < 1440 && <Pill tone="attention">{windowLabel(workshop.payment_window_min)}</Pill>}
      </div>
      {latest && (
        <p className="-mt-3 text-sm text-ink-soft">
          Latest possible deadline:{' '}
          <time dateTime={latest} className="font-semibold text-ink tabular-nums">
            {formatMoment(latest, tz)}
          </time>
          {proposal.extended_from ? '. It was already extended once.' : '. The organizer can extend the deadline once, up to then.'}
        </p>
      )}

      {enrollment?.status === 'SEAT_RESERVED' &&
        (pending ? (
          <Notice tone="hold">PayPal is reviewing your hold. Your spot is kept until PayPal decides.</Notice>
        ) : held ? (
          <Notice tone="hold">
            Your spot is held until {formatClock(earlier(enrollment.seat_expires_at, proposal.closes_at), tz)}.{' '}
            {enrollment.approval_cancelled_at ? 'You cancelled at PayPal; reserve again to keep it.' : 'Finish at PayPal to keep it.'}
          </Notice>
        ) : (
          <Notice>
            {proposal.payments_open ? 'Your reservation expired. Reserve again to take a spot if one is still free.' : 'Your reservation expired. Nothing was charged.'}
          </Notice>
        ))}
      {!enrollment && proposal.payments_open && <p className="text-ink-soft">You haven’t reserved yet.</p>}
      {enrollment?.status === 'PAID' && <Notice tone="paid">You’re paid and in. Your payment is refunded only if the workshop is cancelled.</Notice>}

      <div className="flex flex-col gap-3">
        <h3 className="flex items-center gap-2 text-base">
          <Icon name="lock" className="text-brand" />
          {canAuthorize ? 'What you agree to' : 'The terms'}
        </h3>
        <div id="consent-text" className="rounded-control bg-surface-muted p-4 text-[0.9375rem] leading-relaxed text-ink">
          <p className="max-w-prose">{consent.text}</p>
        </div>
        {canAuthorize ? (
          <div className="flex flex-col gap-2">
            <p id="consent-action" className="text-sm text-ink-soft">
              Reserving accepts these terms and opens PayPal in this tab. PayPal holds {formatMoney(proposal.price)}; you are charged only if it happens.
            </p>
            <Button size="lg" block onClick={authorize} busy={busy} busyLabel="Opening PayPal…" aria-describedby="consent-action consent-text">
              Reserve with PayPal
            </Button>
          </div>
        ) : (
          !proposal.payments_open && <p className="text-ink-soft">Reservations are closed for this workshop.</p>
        )}
        {error && (
          <Notice tone="attention" role="alert">
            {error}
          </Notice>
        )}
      </div>

      {canWithdraw && (
        <div className="border-t border-line pt-4">
          <Button variant="quiet" size="sm" onClick={() => setLeaving(kind)}>
            {kind === 'void' ? 'Leave and release my hold…' : 'Give up my spot…'}
          </Button>
        </div>
      )}
      {withdrawn && <Notice>{withdrawn}</Notice>}

      <ConfirmDialog
        open={leaving !== null && canWithdraw}
        title={leaving === 'void' ? 'Leave and release your hold?' : 'Give up your spot?'}
        confirmLabel="Yes, leave"
        cancelLabel="Keep my spot"
        busy={busy}
        busyLabel="Leaving…"
        tone="danger"
        onConfirm={withdraw}
        onClose={() => setLeaving(null)}
      >
        {leaving === 'void'
          ? `PayPal releases your hold of ${formatMoney(proposal.price)} and you are not charged. Your spot goes back to the group.`
          : 'Your spot goes back to the group. Nothing was held at PayPal.'}
      </ConfirmDialog>
    </Tile>
  )
}
