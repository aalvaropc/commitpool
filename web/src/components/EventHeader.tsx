import type { PublicView } from '../api'
import { dateRows, groupSummary } from '../lib/event'
import { formatMoney } from '../lib/money'
import { count } from '../lib/plural'
import { durationLabel, zoneName } from '../lib/time'
import Deadline from './Deadline'
import { Avatar, Icon, Meter, Pill, Tile } from './ui'

export function EventHero({ view, compact = false }: { view: PublicView; compact?: boolean }) {
  const w = view.workshop
  return (
    <Tile
      as="header"
      tone="brand"
      className={`relative flex flex-col justify-end overflow-hidden ${compact ? 'min-h-36 sm:min-h-44 sm:p-7' : 'min-h-52 sm:min-h-64 sm:p-8'}`}
    >
      <div className="relative flex min-w-0 flex-col gap-3">
        {w.demo && (
          <span className="w-fit rounded-full bg-on-brand/10 px-3 py-1 text-sm font-semibold text-on-brand">Demo workshop · prepared data</span>
        )}
        <h1 className={`shout max-w-[16ch] leading-[0.92] [overflow-wrap:anywhere] hyphens-auto ${compact ? 'text-4xl sm:text-6xl' : 'text-5xl sm:text-7xl'}`}>
          {w.title}
        </h1>
        {(w.host_name || w.location) && (
          <p className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-base font-semibold sm:text-lg">
            {w.host_name && (
              <span className="flex items-center gap-2.5">
                <Avatar name={w.host_name} className="ring-2 ring-on-brand/25" />
                with {w.host_name}
              </span>
            )}
            {w.host_name && w.location && (
              <span aria-hidden="true" className="hidden sm:inline">
                ·
              </span>
            )}
            {w.location && <span className="min-w-0 basis-full font-medium text-on-brand/80 [overflow-wrap:anywhere] sm:basis-auto">{w.location}</span>}
          </p>
        )}
      </div>
    </Tile>
  )
}

function groupState(view: PublicView): string | null {
  const { workshop: w, proposal } = view
  if (w.status === 'CANCELLED') return 'This class was cancelled. Every hold is released and every payment refunded.'
  if (w.status === 'CONFIRMED' && !proposal?.payments_open) return 'It’s on. Reservations are closed.'
  if (proposal && !proposal.payments_open) return 'Reservations are closed. The result follows in a moment.'
  return null
}

export function EventDetails({ view }: { view: PublicView }) {
  const { workshop: w, proposal, tally, group } = view
  const rows = dateRows(view.dates, group, proposal?.date_id ?? null, w.timezone)
  const reserving = proposal?.payments_open ?? false
  const accepting = w.status === 'COLLECTING' || reserving
  const state = groupState(view)
  const summary = groupSummary(group, tally.minimum)
  const capacity = proposal?.capacity ?? w.capacity
  const interest = Math.max(0, ...rows.map((r) => r.canCome))
  return (
    <div className="grid grid-cols-[minmax(0,1fr)] gap-3 sm:gap-4 lg:grid-cols-12">
      <div className="flex flex-col gap-3 sm:gap-4 lg:col-span-7">
      <section aria-labelledby="dates-title" className="flex flex-1 flex-col gap-4 bg-surface p-5 sm:p-7">
        <div className="flex items-baseline justify-between gap-3">
          <h2 id="dates-title" className="text-2xl">
            {proposal || rows.length === 1 ? 'The date' : accepting ? 'Pick from these dates' : 'Dates'}
          </h2>
          <span className="text-sm text-ink-soft">{zoneName(w.timezone)}</span>
        </div>
        <ul className="flex flex-col gap-2">
          {rows.map((r) => {
            const strong = r.best || r.chosen
            return (
              <li key={r.id} className={`flex min-h-16 items-center justify-between gap-3 px-4 py-3 ${strong ? 'bg-brand text-on-brand' : 'bg-canvas'}`}>
                <span className="flex items-center gap-3">
                  <Icon name="calendar" size={22} />
                  <span className="flex flex-col leading-tight">
                    <span className="font-display text-xl font-bold">{r.day}</span>
                    <span className={`text-sm ${strong ? 'text-on-brand/80' : 'text-ink-soft'}`}>{r.time}</span>
                  </span>
                </span>
                {group.answered > 0 && !proposal && (
                  <span className="text-right text-sm font-semibold">
                    {r.canCome} can come
                    {r.best && <span className="block font-medium text-on-brand/80">most so far</span>}
                  </span>
                )}
              </li>
            )
          })}
        </ul>
      </section>

        <div className="grid grid-cols-2 gap-3 sm:gap-4">
      <section aria-labelledby="price-title" className="flex flex-col justify-between gap-2 bg-surface p-5 sm:p-7">
        <h2 id="price-title" className="text-sm font-semibold text-ink-soft">
          Price
        </h2>
        <p className="font-display text-4xl font-bold tracking-tight tabular-nums">{formatMoney(proposal?.price ?? w.price)}</p>
        <p className="text-sm text-ink-soft">{accepting ? 'Charged only if it happens' : 'Not taking reservations'}</p>
      </section>

      <section aria-labelledby="class-title" className="flex flex-col justify-between gap-2 bg-surface p-5 sm:p-7">
        <h2 id="class-title" className="text-sm font-semibold text-ink-soft">
          Class
        </h2>
        <p className="font-display text-4xl font-bold tracking-tight">{durationLabel(w.duration_min)}</p>
        <p className="text-sm text-ink-soft">{count(capacity, 'spot')}</p>
      </section>

        </div>
      </div>

      <section aria-labelledby="group-title" className="flex flex-col items-center gap-4 bg-surface p-5 text-center sm:p-7 lg:col-span-5 lg:items-start lg:text-left">
        <div className="flex w-full items-center justify-between gap-3">
          <h2 id="group-title" className="text-2xl">
            {reserving ? 'Who’s in' : 'The group'}
          </h2>
          {reserving && <Pill tone="hold">Reservations open</Pill>}
        </div>
        {state ? (
          <p className="text-lg">{state}</p>
        ) : proposal ? (
          <>
            <Meter capacity={tally.capacity} needed={tally.minimum} paid={tally.paid} reserved={tally.authorized + tally.charging} />
            {reserving && <Deadline closesAt={proposal.closes_at} timeZone={w.timezone} open />}
          </>
        ) : (
          <>
            <Meter capacity={capacity} needed={tally.minimum} paid={0} reserved={interest} interest className="w-full" />
            <p className="text-lg">
              {summary.count !== null && <span className="font-display text-3xl font-bold tabular-nums">{summary.count} </span>}
              {summary.text}
            </p>
            {summary.count !== null && interest > 0 && <p className="text-sm text-ink-soft">{interest} can come on the best date.</p>}
          </>
        )}
      </section>

      {w.description && (
        <section aria-labelledby="about-title" className="bg-surface p-5 sm:p-7 lg:col-span-12">
          <h2 id="about-title" className="text-sm font-semibold text-ink-soft">
            About this class
          </h2>
          <p className="mt-2 max-w-prose whitespace-pre-line text-lg">{w.description}</p>
        </section>
      )}
    </div>
  )
}
