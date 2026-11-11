import type { ReactNode } from 'react'
import { Icon } from './ui'

export default function ExternalLink({ href, className, children }: { href: string; className: string; children: ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className={className}>
      {children}
      <Icon name="out" size={14} />
      <span className="sr-only"> (opens in a new tab)</span>
    </a>
  )
}
