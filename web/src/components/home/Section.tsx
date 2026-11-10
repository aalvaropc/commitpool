import type { ReactNode } from 'react'

export function PaperSection({ id, labelledBy, className = '', children }: { id?: string; labelledBy: string; className?: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={labelledBy} className={`paper grain mx-[clamp(0.5rem,1.4vw,1.25rem)] scroll-mt-4 rounded-sheet ${className}`}>
      {children}
    </section>
  )
}

interface HeadProps {
  id: string
  title: ReactNode
  children?: ReactNode
  tone?: 'kiln' | 'paper'
  stacked?: boolean
  className?: string
}

export function SectionHead({ id, title, children, tone = 'kiln', stacked = false, className = 'mb-[clamp(2.25rem,5vw,4rem)]' }: HeadProps) {
  return (
    <div className={`grid gap-[1.125rem] ${stacked ? '' : 'lg:grid-cols-12 lg:items-end lg:gap-x-8'} ${className}`}>
      <h2 id={id} className={`shout text-section ${stacked ? '' : 'lg:col-span-6'}`}>
        {title}
      </h2>
      {children && <p className={`max-w-[40ch] text-intro ${tone === 'paper' ? 'text-graphite' : 'text-ink-soft'} ${stacked ? '' : 'lg:col-span-5 lg:col-start-8'}`}>{children}</p>}
    </div>
  )
}
