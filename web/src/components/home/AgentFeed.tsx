import type { ReactNode } from 'react'
import { Avatar, Icon, Pill, VerdictChip, type IconName } from '../ui'
import { SectionHead } from './Section'

const people = ['Priya Raman', 'Ben Whitaker', 'Lucía Torres', 'Hannah Kim', 'Tomás Silva', 'Grace Liu']

const rules = [
  { who: 'Claude', does: 'Reads, asks, explains, drafts.' },
  { who: 'CommitPool', does: 'Counts who can come and picks the date.' },
  { who: 'Iris', does: 'Opens reservations and approves any extension.' },
]

function Step({ icon, tone, time, title, children }: { icon: IconName; tone: 'claude' | 'done'; time: string; title: string; children: ReactNode }) {
  return (
    <li className="relative grid grid-cols-[2.25rem_minmax(0,1fr)] gap-[1.125rem] pb-[2.125rem] last:pb-0">
      <span aria-hidden="true" className={`relative z-[1] grid size-9 place-items-center rounded-full ${tone === 'done' ? 'bg-paid-wash text-paid' : 'bg-brand-wash text-brand'}`}>
        <Icon name={icon} size={17} />
      </span>
      <div className="min-w-0">
        <p className="mt-[0.4375rem] text-[0.8125rem] text-ink-faint tabular-nums">{time}</p>
        <h3 className="mt-0.5 text-[1.3125rem] tracking-[-0.015em]">{title}</h3>
        <div className="mt-3 grid gap-2.5">{children}</div>
      </div>
    </li>
  )
}

function Quote({ by, ask = false, children }: { by: string; ask?: boolean; children: ReactNode }) {
  return (
    <div className={`rounded-control px-3.5 py-3 text-[0.9375rem] ${ask ? 'bg-brand-wash' : 'bg-surface'}`}>
      <p className={`mb-1.5 flex items-center gap-2 text-[0.8125rem] ${ask ? 'font-semibold text-brand' : 'text-ink-faint'}`}>
        {!ask && <Avatar name={by} size="sm" />}
        {ask ? `Claude asks ${by}` : by}
      </p>
      {children}
    </div>
  )
}

export default function AgentFeed() {
  return (
    <section aria-labelledby="feed-title" className="section-pad">
      <div className="wrap grid gap-12 lg:grid-cols-12 lg:gap-x-8">
        <div className="lg:sticky lg:top-8 lg:col-span-5 lg:self-start">
          <SectionHead id="feed-title" title="Claude finds the date." stacked className="">
            Claude reads the pasted chat and every later answer, and asks one question when it matters. CommitPool counts and picks. Iris approves anything that touches money.
          </SectionHead>
          <dl className="mt-7 grid max-w-[26rem] gap-2.5">
            {rules.map((r) => (
              <div key={r.who} className="grid grid-cols-[7rem_1fr] gap-3 border-t border-line py-3 text-[0.9375rem]">
                <dt className="font-bold">{r.who}</dt>
                <dd className="text-ink-soft">{r.does}</dd>
              </div>
            ))}
          </dl>
        </div>
        <ol aria-label="How Iris’s class found its date" className="relative before:absolute before:top-3 before:bottom-3 before:left-[1.0625rem] before:w-0.5 before:bg-line lg:col-span-6 lg:col-start-7">
          <Step icon="quote" tone="claude" time="9:02 PM" title="Reads the group chat Iris pasted">
            <div role="img" aria-label="Priya, Ben, Lucía, Hannah, Tomás and Grace" className="flex flex-wrap gap-1.5">
              {people.map((n) => (
                <Avatar key={n} name={n} size="sm" />
              ))}
            </div>
            <Quote by="Lucía Torres">
              “El sábado no puedo, pero el domingo me viene perfecto.”
              <span className="mt-2 flex flex-wrap items-center gap-1.5">
                <span className="mr-0.5 text-xs font-semibold text-brand">Read by Claude</span>
                <VerdictChip verdict="unavailable" size="sm">
                  Sat · Can’t
                </VerdictChip>
                <VerdictChip verdict="available" size="sm">
                  Sun · Can come
                </VerdictChip>
              </span>
            </Quote>
          </Step>
          <Step icon="help" tone="claude" time="9:03 PM" title="Asked Grace about the hours">
            <Quote by="Grace" ask>
              The class runs 10 AM–12 PM on Saturday and Sunday — can you make either morning, and which?
            </Quote>
            <Quote by="Grace Liu">“No problem — I swapped Saturday. Sunday I’m still at the market.”</Quote>
          </Step>
          <Step icon="users" tone="done" time="9:11 PM" title="Saturday went from 2 to 3">
            <p className="flex flex-wrap items-center gap-3.5">
              <b aria-hidden="true" className="shout text-[3.5rem] leading-[0.9] text-ink-faint line-through decoration-4">
                2
              </b>
              <Icon name="arrowRight" size={28} className="text-brand" />
              <b className="shout text-[3.5rem] leading-[0.9]">3</b>
              <span className="text-[0.9375rem] text-ink-soft">of the 3 Iris needs</span>
            </p>
            <p className="max-w-[46ch] text-ink-soft">Priya, Ben and Grace can come. Hannah has to leave by 11:30, so she isn’t counted for a class that ends at noon.</p>
          </Step>
          <Step icon="calendar" tone="done" time="9:12 PM" title="CommitPool recommended Saturday">
            <p className="flex flex-wrap items-center gap-2 text-ink-soft">
              Iris opened reservations from her board.
              <Pill tone="hold">Reservations open</Pill>
            </p>
          </Step>
          <li aria-hidden="true" className="mb-[1.875rem] ml-[3.375rem] flex items-center gap-3.5 text-[0.8125rem] font-semibold text-ink-faint after:h-px after:flex-1 after:bg-line">
            The next afternoon
          </li>
          <Step icon="spark" tone="claude" time="4:30 PM" title="One short with under 5 hours left: proposed one extension">
            <div className="grid gap-3 rounded-[1.25rem] border border-line bg-surface p-4">
              <p className="flex flex-wrap items-center gap-2.5">
                <Pill tone="hold">2 of 3 reserved</Pill>
                <Pill tone="attention">Closes 9:12 PM</Pill>
              </p>
              <p className="text-[1.0625rem] font-bold">Extend the deadline once? Holds stay in place, and anyone can still leave until charging starts.</p>
              <p className="text-[0.9375rem] text-ink-soft">CommitPool offers the latest deadline it allows; Claude drafts the message for the group, ready for Iris to send once she extends.</p>
              <p className="flex flex-wrap items-center gap-2.5">
                <Avatar name="Iris Calder" size="sm" />
                <VerdictChip verdict="available" size="sm">
                  Approved by Iris
                </VerdictChip>
              </p>
            </div>
          </Step>
        </ol>
      </div>
    </section>
  )
}
