import type { ReactNode } from 'react'
import type { Tone } from '../lib/status'
import { toneFill, toneText, toneWash } from './ui'

interface Props {
  headline: string
  tone: Tone
  children?: ReactNode
}

/** The workshop's state in one line; the headline is announced when it changes. */
export default function StatusBanner({ headline, tone, children }: Props) {
  return (
    <section aria-label="Workshop status" className={`rounded-sm px-4 py-4 sm:px-5 ${toneWash[tone]}`}>
      <div role="status" aria-live="polite" className="flex items-start gap-3">
        <span aria-hidden="true" className={`mt-1.5 size-3.5 shrink-0 ${toneFill[tone]}`} />
        <p className={`semiwide text-xl font-bold leading-snug ${toneText[tone]}`}>{headline}</p>
      </div>
      {children && <div className="mt-2 pl-[1.625rem] text-ink">{children}</div>}
    </section>
  )
}
