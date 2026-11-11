import type { ApprovalState, ApprovalView } from '../api'
import type { Tone } from './status'
import { lockedTerms, type TermItem } from './terms'
import { formatDateRange, formatMoment } from './time'

export type ApprovalOutcome = Exclude<ApprovalState, 'pending'> | 'declined'

export interface ApprovalMessage {
  title: string
  body: string
  tone: Tone
}

const MESSAGES: Record<ApprovalOutcome, ApprovalMessage> = {
  approved: {
    title: 'Approved. Your assistant can now open reservations.',
    body: 'It can open them once, on exactly these terms. Nothing is charged until enough people reserve.',
    tone: 'paid',
  },
  used: {
    title: 'Reservations are open',
    body: 'Your assistant opened reservations on the terms you approved. Your organizer page shows who reserves.',
    tone: 'hold',
  },
  expired: {
    title: 'This request expired',
    body: 'Nothing was opened. Ask your assistant to prepare the terms again; it will send you a new link.',
    tone: 'attention',
  },
  changed: {
    title: 'The terms changed',
    body: 'The workshop changed after your assistant prepared this request, so it can no longer be approved. Ask your assistant to prepare the terms again.',
    tone: 'attention',
  },
  closed: {
    title: 'Nothing to approve',
    body: 'Reservations are already open, or the workshop was cancelled. Your organizer page shows where it stands.',
    tone: 'neutral',
  },
  declined: {
    title: 'Not approved',
    body: 'Nothing changed, and your assistant cannot open reservations unless you approve. Tell it what to change, or approve here later.',
    tone: 'neutral',
  },
}

export function approvalOutcome(state: ApprovalState, declined: boolean): ApprovalOutcome | null {
  if (state !== 'pending') return state
  return declined ? 'declined' : null
}

export function approvalMessage(outcome: ApprovalOutcome): ApprovalMessage {
  return MESSAGES[outcome]
}

export function refusedState(code: string): ApprovalState | null {
  switch (code) {
    case 'approval_expired':
      return 'expired'
    case 'approval_used':
      return 'used'
    case 'terms_changed':
      return 'changed'
    case 'approval_closed':
      return 'closed'
  }
  return null
}

export function approvalTerms(v: ApprovalView): TermItem[] {
  const a = v.approval
  const zone = v.workshop.timezone
  return lockedTerms({
    timeZone: zone,
    dateLabel: formatDateRange(a.starts_at, a.ends_at, zone),
    price: a.price,
    durationMin: v.workshop.duration_min,
    windowMin: a.payment_window_min,
    minimum: a.minimum,
    capacity: a.capacity,
    payoutTo: a.payout_email || 'No PayPal email given',
  })
}

export function confirmDeadline(v: ApprovalView): string {
  return `Your assistant has until ${formatMoment(v.approval.expires_at, v.workshop.timezone)} to open them; after that it needs your approval again.`
}

export interface ApproveFailure {
  refresh: boolean
  message: string
}

export function approveFailure(status: number, code: string, message: string, retried: boolean): ApproveFailure {
  if (status === 403 && code === 'approval_needs_page') {
    if (!retried) return { refresh: true, message: 'This page had to refresh before approving. Tap Approve again.' }
    return {
      refresh: false,
      message: 'This browser could not confirm the tap came from this page, so nothing was approved. Open the link in an up-to-date browser with cookies allowed for this site.',
    }
  }
  return { refresh: false, message }
}
