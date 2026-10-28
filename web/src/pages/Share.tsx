import { useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { api } from '../api'
import Deadline from '../components/Deadline'
import { Loading, LoadError } from '../components/PageState'
import SeatMeter from '../components/SeatMeter'
import Terms from '../components/Terms'
import { Button, Field, Notice, Page, describedBy, inputClass } from '../components/ui'
import { useResource } from '../lib/hooks'
import { formatMoney } from '../lib/money'
import { rememberParticipant, tokenFromParticipantURL } from '../lib/participant'
import { durationLabel, formatInZone } from '../lib/time'

export default function Share() {
  const { share = '' } = useParams()
  const navigate = useNavigate()
  const view = useResource(() => api.shared(share), share)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function join(e: FormEvent) {
    e.preventDefault()
    if (!name.trim()) {
      setError('Enter the name the organizer knows you by.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      const res = await api.join(share, name.trim())
      const token = tokenFromParticipantURL(res.participant_url)
      if (!token) throw new Error('The server answered with an address this page cannot open.')
      rememberParticipant(token)
      navigate(`/p/${token}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'You could not join right now.')
      setBusy(false)
    }
  }

  const d = view.data
  if (!d) {
    if (view.error) return <LoadError error={view.error} missing="Check that you opened the whole link the organizer shared." onRetry={view.reload} />
    return <Loading what="the workshop" />
  }

  const w = d.workshop
  const open = w.status === 'COLLECTING' || (d.proposal?.payments_open ?? false)
  const chosen = d.proposal ? d.dates.find((x) => x.id === d.proposal?.date_id) : undefined

  return (
    <Page>
      <title>{`${w.title} – CommitPool`}</title>
      <div className="flex flex-col gap-10">
        <header className="flex flex-col gap-4">
          <p className="text-ink-soft">You are invited to</p>
          <h1 className="text-3xl font-bold sm:text-4xl">{w.title}</h1>
          {w.description && <p className="max-w-prose whitespace-pre-line">{w.description}</p>}
          <Terms
            items={[
              { label: 'Price per seat', value: formatMoney(w.price) },
              { label: 'Minimum paid seats', value: w.minimum },
              { label: 'Capacity', value: w.capacity },
              { label: 'Duration', value: durationLabel(w.duration_min) },
            ]}
          />
        </header>

        {d.proposal ? (
          <section aria-labelledby="date-title" className="flex flex-col gap-4">
            <h2 id="date-title" className="text-2xl font-bold">
              The date is set
            </h2>
            <p className="wide text-xl font-semibold tabular-nums">{chosen?.label ?? formatInZone(d.proposal.starts_at, w.timezone)}</p>
            {d.proposal.payments_open && <Deadline closesAt={d.proposal.closes_at} timeZone={w.timezone} open />}
            <SeatMeter capacity={d.tally.capacity} minimum={d.tally.minimum} paid={d.tally.paid} authorized={d.tally.authorized} label="Group progress" />
          </section>
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
            <p className="text-ink-soft">
              {d.tally.interested} {d.tally.interested === 1 ? 'person has' : 'people have'} joined so far.
            </p>
          </section>
        )}

        {open ? (
          <section aria-labelledby="join-title" className="flex flex-col gap-4 rounded-sm border border-rule bg-sheet p-5 sm:p-7">
            <h2 id="join-title" className="text-2xl font-bold">
              Join
            </h2>
            <p className="max-w-prose text-ink-soft">
              Joining gives you a personal page to say when you can come and, once the date is set, to authorize your seat. Nothing is
              charged when you join.
            </p>
            <form onSubmit={join} noValidate className="flex flex-col gap-4">
              <Field id="display-name" label="Your name" error={error ?? undefined} hint="The organizer sees this name next to your availability.">
                <input
                  id="display-name"
                  autoComplete="name"
                  maxLength={80}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  aria-invalid={error ? true : undefined}
                  aria-describedby={describedBy('display-name', true, error ?? undefined)}
                  className={`${inputClass} sm:max-w-sm`}
                />
              </Field>
              <div>
                <Button type="submit" busy={busy} busyLabel="Joining…">
                  Join workshop
                </Button>
              </div>
            </form>
          </section>
        ) : (
          <Notice title="This workshop is not taking new participants">
            {w.status === 'CANCELLED' ? 'It was cancelled.' : 'Its payment window has closed.'}
          </Notice>
        )}
      </div>
    </Page>
  )
}
