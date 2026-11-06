import { useRef, useState } from 'react'
import { useParams } from 'react-router'
import { api, type OwnerView } from '../api'
import AvailabilityGrid from '../components/AvailabilityGrid'
import CancelWorkshop from '../components/CancelWorkshop'
import ClaudeRead from '../components/ClaudeRead'
import CopyField from '../components/CopyField'
import Deadline from '../components/Deadline'
import DemoControls from '../components/DemoControls'
import EnrollmentList from '../components/EnrollmentList'
import NextStep from '../components/NextStep'
import OpenReservationsDialog from '../components/OpenReservationsDialog'
import OperatorPanel from '../components/OperatorPanel'
import { Loading, LoadError, RefreshError } from '../components/PageState'
import SpotsTile from '../components/SpotsTile'
import StatusBanner from '../components/StatusBanner'
import { TermsList } from '../components/Terms'
import { Icon, Page, Tile, TileLabel } from '../components/ui'
import { usePolling, useResource } from '../lib/hooks'
import { nextStep, ownerRefresh } from '../lib/owner'
import { count } from '../lib/plural'
import { headline, heldAny, refundsAny, releasesAny, tone } from '../lib/status'
import { lockedTerms } from '../lib/terms'
import { formatDateRange, windowLabel, zoneName } from '../lib/time'

const STALLED_AFTER_MS = 20000

function cancellationDetail(v: OwnerView): string {
  const howMany = (pred: (s: string) => boolean) => v.enrollments.filter((e) => pred(e.status)).length
  const voided = (e: OwnerView['enrollments'][number]) => e.status === 'VOIDED' || e.payment?.authorization_status === 'VOIDED'
  const released = v.enrollments.filter(voided).length
  const releasing = v.enrollments.filter((e) => (e.status === 'VOIDING' || e.status === 'CAPTURE_FAILED') && !voided(e)).length
  const refunded = howMany((s) => s === 'REFUNDED')
  const refunding = howMany((s) => s === 'PAID' || s === 'REFUNDING' || s === 'REFUND_PENDING')
  const failed = v.enrollments.filter((e) => e.status === 'REFUND_FAILED' || e.status === 'NEEDS_ATTENTION' || e.needs_attention).length
  const parts = [count(released, 'hold') + ' released', count(refunded, 'refund') + ' completed', `${refunding + releasing} in progress`]
  if (failed) parts.push(`${failed} ${failed === 1 ? 'needs' : 'need'} attention`)
  return parts.join(', ') + '.'
}

export default function Owner() {
  const { token = '' } = useParams()
  const view = useResource(() => api.owner(token), token)
  const [opening, setOpening] = useState<string | null>(null)
  const pendingSince = useRef<number | null>(null)
  const d = view.data
  const pending = d?.recommendation.explanation_pending ?? false
  if (!pending) pendingSince.current = null
  else if (pendingSince.current === null) pendingSince.current = Date.now()
  const refresh = d
    ? ownerRefresh({
        status: d.workshop.status,
        resolution: d.workshop.resolution,
        paymentsOpen: d.proposal?.payments_open ?? false,
        enrollments: d.enrollments.map((e) => e.status),
        explanationPending: pending,
      })
    : null
  usePolling(view.reload, refresh ?? 0, refresh !== null)

  if (!d) {
    if (view.error) return <LoadError error={view.error} missing="Check that you opened the whole organizer link you saved when you created the workshop." onRetry={view.reload} />
    return <Loading what="your workshop" />
  }

  const w = d.workshop
  const t = d.tally
  const step = nextStep(d)
  const stalled = pendingSince.current !== null && Date.now() - pendingSince.current > STALLED_AFTER_MS
  const chosen = d.proposal ? d.dates.find((x) => x.id === d.proposal?.date_id) : undefined
  return (
    <Page wide>
      <title>{`${w.title} – organizer – CommitPool`}</title>
      <header className="mb-5 flex flex-col gap-2 sm:mb-6">
        <h1 className="text-3xl sm:text-5xl">{w.title}</h1>
        <p className="text-ink-soft">{[w.host_name && `With ${w.host_name}`, w.location, `Times in ${zoneName(w.timezone)}`].filter(Boolean).join(' · ')}</p>
        <p className="flex items-center gap-1.5 text-sm font-semibold text-brand">
          <Icon name="lock" size={16} />
          Your organizer page. Keep this link to yourself.
        </p>
      </header>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_24rem] lg:items-start lg:gap-5">
        <div className="flex min-w-0 flex-col gap-4 lg:gap-5">
          <OperatorPanel token={token} view={d} onChange={view.reload} />
          <StatusBanner headline={headline({ status: w.status, resolution: w.resolution, refunds: refundsAny(d.enrollments), held: heldAny(d.enrollments), releases: releasesAny(d.enrollments) })} tone={tone(w.status, w.resolution)}>
            {d.proposal && d.proposal.payments_open && <Deadline closesAt={d.proposal.closes_at} timeZone={w.timezone} open />}
            {w.status === 'CANCELLED' && <p>{cancellationDetail(d)}</p>}
            {w.status === 'CONFIRMED' && <p>{count(t.paid, 'payment')} reached the {t.minimum} needed.</p>}
          </StatusBanner>
          {view.error && <RefreshError polling={refresh !== null} onRetry={() => void view.reload()} />}
          <NextStep view={d} step={step} onOpen={setOpening} />
          <ClaudeRead view={d} stalled={stalled} onCheck={() => void view.reload()} />

          {d.proposal && (
            <Tile as="section" aria-labelledby="locked-title" className="flex flex-col gap-4 sm:p-6">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h2 id="locked-title" className="text-2xl">
                  {d.proposal.payments_open ? 'Reservations open' : 'Reservations closed'}
                </h2>
                <span className="flex items-center gap-1.5 text-sm text-ink-soft">
                  <Icon name="lock" size={16} />
                  Terms locked
                </span>
              </div>
              {chosen && <p className="font-display text-xl font-bold tabular-nums">{formatDateRange(chosen.starts_at, chosen.ends_at, w.timezone)}</p>}
              <TermsList
                items={lockedTerms({
                  timeZone: w.timezone,
                  price: d.proposal.price,
                  durationMin: w.duration_min,
                  minimum: d.proposal.minimum,
                  capacity: d.proposal.capacity,
                  opensAt: d.proposal.opens_at,
                  closesAt: d.proposal.closes_at,
                  paymentsOpen: d.proposal.payments_open,
                })}
              />
            </Tile>
          )}
          {d.proposal && <EnrollmentList token={token} view={d} onChange={view.reload} />}
          <AvailabilityGrid view={d} onOpen={setOpening} />
        </div>

        <aside className="flex flex-col gap-4 lg:sticky lg:top-4 lg:gap-5" aria-label="Workshop summary">
          <SpotsTile view={d} />
          <Tile as="section" aria-labelledby="manage-title" className="flex flex-col gap-5">
            <h2 id="manage-title" className="text-xl">
              Manage
            </h2>
            {step.action.kind !== 'share' && d.share_url && (
              <CopyField id="share-url" label="Invite link" value={d.share_url} hint="Anyone with this link can join and say when they can come." />
            )}
            <div className="flex flex-col gap-1">
              <TileLabel>Time to reserve</TileLabel>
              <p className="text-sm">{w.payment_window_min < 1440 ? windowLabel(w.payment_window_min) : `Reservations stay open ${windowLabel(w.payment_window_min)}`}</p>
            </div>
            <CancelWorkshop token={token} view={d} onChange={view.reload} />
            <DemoControls token={token} view={d} onChange={view.reload} />
          </Tile>
        </aside>
      </div>

      <OpenReservationsDialog token={token} view={d} dateId={opening} onClose={() => setOpening(null)} onOpened={view.reload} />
    </Page>
  )
}
