import type { ReactNode } from 'react'

export interface Term {
  label: string
  value: ReactNode
}

/** A workshop's terms as label and value pairs, numbers set wide. */
export default function Terms({ items }: { items: Term[] }) {
  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-4">
      {items.map((t) => (
        <div key={t.label} className="flex flex-col gap-0.5">
          <dt className="text-sm text-ink-soft">{t.label}</dt>
          <dd className="semiwide text-lg font-semibold tabular-nums">{t.value}</dd>
        </div>
      ))}
    </dl>
  )
}
