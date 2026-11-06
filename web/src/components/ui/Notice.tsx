import type { ReactNode } from 'react'
import type { Tone } from '../../lib/status'
import { toneText, toneWash } from './tone'

interface NoticeProps {
  tone?: Tone
  title?: string
  role?: 'alert' | 'status'
  children: ReactNode
  className?: string
}

export function Notice({ tone = 'neutral', title, role = 'status', children, className = '' }: NoticeProps) {
  return (
    <div role={role} className={`rounded-control px-4 py-3 ${toneWash[tone]} ${className}`}>
      {title && <p className={`font-semibold ${toneText[tone]}`}>{title}</p>}
      <div className={title ? 'mt-0.5 text-ink' : tone === 'neutral' ? 'text-ink' : `font-medium ${toneText[tone]}`}>{children}</div>
    </div>
  )
}
