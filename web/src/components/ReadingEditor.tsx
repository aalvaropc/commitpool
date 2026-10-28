import { useState } from 'react'
import { api, type Availability, type CandidateDate, type ConditionType, type DateVerdict, type Verdict } from '../api'
import { correctedReading, saveButton, verdictText } from '../lib/reading'
import { Button, Notice, inputClass } from './ui'

interface Props {
  token: string
  availability: Availability
  dates: CandidateDate[]
  editable: boolean
  onSaved: () => Promise<unknown>
}

const VERDICTS: { value: Verdict; label: string }[] = [
  { value: 'available', label: 'I can come' },
  { value: 'unavailable', label: 'I can’t come' },
  { value: 'conditional', label: 'Only with a condition' },
  { value: 'unknown', label: 'Not sure' },
]

const selectClass = `${inputClass} w-auto py-2`

/** The reading of a participant's message, one verdict per date, correctable by hand. */
export default function ReadingEditor({ token, availability, dates, editable, onSaved }: Props) {
  const reading = availability.corrected ?? availability.interpretation
  const [draft, setDraft] = useState<DateVerdict[]>(reading.per_date)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dirty = JSON.stringify(draft) !== JSON.stringify(reading.per_date)
  const button = saveButton(dirty, reading.needs_clarification)

  function update(i: number, patch: Partial<DateVerdict>) {
    setDraft((d) => d.map((v, j) => (j === i ? { ...v, ...patch } : v)))
  }

  function setVerdict(i: number, verdict: Verdict) {
    const v = draft[i]
    const condition = verdict === 'conditional' && v.condition.type === 'none' ? { type: 'end_before' as const, time: '' } : v.condition
    update(i, { verdict, condition })
  }

  async function save() {
    setError(null)
    const missing = draft.find((v) => v.verdict === 'conditional' && !/^\d{2}:\d{2}$/.test(v.condition.time))
    if (missing) {
      setError('Give a time for every date that has a condition.')
      return
    }
    setBusy(true)
    try {
      await api.correctAvailability(token, availability.id, correctedReading(reading, draft))
      await onSaved()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Your correction could not be saved.')
    } finally {
      setBusy(false)
    }
  }

  const label = (id: string) => dates.find((d) => d.id === id)?.label ?? 'Candidate date'

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-lg font-bold">How we read it</h3>
        <p className="text-sm text-ink-soft">
          {availability.corrected
            ? 'You corrected this reading; the organizer plans with it.'
            : 'Read automatically from your message. The organizer plans with it, so fix anything that is wrong.'}
        </p>
      </div>

      {reading.needs_clarification && (
        <Notice tone="hold" title="One detail is unclear">
          {reading.ambiguities.length > 0 && (
            <ul className="list-disc pl-5">
              {reading.ambiguities.map((a, i) => (
                <li key={i}>{a.question}</li>
              ))}
            </ul>
          )}
          <p>
            Until it is answered your message counts for no date. Check the answer for each date below and confirm it, or rewrite your
            message.
          </p>
        </Notice>
      )}

      <ul className="flex flex-col">
        {draft.map((v, i) => (
          <li key={v.candidate_date_id} className="flex flex-col gap-2 border-t border-rule py-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
            <div className="min-w-0">
              <p className="font-semibold">{label(v.candidate_date_id)}</p>
              {v.evidence && <p className="text-sm text-ink-soft">From your message: “{v.evidence}”</p>}
            </div>
            {editable ? (
              <div className="flex flex-wrap items-center gap-2">
                <label htmlFor={`verdict-${v.candidate_date_id}`} className="sr-only">
                  Can you come on {label(v.candidate_date_id)}?
                </label>
                <select
                  id={`verdict-${v.candidate_date_id}`}
                  value={v.verdict}
                  onChange={(e) => setVerdict(i, e.target.value as Verdict)}
                  className={selectClass}
                >
                  {VERDICTS.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
                {v.verdict === 'conditional' && (
                  <>
                    <label htmlFor={`condition-${v.candidate_date_id}`} className="sr-only">
                      Condition for {label(v.candidate_date_id)}
                    </label>
                    <select
                      id={`condition-${v.candidate_date_id}`}
                      value={v.condition.type}
                      onChange={(e) => update(i, { condition: { ...v.condition, type: e.target.value as ConditionType } })}
                      className={selectClass}
                    >
                      <option value="end_before">if it ends by</option>
                      <option value="start_after">if it starts at or after</option>
                    </select>
                    <label htmlFor={`time-${v.candidate_date_id}`} className="sr-only">
                      Time for {label(v.candidate_date_id)}
                    </label>
                    <input
                      id={`time-${v.candidate_date_id}`}
                      type="time"
                      required
                      value={v.condition.time}
                      onChange={(e) => update(i, { condition: { ...v.condition, time: e.target.value } })}
                      className={`${inputClass} w-32 py-2`}
                    />
                  </>
                )}
              </div>
            ) : (
              <p className="font-medium">{verdictText(v)}</p>
            )}
          </li>
        ))}
      </ul>

      {editable && (
        <div className="flex flex-col items-start gap-3">
          <Button variant={reading.needs_clarification ? 'primary' : 'secondary'} onClick={save} disabled={!button.enabled} busy={busy} busyLabel="Saving…">
            {button.label}
          </Button>
          {error && (
            <Notice tone="attention" role="alert">
              {error}
            </Notice>
          )}
        </div>
      )}
    </div>
  )
}
