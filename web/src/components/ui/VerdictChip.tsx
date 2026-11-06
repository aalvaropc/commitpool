import type { Verdict } from '../../api'
import { Icon, type IconName } from './Icon'

const looks: Record<Verdict, { icon: IconName; className: string }> = {
  available: { icon: 'check', className: 'bg-paid-wash text-paid' },
  conditional: { icon: 'clock', className: 'bg-maybe-wash text-maybe' },
  unavailable: { icon: 'x', className: 'bg-no-wash text-no' },
  unknown: { icon: 'help', className: 'bg-surface-muted text-ink-soft ring-1 ring-line ring-inset' },
}

export function VerdictChip({ verdict, children, className = '' }: { verdict: Verdict; children: string; className?: string }) {
  const look = looks[verdict]
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-sm leading-tight font-semibold ${look.className} ${className}`}>
      <Icon name={look.icon} size={15} />
      {children}
    </span>
  )
}
