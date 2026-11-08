import { useState, type FormEvent } from 'react'
import { api, type ParticipantView } from '../api'
import { answerForm, type AnswerForm } from '../lib/participant'
import ReadingEditor from './ReadingEditor'
import { Button, Field, Icon, Notice, Tile, describedBy, inputClass } from './ui'

interface Props {
  token: string
  view: ParticipantView
  onSaved: () => Promise<unknown>
}

function AnswerBox({ token, form, onSaved }: { token: string; form: AnswerForm; onSaved: () => Promise<unknown> }) {
  const [message, setMessage] = useState(form.initial)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!message.trim()) {
      setError(form.clarifying ? 'Answer Claude’s question in a sentence or two.' : 'Write when you can come, in a sentence or two.')
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

  const described = [form.question ? 'claude-question' : '', describedBy('availability-message', true, error ?? undefined) ?? ''].filter(Boolean).join(' ')
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-3">
      {form.question && (
        <div className="flex flex-col gap-1.5 rounded-control bg-brand-wash p-4 ring-1 ring-brand/40 ring-inset sm:p-5">
          <p className="flex items-center gap-2 text-sm font-semibold text-brand">
            <Icon name="quote" size={16} />
            Claude asks you
          </p>
          <p id="claude-question" className="font-display text-xl leading-snug font-bold [overflow-wrap:anywhere]">
            {form.question}
          </p>
          <p className="text-sm text-ink-soft">Until you answer, your message counts for no date.</p>
        </div>
      )}
      <Field
        id="availability-message"
        label={form.label}
        hint={
          form.clarifying
            ? 'Answer in your own words. Claude reads it together with your previous message.'
            : 'Write it like you’d text a friend: “Saturday works if we finish by noon; Sunday I can’t.”'
        }
        error={error ?? undefined}
      >
        <textarea
          id="availability-message"
          rows={3}
          maxLength={2000}
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          aria-invalid={error ? true : undefined}
          aria-describedby={described || undefined}
          className={`${inputClass} resize-y text-[1.0625rem]`}
        />
      </Field>
      <div>
        <Button type="submit" busy={busy} busyLabel="Claude is reading it…">
          {form.button}
        </Button>
      </div>
    </form>
  )
}

export default function AvailabilityPanel({ token, view, onSaved }: Props) {
  const { availability, workshop, proposal } = view
  const accepting = workshop.status === 'COLLECTING' || (proposal?.payments_open ?? false)
  const form = answerForm(availability)

  if (!accepting && !availability) return null

  return (
    <Tile as="section" aria-labelledby="availability-title" className="flex flex-col gap-5 sm:p-7">
      <div className="flex flex-col gap-1">
        <h2 id="availability-title" className="text-2xl">
          Your answer
        </h2>
        {!proposal && <p className="max-w-prose text-ink-soft">The organizer picks the date most people can make. Nothing is charged at this step.</p>}
      </div>

      {accepting && <AnswerBox key={`${availability?.id ?? 'first'}:${form.clarifying}`} token={token} form={form} onSaved={onSaved} />}

      {availability ? (
        <ReadingEditor
          key={`${availability.id}:${availability.corrected ? JSON.stringify(availability.corrected) : 'reading'}`}
          token={token}
          availability={availability}
          dates={view.dates}
          timeZone={view.workshop.timezone}
          editable={accepting}
          asked={accepting && form.clarifying}
          onSaved={onSaved}
        />
      ) : (
        accepting && <Notice>Once you send it, Claude shows how it read your message for each date, and you can correct it.</Notice>
      )}
    </Tile>
  )
}
