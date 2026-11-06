import type { EnrollmentStatus, WorkshopStatus } from '../api'
import { count } from './plural'

export function canCancel(status: WorkshopStatus, startsAt: string | null | undefined, now: number): boolean {
  if (status === 'CANCELLED') return false
  return !startsAt || Date.parse(startsAt) > now
}

export function cancelEffect(enrollments: readonly { status: EnrollmentStatus }[]): string {
  const n = (s: EnrollmentStatus[]) => enrollments.filter((e) => s.includes(e.status)).length
  const holds = n(['AUTHORIZED'])
  const paid = n(['PAID'])
  const inFlight = n(['CAPTURING', 'CAPTURE_PENDING'])
  const moves = [holds ? `releases ${count(holds, 'hold')}` : '', paid ? `refunds ${count(paid, 'payment')}` : ''].filter(Boolean)
  const parts = moves.length ? [`PayPal ${moves.join(' and ')}.`] : []
  if (inFlight) parts.push(`${count(inFlight, 'charge')} in progress ${inFlight === 1 ? 'is' : 'are'} refunded once ${inFlight === 1 ? 'it completes' : 'they complete'}.`)
  return parts.length ? parts.join(' ') : 'Nothing is held or charged yet, so no money moves.'
}

export type WithdrawKind = 'release' | 'void'

export function withdrawKind(
  e: { status: EnrollmentStatus; needs_attention?: boolean; payment: { authorization_status: string | null } | null } | null | undefined,
  workshop: WorkshopStatus,
): WithdrawKind | null {
  if (!e || e.needs_attention || workshop === 'CANCELLED') return null
  if (e.status === 'SEAT_RESERVED') return e.payment?.authorization_status === 'PENDING' ? null : 'release'
  if (e.status === 'AUTHORIZED') return 'void'
  return null
}

export type OperatorAction = 'reconcile' | 'retry_refund' | 'clear'

export function operatorActions(e: { status: EnrollmentStatus; money_status?: EnrollmentStatus; needs_attention?: boolean }): OperatorAction[] {
  const out: OperatorAction[] = []
  if (e.needs_attention) out.push('reconcile')
  if ((e.money_status ?? e.status) === 'REFUND_FAILED') out.push('retry_refund')
  if (e.needs_attention) out.push('clear')
  return out
}
