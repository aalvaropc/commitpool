import { meter, type MeterInput } from '../../lib/meter'

interface MeterProps extends MeterInput {
  interest?: boolean
  className?: string
}

export function Meter({ interest = false, className = '', ...input }: MeterProps) {
  const m = meter(input)
  const filled = m.paidPct + m.reservedPct
  return (
    <figure aria-hidden={interest || undefined} className={`flex flex-col gap-3 ${className}`}>
      <div aria-hidden="true" className="relative h-4 rounded-full bg-surface-muted">
        <div className="absolute inset-y-0 left-0 flex overflow-hidden rounded-full transition-[width] duration-700 ease-(--ease-out)" style={{ width: `${filled}%` }}>
          {!interest && <span className="h-full bg-paid" style={{ width: `${filled > 0 ? (m.paidPct / filled) * 100 : 0}%` }} />}
          <span className={`h-full flex-1 ${interest ? 'bg-ink-faint' : 'bg-hold'}`} />
        </div>
        <span className="absolute -inset-y-1.5 w-[3px] -translate-x-1/2 rounded-full bg-ink" style={{ left: `${m.neededPct}%` }} />
      </div>
      {!interest && (
        <figcaption className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
          <span className="font-display text-xl font-bold">{m.caption}</span>
          <span className="sr-only">{m.summary}</span>
          <span aria-hidden="true" className="text-sm text-ink-soft tabular-nums">
            {input.paid} paid · {input.reserved} reserved · {input.needed} needed
          </span>
        </figcaption>
      )}
    </figure>
  )
}
