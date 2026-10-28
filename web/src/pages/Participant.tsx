import { useEffect, useState } from 'react'
import { useParams, useSearchParams } from 'react-router'
import { api } from '../api'
import AvailabilityPanel from '../components/AvailabilityPanel'
import CommitmentCard from '../components/CommitmentCard'
import Deadline from '../components/Deadline'
import { Loading, LoadError } from '../components/PageState'
import StatusBanner from '../components/StatusBanner'
import Terms from '../components/Terms'
import { Notice, Page } from '../components/ui'
import { useNow, usePolling, useResource } from '../lib/hooks'
import { formatMoney } from '../lib/money'
import { rememberParticipant } from '../lib/participant'
import { headline, shouldPollParticipant, tone } from '../lib/status'
import { durationLabel } from '../lib/time'

const CHECK_PATIENCE_MS = 30000

export default function Participant() {
  const { token = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const checking = params.get('paid') === 'pending'
  const [checkingSince] = useState(() => Date.now())
  const now = useNow(5000)

  useEffect(() => {
    if (token) rememberParticipant(token)
  }, [token])

  const view = useResource(() => api.participant(token), token)
  const d = view.data
  const own = d?.enrollment?.status ?? null
  const polling = d
    ? shouldPollParticipant({
        status: d.workshop.status,
        resolution: d.workshop.resolution,
        paymentsOpen: d.proposal?.payments_open ?? false,
        enrollment: own,
      })
    : false
  usePolling(view.reload, 3000, polling)

  // The return from PayPal asks for a check; it ends once the payment moved
  // past the checkout or nothing more will change on its own.
  useEffect(() => {
    if (!checking || !d) return
    if ((own && own !== 'SEAT_RESERVED') || !polling) {
      setParams(
        (p) => {
          p.delete('paid')
          return p
        },
        { replace: true },
      )
    }
  }, [checking, d, own, polling, setParams])

  if (!d) {
    if (view.error) return <LoadError error={view.error} missing="Check that you opened the whole personal link you got when you joined." onRetry={view.reload} />
    return <Loading what="your workshop" />
  }

  const w = d.workshop
  return (
    <Page>
      <title>{`${w.title} – CommitPool`}</title>
      <div className="flex flex-col gap-10">
        <div className="flex flex-col gap-4">
          <StatusBanner headline={headline({ status: w.status, resolution: w.resolution, enrollment: own })} tone={tone(w.status, w.resolution)}>
            {w.status === 'FUNDING' && d.proposal && <Deadline closesAt={d.proposal.closes_at} timeZone={w.timezone} open={d.proposal.payments_open} />}
          </StatusBanner>
          {checking && (
            <Notice tone="hold" title="Checking your payment…">
              {now - checkingSince < CHECK_PATIENCE_MS
                ? 'PayPal is confirming your hold. This page updates on its own.'
                : 'PayPal has not confirmed your hold yet. This page keeps checking; if you left PayPal before approving, authorize again below.'}
            </Notice>
          )}
          {view.error && <Notice tone="attention">This page could not refresh. It will try again.</Notice>}
        </div>

        <header className="flex flex-col gap-4">
          <p className="text-ink-soft">You joined as {d.participant.display_name}.</p>
          <h1 className="text-3xl font-bold sm:text-4xl">{w.title}</h1>
          {w.description && <p className="max-w-prose whitespace-pre-line">{w.description}</p>}
          {!d.proposal && (
          <Terms
            items={[
              { label: 'Price per seat', value: formatMoney(w.price) },
              { label: 'Minimum paid seats', value: w.minimum },
              { label: 'Capacity', value: w.capacity },
              { label: 'Duration', value: durationLabel(w.duration_min) },
            ]}
          />
          )}
        </header>

        {d.proposal ? (
          <CommitmentCard token={token} view={d} onChange={view.reload} />
        ) : (
          <section aria-labelledby="dates-title" className="flex flex-col gap-3">
            <h2 id="dates-title" className="text-2xl font-bold">
              Candidate dates
            </h2>
            <ul className="flex flex-col">
              {d.dates.map((date) => (
                <li key={date.id} className="semiwide border-t border-rule py-3 font-semibold">
                  {date.label}
                </li>
              ))}
            </ul>
          </section>
        )}

        <AvailabilityPanel token={token} view={d} onSaved={view.reload} />
      </div>
    </Page>
  )
}
