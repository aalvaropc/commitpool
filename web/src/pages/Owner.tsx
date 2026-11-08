import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router'
import { api } from '../api'
import AvailabilityGrid from '../components/AvailabilityGrid'
import AddToCalendar from '../components/AddToCalendar'
import CancelWorkshop from '../components/CancelWorkshop'
import ClaudeRead from '../components/ClaudeRead'
import ClaudeSuggests from '../components/ClaudeSuggests'
import Deadline from '../components/Deadline'
import DemoControls from '../components/DemoControls'
import DemoGuide from '../components/DemoGuide'
import EnrollmentList from '../components/EnrollmentList'
import NextStep, { InviteLink } from '../components/NextStep'
import OpenReservationsDialog from '../components/OpenReservationsDialog'
import OperatorPanel from '../components/OperatorPanel'
import { Loading, LoadError, RefreshError } from '../components/PageState'
import SpotsTile from '../components/SpotsTile'
import StatusBanner from '../components/StatusBanner'
import { TermsList } from '../components/Terms'
import { Icon, Page, Tile, TileLabel } from '../components/ui'
import { usePolling, useResource } from '../lib/hooks'
import { cancellationDetail, nextStep, ownerRefresh } from '../lib/owner'
import { count } from '../lib/plural'
import { headline, heldAny, refundsAny, releasesAny, tone } from '../lib/status'
import { lockedTerms, workshopTerms } from '../lib/terms'
import { formatDateRange, windowLabel, zoneName } from '../lib/time'

const STALLED_AFTER_MS = 20000

export default function Owner() {
  const { token = '' } = useParams()
  const view = useResource(() => api.owner(token), token)
  const [opening, setOpening] = useState<string | null>(null)
  const pendingSince = useRef<number | null>(null)
  const lockedTitle = useRef<HTMLHeadingElement>(null)
  const focusLocked = useRef(false)
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
        explanationPending: pending || d.suggestions.some((s) => s.pending),
        demoFilling: d.workshop.demo === true && d.availabilities.length < d.participants.length,
      })
    : null
  usePolling(view.reload, refresh ?? 0, refresh !== null)
  const proposalId = d?.proposal?.date_id
  useEffect(() => {
    if (!focusLocked.current || !proposalId) return
    focusLocked.current = false
    lockedTitle.current?.focus()
  }, [proposalId])

  const openedReservations = () => {
    focusLocked.current = true
    return view.reload()
  }

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

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem] lg:gap-5">
        <div className="flex min-w-0 flex-col gap-4 lg:gap-5">
          <DemoGuide view={d} />
          <OperatorPanel token={token} view={d} onChange={view.reload} />
          <StatusBanner headline={headline({ status: w.status, resolution: w.resolution, refunds: refundsAny(d.enrollments), held: heldAny(d.enrollments), releases: releasesAny(d.enrollments) })} tone={tone(w.status, w.resolution)}>
            {d.proposal && d.proposal.payments_open && <Deadline closesAt={d.proposal.closes_at} timeZone={w.timezone} open />}
            {w.status === 'CANCELLED' && <p>{cancellationDetail(d)}</p>}
            {w.status === 'CONFIRMED' && <p>{count(t.paid, 'payment')} reached the {t.minimum} needed.</p>}
          </StatusBanner>
          {view.error && <RefreshError polling={refresh !== null} onRetry={() => void view.reload()} />}
          <NextStep view={d} step={step} onOpen={setOpening} />
          <ClaudeSuggests suggestions={d.suggestions} />
          <ClaudeRead view={d} stalled={stalled} onCheck={() => void view.reload()} />

          {d.proposal && (
            <Tile as="section" aria-labelledby="locked-title" className="flex flex-col gap-4 sm:p-6">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h2 id="locked-title" ref={lockedTitle} tabIndex={-1} className="text-2xl">
                  {d.proposal.payments_open ? 'Reservations open' : 'Reservations closed'}
                </h2>
                <span className="flex items-center gap-1.5 text-sm text-ink-soft">
                  <Icon name="lock" size={16} />
                  Terms locked
                </span>
              </div>
              {chosen && <p className="font-display text-xl font-bold tabular-nums">{formatDateRange(chosen.starts_at, chosen.ends_at, w.timezone)}</p>}
              {w.status !== 'CANCELLED' && <AddToCalendar path={d.proposal.calendar_path} />}
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
        </div>

        <aside aria-label="Workshop summary">
          <div className="lg:sticky lg:top-4">
            <SpotsTile view={d} />
          </div>
        </aside>

        <div className="min-w-0 lg:col-span-2">
          <AvailabilityGrid view={d} onOpen={setOpening} />
        </div>

        <div className="grid gap-4 lg:col-span-2 lg:grid-cols-2 lg:items-start lg:gap-5">
          {(w.description || !d.proposal) && (
            <Tile as="section" aria-labelledby="class-title" className="flex flex-col gap-4">
              <h2 id="class-title" className="text-xl">
                The class
              </h2>
              {w.description && <p className="max-w-prose whitespace-pre-line [overflow-wrap:anywhere]">{w.description}</p>}
              {!d.proposal && <TermsList items={workshopTerms(w)} />}
            </Tile>
          )}
          <Tile as="section" aria-labelledby="manage-title" className="flex flex-col gap-5">
            <h2 id="manage-title" className="text-xl">
              Manage
            </h2>
            {step.action.kind !== 'share' && <InviteLink url={d.share_url} hint="Anyone with this link can join and say when they can come." />}
            <div className="flex flex-col gap-1">
              <TileLabel>Time to reserve</TileLabel>
              <p className="text-sm">{w.payment_window_min < 1440 ? windowLabel(w.payment_window_min) : `Reservations stay open ${windowLabel(w.payment_window_min)}`}</p>
            </div>
            <CancelWorkshop token={token} view={d} onChange={view.reload} />
            <DemoControls token={token} view={d} onChange={view.reload} />
          </Tile>
        </div>
      </div>

      <OpenReservationsDialog token={token} view={d} dateId={opening} onClose={() => setOpening(null)} onOpened={openedReservations} />
    </Page>
  )
}
