import { useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { api, ApiError } from '../api'
import { EventDetails, EventHero } from '../components/EventHeader'
import { Loading, LoadError } from '../components/PageState'
import { Button, ButtonLink, Field, Notice, Page, Tile, describedBy, inputClass } from '../components/ui'
import { useResource } from '../lib/hooks'
import { joinedAs, rememberJoin, tokenFromParticipantURL } from '../lib/participant'

export default function Share() {
  const { share = '' } = useParams()
  const navigate = useNavigate()
  const view = useResource(() => api.shared(share), share)
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [emailError, setEmailError] = useState<string | null>(null)
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
    setEmailError(null)
    try {
      const res = await api.join(share, name.trim(), email.trim())
      const token = tokenFromParticipantURL(res.participant_url)
      if (!token) throw new Error('The server answered with an address this page cannot open.')
      rememberJoin(share, token, name.trim())
      navigate(`/p/${token}`)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'invalid_email') setEmailError(err.message)
      else setError(err instanceof Error ? err.message : 'You could not join right now.')
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
  const already = joinedAs(share)

  return (
    <Page wide>
      <title>{`${w.title} – CommitPool`}</title>
      <div className="mx-auto flex max-w-4xl flex-col gap-4 sm:gap-5">
        <EventHero view={d} />
        {!open && (
          <Notice tone={w.status === 'CANCELLED' ? 'cancel' : 'neutral'} title="This class is not taking new people">
            {w.status === 'CANCELLED' ? 'It was cancelled.' : 'Reservations have closed.'}
          </Notice>
        )}
        <EventDetails view={d} />

        {already && (
          <Tile as="section" aria-labelledby="continue-title" className="flex flex-col items-start gap-3">
            <h2 id="continue-title" className="text-xl">
              You already joined
            </h2>
            <p className="text-ink-soft">This browser tab joined as {already.name}.</p>
            <ButtonLink to={`/p/${already.token}`}>Continue as {already.name}</ButtonLink>
          </Tile>
        )}

        {open ? (
          <Tile as="section" aria-labelledby="join-title" className="flex flex-col gap-5 sm:p-7">
            <div className="flex flex-col gap-1">
              <h2 id="join-title" className="text-2xl">
                {already ? 'Join as someone else' : 'Join and say when you can come'}
              </h2>
              <p className="max-w-prose text-ink-soft">
                You get a personal page to answer and, once the date is set, to reserve your spot. Nothing is charged when you join.
              </p>
            </div>
            <form onSubmit={join} noValidate className="flex flex-col gap-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field id="display-name" label="Your name" error={error ?? undefined} hint="The organizer sees this name next to your answer.">
                  <input
                    id="display-name"
                    autoComplete="name"
                    maxLength={80}
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    aria-invalid={error ? true : undefined}
                    aria-describedby={describedBy('display-name', true, error ?? undefined)}
                    className={inputClass}
                  />
                </Field>
                <Field id="email" label="Email (optional)" error={emailError ?? undefined} hint="Only for your personal link and news about your spot.">
                  <input
                    id="email"
                    type="email"
                    autoComplete="email"
                    maxLength={254}
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    aria-invalid={emailError ? true : undefined}
                    aria-describedby={describedBy('email', true, emailError ?? undefined)}
                    className={inputClass}
                  />
                </Field>
              </div>
              <Button type="submit" size="lg" block busy={busy} busyLabel="Joining…">
                Join the class
              </Button>
            </form>
          </Tile>
        ) : null}
      </div>
    </Page>
  )
}
