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
import { Avatar, Icon, Notice, Page, Tile } from '../components/ui'
import { hostFirstName } from '../lib/chat'
import { useNow, usePolling, useResource } from '../lib/hooks'
import { rememberParticipant } from '../lib/participant'
import { authorizationPending, countdownShown, headline, returnCheckSettled, seatHeld, shouldPollParticipant, tone } from '../lib/status'

const CHECK_PATIENCE_MS = 30000

export function FromTheChat({ host, message, canAnswer }: { host: string; message: string; canAnswer: boolean }) {
  return (
    <Tile as="section" aria-labelledby="chat-said-title" tone="muted" className="flex flex-col gap-2 sm:p-6">
      <h2 id="chat-said-title" className="flex items-center gap-2 text-lg">
        <Icon name="quote" className="text-brand" />
        What you said in the chat
      </h2>
      <p className="text-ink-soft">{hostFirstName(host)} added this from the group chat:</p>
      <blockquote className="max-w-prose border-l-2 border-brand pl-4 text-[1.0625rem] whitespace-pre-line [overflow-wrap:anywhere]">{message}</blockquote>
      {canAnswer && <p className="text-sm text-ink-soft">If it’s wrong, say it again in your own words.</p>}
    </Tile>
  )
}

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
          {w.status === 'FUNDING' && d.proposal && <Deadline closesAt={d.proposal.closes_at} timeZone={w.timezone} open={d.proposal.payments_open} />}
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
        {d.participant.source === 'chat' && d.participant.chat_message && (
          <FromTheChat host={w.host_name} message={d.participant.chat_message} canAnswer={w.status === 'COLLECTING' || (d.proposal?.payments_open ?? false)} />
        )}
        <AvailabilityPanel token={token} view={d} onSaved={view.reload} />
        <EventDetails view={d} counting={countdownShown(w.status, own)} />
      </div>
    </Page>
  )
}
