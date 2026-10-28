import { count } from '../lib/plural'

interface Props {
  capacity: number
  minimum: number
  paid: number
  authorized: number
  /** Seats held at checkout without a hold yet; only the owner sees them. */
  reserved?: number
  label?: string
  size?: 'md' | 'lg'
}

type Cell = 'paid' | 'hold' | 'reserved' | 'open'

const cellClass: Record<Cell, string> = {
  paid: 'bg-paid-fill',
  hold: 'hatch border-2 border-hold-fill',
  reserved: 'border-2 border-dashed border-hold-fill bg-sheet',
  open: 'border border-edge bg-sheet',
}

const MAX_CELLS = 40
const GAP = 4

/**
 * One cell per seat: paid seats solid, authorized holds hatched, and a bar at
 * the minimum. The workshop is confirmed when the solid cells reach the bar.
 */
export default function SeatMeter({ capacity, minimum, paid, authorized, reserved = 0, label = 'Seats', size = 'md' }: Props) {
  const cell = size === 'lg' ? 52 : 30
  const total = Math.max(capacity, 1)
  const taken = Math.min(paid + authorized + reserved, total)
  const gap = total <= MAX_CELLS ? GAP : 0
  const cells: Cell[] = []
  if (total <= MAX_CELLS) {
    for (let i = 0; i < total; i++) {
      cells.push(i < paid ? 'paid' : i < paid + authorized ? 'hold' : i < taken ? 'reserved' : 'open')
    }
  }
  const min = Math.min(Math.max(minimum, 0), total)
  const marker = `calc((100% - ${(total - 1) * gap}px) * ${min / total} + ${Math.max(min - 0.5, 0) * gap}px)`
  const reached = paid >= minimum

  return (
    <figure className="flex flex-col gap-2">
      <figcaption className="sr-only">
        {label}: {paid} paid of {minimum} needed, {count(authorized, 'hold')} authorized
        {reserved > 0 ? `, ${reserved} reserved` : ''}, capacity {capacity}.
      </figcaption>
      <div aria-hidden="true" className="relative pt-1 pb-6" style={{ maxWidth: total <= MAX_CELLS ? `${total * cell}px` : undefined }}>
        {total <= MAX_CELLS ? (
          <div className={`grid ${size === 'lg' ? 'h-16' : 'h-7'}`} style={{ gridTemplateColumns: `repeat(${total}, minmax(0, 1fr))`, gap: `${gap}px` }}>
            {cells.map((c, i) => (
              <span key={i} className={cellClass[c]} />
            ))}
          </div>
        ) : (
          <div className={`flex ${size === 'lg' ? 'h-16' : 'h-7'} border border-edge bg-sheet`}>
            <span className="bg-paid-fill" style={{ width: `${(paid / total) * 100}%` }} />
            <span className="hatch" style={{ width: `${(authorized / total) * 100}%` }} />
          </div>
        )}
        <span className="absolute top-0 bottom-5 w-0.5 -translate-x-1/2 bg-ink" style={{ left: marker }} />
        <span
          className={`absolute bottom-0 ${size === 'lg' ? 'text-sm' : 'text-xs'} font-semibold whitespace-nowrap ${reached ? 'text-paid' : 'text-ink'} ${min / total > 0.6 ? '-translate-x-full' : ''}`}
          style={{ left: marker }}
        >
          Minimum {minimum}
        </span>
      </div>
      <ul aria-hidden="true" className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-ink-soft">
        <li className="flex items-center gap-1.5">
          <span className="size-3 bg-paid-fill" /> {paid} paid
        </li>
        <li className="flex items-center gap-1.5">
          <span className="hatch size-3 border border-hold-fill" /> {count(authorized, 'hold')} authorized
        </li>
        {reserved > 0 && (
          <li className="flex items-center gap-1.5">
            <span className="size-3 border border-dashed border-hold-fill" /> {reserved} reserved
          </li>
        )}
        <li className="flex items-center gap-1.5">
          <span className="size-3 border border-edge bg-sheet" /> {count(capacity, 'seat')}
        </li>
      </ul>
    </figure>
  )
}
