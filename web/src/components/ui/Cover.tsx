import { cover } from '../../lib/cover'

export function Cover({ seed, className = '' }: { seed: string; className?: string }) {
  const { rings } = cover(seed)
  return (
    <svg aria-hidden="true" focusable="false" viewBox="0 0 100 100" preserveAspectRatio="none" className={`pointer-events-none absolute inset-0 size-full ${className}`}>
      {rings.map((r, i) => (
        <circle key={i} cx={r.cx} cy={r.cy} r={r.r} fill="none" stroke="currentColor" strokeWidth={r.width} vectorEffect="non-scaling-stroke" opacity={i === 0 ? 0.14 : 0.1} />
      ))}
    </svg>
  )
}
