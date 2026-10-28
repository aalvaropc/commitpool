import type { TermItem } from '../lib/terms'

const valueClass: Record<NonNullable<TermItem['kind']>, string> = {
  money: 'wide text-lg font-semibold tabular-nums',
  number: 'semiwide text-lg font-semibold tabular-nums',
  text: 'font-semibold',
}

/** A workshop's terms at a glance, in a row of four. */
export default function Terms({ items }: { items: TermItem[] }) {
  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-4">
      {items.map((t) => (
        <div key={t.label} className="flex flex-col gap-0.5">
          <dt className="text-sm text-ink-soft">{t.label}</dt>
          <dd className={valueClass[t.kind ?? 'number']}>{t.value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** The terms that lock when payments open, two to a row. */
export function TermsList({ items, className = '' }: { items: TermItem[]; className?: string }) {
  return (
    <dl className={`grid gap-x-6 gap-y-3 sm:grid-cols-2 ${className}`}>
      {items.map((t) => (
        <div key={t.label} className={t.span ? 'sm:col-span-2' : ''}>
          <dt className="text-sm text-ink-soft">{t.label}</dt>
          <dd className={valueClass[t.kind ?? 'text']}>{t.value}</dd>
        </div>
      ))}
    </dl>
  )
}
