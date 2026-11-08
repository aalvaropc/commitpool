import type { ReactNode } from 'react'
import type { Tone } from '../lib/status'
import { Icon, type IconName, toneText, toneWash } from './ui'

export const statusIcons: Record<Tone, IconName> = {
  neutral: 'calendar',
  hold: 'clock',
  paid: 'check',
  cancel: 'x',
  attention: 'help',
}

interface Props {
  headline: string
  tone: Tone
  children?: ReactNode
}

export default function StatusBanner({ headline, tone, children }: Props) {
  return (
    <section aria-label="Workshop status" className="rounded-tile bg-surface p-4 sm:p-5">
      <div role="status" aria-live="polite" className="flex items-center gap-3">
        <span aria-hidden="true" className={`grid size-10 shrink-0 place-items-center rounded-full ${toneWash[tone]} ${toneText[tone]}`}>
          <Icon name={statusIcons[tone]} size={20} />
        </span>
        <p className={`font-display text-xl leading-snug font-bold sm:text-2xl ${tone === 'neutral' ? 'text-ink' : toneText[tone]}`}>{headline}</p>
      </div>
      {children && <div className="mt-3 text-ink sm:pl-[3.25rem]">{children}</div>}
    </section>
  )
}
