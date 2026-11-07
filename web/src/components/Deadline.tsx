import { useNow } from '../lib/hooks'
import { formatMoment, timeLeft } from '../lib/time'

interface Props {
  closesAt: string
  timeZone: string
  open: boolean
  size?: 'sm' | 'lg'
}

export default function Deadline({ closesAt, timeZone, open, size = 'sm' }: Props) {
  const now = Math.max(useNow(15000), Date.now())
  const left = open ? timeLeft(closesAt, now) : null
  return (
    <p className={size === 'lg' ? 'flex flex-col gap-0.5' : 'text-sm'}>
      <span className={size === 'lg' ? 'text-sm font-semibold text-ink-soft' : 'text-ink-soft'}>
        {left ? 'Reservations close ' : 'Reservations closed '}
      </span>
      <time dateTime={closesAt} className={`tabular-nums ${size === 'lg' ? 'font-display text-xl font-bold' : 'font-semibold'}`}>
        {formatMoment(closesAt, timeZone)}
      </time>
      {left && <span className={size === 'lg' ? 'font-semibold text-brand' : 'text-ink-soft'}> {size === 'lg' ? `${left} left` : `(${left} left)`}</span>}
    </p>
  )
}
