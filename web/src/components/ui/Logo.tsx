export function LogoMark({ size = 28, className = '' }: { size?: number; className?: string }) {
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} aria-hidden="true" focusable="false" className={`shrink-0 ${className}`}>
      <path d="M2 11h28a14 14 0 0 1-28 0Z" fill="var(--color-brand)" />
      <path d="M6 11h20" stroke="var(--color-canvas)" strokeWidth="2" strokeLinecap="round" />
      <path d="M9 7.5h14" stroke="var(--color-ink)" strokeWidth="2.4" strokeLinecap="round" />
    </svg>
  )
}

export function Logo({ className = '', size = 'text-xl', mark = 28 }: { className?: string; size?: string; mark?: number }) {
  return (
    <span className={`inline-flex items-center gap-2 font-display ${size} font-extrabold tracking-tight text-ink ${className}`}>
      <LogoMark size={mark} />
      CommitPool
    </span>
  )
}
