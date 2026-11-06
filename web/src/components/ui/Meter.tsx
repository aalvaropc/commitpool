import { meter, type MeterInput } from '../../lib/meter'

interface MeterProps extends MeterInput {
  showCaption?: boolean
  className?: string
}

export function Meter({ showCaption = true, className = '', ...input }: MeterProps) {
  const m = meter(input)
  const filled = m.paidPct + m.reservedPct
  return (
    <figure className={`flex flex-col gap-2 ${className}`}>
      <div aria-hidden="true" className="relative h-3 rounded-full bg-brand-wash">
        <div className="absolute inset-y-0 left-0 flex overflow-hidden rounded-full transition-[width] duration-700 ease-(--ease-out)" style={{ width: `${filled}%` }}>
          <span className="h-full bg-paid" style={{ width: `${filled > 0 ? (m.paidPct / filled) * 100 : 0}%` }} />
          <span className="hatch h-full flex-1" />
        </div>
        {!m.reached && <span className="absolute -inset-y-1.5 w-[3px] -translate-x-1/2 rounded-full bg-ink" style={{ left: `${m.neededPct}%` }} />}
      </div>
      <figcaption className={showCaption ? 'flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-sm' : 'sr-only'}>
        <span className="font-semibold text-ink">{m.caption}</span>
        <span className="sr-only">{m.summary}</span>
        {showCaption && (
          <span aria-hidden="true" className="text-ink-soft tabular-nums">
            {input.paid} paid · {input.reserved} reserved · {input.needed} needed
          </span>
        )}
      </figcaption>
    </figure>
  )
}
