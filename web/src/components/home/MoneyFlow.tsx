import type { ReactNode } from 'react'
import { NEEDED, PRICE, SESSION_COST } from '../../lib/chatScript'
import { Icon, type IconName } from '../ui'
import { Easels } from './Drawings'
import { PaperSection, SectionHead } from './Section'

export const SANDBOX_RUN = { date: 'Nov 10, 2026', collected: 13_500, fees: 819, net: 12_681 }

function money(c: number): string {
  return `$${(c / 100).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

const chips: Record<string, { icon: IconName; className: string }> = {
  Reserved: { icon: 'lock', className: 'bg-hold-wash text-hold' },
  Paid: { icon: 'check', className: 'bg-paid-wash text-paid' },
  'If short': { icon: 'x', className: 'bg-no-wash text-no' },
  'It’s on': { icon: 'spark', className: 'bg-brand text-on-brand' },
}

function Ledger({ chip, title, amount, branch = false, children }: { chip: string; title: string; amount: string; branch?: boolean; children: ReactNode }) {
  const look = chips[chip]
  return (
    <li
      className={`grid content-start gap-2.5 border-b border-paper-rule pt-[1.375rem] pb-6 md:border-b-0 md:border-l md:px-[1.375rem] md:pt-6 md:pb-[1.625rem] md:first:border-l-0 md:first:pl-0 ${branch ? 'border-dashed' : ''}`}
    >
      <span className={`inline-flex items-center gap-1.5 justify-self-start rounded-full py-[0.1875rem] pr-2.5 pl-2 text-[0.8125rem] leading-[1.3] font-semibold ${look.className}`}>
        <Icon name={look.icon} size={14} strokeWidth={2.2} />
        {chip}
      </span>
      <h3 className="text-[1.375rem] tracking-[-0.02em]">{title}</h3>
      <p className="text-graphite">{children}</p>
      <p className={`shout mt-1 text-[1.75rem] tracking-[-0.01em] normal-case tabular-nums ${branch ? 'text-graphite' : ''}`}>{amount}</p>
    </li>
  )
}

export default function MoneyFlow() {
  const m = SANDBOX_RUN
  return (
    <PaperSection id="money" labelledBy="money-title">
      <div className="wrap section-pad">
        <div className="mb-[clamp(2.25rem,5vw,3.5rem)] grid items-end gap-9 lg:grid-cols-12 lg:gap-x-8">
          <SectionHead id="money-title" title="The money comes last." tone="paper" stacked className="lg:col-span-6">
            Nobody pays for a class that doesn’t happen. Iris is paid when it starts.
          </SectionHead>
          <figure className="m-0 w-full max-w-[33.75rem] lg:col-span-6 lg:justify-self-end">
            <Easels reserved={['Priya', 'Ben', 'Grace']} open={3} className="w-full" />
          </figure>
        </div>
        <ol aria-label="Where the money goes" className="grid border-t-2 border-charcoal md:grid-cols-4">
          <Ledger chip="Reserved" title="A hold, not a charge" amount={`$${PRICE} held`}>
            Each person reserves at PayPal. PayPal holds ${PRICE} on their account; nothing is charged.
          </Ledger>
          <Ledger chip="Paid" title="Charged only at the minimum" amount={`$${PRICE * NEEDED} charged`}>
            The third reservation reaches Iris’s minimum. CommitPool charges the three holds.
          </Ledger>
          <Ledger chip="If short" title="Released or refunded" amount="$0 for anyone" branch>
            Fewer than {NEEDED} by the deadline? Every hold is released and any charge is refunded.
          </Ledger>
          <Ledger chip="It’s on" title="Paid out to Iris at class start" amount={`${money(m.net)} sent`}>
            When the class starts, CommitPool sends Iris her money with PayPal Payouts.
          </Ledger>
        </ol>
        <div className="kiln mt-9 flex flex-wrap items-center justify-between gap-x-8 gap-y-4 rounded-[1.25rem] bg-charcoal px-6 py-[1.375rem]">
          <p className="text-[clamp(1.125rem,0.9rem+1vw,1.625rem)] font-bold tracking-[-0.015em] tabular-nums">
            {money(m.collected)} collected <span className="font-medium text-ink-faint">·</span> {money(m.fees)} PayPal fees <span className="font-medium text-ink-faint">·</span>{' '}
            <b className="font-extrabold text-paid">{money(m.net)} net</b>
          </p>
          <p className="text-[0.9375rem] text-ink-soft">
            Covers the ${SESSION_COST} room and model. Figures from a real PayPal Sandbox run on {m.date}, with the fees PayPal charged.
          </p>
        </div>
      </div>
    </PaperSection>
  )
}
