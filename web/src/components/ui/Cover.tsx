import { cover } from '../../lib/cover'

export function Cover({ seed, className = '' }: { seed: string; className?: string }) {
  const { rings } = cover(seed)
  return (
    <div aria-hidden="true" className={`pointer-events-none absolute inset-0 overflow-hidden ${className}`}>
      {rings.map((r, i) => (
        <span
          key={i}
          className="absolute aspect-square -translate-x-1/2 -translate-y-1/2 rounded-full border-current"
          style={{ left: `${r.cx}%`, top: `${r.cy}%`, width: `${r.r * 1.4}%`, borderWidth: `${r.width * 2}px`, opacity: i === 0 ? 0.14 : 0.1 }}
        />
      ))}
    </div>
  )
}
