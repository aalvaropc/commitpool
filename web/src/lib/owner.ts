import type { Availability, DateSummary, OwnerView, Verdict } from '../api'
import { dateNames } from './event'
import { formatMoney } from './money'
import { count } from './plural'
import { heldAny, shouldPollOwner } from './status'
import { verdictText } from './reading'
import { formatTimeRange } from './time'

export type NextAction = { kind: 'share' } | { kind: 'open'; dateId: string; day: string } | { kind: 'none' }

export interface NextStep {
  title: string
  detail: string
  action: NextAction
}

const usd = (minor: number, currency: string) => formatMoney({ minor, currency })

const MAX_NAMED = 3

function clarifying(v: OwnerView): string {
  const names = v.availabilities.filter((a) => a.needs_clarification).map((a) => a.display_name ?? 'Someone')
  if (names.length === 0) return ''
  if (names.length === 1) return `${names[0]}’s message needs clarifying. `
  const shown = names.slice(0, MAX_NAMED)
  const rest = names.length - shown.length
  const list = rest > 0 ? `${shown.join(', ')} and ${count(rest, 'other')}` : `${shown.slice(0, -1).join(', ')} and ${shown.at(-1)}`
  return `${list} need to clarify their message. `
}

export function cancellationDetail(v: OwnerView): string {
  const voided = (e: OwnerView['enrollments'][number]) => e.status === 'VOIDED' || e.payment?.authorization_status === 'VOIDED'
  const released = v.enrollments.filter(voided).length
  const releasing = v.enrollments.filter((e) => (e.status === 'VOIDING' || e.status === 'CAPTURE_FAILED') && !voided(e)).length
  const refunded = v.enrollments.filter((e) => e.status === 'REFUNDED').length
  const refunding = v.enrollments.filter((e) => e.status === 'PAID' || e.status === 'REFUNDING' || e.status === 'REFUND_PENDING').length
  const failed = v.enrollments.filter((e) => e.status === 'REFUND_FAILED' || e.status === 'NEEDS_ATTENTION' || e.needs_attention).length
  const parts = [count(released, 'hold') + ' released', count(refunded, 'refund') + ' completed', `${refunding + releasing} in progress`]
  if (failed) parts.push(`${failed} ${failed === 1 ? 'needs' : 'need'} attention`)
  return parts.join(', ') + '.'
}

function collecting(v: OwnerView): NextStep {
  const { recommendation: rec, workshop: w } = v
  if (v.availabilities.length === 0 || !rec.date_id) {
    return { title: 'Share the invite to get answers', detail: 'Answers show up here as people reply.', action: { kind: 'share' } }
  }
  const day = dateNames(v.dates, w.timezone).get(rec.date_id) ?? 'The recommended date'
  const can = rec.per_date?.find((d) => d.date_id === rec.date_id)?.compatible?.length ?? 0
  if (can === 0) {
    return { title: 'Nobody can make a date yet.', detail: `${clarifying(v)}Share the invite to reach more people.`, action: { kind: 'share' } }
  }
  if (rec.meets_minimum) {
    return {
      title: `${day} works for ${can} — you need ${w.minimum}.`,
      detail: `${clarifying(v)}Open reservations and people can reserve their spot with PayPal.`,
      action: { kind: 'open', dateId: rec.date_id, day },
    }
  }
  return { title: `Best so far: ${day}, with ${can} of the ${w.minimum} needed.`, detail: `${clarifying(v)}Share the invite to reach more people.`, action: { kind: 'share' } }
}

function funding(v: OwnerView): NextStep {
  const { tally: t, proposal: p } = v
  const committed = t.paid + t.authorized + t.capturing + t.pending
  const more = Math.max(t.minimum - committed, 0)
  if (!p?.payments_open) {
    if (more > 0) {
      return {
        title: `Only ${committed} of the ${t.minimum} needed reserved.`,
        detail: 'The class is being cancelled: holds are released and any payment refunded.',
        action: { kind: 'none' },
      }
    }
    return { title: `${committed} reserved — confirming payments.`, detail: 'The result follows in a moment.', action: { kind: 'none' } }
  }
  if (more === 0) return { title: `${committed} reserved — confirming payments.`, detail: 'Holds are being charged.', action: { kind: 'share' } }
  return { title: `${committed} of ${t.minimum} reserved — ${more} more and it’s on.`, detail: 'Share the invite again.', action: { kind: 'share' } }
}

function settling(v: OwnerView): NextStep {
  const none: NextAction = { kind: 'none' }
  if (v.enrollments.length === 0) return { title: 'Nothing to settle.', detail: 'Nobody had reserved, so nothing was held or charged.', action: none }
  if (!heldAny(v.enrollments)) return { title: 'Nothing to settle.', detail: 'No hold or payment went through, so nothing was charged.', action: none }
  if (v.workshop.resolution !== 'SETTLED') {
    return { title: 'PayPal is returning the money.', detail: 'Holds are released and payments refunded on their own. This page updates as each one completes.', action: none }
  }
  return { title: 'Everything is settled.', detail: 'There is nothing else to do.', action: none }
}

export function nextStep(v: OwnerView): NextStep {
  const { workshop: w, tally: t, proposal: p } = v
  if (w.resolution === 'NEEDS_ATTENTION') {
    return { title: 'PayPal left a payment unsettled.', detail: 'The check below says what happened and offers the fix.', action: { kind: 'none' } }
  }
  switch (w.status) {
    case 'COLLECTING':
      return collecting(v)
    case 'FUNDING':
      return funding(v)
    case 'CONFIRMED': {
      const price = p?.price ?? w.price
      return {
        title: `${t.paid} paid — ${usd(t.paid * price.minor, price.currency)} collected.`,
        detail: p?.payments_open ? 'Spots still open can be reserved until reservations close.' : 'Everyone who paid got their spot.',
        action: p?.payments_open ? { kind: 'share' } : { kind: 'none' },
      }
    }
    case 'CANCELLED':
      return settling(v)
  }
}

export type DecisionBar = { kind: 'interest'; can: number } | { kind: 'seats'; paid: number; reserved: number; taken: number }

export interface DecisionCounts {
  bar: DecisionBar | null
  needed: number
  capacity: number
  joined: number
  charging: number
  failed: number
}

function decisionBar(v: OwnerView): DecisionBar | null {
  const { workshop: w, tally: t, recommendation: rec } = v
  if (w.status === 'CANCELLED') return null
  if (v.proposal) return { kind: 'seats', paid: t.paid, reserved: t.authorized + t.capturing + t.pending, taken: t.seats_taken }
  if (v.availabilities.length === 0 || !rec.date_id) return null
  return { kind: 'interest', can: rec.per_date?.find((d) => d.date_id === rec.date_id)?.compatible?.length ?? 0 }
}

export function decisionCounts(v: OwnerView): DecisionCounts {
  const { workshop: w, tally: t } = v
  return {
    bar: decisionBar(v),
    needed: v.proposal ? t.minimum : w.minimum,
    capacity: v.proposal ? t.capacity : w.capacity,
    joined: t.interested,
    charging: t.capturing + t.pending,
    failed: t.failed,
  }
}

export function countFacts(c: DecisionCounts): string[] {
  const facts = [`${c.joined} joined`]
  if (c.bar?.kind === 'seats') facts.push(`${c.bar.taken} of ${count(c.capacity, 'spot')} taken`)
  if (c.charging) facts.push(`${count(c.charging, 'charge')} in progress`)
  if (c.failed) facts.push(`${c.failed} failed`)
  return facts
}

export function sessionPlan(v: OwnerView): string | null {
  const { workshop: w } = v
  const cost = w.venue_cost
  if (!cost || v.proposal || w.status !== 'COLLECTING' || w.price.minor <= 0) return null
  const seats = w.break_even_seats || Math.ceil(cost.minor / w.price.minor)
  const money = (minor: number, currency: string) => usd(minor, currency)
  const session = `Your session costs ${money(cost.minor, cost.currency)}`
  const price = money(w.price.minor, w.price.currency)
  return seats <= w.capacity ? `${session} — ${seats} × ${price} covers it before fees` : `${session} — even ${w.capacity} × ${price} falls short before fees`
}

export interface Coverage {
  covered: number
  cost: number
  text: string
}

export function sessionCovered(v: OwnerView): Coverage | null {
  const cost = v.workshop.venue_cost
  if (!cost || v.workshop.status === 'CANCELLED') return null
  const price = v.proposal?.price ?? v.workshop.price
  const covered = v.tally.paid * price.minor
  const money = (minor: number) => usd(minor, cost.currency)
  const text = covered >= cost.minor ? `Your ${money(cost.minor)} session costs are covered before fees, with ${money(covered - cost.minor)} to spare` : `${money(covered)} toward your ${money(cost.minor)} session costs (before fees)`
  return { covered, cost: cost.minor, text }
}

interface RefreshInput {
  status: OwnerView['workshop']['status']
  resolution: OwnerView['workshop']['resolution']
  paymentsOpen: boolean
  enrollments: OwnerView['enrollments'][number]['status'][]
  explanationPending: boolean
  demoFilling?: boolean
}

export function ownerRefresh(s: RefreshInput): number | null {
  if (s.demoFilling && s.status === 'COLLECTING') return 2000
  if (s.explanationPending && s.status === 'COLLECTING') return 3000
  if (shouldPollOwner(s)) return 5000
  if (s.status === 'COLLECTING') return 8000
  return null
}

export interface DateColumn {
  id: string
  name: string
  time: string
  can: number
  mark: 'Recommended' | 'Chosen date' | null
  canOpen: boolean
}

function markFor(v: OwnerView, id: string): DateColumn['mark'] {
  if (v.proposal) return v.proposal.date_id === id ? 'Chosen date' : null
  return v.availabilities.length > 0 && v.recommendation.date_id === id ? 'Recommended' : null
}

export function dateColumns(v: OwnerView): DateColumn[] {
  const { workshop: w, recommendation: rec } = v
  const names = dateNames(v.dates, w.timezone)
  const can = new Map((rec.per_date ?? []).map((d) => [d.date_id, d.compatible?.length ?? 0]))
  return v.dates.map((d) => {
    const n = can.get(d.id) ?? 0
    return {
      id: d.id,
      name: names.get(d.id) ?? '',
      time: formatTimeRange(d.starts_at, d.ends_at, w.timezone),
      can: n,
      mark: markFor(v, d.id),
      canOpen: w.status === 'COLLECTING' && !v.proposal && n >= w.minimum && d.id !== rec.date_id,
    }
  })
}

export interface CellNotes {
  fit: string | null
  claudeRead: string | null
}

export function cellNotes(a: Availability, dateId: string, summary: DateSummary | undefined): CellNotes {
  const find = (r: Availability['interpretation']) => r.per_date.find((x) => x.candidate_date_id === dateId)
  const reading = a.corrected ?? a.interpretation
  const current = find(reading)
  const original = a.corrected ? find(a.interpretation) : undefined
  const id = a.participant_id ?? ''
  let fit: string | null = null
  if (current?.verdict === 'conditional' && !reading.needs_clarification) {
    if (summary?.compatible?.includes(id)) fit = 'Fits this time'
    else if (summary?.blockers?.[id]) fit = 'Doesn’t fit this time'
  }
  const claudeRead = original && current && verdictText(original) !== verdictText(current) ? `Claude read: ${verdictText(original)}` : null
  return { fit, claudeRead }
}

export interface CellStatus {
  chip: { verdict: Verdict; text: string } | null
  pill: string | null
  readAs: string | null
}

export const NOT_COUNTED = 'Not counted — needs clarifying'

export function cellStatus(a: Availability, dateId: string): CellStatus {
  const reading = a.corrected ?? a.interpretation
  const v = reading.per_date.find((x) => x.candidate_date_id === dateId)
  if (reading.needs_clarification) {
    return { chip: null, pill: NOT_COUNTED, readAs: v && v.verdict !== 'unknown' ? `Read as: ${verdictText(v)}` : null }
  }
  if (!v) return { chip: null, pill: null, readAs: null }
  return { chip: { verdict: v.verdict, text: verdictText(v) }, pill: null, readAs: null }
}
