import type { ReactNode } from 'react'

export function Stat({ label, value, note, className = '' }: { label: string; value: ReactNode; note?: ReactNode; className?: string }) {
  return (
    <div className={`flex flex-col ${className}`}>
      <dt className="text-xs font-semibold tracking-[0.06em] text-ink-soft uppercase">{label}</dt>
      <dd className="mt-1 font-display text-3xl font-bold tracking-tight tabular-nums">{value}</dd>
      {note && <dd className="mt-0.5 text-sm text-ink-soft">{note}</dd>}
    </div>
  )
}
