export function LogoMark({ size = 28, className = '' }: { size?: number; className?: string }) {
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} aria-hidden="true" focusable="false" className={`shrink-0 ${className}`}>
      <rect width="32" height="32" rx="9" fill="var(--color-brand)" />
      <rect x="7.5" y="17" width="4.5" height="8" rx="2.25" fill="#ffffff" />
      <rect x="13.75" y="12.5" width="4.5" height="12.5" rx="2.25" fill="#ffffff" />
      <rect x="20" y="8" width="4.5" height="17" rx="2.25" fill="#ffffff" />
      <rect x="5.5" y="13.25" width="21" height="2.5" rx="1.25" fill="var(--color-go)" />
    </svg>
  )
}

export function Logo({ className = '' }: { className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2 font-display text-lg font-bold tracking-tight text-ink ${className}`}>
      <LogoMark />
      CommitPool
    </span>
  )
}
