import type { Ref } from 'react'
import type { OwnerView } from '../api'
import { cancellationDetail, countFacts, decisionCounts, studioCovered, studioPlan, type DecisionCounts, type NextStep as Step } from '../lib/owner'
import { headline, heldAny, refundsAny, releasesAny, tone } from '../lib/status'
import { formatDateRange } from '../lib/time'
import CopyField from './CopyField'
import Deadline from './Deadline'
import { statusIcons } from './StatusBanner'
import { Button, Icon, Meter, toneText, toneWash } from './ui'

interface Props {
  view: OwnerView
  step: Step
  onOpen: (dateId: string) => void
  titleRef?: Ref<HTMLHeadingElement>
}

export function InviteLink({ url, hint }: { url?: string; hint: string }) {
  if (!url) return <p className="text-sm text-ink-soft">This workshop was created before invite links were kept, so there is none to show.</p>
  return <CopyField id="share-url" label="Invite link" value={url} hint={hint} />
}

function Key({ swatch, children }: { swatch: string; children: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span aria-hidden="true" className={swatch} />
      {children}
    </span>
  )
}

function Counts({ counts }: { counts: DecisionCounts }) {
  const bar = counts.bar
  return (
    <div className="flex flex-col gap-2">
      {bar?.kind === 'interest' && (
        <>
          <Meter capacity={counts.capacity} needed={counts.needed} paid={0} reserved={bar.can} interest />
          <p aria-hidden="true" className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-ink-soft tabular-nums">
            <Key swatch="size-2.5 rounded-full bg-ink-faint">{`${bar.can} can come`}</Key>
            <Key swatch="h-3 w-[3px] rounded-full bg-ink">{`${counts.needed} needed`}</Key>
            <span>{`${counts.capacity} spots`}</span>
          </p>
        </>
      )}
      {bar?.kind === 'seats' && (
        <>
          <Meter capacity={counts.capacity} needed={counts.needed} paid={bar.paid} reserved={bar.reserved} caption={false} />
          <p aria-hidden="true" className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-ink-soft tabular-nums">
            <Key swatch="size-2.5 rounded-full bg-paid">{`${bar.paid} paid`}</Key>
            <Key swatch="size-2.5 rounded-full bg-hold">{`${bar.reserved} reserved`}</Key>
            <Key swatch="h-3 w-[3px] rounded-full bg-ink">{`${counts.needed} needed`}</Key>
          </p>
        </>
      )}
      <p className="text-sm text-ink-soft tabular-nums">{countFacts(counts).join(' · ')}</p>
    </div>
  )
}

export default function Decision({ view, step, onOpen, titleRef }: Props) {
  const w = view.workshop
  const p = view.proposal
  const held = heldAny(view.enrollments)
  const status = headline({
    status: w.status,
    resolution: w.resolution,
    paymentsOpen: p?.payments_open ?? false,
    refunds: refundsAny(view.enrollments),
    held,
    releases: releasesAny(view.enrollments),
  })
  const t = tone(w.status, w.resolution)
  const chosen = p ? view.dates.find((d) => d.id === p.date_id) : undefined
  const coverage = p ? studioCovered(view) : null
  const plan = studioPlan(view)
  const action = step.action
  return (
    <div className="flex flex-col gap-4 rounded-tile bg-surface p-5 sm:p-7">
      <section aria-label="Workshop status">
        <div role="status" aria-live="polite" className="flex items-center gap-2.5">
          <span aria-hidden="true" className={`grid size-8 shrink-0 place-items-center rounded-full ${toneWash[t]} ${toneText[t]}`}>
            <Icon name={statusIcons[t]} size={16} />
          </span>
          <p className={`font-display text-lg leading-snug font-bold ${t === 'neutral' ? 'text-ink-soft' : toneText[t]}`}>{status}</p>
        </div>
      </section>
      <section aria-label="Next step" className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <h2 ref={titleRef} tabIndex={-1} className="max-w-[26ch] font-display text-[1.75rem] leading-tight font-bold sm:text-[2rem]">
            {step.title}
          </h2>
          <p className="max-w-prose text-ink-soft">{step.detail}</p>
          {w.status === 'CANCELLED' && held && <p className="max-w-prose">{cancellationDetail(view)}</p>}
        </div>
        {(chosen || p?.payments_open) && (
          <div className="flex flex-col gap-1.5 sm:flex-row sm:flex-wrap sm:items-baseline sm:gap-x-6">
            {chosen && (
              <p className="flex items-center gap-2 text-sm font-semibold tabular-nums">
                <Icon name="calendar" size={16} className="shrink-0 text-ink-soft" />
                {formatDateRange(chosen.starts_at, chosen.ends_at, w.timezone)}
              </p>
            )}
            {p?.payments_open && <Deadline closesAt={p.closes_at} timeZone={w.timezone} open />}
          </div>
        )}
        <div className="flex flex-col gap-3 lg:hidden">
          <Counts counts={decisionCounts(view)} />
          {coverage && <p className="text-sm font-semibold">{coverage.text}</p>}
        </div>
        {plan && <p className="text-sm font-semibold">{plan}</p>}
        {action.kind === 'open' && (
          <div>
            <Button size="lg" onClick={() => onOpen(action.dateId)}>
              Open reservations for {action.day}
              <Icon name="arrowRight" size={20} />
            </Button>
          </div>
        )}
        {action.kind === 'share' && <InviteLink url={view.share_url} hint="Send it to your group chat. Anyone with it can join and say when they can come." />}
      </section>
    </div>
  )
}
