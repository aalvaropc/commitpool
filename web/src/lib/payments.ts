import type { Enrollment, Money, VerifyResource, VerifyResult } from '../api'
import { formatAmount } from './money'
import { count } from './plural'
import { formatClock } from './time'

const isPaid = (e: Enrollment) => (e.money_status ?? e.status) === 'PAID'

const recorded = (e: Enrollment) => (e.payment?.fee && e.payment.net ? { fee: e.payment.fee.minor, net: e.payment.net.minor } : null)

const stillEarning = (e: Enrollment) => isPaid(e) && !e.needs_attention && !e.payment?.refund_id

function feeFigures(enrollments: readonly Enrollment[]): { fees: number; net: number } | null {
  const paid = enrollments.filter(isPaid)
  const known = paid.flatMap((e) => recorded(e) ?? [])
  if (paid.length === 0 || known.length !== paid.length) return null
  return { fees: known.reduce((sum, k) => sum + k.fee, 0), net: known.reduce((sum, k) => sum + k.net, 0) }
}

export function moneySummary(enrollments: readonly Enrollment[], price: Money): string | null {
  const paid = enrollments.filter(isPaid)
  if (paid.length === 0) return null
  const amount = (minor: number) => formatAmount({ minor, currency: price.currency })
  const parts = [`${amount(paid.length * price.minor)} collected`]
  const figures = feeFigures(enrollments)
  if (figures) {
    parts.push(`${amount(figures.fees)} PayPal fees`)
    parts.push(`${amount(figures.net)} net`)
  }
  return parts.join(' · ')
}

export function feesAreShown(enrollments: readonly Enrollment[]): boolean {
  return feeFigures(enrollments) !== null
}

export function earningSeats(enrollments: readonly Enrollment[]): number {
  return enrollments.filter(stillEarning).length
}

export function netCollected(enrollments: readonly Enrollment[]): { seats: number; net: number } | null {
  const seats = enrollments.filter(stillEarning)
  const known = seats.flatMap((e) => recorded(e) ?? [])
  if (known.length === 0 || known.length !== seats.length) return null
  return { seats: seats.length, net: known.reduce((sum, k) => sum + k.net, 0) }
}

export const INTERNATIONAL_FEE_NOTE = 'The Sandbox merchant is registered outside the US, so PayPal charges its international rate.'

const RESOURCE_LABELS: Record<VerifyResource, string> = {
  order: 'PayPal order',
  authorization: 'Hold',
  capture: 'Charge',
  refund: 'Refund',
}

export function resourceLabel(r: VerifyResource): string {
  return RESOURCE_LABELS[r]
}

export function verifyHeadline(result: VerifyResult, timeZone: string): string {
  const { records, matching } = result.summary
  const checked = `checked ${formatClock(result.checked_at, timeZone)}`
  if (records === 0) return `No payment has reached PayPal yet · ${checked}`
  const verb = matching === 1 ? 'matches' : 'match'
  return `${matching} of ${count(records, 'record')} ${verb} PayPal · ${checked}`
}

export function verifySkipped(result: VerifyResult): string | null {
  const { seats, skipped } = result.summary
  if (skipped === 0) return null
  return `Checked the first ${count(seats, 'spot')}; ${count(skipped, 'more spot', 'more spots')} not checked this time.`
}

export interface VerifyDifference {
  key: string
  text: string
  debugId?: string
}

export function verifyDifferences(result: VerifyResult): VerifyDifference[] {
  return result.seats.flatMap((seat) =>
    seat.checks
      .filter((c) => !c.match)
      .map((c) => ({
        key: `${seat.enrollment_id}-${c.resource}`,
        text: `${seat.seat || 'A spot'} — ${resourceLabel(c.resource)} ${c.id}: CommitPool has ${c.local}; PayPal has ${c.paypal}.`,
        debugId: c.debug_id || undefined,
      })),
  )
}
