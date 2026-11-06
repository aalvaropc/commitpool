import type { OwnerView } from '../api'
import { verdictText } from '../lib/reading'
import { formatDay, formatTimeRange } from '../lib/time'
import { Avatar, Button, Pill, Tile, VerdictChip } from './ui'

export default function AvailabilityGrid({ view, onOpen }: { view: OwnerView; onOpen: (dateId: string) => void }) {
  const { dates, workshop: w, recommendation: rec } = view
  const byParticipant = new Map(view.availabilities.map((a) => [a.participant_id, a]))
  const perDate = new Map((rec.per_date ?? []).map((d) => [d.date_id, d.compatible?.length ?? 0]))
  const anyone = view.availabilities.length > 0
  const people = view.participants

  return (
    <Tile as="section" aria-labelledby="grid-title" className="flex flex-col gap-4 sm:p-6">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="grid-title" className="text-2xl">
          Who can come
        </h2>
        <p className="text-sm text-ink-soft">
          {anyone ? `${view.availabilities.length} of ${people.length} answered · read by Claude` : 'No one has answered yet'}
        </p>
      </div>

      {people.length === 0 ? (
        <p className="text-ink-soft">Share the invite. Each person appears here with their answer for every date.</p>
      ) : (
        <>
        <ul className="flex flex-col gap-3 sm:hidden">
          {people.map((p) => {
            const a = byParticipant.get(p.id)
            const reading = a ? (a.corrected ?? a.interpretation) : null
            return (
              <li key={p.id} className="flex flex-col gap-2.5 rounded-control p-3.5 ring-1 ring-line ring-inset">
                <p className="flex items-center gap-2.5">
                  <Avatar name={p.display_name} size="sm" />
                  <span className="font-semibold">{p.display_name}</span>
                  {a?.needs_clarification && <Pill tone="attention">Needs clarifying</Pill>}
                </p>
                {a ? <p className="text-sm text-ink-soft italic">“{a.message}”</p> : <p className="text-sm text-ink-soft">No answer yet</p>}
                {reading && (
                  <ul className="flex flex-col gap-1.5">
                    {dates.map((d) => {
                      const v = reading.per_date.find((x) => x.candidate_date_id === d.id)
                      return (
                        <li key={d.id} className="flex flex-wrap items-center justify-between gap-2">
                          <span className="text-sm font-semibold">{formatDay(d.starts_at, w.timezone)}</span>
                          {v && <VerdictChip verdict={v.verdict}>{verdictText(v)}</VerdictChip>}
                        </li>
                      )
                    })}
                  </ul>
                )}
                {reading?.needs_clarification && reading.ambiguities.length > 0 && (
                  <p className="text-sm text-attention">Open question: {reading.ambiguities.map((q) => q.question).join(' ')}</p>
                )}
              </li>
            )
          })}
        </ul>
        <div role="region" aria-labelledby="grid-title" tabIndex={0} className="-mx-1 hidden overflow-x-auto rounded-control px-1 pb-1 sm:block">
          <table className="w-full min-w-[34rem] border-separate border-spacing-0 text-left">
            <caption className="sr-only">Each person’s answer for every candidate date, as Claude read it from their message</caption>
            <thead>
              <tr>
                <th scope="col" className="w-[30%] pb-3 align-bottom text-xs font-semibold tracking-[0.06em] text-ink-soft uppercase">
                  Person
                </th>
                {dates.map((d) => {
                  const recommended = anyone && rec.date_id === d.id
                  return (
                    <th key={d.id} scope="col" className={`rounded-t-control px-3 pt-3 pb-3 align-bottom ${recommended ? 'bg-brand-wash' : ''}`}>
                      <span className="flex flex-col gap-1">
                        {recommended && (
                          <span>
                            <Pill tone={rec.meets_minimum ? 'paid' : 'neutral'}>Recommended</Pill>
                          </span>
                        )}
                        <span className="font-display text-base font-bold">{formatDay(d.starts_at, w.timezone)}</span>
                        <span className="text-sm font-normal text-ink-soft">{formatTimeRange(d.starts_at, d.ends_at, w.timezone)}</span>
                        <span className="font-semibold text-ink tabular-nums">
                          {perDate.get(d.id) ?? 0} can come <span className="font-normal text-ink-soft">· {w.minimum} needed</span>
                        </span>
                        {w.status === 'COLLECTING' && !recommended && (perDate.get(d.id) ?? 0) >= w.minimum && (
                          <span>
                            <Button variant="quiet" size="sm" onClick={() => onOpen(d.id)} className="-ml-3">
                              Open reservations here
                            </Button>
                          </span>
                        )}
                      </span>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {people.map((p) => {
                const a = byParticipant.get(p.id)
                const reading = a ? (a.corrected ?? a.interpretation) : null
                return (
                  <tr key={p.id}>
                    <th scope="row" className="border-t border-line py-3 pr-3 align-top font-normal">
                      <span className="flex items-start gap-2.5">
                        <Avatar name={p.display_name} size="sm" />
                        <span className="flex min-w-0 flex-col gap-1">
                          <span className="font-semibold text-ink">{p.display_name}</span>
                          {a && <span className="text-sm text-ink-soft italic">“{a.message}”</span>}
                          {a?.needs_clarification && (
                            <span>
                              <Pill tone="attention">Needs clarifying</Pill>
                            </span>
                          )}
                          {a?.corrected && <span className="text-xs text-ink-soft">Corrected by {p.display_name}</span>}
                          {reading?.needs_clarification && reading.ambiguities.length > 0 && (
                            <span className="text-sm text-attention">Open question: {reading.ambiguities.map((q) => q.question).join(' ')}</span>
                          )}
                        </span>
                      </span>
                    </th>
                    {dates.map((d) => {
                      const v = reading?.per_date.find((x) => x.candidate_date_id === d.id)
                      const recommended = anyone && rec.date_id === d.id
                      return (
                        <td key={d.id} className={`border-t border-line px-3 py-3 align-top ${recommended ? 'bg-brand-wash/60' : ''}`}>
                          {v ? (
                            <span className="flex flex-col items-start gap-1">
                              <VerdictChip verdict={v.verdict}>{verdictText(v)}</VerdictChip>
                              {v.evidence && <span className="text-xs text-ink-soft">“{v.evidence}”</span>}
                            </span>
                          ) : (
                            <span className="text-sm text-ink-soft">No answer yet</span>
                          )}
                        </td>
                      )
                    })}
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
        </>
      )}
    </Tile>
  )
}
