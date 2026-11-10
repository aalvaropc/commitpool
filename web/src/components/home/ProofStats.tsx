import type { ReactNode } from 'react'
import { Icon } from '../ui'

function Source({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex min-h-11 items-center gap-1.5 text-[0.8125rem] text-ink-faint no-underline transition-colors duration-150 hover:text-ink"
    >
      <span className="border-b border-line-strong pb-px">{children}</span>
      <Icon name="out" size={13} />
      <span className="sr-only">(opens in a new tab)</span>
    </a>
  )
}

function Figures({ first, joiner, second }: { first: string; joiner: string; second: string }) {
  return (
    <p className="flex flex-wrap items-baseline gap-x-3.5 gap-y-1.5">
      <b className="shout text-[clamp(2.75rem,7vw,4.25rem)] leading-[0.9] tabular-nums">{first}</b>
      <span className="text-base font-semibold text-ink-faint">{joiner}</span>
      <b className="shout text-[clamp(2.75rem,7vw,4.25rem)] leading-[0.9] text-brand tabular-nums">{second}</b>
    </p>
  )
}

export default function ProofStats() {
  return (
    <section aria-label="Why a hold, not a poll" className="pt-14 pb-[4.5rem]">
      <div className="wrap">
        <div className="grid gap-9 border-t border-line pt-9 sm:grid-cols-2 sm:gap-12">
          <div>
            <Figures first="~37%" joiner="vs" second="~17%" />
            <p className="mt-3 max-w-[38ch] text-ink-soft">median no-show at small free events, against small paid ones.</p>
            <div className="mt-1">
              <Source href="https://pheedloop.com/blog/event-data-lab-report-5">PheedLoop Event Data Lab, 2026</Source>
            </div>
          </div>
          <div className="sm:border-l sm:border-line sm:pl-12">
            <Figures first="419,100" joiner="and" second="388,400" />
            <p className="mt-3 max-w-[38ch] text-ink-soft">self-enrichment teachers, and exercise trainers and group fitness instructors, in the US.</p>
            <div className="mt-1 flex flex-wrap gap-x-5">
              <Source href="https://www.onetonline.org/link/summary/25-3021.00">O*NET / BLS, 2025: teachers</Source>
              <Source href="https://www.onetonline.org/link/summary/39-9031.00">O*NET / BLS, 2025: instructors</Source>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
