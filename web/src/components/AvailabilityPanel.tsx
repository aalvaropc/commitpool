import { useState, type FormEvent } from 'react'
import { api, type ParticipantView } from '../api'
import ReadingEditor from './ReadingEditor'
import { Button, Field, Notice, Tile, describedBy, inputClass } from './ui'

interface Props {
  token: string
  view: ParticipantView
  onSaved: () => Promise<unknown>
}

export default function AvailabilityPanel({ token, view, onSaved }: Props) {
  const { availability, workshop, proposal } = view
  const accepting = workshop.status === 'COLLECTING' || (proposal?.payments_open ?? false)
  const [message, setMessage] = useState(availability?.message ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!message.trim()) {
      setError('Write when you can come, in a sentence or two.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await api.submitAvailability(token, message.trim())
      await onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Your message could not be sent.')
    } finally {
      setBusy(false)
    }
  }

  if (!accepting && !availability) return null

  return (
    <Tile as="section" aria-labelledby="availability-title" className="flex flex-col gap-5 sm:p-7">
      <div className="flex flex-col gap-1">
        <h2 id="availability-title" className="text-2xl">
          Your answer
        </h2>
        {!proposal && <p className="max-w-prose text-ink-soft">The organizer picks the date most people can make. Nothing is charged at this step.</p>}
      </div>

      {accepting && (
        <form onSubmit={submit} noValidate className="flex flex-col gap-3">
          <Field
            id="availability-message"
            label="When can you come?"
            hint="Write it like you’d text a friend: “Saturday works if we finish by noon; Sunday I can’t.”"
            error={error ?? undefined}
          >
            <textarea
              id="availability-message"
              rows={3}
              maxLength={2000}
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              aria-invalid={error ? true : undefined}
              aria-describedby={describedBy('availability-message', true, error ?? undefined)}
              className={`${inputClass} resize-y text-[1.0625rem]`}
            />
          </Field>
          <div>
            <Button type="submit" busy={busy} busyLabel="Claude is reading it…">
              {availability ? 'Update my availability' : 'Send my availability'}
            </Button>
          </div>
        </form>
      )}

      {availability ? (
        <ReadingEditor
          key={`${availability.id}:${availability.corrected ? JSON.stringify(availability.corrected) : 'reading'}`}
          token={token}
          availability={availability}
          dates={view.dates}
          timeZone={view.workshop.timezone}
          editable={accepting}
          onSaved={onSaved}
        />
      ) : (
        accepting && <Notice>Once you send it, Claude shows how it read your message for each date, and you can correct it.</Notice>
      )}
    </Tile>
  )
}
