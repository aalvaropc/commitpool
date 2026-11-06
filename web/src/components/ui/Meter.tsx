import { meter, type MeterInput } from '../../lib/meter'

interface MeterProps extends MeterInput {
  showCaption?: boolean
  className?: string
}

export function Meter({ showCaption = true, className = '', ...input }: MeterProps) {
  const m = meter(input)
  return (
    <figure className={`flex flex-col gap-2 ${className}`}>
      <div aria-hidden="true" className="relative h-3 rounded-full bg-brand-wash">
        <div className="absolute inset-y-0 left-0 flex overflow-hidden rounded-full" style={{ width: `${m.paidPct + m.reservedPct}%` }}>
          <span className="h-full bg-paid transition-[width] duration-700 ease-(--ease-out)" style={{ width: `${m.paidPct + m.reservedPct > 0 ? (m.paidPct / (m.paidPct + m.reservedPct)) * 100 : 0}%` }} />
          <span className="h-full flex-1 bg-brand" />
        </div>
        {!m.reached && <span className="absolute -inset-y-1.5 w-[3px] -translate-x-1/2 rounded-full bg-go" style={{ left: `${m.neededPct}%` }} />}
      </div>
      <figcaption className={showCaption ? 'flex flex-wrap items-baseline justify-between gap-x-3 text-sm' : 'sr-only'}>
        <span className="font-semibold text-ink">{m.caption}</span>
        <span className="sr-only">{m.summary}</span>
        {showCaption && (
          <span aria-hidden="true" className="text-ink-soft">
            {input.needed} needed · {input.capacity} spots
          </span>
        )}
      </figcaption>
    </figure>
  )
}
