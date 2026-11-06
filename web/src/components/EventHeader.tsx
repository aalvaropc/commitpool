import type { PublicView } from '../api'
import { dateRows, groupLine } from '../lib/event'
import { formatMoney } from '../lib/money'
import { count } from '../lib/plural'
import { durationLabel, zoneName } from '../lib/time'
import { Avatar, Cover, Icon, Meter, Pill, Tile, TileLabel } from './ui'

export function EventHero({ view, seed, compact = false }: { view: PublicView; seed: string; compact?: boolean }) {
  const w = view.workshop
  return (
    <Tile
      as="header"
      tone="brand"
      className={`relative flex flex-col justify-end overflow-hidden ${compact ? 'min-h-36 sm:min-h-44 sm:p-7' : 'min-h-52 sm:min-h-64 sm:p-8'}`}
    >
      <Cover seed={seed} className="text-white" />
      <div className="relative flex flex-col gap-3">
        <h1 className={`max-w-[18ch] leading-[1.02] font-extrabold ${compact ? 'text-3xl sm:text-5xl' : 'text-[2.375rem] sm:text-6xl'}`}>{w.title}</h1>
        {(w.host_name || w.location) && (
          <p className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-base font-semibold sm:text-lg">
            {w.host_name && (
              <span className="flex items-center gap-2.5">
                <Avatar name={w.host_name} className="ring-2 ring-white/40" />
                with {w.host_name}
              </span>
            )}
            {w.host_name && w.location && <span aria-hidden="true">·</span>}
            {w.location && <span className="font-medium text-on-brand-soft">{w.location}</span>}
          </p>
        )}
      </div>
    </Tile>
  )
}

export function EventDetails({ view }: { view: PublicView }) {
  const { workshop: w, proposal, tally, group } = view
  const rows = dateRows(view.dates, group, proposal?.date_id ?? null, w.timezone)
  const reserving = proposal?.payments_open ?? false
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 sm:gap-4">
      <Tile className="flex flex-col justify-between gap-2">
        <TileLabel>Price</TileLabel>
        <p className="font-display text-3xl font-bold tracking-tight tabular-nums">{formatMoney(proposal?.price ?? w.price)}</p>
        <p className="text-sm text-ink-soft">Charged only if it happens</p>
      </Tile>

      <Tile className="flex flex-col justify-between gap-2">
        <TileLabel>Class</TileLabel>
        <p className="font-display text-3xl font-bold tracking-tight">{durationLabel(w.duration_min)}</p>
        <p className="text-sm text-ink-soft">{count(proposal?.capacity ?? w.capacity, 'spot')}</p>
      </Tile>

      <Tile as="section" aria-labelledby="dates-title" className="col-span-2 flex flex-col gap-3">
        <div className="flex items-baseline justify-between gap-3">
          <TileLabel>
            <span id="dates-title">{proposal || rows.length === 1 ? 'The date' : 'Pick from these dates'}</span>
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
                <span className={`text-sm font-semibold whitespace-nowrap ${r.best ? 'text-brand' : 'text-ink-soft'}`}>{r.canCome} can come</span>
              )}
            </li>
          ))}
        </ul>
      </Tile>

      <Tile as="section" aria-labelledby="group-title" className="col-span-2 flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3">
          <TileLabel>
            <span id="group-title">{reserving ? 'Who’s in' : 'The group'}</span>
          </TileLabel>
          {reserving && <Pill tone="hold">Reservations open</Pill>}
        </div>
        {proposal ? (
          <Meter capacity={tally.capacity} needed={tally.minimum} paid={tally.paid} reserved={tally.authorized} />
        ) : (
          <p className="flex items-baseline gap-2">
            <span className="font-display text-3xl font-bold tabular-nums">{group.answered}</span>
            <span className="text-ink-soft">{groupLine(group, tally.minimum).replace(/^\d+ /, '')}</span>
          </p>
        )}
      </Tile>

      {w.description && (
        <Tile className="col-span-2 sm:col-span-4">
          <TileLabel>About this class</TileLabel>
          <p className="mt-2 max-w-prose whitespace-pre-line">{w.description}</p>
        </Tile>
      )}
    </div>
  )
}
