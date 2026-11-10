import type { Verdict } from '../../api'
import { Icon, type IconName } from './Icon'

const looks: Record<Verdict, { icon: IconName; className: string }> = {
  available: { icon: 'check', className: 'bg-paid-wash text-paid' },
  conditional: { icon: 'clock', className: 'bg-maybe-wash text-maybe' },
  unavailable: { icon: 'x', className: 'bg-no-wash text-no' },
  unknown: { icon: 'help', className: 'bg-surface-muted text-ink-soft' },
}

const sizes = { md: 'px-3 py-1 text-sm', sm: 'px-2.5 py-0.5 text-[0.8125rem]' } as const

export function VerdictChip({ verdict, children, size = 'md', className = '' }: { verdict: Verdict; children: string; size?: keyof typeof sizes; className?: string }) {
  const look = looks[verdict]
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full leading-tight font-semibold ${sizes[size]} ${look.className} ${className}`}>
      <Icon name={look.icon} size={size === 'sm' ? 14 : 15} />
      {children}
    </span>
  )
}
