import type { ReactNode } from 'react'
import type { Tone } from '../../lib/status'
import { toneFill, toneText, toneWash } from './tone'

export function Pill({ tone = 'neutral', dot = true, children, className = '' }: { tone?: Tone; dot?: boolean; children: ReactNode; className?: string }) {
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-sm leading-none font-semibold ${toneWash[tone]} ${toneText[tone]} ${className}`}>
      {dot && <span aria-hidden="true" className={`size-2 rounded-full ${toneFill[tone]}`} />}
      {children}
    </span>
  )
}

export function DemoTag() {
  return <span className="rounded-full bg-attention-wash px-2.5 py-0.5 text-sm font-semibold text-attention">Demo only</span>
}
