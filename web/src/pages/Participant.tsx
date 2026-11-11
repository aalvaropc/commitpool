import { useEffect, useState } from 'react'
import { useParams, useSearchParams } from 'react-router'
import { api } from '../api'
import AvailabilityPanel from '../components/AvailabilityPanel'
import CommitmentCard from '../components/CommitmentCard'
import Deadline from '../components/Deadline'
import DeadlineMoved from '../components/DeadlineMoved'
import { EventDetails, EventHero } from '../components/EventHeader'
import { Loading, LoadError, RefreshError } from '../components/PageState'
import StatusBanner from '../components/StatusBanner'
import { Avatar, Notice, Page } from '../components/ui'
import { useNow, usePolling, useResource } from '../lib/hooks'
import { rememberParticipant } from '../lib/participant'
import { authorizationPending, headline, returnCheckSettled, seatHeld, shouldPollParticipant, tone } from '../lib/status'

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
  const pending = authorizationPending(d?.enrollment)
  const polling = d
    ? shouldPollParticipant({
        status: d.workshop.status,
        resolution: d.workshop.resolution,
        paymentsOpen: d.proposal?.payments_open ?? false,
        enrollment: own,
      })
    : false
  usePolling(view.reload, 3000, polling)

  useEffect(() => {
    if (!checking || !d) return
    if (returnCheckSettled({ enrollment: own, polling, authPending: pending })) {
      setParams(
        (p) => {
          p.delete('paid')
          return p
        },
        { replace: true },
      )
    }
  }, [checking, d, own, polling, pending, setParams])

  if (!d) {
    if (view.error) return <LoadError error={view.error} missing="Check that you opened the whole personal link you got when you joined." onRetry={view.reload} />
    return <Loading what="your workshop" />
  }

  const w = d.workshop
  const held = seatHeld(d.enrollment, d.proposal?.payments_open ?? false, now)
  return (
    <Page wide>
      <title>{`${w.title} – CommitPool`}</title>
      <div className="mx-auto flex max-w-4xl flex-col gap-4 sm:gap-5">
        <EventHero view={d} compact />
        <p className="flex items-center gap-2 text-sm text-ink-soft">
          <Avatar name={d.participant.display_name} size="sm" />
          You joined as {d.participant.display_name}.
        </p>
        <StatusBanner
          headline={headline({ status: w.status, resolution: w.resolution, enrollment: own, seatHeld: held, authPending: pending, paymentsOpen: d.proposal?.payments_open ?? false })}
          tone={tone(w.status, w.resolution, own)}
        >
          {w.status === 'FUNDING' && d.proposal && <Deadline closesAt={d.proposal.closes_at} timeZone={w.timezone} open={d.proposal.payments_open} counting={own !== 'PAID'} />}
        </StatusBanner>
        <DeadlineMoved proposal={d.proposal} status={w.status} timeZone={w.timezone} audience="participant" />
        {checking && (
          <Notice tone="hold" title="Checking your payment…">
            {now - checkingSince < CHECK_PATIENCE_MS
              ? 'PayPal is confirming your hold. This page updates on its own.'
              : 'PayPal has not confirmed your hold yet. This page keeps checking; if you left PayPal before approving, reserve again below.'}
          </Notice>
        )}
        {view.error && <RefreshError polling={polling} onRetry={() => void view.reload()} />}

        {d.proposal && <CommitmentCard token={token} view={d} onChange={view.reload} />}
        <AvailabilityPanel token={token} view={d} onSaved={view.reload} />
        <EventDetails view={d} counting={own !== 'PAID'} />
      </div>
    </Page>
  )
}
