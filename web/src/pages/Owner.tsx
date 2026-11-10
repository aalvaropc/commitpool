import { useEffect, useRef, useState } from 'react'
import { useParams, useSearchParams } from 'react-router'
import { api, type OwnerView } from '../api'
import AvailabilityGrid from '../components/AvailabilityGrid'
import AddToCalendar from '../components/AddToCalendar'
import CancelWorkshop from '../components/CancelWorkshop'
import ClaudeRead from '../components/ClaudeRead'
import ClaudeSuggests from '../components/ClaudeSuggests'
import DeadlineMoved from '../components/DeadlineMoved'
import Decision, { InviteLink } from '../components/Decision'
import DemoClock from '../components/DemoClock'
import DemoControls from '../components/DemoControls'
import DemoGuide from '../components/DemoGuide'
import EnrollmentList from '../components/EnrollmentList'
import OpenReservationsDialog from '../components/OpenReservationsDialog'
import OperatorPanel from '../components/OperatorPanel'
import PayoutTile from '../components/PayoutTile'
import { Loading, LoadError, RefreshError } from '../components/PageState'
import { ExtendDialog } from '../components/RescueCard'
import SpotsTile from '../components/SpotsTile'
import { TermsList } from '../components/Terms'
import { Button, Icon, Notice, Page, Tile, TileLabel } from '../components/ui'
import { extendLink } from '../lib/extend'
import { usePolling, useResource } from '../lib/hooks'
import { nextStep, ownerRefresh } from '../lib/owner'
import { lockedTerms, workshopTerms } from '../lib/terms'
import { formatDateRange, windowLabel, zoneName } from '../lib/time'

const STALLED_AFTER_MS = 20000

function AgreedTerms({ view }: { view: OwnerView }) {
  const w = view.workshop
  const p = view.proposal
  if (!p) return null
  const chosen = view.dates.find((x) => x.id === p.date_id)
  return (
    <Tile as="section" aria-labelledby="locked-title" className="flex flex-col gap-2 sm:p-6">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h2 id="locked-title" className="text-xl">
          Terms people agreed to
        </h2>
        <span className="flex items-center gap-1.5 text-sm text-ink-soft">
          <Icon name="lock" size={16} />
          Locked
        </span>
      </div>
      <details className="group">
        <summary className="-mx-2 flex min-h-11 w-fit cursor-pointer list-none items-center gap-2 rounded-control px-2 font-semibold text-brand [&::-webkit-details-marker]:hidden">
          <Icon name="arrowRight" size={18} className="shrink-0 transition-transform duration-200 group-open:rotate-90" />
          All the terms
        </summary>
        <TermsList
          className="mt-3 mb-2"
          items={lockedTerms({
            timeZone: w.timezone,
            dateLabel: chosen ? formatDateRange(chosen.starts_at, chosen.ends_at, w.timezone) : undefined,
            price: p.price,
            durationMin: w.duration_min,
            minimum: p.minimum,
            capacity: p.capacity,
            opensAt: p.opens_at,
            closesAt: p.closes_at,
            paymentsOpen: p.payments_open,
          })}
        />
      </details>
      {w.status !== 'CANCELLED' && <AddToCalendar path={p.calendar_path} />}
    </Tile>
  )
}

export default function Owner() {
  const { token = '' } = useParams()
  const view = useResource(() => api.owner(token), token)
  const [opening, setOpening] = useState<string | null>(null)
  const [extending, setExtending] = useState(false)
  const [params, setParams] = useSearchParams()
  const pendingSince = useRef<number | null>(null)
  const decisionTitle = useRef<HTMLHeadingElement>(null)
  const focusDecision = useRef(false)
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
        payoutMoving: d.payout?.state === 'created' || d.payout?.state === 'sending' || (d.payout?.state === 'waiting' && d.payout.waiting === 'payments_in_flight'),
      })
    : null
  usePolling(view.reload, refresh ?? 0, refresh !== null)
  const linked = d ? extendLink(params.get('extend'), d.extension) : null
  useEffect(() => {
    if (linked === 'open') setExtending(true)
  }, [linked])
  const forgetLink = () =>
    setParams(
      (p) => {
        p.delete('extend')
        return p
      },
      { replace: true },
    )
  const closeExtend = () => {
    setExtending(false)
    if (params.has('extend')) forgetLink()
  }
  const proposalId = d?.proposal?.date_id
  useEffect(() => {
    if (!focusDecision.current || !proposalId) return
    focusDecision.current = false
    decisionTitle.current?.focus()
  }, [proposalId])

  const openedReservations = () => {
    focusDecision.current = true
    return view.reload()
  }

  if (!d) {
    if (view.error) return <LoadError error={view.error} missing="Check that you opened the whole organizer link you saved when you created the workshop." onRetry={view.reload} />
    return <Loading what="your workshop" />
  }

  const w = d.workshop
  const step = nextStep(d)
  const stalled = pendingSince.current !== null && Date.now() - pendingSince.current > STALLED_AFTER_MS
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
          {view.error && <RefreshError polling={refresh !== null} onRetry={() => void view.reload()} />}
          <Decision view={d} step={step} onOpen={setOpening} titleRef={decisionTitle} />
          {linked === 'stale' && (
            <Notice tone="attention" title="This extension link is out of date">
              <p>{d.extension?.reason ?? 'Reservations are not open, so there is no deadline to extend.'}</p>
              <Button variant="quiet" size="sm" className="-ml-4" onClick={forgetLink}>
                Dismiss
              </Button>
            </Notice>
          )}
          <DeadlineMoved proposal={d.proposal} status={w.status} timeZone={w.timezone} audience="owner" />
          <OperatorPanel token={token} view={d} onChange={view.reload} />
          <DemoGuide view={d} />
          <ClaudeSuggests suggestions={d.suggestions} rescue={{ extension: d.extension ?? null, timeZone: w.timezone, onExtend: () => setExtending(true) }} />
          <ClaudeRead view={d} stalled={stalled} onCheck={() => void view.reload()} />
          {d.proposal && <EnrollmentList token={token} view={d} onChange={view.reload} />}
          <PayoutTile token={token} view={d} onChange={view.reload} />
          <AgreedTerms view={d} />
        </div>

        <aside aria-label="Workshop summary" className="hidden lg:block">
          <div className="sticky top-4">
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
            <DemoClock token={token} view={d} onChange={view.reload} />
            <DemoControls token={token} view={d} onChange={view.reload} />
          </Tile>
        </div>
      </div>

      <OpenReservationsDialog token={token} view={d} dateId={opening} onClose={() => setOpening(null)} onOpened={openedReservations} />
      <ExtendDialog token={token} view={d} open={extending} onClose={closeExtend} onExtended={view.reload} />
    </Page>
  )
}
