import type { OwnerView, Payout } from '../api'
import { formatAmount } from './money'
import { count } from './plural'
import type { Tone } from './status'
import { formatMoment } from './time'

export interface PayoutSummary {
  tone: Tone
  title: string
  detail: string
}

const SETTLING: Record<string, string> = {
  payments_in_flight: 'A payment is still moving at PayPal.',
  fees_missing: 'PayPal has not reported every fee yet.',
}

function waiting(p: Payout, to: string): PayoutSummary {
  const reason = p.waiting ?? ''
  if (reason === 'nothing_to_pay') {
    return { tone: 'neutral', title: 'Nothing to pay out', detail: 'Nobody’s payment went through, so there is nothing to send.' }
  }
  if (reason === 'payment_flagged' || reason === 'needs_attention') {
    return {
      tone: 'attention',
      title: 'Your payout waits for a check',
      detail: `A payment needs your check first, in Needs your check. Your payout to ${to} waits until it is cleared.`,
    }
  }
  return {
    tone: 'hold',
    title: 'Waiting for payments to settle',
    detail: `${SETTLING[reason] ?? 'A payment has not settled yet.'} Your payout to ${to} goes out on its own once it does.`,
  }
}

const money = (p: Payout, key: 'amount' | 'gross' | 'fees') => {
  const m = p[key]
  return m ? formatAmount(m) : null
}

export function payoutBreakdown(p: Payout): string | null {
  const gross = money(p, 'gross')
  if (!gross || p.seats === 0) return null
  const fees = money(p, 'fees')
  const amount = money(p, 'amount')
  const parts = [`${gross} paid by ${count(p.seats, 'person', 'people')}`]
  if (fees && amount) parts.push(`${fees} PayPal fees`, `${amount} to you`)
  return parts.join(' · ')
}

export function payoutSummary(p: Payout, timeZone: string): PayoutSummary {
  const to = p.receiver ?? 'your PayPal email'
  const amount = money(p, 'amount')
  const sum = amount ?? 'what people paid, less PayPal’s fees,'
  switch (p.state) {
    case 'no_email':
      return {
        tone: 'neutral',
        title: 'No payout email',
        detail: 'You didn’t give a PayPal email when you created the workshop, so CommitPool doesn’t pay you out automatically.',
      }
    case 'off':
      return { tone: 'neutral', title: 'Payouts are off on this server', detail: `Nothing is sent to ${to} automatically.` }
    case 'scheduled':
      return {
        tone: 'hold',
        title: p.scheduled_at ? `Paid out when the class starts, ${formatMoment(p.scheduled_at, timeZone)}` : 'Paid out when the class starts',
        detail: `CommitPool sends ${sum} to ${to} through PayPal.${amount ? ' The amount is final once the class starts.' : ''}`,
      }
    case 'waiting':
      return waiting(p, to)
    case 'created':
      return { tone: 'hold', title: `Getting ${amount ?? 'your payout'} ready for ${to}`, detail: 'The amount is final. PayPal has not confirmed the payout yet.' }
    case 'parked':
      return {
        tone: 'attention',
        title: 'The payout needs a check',
        detail: `PayPal did not answer clearly, so the payout of ${amount ?? 'your money'} to ${to} waits for someone to try it again. It is never sent twice.`,
      }
    case 'sending':
      return { tone: 'hold', title: `Sending ${amount ?? 'your payout'} to ${to}`, detail: 'PayPal is processing the payout.' }
    case 'paid':
      return {
        tone: 'paid',
        title: `Paid out ${amount ?? 'your payout'} to ${to}${p.payout_batch_id ? ` · PayPal batch ${p.payout_batch_id}` : ''}`,
        detail: 'PayPal shows it in that account’s activity.',
      }
    case 'unclaimed':
      return {
        tone: 'hold',
        title: `Waiting for ${to} to claim ${amount ?? 'the payout'}`,
        detail: 'PayPal holds it until that address signs in to PayPal and accepts it. Unclaimed payouts go back after 30 days.',
      }
    case 'failed':
      return {
        tone: 'attention',
        title: 'The payout didn’t go through',
        detail: p.issue ? `PayPal answered ${p.issue}. Quote the debug ID below to PayPal support.` : 'PayPal did not complete it. Quote the IDs below to PayPal support.',
      }
    default:
      return {
        tone: 'attention',
        title: 'The payout needs a check',
        detail: 'CommitPool could not confirm whether PayPal made the payout, so it will not send it again until someone checks at PayPal.',
      }
  }
}

export function payoutIds(p: Payout): [string, string][] {
  const ids: [string, string | undefined][] = [
    ['Sender batch', p.sender_batch_id],
    ['PayPal batch', p.payout_batch_id],
    ['Payout item', p.payout_item_id],
    ['Transaction', p.transaction_id],
    ['Batch status', p.batch_status],
    ['Item status', p.item_status],
    ['Debug ID', p.debug_id],
  ]
  return ids.filter((x): x is [string, string] => Boolean(x[1]))
}

export function payoutCheckable(p: Payout | null): boolean {
  return Boolean(p?.payout_batch_id) && (p?.state === 'sending' || p?.state === 'unclaimed' || p?.state === 'unknown')
}

export function canStartClass(v: { demo: { demo_mode: boolean }; workshop: { status: string }; proposal: { payments_open: boolean } | null; payout: Payout | null }): boolean {
  return v.demo.demo_mode && v.workshop.status === 'CONFIRMED' && v.proposal !== null && !v.proposal.payments_open && (v.payout?.state === 'scheduled' || v.payout?.state === 'waiting')
}

export interface ClassStarted {
  headline: string
  sub: string | null
}

export function classStarted(v: Pick<OwnerView, 'workshop' | 'payout'>): ClassStarted | null {
  const p = v.payout
  const amount = p?.state === 'paid' && p.amount ? formatAmount(p.amount) : null
  const host = v.workshop.host_name?.trim().split(/\s+/)[0]
  if (!p || !amount || !host || v.workshop.status !== 'CONFIRMED') return null
  const status = p.batch_status ?? p.item_status
  const sub = p.payout_batch_id ? `PayPal batch ${p.payout_batch_id}${status ? ` · ${status}` : ''}` : null
  return { headline: `Class started · ${host} paid ${amount}`, sub }
}
