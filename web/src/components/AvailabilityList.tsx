import type { OwnerView } from '../api'
import { verdictText } from '../lib/reading'
import { formatDateRange } from '../lib/time'
import { Tag } from './ui'

export default function AvailabilityList({ view }: { view: OwnerView }) {
  const label = new Map(view.dates.map((d) => [d.id, formatDateRange(d.starts_at, d.ends_at, view.workshop.timezone)]))
  const answered = new Set(view.availabilities.map((a) => a.participant_id))
  const waiting = view.participants.filter((p) => !answered.has(p.id))

  return (
    <section aria-labelledby="availabilities-title" className="flex flex-col gap-4">
      <h2 id="availabilities-title" className="text-2xl font-bold">
        Availability
      </h2>
      {view.availabilities.length === 0 && waiting.length === 0 && (
        <p className="text-ink-soft">No one has joined yet. Participants appear here once they open the share link.</p>
      )}
      <ul className="flex flex-col">
        {view.availabilities.map((a) => {
          const reading = a.corrected ?? a.interpretation
          return (
            <li key={a.id} className="flex flex-col gap-3 border-t border-rule py-4">
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="mr-auto text-lg font-bold">{a.display_name}</h3>
                {a.corrected && <Tag tone="neutral">Corrected by participant</Tag>}
                {a.needs_clarification && <Tag tone="hold">Needs clarification</Tag>}
              </div>
              <blockquote className="max-w-prose border-l-2 border-rule pl-3 text-ink-soft">“{a.message}”</blockquote>
              <ul className="flex flex-col gap-1">
                {reading.per_date.map((v) => {
                  const original = a.corrected ? a.interpretation.per_date.find((o) => o.candidate_date_id === v.candidate_date_id) : undefined
                  const changed = original && verdictText(original) !== verdictText(v)
                  return (
                    <li key={v.candidate_date_id} className="grid gap-x-4 sm:grid-cols-[minmax(0,1fr)_auto]">
                      <span className="text-sm text-ink-soft">{label.get(v.candidate_date_id) ?? 'Candidate date'}</span>
                      <span className="font-medium">
                        {verdictText(v)}
                        {changed && <span className="text-sm font-normal text-ink-soft"> (read as: {verdictText(original)})</span>}
                      </span>
                    </li>
                  )
                })}
              </ul>
              {reading.needs_clarification && reading.ambiguities.length > 0 && (
                <p className="text-sm text-hold">Open question: {reading.ambiguities.map((q) => q.question).join(' ')}</p>
              )}
            </li>
          )
        })}
      </ul>
      {waiting.length > 0 && (
        <p className="text-ink-soft">
          Joined without availability yet: {waiting.map((p) => p.display_name).join(', ')}.
        </p>
      )}
    </section>
  )
}
