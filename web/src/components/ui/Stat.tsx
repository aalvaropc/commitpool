import type { ReactNode } from 'react'

export function Stat({ label, value, note, className = '' }: { label: string; value: ReactNode; note?: ReactNode; className?: string }) {
  return (
    <dl className={`flex flex-col ${className}`}>
      <dt className="text-sm font-semibold text-ink-soft">{label}</dt>
      <dd className="mt-1 font-display text-3xl font-bold tracking-tight tabular-nums">{value}</dd>
      {note && <dd className="mt-0.5 text-sm text-ink-soft">{note}</dd>}
    </dl>
  )
}
