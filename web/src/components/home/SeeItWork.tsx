import type { ReactNode } from 'react'
import board from '../../assets/shots/board.webp'
import grace from '../../assets/shots/grace.webp'
import reserve from '../../assets/shots/reserve.webp'
import { Icon } from '../ui'
import { HandArrow } from './Drawings'
import { PaperSection, SectionHead } from './Section'

function BrowserFrame({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="kiln overflow-hidden rounded-[1.125rem] border border-charcoal bg-composer">
      <div aria-hidden="true" className="flex h-10 items-center gap-3.5 bg-surface-muted px-3.5">
        <span className="flex gap-1.5">
          <i className="size-2.5 rounded-full bg-line" />
          <i className="size-2.5 rounded-full bg-line" />
          <i className="size-2.5 rounded-full bg-line" />
        </span>
        <span className="mx-auto flex h-6 max-w-[22.5rem] flex-1 items-center justify-center gap-1.5 rounded-full bg-canvas text-xs text-ink-faint">
          <Icon name="lock" size={12} />
          {label}
        </span>
      </div>
      {children}
    </div>
  )
}

function PhoneFrame({ children }: { children: ReactNode }) {
  return (
    <div className="kiln relative aspect-[9/19] w-full max-w-[16.75rem] rounded-[2.75rem] border border-line bg-charcoal p-[0.5625rem]">
      <div className="relative h-full w-full overflow-hidden rounded-[2.2rem] bg-canvas">
        <div aria-hidden="true" className="absolute inset-x-0 top-0 z-10 flex h-10 items-center justify-between bg-canvas px-6 pt-1.5 text-[0.6875rem] font-bold">
          <span>9:41</span>
          <span>5G</span>
        </div>
        <span aria-hidden="true" className="absolute top-[1.125rem] left-1/2 z-20 h-[1.375rem] w-[4.75rem] -translate-x-1/2 rounded-full bg-charcoal" />
        {children}
      </div>
    </div>
  )
}

function Caption({ title, children, className = '' }: { title: string; children: ReactNode; className?: string }) {
  return (
    <div className={className}>
      <HandArrow className="mb-3 hidden h-[3.25rem] w-24 -scale-x-100 text-charcoal lg:block" />
      <h3 className="text-[1.375rem] tracking-[-0.02em]">{title}</h3>
      <p className="mt-2 max-w-[34ch] text-graphite">{children}</p>
    </div>
  )
}

const phoneShot = 'block h-full w-full object-cover object-top pt-10'

export default function SeeItWork() {
  return (
    <PaperSection id="see" labelledBy="see-title">
      <div className="wrap section-pad">
        <SectionHead id="see-title" title="See it work." tone="paper">
          Screens from the running demo: Iris’s board, Claude’s question to Grace, and Priya’s reservation.
        </SectionHead>
        <div className="grid gap-14 lg:grid-cols-12 lg:gap-x-8 lg:gap-y-[4.5rem]">
          <figure className="m-0 lg:col-span-9">
            <BrowserFrame label="commitpool · organizer board">
              <img
                src={board}
                width={1600}
                height={1565}
                loading="lazy"
                decoding="async"
                alt="Iris’s board, Who can come: six people, their messages, and each answer read for Saturday and Sunday. Saturday shows 2 of the 3 needed, and Grace’s message is marked as needing a question."
                className="block h-80 w-full object-cover object-left-top sm:h-auto sm:object-fill"
              />
            </BrowserFrame>
          </figure>
          <Caption title="Iris sees who can come, in their own words." className="lg:col-span-3 lg:self-center">
            Every answer is read against every date, with the words it came from. Grace’s is unclear, so she isn’t counted yet.
          </Caption>
          <div className="grid gap-12 sm:grid-cols-2 sm:gap-8 lg:col-span-10 lg:col-start-2">
            <div className="grid justify-items-center gap-[1.375rem] lg:grid-cols-[16.75rem_1fr] lg:items-center lg:justify-items-start lg:gap-7">
              <PhoneFrame>
                <img src={grace} width={600} height={1218} loading="lazy" decoding="async" alt="Grace’s page on a phone: Claude asks which morning she can make, and she answers that she swapped Saturday." className={phoneShot} />
              </PhoneFrame>
              <Caption title="Claude asks instead of guessing." className="max-w-[22rem]">
                Grace wrote <q className="font-semibold text-charcoal">I might be able to swap one. Not sure which day yet.</q> Claude asks her which morning she can make, tied to the class hours.
              </Caption>
            </div>
            <div className="grid justify-items-center gap-[1.375rem] lg:grid-cols-[16.75rem_1fr] lg:items-center lg:justify-items-start lg:gap-7">
              <PhoneFrame>
                <img src={reserve} width={600} height={1218} loading="lazy" decoding="async" alt="Priya’s reservation on a phone: the terms of the PayPal hold and a Reserve with PayPal button." className={phoneShot} />
              </PhoneFrame>
              <Caption title="Priya reserves with a hold." className="max-w-[22rem]">
                PayPal holds $45 on her account. She’s charged only once 3 people reserve; if they don’t, the hold is released.
              </Caption>
            </div>
          </div>
        </div>
      </div>
    </PaperSection>
  )
}
