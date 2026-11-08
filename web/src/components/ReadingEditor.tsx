import { useState } from 'react'
import { api, type Availability, type CandidateDate, type ConditionType, type DateVerdict, type Verdict } from '../api'
import { correctedReading, saveButton, verdictText } from '../lib/reading'
import { formatDateRange } from '../lib/time'
import { Button, Icon, Notice, Pill, SegmentedControl, VerdictChip, inputClass, type Segment } from './ui'

interface Props {
  token: string
  availability: Availability
  dates: CandidateDate[]
  timeZone: string
  editable: boolean
  asked?: boolean
  onSaved: () => Promise<unknown>
}

const VERDICTS: Segment<Verdict>[] = [
  { value: 'available', label: 'Can come' },
  { value: 'conditional', label: 'Only if…' },
  { value: 'unavailable', label: 'Can’t' },
  { value: 'unknown', label: 'Not sure' },
]

export default function ReadingEditor({ token, availability, dates, timeZone, editable, asked = false, onSaved }: Props) {
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

  const label = (id: string) => {
    const d = dates.find((x) => x.id === id)
    return d ? formatDateRange(d.starts_at, d.ends_at, timeZone) : 'Candidate date'
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="text-lg">How we read it</h3>
          <Pill tone="hold" dot={false}>
            {availability.corrected ? 'Corrected by you' : 'Read by Claude'}
          </Pill>
        </div>
        <p className="text-sm text-ink-soft">
          {availability.corrected ? 'The organizer plans with your corrected answer.' : 'The organizer plans with this reading, so fix anything that is wrong.'}
        </p>
      </div>

      {reading.needs_clarification &&
        (asked ? (
          <Notice tone="attention" title="One detail is unclear">
            <p>Answer Claude’s question above, or set each date yourself under Edit reading and confirm it.</p>
          </Notice>
        ) : (
          <Notice tone="attention" title="One detail is unclear">
            {reading.ambiguities.length > 0 && (
              <ul className="list-disc pl-5">
                {reading.ambiguities.map((a, i) => (
                  <li key={i}>{a.question}</li>
                ))}
              </ul>
            )}
            <p>Until it is answered your message counts for no date.{editable ? ' Check each date under Edit reading and confirm it, or rewrite your message.' : ''}</p>
          </Notice>
        ))}

      <ul className="flex flex-col gap-3" aria-label="Your answer for each date">
        {reading.per_date.map((v) => (
          <li key={v.candidate_date_id} className="flex flex-col gap-2 rounded-control p-3.5 ring-1 ring-line ring-inset sm:p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="font-semibold">{label(v.candidate_date_id)}</p>
              <VerdictChip verdict={v.verdict}>{verdictText(v)}</VerdictChip>
            </div>
            {v.evidence && <p className="text-sm text-ink-soft">From your message: <span className="font-medium">“{v.evidence}”</span></p>}
          </li>
        ))}
      </ul>

      {editable && (
        <details className="group rounded-control ring-1 ring-line ring-inset">
          <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 rounded-control px-4 py-2.5 font-semibold [&::-webkit-details-marker]:hidden">
            Edit reading
            <Icon name="arrowRight" size={18} className="shrink-0 text-brand transition-transform duration-200 group-open:rotate-90" />
          </summary>
          <div className="flex flex-col gap-4 px-4 pt-1 pb-4">
            {draft.map((v, i) => (
              <div key={v.candidate_date_id} className="flex flex-col gap-2">
                <SegmentedControl
                  legend={`Can you come on ${label(v.candidate_date_id)}?`}
                  legendHidden={false}
                  name={`verdict-${v.candidate_date_id}`}
                  value={v.verdict}
                  options={VERDICTS}
                  onChange={(verdict) => setVerdict(i, verdict)}
                />
                {v.verdict === 'conditional' && (
                  <div className="flex flex-wrap items-center gap-2">
                    <label htmlFor={`condition-${v.candidate_date_id}`} className="sr-only">
                      Condition for {label(v.candidate_date_id)}
                    </label>
                    <select
                      id={`condition-${v.candidate_date_id}`}
                      value={v.condition.type}
                      onChange={(e) => update(i, { condition: { ...v.condition, type: e.target.value as ConditionType } })}
                      className={`${inputClass} w-auto`}
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
                      className={`${inputClass} w-36`}
                    />
                  </div>
                )}
              </div>
            ))}
            <div className="flex flex-col items-start gap-3">
              <Button variant={reading.needs_clarification && !asked ? 'primary' : 'secondary'} onClick={save} disabled={!button.enabled} busy={busy} busyLabel="Saving…">
                {button.label}
              </Button>
              {error && (
                <Notice tone="attention" role="alert">
                  {error}
                </Notice>
              )}
            </div>
          </div>
        </details>
      )}
    </div>
  )
}
