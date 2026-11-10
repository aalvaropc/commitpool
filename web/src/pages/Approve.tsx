import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router'
import { api, ApiError, type ApprovalView } from '../api'
import { Loading, LoadError } from '../components/PageState'
import { TermsList } from '../components/Terms'
import { Button, ButtonLink, Icon, Notice, Page, Tile, toneText, toneWash, type IconName } from '../components/ui'
import { approvalMessage, approvalOutcome, approvalTerms, approveFailure, confirmDeadline, refusedState, type ApprovalOutcome } from '../lib/approval'
import { useResource } from '../lib/hooks'
import { count } from '../lib/plural'
import { formatMoment, zoneName } from '../lib/time'

const outcomeIcons: Record<ApprovalOutcome, IconName> = {
  approved: 'check',
  used: 'check',
  expired: 'clock',
  changed: 'x',
  closed: 'lock',
  declined: 'x',
}

function Ask({ view, busy, error, onApprove, onDecline }: { view: ApprovalView; busy: boolean; error: string | null; onApprove: () => void; onDecline: () => void }) {
  const a = view.approval
  return (
    <Tile as="section" aria-labelledby="ask-title" className="flex flex-col gap-5 sm:p-8">
      <div className="flex items-start gap-3">
        <span aria-hidden="true" className="grid size-10 shrink-0 place-items-center rounded-full bg-hold-wash text-hold">
          <Icon name="spark" size={20} />
        </span>
        <h2 id="ask-title" className="pt-1 text-2xl sm:text-3xl">
          Your assistant wants to open reservations on these terms:
        </h2>
      </div>
      <TermsList items={approvalTerms(view)} />
      <div className="flex flex-col gap-2 rounded-control bg-surface-muted px-4 py-3">
        <p className="font-semibold">Nothing is charged until enough people reserve.</p>
        <p className="text-sm text-ink-soft">
          Each person reserves with a PayPal hold. Holds are charged only once {count(a.minimum, 'person', 'people')} {a.minimum === 1 ? 'has' : 'have'} reserved; if not enough reserve in time, every hold is
          released. These terms lock when reservations open.
        </p>
      </div>
      <p className="text-sm text-ink-soft">
        This request works until {formatMoment(a.expires_at, view.workshop.timezone)}. Only someone with your organizer link can approve it, here in a browser. Your assistant's tools cannot.
      </p>
      {error && (
        <Notice tone="attention" role="alert">
          {error}
        </Notice>
      )}
      <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <Button variant="secondary" size="lg" onClick={onDecline} disabled={busy}>
          Not now
        </Button>
        <Button size="lg" onClick={onApprove} busy={busy} busyLabel="Approving…">
          <Icon name="check" size={20} />
          Approve
        </Button>
      </div>
    </Tile>
  )
}

export default function Approve() {
  const { token = '', id = '' } = useParams()
  const view = useResource(() => api.approval(token, id), `${token}/${id}`)
  const [shown, setShown] = useState<ApprovalView | null>(null)
  const [declined, setDeclined] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const refreshed = useRef(false)
  const resultTitle = useRef<HTMLHeadingElement>(null)
  const focusResult = useRef(false)
  const d = shown ?? view.data
  const outcome = d ? approvalOutcome(d.approval.state, declined) : null

  useEffect(() => {
    if (!focusResult.current || !outcome) return
    focusResult.current = false
    resultTitle.current?.focus()
  }, [outcome])

  if (!d) {
    if (view.error) return <LoadError error={view.error} missing="Check that you opened the whole link your assistant sent." onRetry={view.reload} />
    return <Loading what="the terms to approve" />
  }

  async function approve() {
    if (!d) return
    setBusy(true)
    setError(null)
    try {
      const next = await api.approveOpen(token, id)
      focusResult.current = true
      setShown(next)
    } catch (e) {
      const state = e instanceof ApiError ? refusedState(e.code) : null
      if (state) {
        focusResult.current = true
        setShown({ ...d, approval: { ...d.approval, state } })
      } else {
        const failure = e instanceof ApiError ? approveFailure(e.status, e.code, e.message, refreshed.current) : { refresh: false, message: 'The approval did not go through.' }
        if (failure.refresh) {
          refreshed.current = true
          setShown(null)
          await view.reload()
        }
        setError(failure.message)
      }
    } finally {
      setBusy(false)
    }
  }

  function decline() {
    focusResult.current = true
    setError(null)
    setDeclined(true)
  }

  function review() {
    setDeclined(false)
  }

  const w = d.workshop
  const message = outcome ? approvalMessage(outcome) : null
  return (
    <Page>
      <title>{`Approve reservations – ${w.title} – CommitPool`}</title>
      <header className="mb-5 flex flex-col gap-2 sm:mb-6">
        <p className="flex items-center gap-1.5 text-sm font-semibold text-brand">
          <Icon name="lock" size={16} />
          Your organizer approval. Keep this link to yourself.
        </p>
        <h1 className="text-3xl [overflow-wrap:anywhere] sm:text-5xl">{w.title}</h1>
        <p className="text-ink-soft">{[w.host_name && `With ${w.host_name}`, w.location, `Times in ${zoneName(w.timezone)}`].filter(Boolean).join(' · ')}</p>
      </header>

      {outcome && message ? (
        <Tile as="section" aria-labelledby="result-title" className="flex flex-col items-start gap-5 sm:p-8">
          <div className="flex flex-col gap-3">
            <span aria-hidden="true" className={`grid size-12 place-items-center rounded-full ${toneWash[message.tone]} ${toneText[message.tone]}`}>
              <Icon name={outcomeIcons[outcome]} size={24} />
            </span>
            <h2 id="result-title" ref={resultTitle} tabIndex={-1} aria-describedby="result-body" className="text-2xl sm:text-3xl">
              {message.title}
            </h2>
            <div id="result-body" className="flex max-w-prose flex-col gap-2">
              {outcome === 'approved' && <p className="font-semibold">{confirmDeadline(d)}</p>}
              <p className="text-ink-soft">{message.body}</p>
            </div>
          </div>
          {(outcome === 'approved' || outcome === 'declined') && <TermsList items={approvalTerms(d)} />}
          <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row">
            <ButtonLink to={`/owner/${encodeURIComponent(token)}`} size="lg">
              Go to your organizer page
            </ButtonLink>
            {outcome === 'declined' && (
              <Button variant="secondary" size="lg" onClick={review}>
                Review the terms again
              </Button>
            )}
          </div>
        </Tile>
      ) : (
        <Ask view={d} busy={busy} error={error} onApprove={() => void approve()} onDecline={decline} />
      )}
    </Page>
  )
}
