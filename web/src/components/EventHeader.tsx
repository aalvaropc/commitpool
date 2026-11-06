import type { PublicView } from '../api'
import { dateRows, groupSummary } from '../lib/event'
import { formatMoney } from '../lib/money'
import { count } from '../lib/plural'
import { durationLabel, zoneName } from '../lib/time'
import Deadline from './Deadline'
import { Avatar, Cover, Icon, Meter, Pill, Tile, TileLabel } from './ui'

export function heroSeed(view: PublicView): string {
  return `${view.workshop.title}:${view.dates[0]?.id ?? ''}`
}

export function EventHero({ view, compact = false }: { view: PublicView; compact?: boolean }) {
  const w = view.workshop
  return (
    <Tile
      as="header"
      tone="brand"
      className={`relative flex flex-col justify-end overflow-hidden ${compact ? 'min-h-36 sm:min-h-44 sm:p-7' : 'min-h-52 sm:min-h-64 sm:p-8'}`}
    >
      <Cover seed={heroSeed(view)} className="text-white" />
      <div className="relative flex min-w-0 flex-col gap-3">
        <h1 className={`max-w-[18ch] leading-[1.02] font-extrabold [overflow-wrap:anywhere] hyphens-auto ${compact ? 'text-3xl sm:text-5xl' : 'text-[2.375rem] sm:text-6xl'}`}>
          {w.title}
        </h1>
        {(w.host_name || w.location) && (
          <p className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-base font-semibold sm:text-lg">
            {w.host_name && (
              <span className="flex items-center gap-2.5">
                <Avatar name={w.host_name} className="ring-2 ring-white/40" />
                with {w.host_name}
              </span>
            )}
            {w.host_name && w.location && <span aria-hidden="true">·</span>}
            {w.location && <span className="min-w-0 font-medium text-on-brand-soft [overflow-wrap:anywhere]">{w.location}</span>}
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
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 sm:gap-4">
      <Tile as="section" aria-labelledby="price-title" className="flex flex-col justify-between gap-2">
        <TileLabel as="h2" id="price-title">
          Price
        </TileLabel>
        <p className="font-display text-3xl font-bold tracking-tight tabular-nums">{formatMoney(proposal?.price ?? w.price)}</p>
        <p className="text-sm text-ink-soft">{accepting ? 'Charged only if it happens' : 'Not taking reservations'}</p>
      </Tile>

      <Tile as="section" aria-labelledby="class-title" className="flex flex-col justify-between gap-2">
        <TileLabel as="h2" id="class-title">
          Class
        </TileLabel>
        <p className="font-display text-3xl font-bold tracking-tight">{durationLabel(w.duration_min)}</p>
        <p className="text-sm text-ink-soft">{count(proposal?.capacity ?? w.capacity, 'spot')}</p>
      </Tile>

      <Tile as="section" aria-labelledby="dates-title" className="col-span-2 flex flex-col gap-3">
        <div className="flex items-baseline justify-between gap-3">
          <TileLabel as="h2" id="dates-title">
            {proposal || rows.length === 1 ? 'The date' : accepting ? 'Pick from these dates' : 'Dates'}
          </TileLabel>
          <span className="text-xs text-ink-soft">{zoneName(w.timezone)}</span>
        </div>
        <ul className="flex flex-col gap-2">
          {rows.map((r) => (
            <li
              key={r.id}
              className={`flex min-h-14 items-center justify-between gap-3 rounded-control px-3.5 py-2.5 ${r.best || r.chosen ? 'bg-brand-wash' : 'ring-1 ring-line ring-inset'}`}
            >
              <span className="flex items-center gap-3">
                <Icon name="calendar" className={r.best || r.chosen ? 'text-brand' : 'text-ink-soft'} />
                <span className="flex flex-col leading-tight">
                  <span className="font-semibold">{r.day}</span>
                  <span className="text-sm text-ink-soft">{r.time}</span>
                </span>
              </span>
              {group.answered > 0 && !proposal && (
                <span className={`text-right text-sm font-semibold ${r.best ? 'text-brand' : 'text-ink-soft'}`}>
                  {r.canCome} can come
                  {r.best && <span className="block text-xs font-medium">most so far</span>}
                </span>
              )}
            </li>
          ))}
        </ul>
      </Tile>

      <Tile as="section" aria-labelledby="group-title" className="col-span-2 flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3">
          <TileLabel as="h2" id="group-title">
            {reserving ? 'Who’s in' : 'The group'}
          </TileLabel>
          {reserving && <Pill tone="hold">Reservations open</Pill>}
        </div>
        {state ? (
          <p className="text-ink">{state}</p>
        ) : proposal ? (
          <>
            <Meter capacity={tally.capacity} needed={tally.minimum} paid={tally.paid} reserved={tally.authorized + tally.charging} />
            {reserving && <Deadline closesAt={proposal.closes_at} timeZone={w.timezone} open />}
          </>
        ) : (
          <p className="flex items-baseline gap-2">
            {summary.count !== null && <span className="font-display text-3xl font-bold tabular-nums">{summary.count}</span>}
            <span className="text-ink-soft">{summary.text}</span>
          </p>
        )}
      </Tile>

      {w.description && (
        <Tile as="section" aria-labelledby="about-title" className="col-span-2 sm:col-span-4">
          <TileLabel as="h2" id="about-title">
            About this class
          </TileLabel>
          <p className="mt-2 max-w-prose whitespace-pre-line">{w.description}</p>
        </Tile>
      )}
    </div>
  )
}
