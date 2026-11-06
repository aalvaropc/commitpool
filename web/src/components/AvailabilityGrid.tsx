import type { Availability, DateSummary, OwnerView, ParticipantRef } from '../api'
import { cellNotes, dateColumns, type DateColumn } from '../lib/owner'
import { verdictText } from '../lib/reading'
import { Avatar, Button, Pill, Tile, VerdictChip } from './ui'

interface Props {
  view: OwnerView
  onOpen: (dateId: string) => void
}

const markTone = (c: DateColumn, meets: boolean) => (c.mark === 'Recommended' ? (meets ? 'paid' : 'neutral') : 'hold')
const wrap = 'min-w-0 [overflow-wrap:anywhere]'

function Cell({ a, dateId, summary }: { a: Availability; dateId: string; summary?: DateSummary }) {
  const v = (a.corrected ?? a.interpretation).per_date.find((x) => x.candidate_date_id === dateId)
  if (!v) return <span className="text-sm text-ink-soft">Not mentioned</span>
  const notes = cellNotes(a, dateId, summary)
  return (
    <span className="flex flex-col items-start gap-1">
      <VerdictChip verdict={v.verdict}>{verdictText(v)}</VerdictChip>
      {notes.fit && <span className="text-xs font-semibold text-ink">{notes.fit}</span>}
      {notes.claudeRead && <span className="text-xs text-ink-soft">{notes.claudeRead}</span>}
      {v.evidence && <span className={`text-xs text-ink-soft ${wrap}`}>“{v.evidence}”</span>}
    </span>
  )
}

function Message({ a, name }: { a?: Availability; name: string }) {
  if (!a) return <span className="text-sm text-ink-soft">No answer yet</span>
  const reading = a.corrected ?? a.interpretation
  return (
    <span className={`flex flex-col items-start gap-1 ${wrap}`}>
      <span className="text-sm text-ink-soft italic">“{a.message}”</span>
      {a.needs_clarification && <Pill tone="attention">Needs clarifying</Pill>}
      {a.corrected && <span className="text-xs text-ink-soft">Corrected by {name}</span>}
      {reading.needs_clarification && reading.ambiguities.length > 0 && (
        <span className="text-sm text-attention">Open question: {reading.ambiguities.map((q) => q.question).join(' ')}</span>
      )}
    </span>
  )
}

function DateSummaryList({ columns, view, onOpen }: { columns: DateColumn[]; view: OwnerView; onOpen: (dateId: string) => void }) {
  return (
    <ul className="flex flex-col gap-2" aria-label="Dates">
      {columns.map((c) => (
        <li key={c.id} className={`flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5 rounded-control p-3 ${c.mark ? 'bg-brand-wash' : 'ring-1 ring-line ring-inset'}`}>
          <span className="flex min-w-0 flex-col">
            <span className="font-display font-bold">{c.name}</span>
            <span className="text-sm text-ink-soft tabular-nums">
              {c.time} · <span className="font-semibold text-ink">{c.can} can come</span> of {view.workshop.minimum} needed
            </span>
          </span>
          {c.mark && <Pill tone={markTone(c, view.recommendation.meets_minimum)}>{c.mark}</Pill>}
          {c.canOpen && (
            <Button variant="quiet" size="sm" onClick={() => onOpen(c.id)} className="-ml-3">
              Open reservations here<span className="sr-only"> for {c.name}</span>
            </Button>
          )}
        </li>
      ))}
    </ul>
  )
}

function PersonCard({ p, a, columns, summaries }: { p: ParticipantRef; a?: Availability; columns: DateColumn[]; summaries: Map<string, DateSummary> }) {
  return (
    <li className="flex flex-col gap-2.5 rounded-control p-3.5 ring-1 ring-line ring-inset">
      <p className="flex min-w-0 items-center gap-2.5">
        <Avatar name={p.display_name} size="sm" />
        <span className={`font-semibold ${wrap}`}>{p.display_name}</span>
      </p>
      <Message a={a} name={p.display_name} />
      {a && (
        <ul className="flex flex-col gap-2">
          {columns.map((c) => (
            <li key={c.id} className="flex flex-wrap items-start justify-between gap-2">
              <span className="text-sm font-semibold">{c.name}</span>
              <Cell a={a} dateId={c.id} summary={summaries.get(c.id)} />
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

export default function AvailabilityGrid({ view, onOpen }: Props) {
  const columns = dateColumns(view)
  const summaries = new Map((view.recommendation.per_date ?? []).map((d) => [d.date_id, d]))
  const byParticipant = new Map(view.availabilities.map((a) => [a.participant_id, a]))
  const people = view.participants
  const openable = columns.some((c) => c.canOpen)

  return (
    <Tile as="section" aria-labelledby="grid-title" className="flex flex-col gap-4 sm:p-6">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="grid-title" className="text-2xl">
          Who can come
        </h2>
        <p className="text-sm text-ink-soft">
          {view.availabilities.length > 0 ? `${view.availabilities.length} of ${people.length} answered · read by Claude` : 'No one has answered yet'}
        </p>
      </div>

      {people.length === 0 ? (
        <p className="text-ink-soft">Share the invite. Each person appears here with their answer for every date.</p>
      ) : (
        <>
          <div className="flex flex-col gap-4 sm:hidden">
            <DateSummaryList columns={columns} view={view} onOpen={onOpen} />
            <ul className="flex flex-col gap-3" aria-label="People">
              {people.map((p) => (
                <PersonCard key={p.id} p={p} a={byParticipant.get(p.id)} columns={columns} summaries={summaries} />
              ))}
            </ul>
          </div>
          <div role="region" aria-label="Who can come, by date" tabIndex={0} className="-mx-1 hidden overflow-x-auto rounded-control px-1 pb-1 sm:block">
            <table className="w-full min-w-[40rem] border-separate border-spacing-0 text-left">
              <caption className="sr-only">Each person’s answer for every candidate date, as Claude read it from their message</caption>
              <thead>
                <tr>
                  <th scope="col" className="w-40 pb-3 align-bottom text-xs font-semibold tracking-[0.06em] text-ink-soft uppercase">
                    Person
                  </th>
                  <th scope="col" className="w-[26%] px-3 pb-3 align-bottom text-xs font-semibold tracking-[0.06em] text-ink-soft uppercase">
                    Their message
                  </th>
                  {columns.map((c) => (
                    <th key={c.id} scope="col" className={`rounded-t-control px-3 pt-3 pb-3 align-bottom ${c.mark ? 'bg-brand-wash' : ''}`}>
                      <span className="flex flex-col gap-1">
                        {c.mark && (
                          <span>
                            <Pill tone={markTone(c, view.recommendation.meets_minimum)}>{c.mark}</Pill>
                          </span>
                        )}
                        <span className="font-display text-base font-bold">{c.name}</span>
                        <span className="text-sm font-normal text-ink-soft">{c.time}</span>
                        <span className="font-semibold text-ink tabular-nums">
                          {c.can} can come <span className="font-normal text-ink-soft">· {view.workshop.minimum} needed</span>
                        </span>
                      </span>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {people.map((p) => {
                  const a = byParticipant.get(p.id)
                  return (
                    <tr key={p.id}>
                      <th scope="row" className="border-t border-line py-3 pr-3 align-top font-normal">
                        <span className="flex min-w-0 items-center gap-2.5">
                          <Avatar name={p.display_name} size="sm" />
                          <span className={`font-semibold text-ink ${wrap}`}>{p.display_name}</span>
                        </span>
                      </th>
                      <td className="border-t border-line px-3 py-3 align-top">
                        <Message a={a} name={p.display_name} />
                      </td>
                      {columns.map((c) => (
                        <td key={c.id} className={`border-t border-line px-3 py-3 align-top ${c.mark ? 'bg-brand-wash/60' : ''}`}>
                          {a ? <Cell a={a} dateId={c.id} summary={summaries.get(c.id)} /> : <span className="text-sm text-ink-soft">—</span>}
                        </td>
                      ))}
                    </tr>
                  )
                })}
              </tbody>
              {openable && (
                <tfoot>
                  <tr>
                    <td colSpan={2} className="border-t border-line pt-3" />
                    {columns.map((c) => (
                      <td key={c.id} className="border-t border-line px-3 pt-3">
                        {c.canOpen && (
                          <Button variant="quiet" size="sm" onClick={() => onOpen(c.id)} className="-ml-3">
                            Open reservations here<span className="sr-only"> for {c.name}</span>
                          </Button>
                        )}
                      </td>
                    ))}
                  </tr>
                </tfoot>
              )}
            </table>
          </div>
        </>
      )}
    </Tile>
  )
}
