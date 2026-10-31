import { useEffect, useRef } from 'react'
import { useParams } from 'react-router'
import { api, type OwnerView } from '../api'
import AvailabilityList from '../components/AvailabilityList'
import CopyField from '../components/CopyField'
import Deadline from '../components/Deadline'
import DemoControls from '../components/DemoControls'
import EnrollmentList from '../components/EnrollmentList'
import { Loading, LoadError, RefreshError } from '../components/PageState'
import RecommendationPanel from '../components/RecommendationPanel'
import SeatMeter from '../components/SeatMeter'
import StatusBanner from '../components/StatusBanner'
import Terms, { TermsList } from '../components/Terms'
import { Button, Page } from '../components/ui'
import { usePolling, useResource } from '../lib/hooks'
import { count } from '../lib/plural'
import { headline, shouldPollOwner, tone } from '../lib/status'
import { lockedTerms, workshopTerms } from '../lib/terms'
import { windowLabel } from '../lib/time'

const EXPLANATION_TRIES = 6

// A seat still paid in a cancelled workshop is about to be refunded.
const REFUND_STATES: ReadonlySet<string> = new Set(['PAID', 'REFUNDING', 'REFUND_PENDING', 'REFUNDED', 'REFUND_FAILED'])

function cancellationDetail(v: OwnerView): string {
  const howMany = (pred: (s: string) => boolean) => v.enrollments.filter((e) => pred(e.status)).length
  const released = howMany((s) => s === 'VOIDED' || s === 'VOIDING' || s === 'CAPTURE_FAILED')
  const refunded = howMany((s) => s === 'REFUNDED')
  const refunding = howMany((s) => s === 'PAID' || s === 'REFUNDING' || s === 'REFUND_PENDING')
  const failed = howMany((s) => s === 'REFUND_FAILED' || s === 'NEEDS_ATTENTION')
  const parts = [count(released, 'hold') + ' released', count(refunded, 'refund') + ' completed', `${refunding} in progress`]
  if (failed) parts.push(`${failed} ${failed === 1 ? 'needs' : 'need'} attention`)
  return parts.join(', ') + '.'
}

export default function Owner() {
  const { token = '' } = useParams()
  const view = useResource(() => api.owner(token), token)
  const d = view.data
  const polling = d
    ? shouldPollOwner({
        status: d.workshop.status,
        resolution: d.workshop.resolution,
        paymentsOpen: d.proposal?.payments_open ?? false,
        enrollments: d.enrollments.map((e) => e.status),
      })
    : false
  usePolling(view.reload, 5000, polling)

  // The explanation is written in the background; read the view again a few
  // times while it is pending.
  const tries = useRef(0)
  const pending = d?.recommendation.explanation_pending ?? false
  const reload = view.reload
  useEffect(() => {
    if (!pending) {
      tries.current = 0
      return
    }
    if (tries.current >= EXPLANATION_TRIES) return
    const t = setTimeout(() => {
      tries.current++
      void reload()
    }, 3000)
    return () => clearTimeout(t)
  }, [pending, d, reload])

  if (!d) {
    if (view.error) return <LoadError error={view.error} missing="Check that you opened the whole organizer link you saved when you created the workshop." onRetry={view.reload} />
    return <Loading what="your workshop" />
  }

  const w = d.workshop
  const t = d.tally
  const refunds = d.enrollments.some((e) => REFUND_STATES.has(e.status))
  return (
    <Page wide>
      <title>{`${w.title} – organizer – CommitPool`}</title>
      <header className="mb-8 flex flex-col gap-2">
        <h1 className="text-3xl font-bold sm:text-4xl">{w.title}</h1>
        <p className="text-ink-soft">Your organizer page. Keep its link to yourself.</p>
      </header>

      <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_22rem] lg:items-start">
        <aside className="flex flex-col gap-8 lg:order-2" aria-label="Workshop summary">
          <StatusBanner headline={headline({ status: w.status, resolution: w.resolution, refunds })} tone={tone(w.status, w.resolution)}>
            {d.proposal && d.proposal.payments_open && <Deadline closesAt={d.proposal.closes_at} timeZone={w.timezone} open />}
            {w.status === 'CANCELLED' && <p>{cancellationDetail(d)}</p>}
            {w.status === 'CONFIRMED' && (
              <p>
                {count(t.paid, 'paid seat')} reached the minimum of {t.minimum}.
              </p>
            )}
          </StatusBanner>
          {view.error && <RefreshError polling={polling} onRetry={() => void view.reload()} />}

          <section aria-labelledby="counts-title" className="flex flex-col gap-4">
            <div className="flex items-center justify-between gap-3">
              <h2 id="counts-title" className="text-xl font-bold">
                Seats
              </h2>
              <Button variant="secondary" onClick={() => void view.reload()} className="px-3 py-1.5 text-sm">
                Refresh
              </Button>
            </div>
            <dl className="grid grid-cols-3 gap-3">
              <div>
                <dt className="text-sm text-ink-soft">Interested</dt>
                <dd className="wide text-3xl font-bold tabular-nums">{t.interested}</dd>
              </div>
              <div>
                <dt className="text-sm text-ink-soft">Authorized</dt>
                <dd className="wide text-3xl font-bold text-hold tabular-nums">{t.authorized}</dd>
              </div>
              <div>
                <dt className="text-sm text-ink-soft">Paid</dt>
                <dd className="wide text-3xl font-bold text-paid tabular-nums">
                  {t.paid}
                  <span className="text-lg font-semibold text-ink-soft">/{t.minimum}</span>
                </dd>
              </div>
            </dl>
            <SeatMeter capacity={t.capacity} minimum={t.minimum} paid={t.paid} authorized={t.authorized} reserved={t.reserved} />
            <p className="text-sm text-ink-soft">
              {t.seats_taken} of {count(t.capacity, 'seat')} taken. {count(t.capturing + t.pending, 'charge')} in progress
              {t.failed ? `, ${t.failed} failed` : ''}.
            </p>
          </section>

          <section aria-labelledby="share-title" className="flex flex-col gap-2">
            <h2 id="share-title" className="text-xl font-bold">
              Invite participants
            </h2>
            {d.share_url ? (
              <CopyField id="share-url" label="Link for participants" value={d.share_url} hint="Anyone with this link can join and say when they can come." />
            ) : (
              <p className="text-ink-soft">This workshop was created before share links were kept, so it has none to show.</p>
            )}
          </section>

          <DemoControls token={token} view={d} onChange={view.reload} />
        </aside>

        <div className="flex flex-col gap-12 lg:order-1">
          <section aria-labelledby="terms-title" className="flex flex-col gap-4">
            <h2 id="terms-title" className="sr-only">
              Terms
            </h2>
            {w.description && <p className="max-w-prose whitespace-pre-line">{w.description}</p>}
            {!d.proposal && <Terms items={workshopTerms(w)} />}
            <p className="text-sm text-ink-soft">
              Times in {w.timezone}. {w.payment_window_min < 1440 ? windowLabel(w.payment_window_min) : `Payment window: ${windowLabel(w.payment_window_min)}`}.
            </p>
          </section>

          {d.proposal ? (
            <section aria-labelledby="locked-title" className="flex flex-col gap-4 rounded-sm border border-rule border-t-4 border-t-ink bg-sheet p-5 sm:p-7">
              <h2 id="locked-title" className="text-2xl font-bold">
                Payments opened
              </h2>
              <p className="wide text-xl font-semibold tabular-nums">{d.dates.find((x) => x.id === d.proposal?.date_id)?.label}</p>
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
              <p className="text-sm text-ink-soft">These terms are locked.</p>
            </section>
          ) : null}

          {d.proposal && <EnrollmentList token={token} view={d} onChange={view.reload} />}

          <RecommendationPanel token={token} view={d} explanationStalled={tries.current >= EXPLANATION_TRIES} onChange={view.reload} />

          <AvailabilityList view={d} />
        </div>
      </div>
    </Page>
  )
}
