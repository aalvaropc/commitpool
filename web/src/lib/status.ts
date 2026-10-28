import type { EnrollmentStatus, Resolution, WorkshopStatus } from '../api'

export type Tone = 'neutral' | 'hold' | 'paid' | 'cancel' | 'attention'

const TERMINAL: ReadonlySet<EnrollmentStatus> = new Set([
  'VOIDED',
  'REFUNDED',
  'REFUND_FAILED',
  'NEEDS_ATTENTION',
  'RELEASED',
  'CAPTURE_FAILED',
])

// Money that is moving without anyone acting: a capture, a void or a refund.
const IN_FLIGHT: ReadonlySet<EnrollmentStatus> = new Set([
  'CAPTURING',
  'CAPTURE_PENDING',
  'VOIDING',
  'REFUNDING',
  'REFUND_PENDING',
])

export function isTerminal(s: EnrollmentStatus): boolean {
  return TERMINAL.has(s)
}

export function isInFlight(s: EnrollmentStatus): boolean {
  return IN_FLIGHT.has(s)
}

export interface HeadlineInput {
  status: WorkshopStatus
  resolution: Resolution
  /** The viewer's own enrollment status; absent for the owner. */
  enrollment?: EnrollmentStatus | null
}

/** The one-line state of a workshop for whoever is looking at it. */
export function headline({ status, resolution, enrollment }: HeadlineInput): string {
  if (resolution === 'NEEDS_ATTENTION') return 'Needs attention'
  switch (status) {
    case 'COLLECTING':
      return 'Collecting availability'
    case 'FUNDING':
      return fundingHeadline(enrollment)
    case 'CONFIRMED':
      return confirmedHeadline(enrollment)
    case 'CANCELLED':
      return cancelledHeadline(resolution, enrollment)
  }
}

function fundingHeadline(e?: EnrollmentStatus | null): string {
  switch (e) {
    case 'SEAT_RESERVED':
      return 'Seat reserved — authorize your hold'
    case 'AUTHORIZED':
      return 'Hold authorized'
    case 'CAPTURING':
      return 'Charging your hold'
    case 'CAPTURE_PENDING':
      return 'Payment pending verification'
    case 'PAID':
      return 'Seat paid'
    case 'CAPTURE_FAILED':
      return 'Payment failed — you can try again'
    case 'RELEASED':
      return 'Seat expired'
    case 'NEEDS_ATTENTION':
      return 'Needs attention'
  }
  return 'Payments are open'
}

function confirmedHeadline(e?: EnrollmentStatus | null): string {
  switch (e) {
    case 'PAID':
      return 'Seat confirmed'
    case 'AUTHORIZED':
    case 'CAPTURING':
    case 'CAPTURE_PENDING':
      return 'Workshop confirmed — charging your hold'
    case 'VOIDING':
    case 'VOIDED':
      return 'Workshop confirmed — your hold was released'
    case 'REFUNDING':
    case 'REFUND_PENDING':
      return 'Workshop confirmed — refund in progress'
    case 'REFUNDED':
      return 'Workshop confirmed — refund completed'
    case 'NEEDS_ATTENTION':
    case 'REFUND_FAILED':
      return 'Needs attention'
  }
  return 'Workshop confirmed'
}

function cancelledHeadline(r: Resolution, e?: EnrollmentStatus | null): string {
  if (e === undefined) {
    if (r === 'RESOLVING') return 'Workshop cancelled — refund in progress'
    if (r === 'SETTLED') return 'Workshop cancelled — refund completed'
    return 'Workshop cancelled'
  }
  switch (e) {
    case 'PAID':
    case 'REFUNDING':
    case 'REFUND_PENDING':
      return 'Workshop cancelled — refund in progress'
    case 'REFUNDED':
      return 'Workshop cancelled — refund completed'
    case 'AUTHORIZED':
    case 'VOIDING':
    case 'VOIDED':
    case 'SEAT_RESERVED':
    case 'RELEASED':
      return 'Workshop cancelled — your hold was released'
    case 'REFUND_FAILED':
    case 'NEEDS_ATTENTION':
      return 'Workshop cancelled — refund needs attention'
  }
  return 'Workshop cancelled'
}

export function tone(status: WorkshopStatus, resolution: Resolution): Tone {
  if (resolution === 'NEEDS_ATTENTION') return 'attention'
  switch (status) {
    case 'COLLECTING':
      return 'neutral'
    case 'FUNDING':
      return 'hold'
    case 'CONFIRMED':
      return 'paid'
    case 'CANCELLED':
      return 'cancel'
  }
}

export function enrollmentTone(s: EnrollmentStatus): Tone {
  switch (s) {
    case 'PAID':
    case 'REFUNDED':
      return 'paid'
    case 'SEAT_RESERVED':
    case 'AUTHORIZED':
    case 'CAPTURING':
    case 'CAPTURE_PENDING':
    case 'REFUNDING':
    case 'REFUND_PENDING':
    case 'VOIDING':
      return 'hold'
    case 'CAPTURE_FAILED':
    case 'REFUND_FAILED':
    case 'NEEDS_ATTENTION':
      return 'attention'
  }
  return 'neutral'
}

interface PollState {
  status: WorkshopStatus
  resolution: Resolution
  /** The proposal's payments_open: FUNDING, or CONFIRMED before closes_at. */
  paymentsOpen: boolean
}

/**
 * The participant page reads its view again while the own status can still
 * change: while payments are open, while a cancellation is being resolved and
 * while the viewer's money is moving; never once the own status is terminal.
 */
export function shouldPollParticipant(s: PollState & { enrollment: EnrollmentStatus | null }): boolean {
  if (s.enrollment && isTerminal(s.enrollment)) return false
  if (s.status === 'FUNDING' || (s.status === 'CONFIRMED' && s.paymentsOpen)) return true
  if (s.resolution === 'RESOLVING') return true
  return s.enrollment !== null && isInFlight(s.enrollment)
}

/** The owner page reads its view again while funding, resolving or with money moving. */
export function shouldPollOwner(s: PollState & { enrollments: EnrollmentStatus[] }): boolean {
  if (s.status === 'FUNDING' || s.resolution === 'RESOLVING' || s.paymentsOpen) return true
  return s.enrollments.some(isInFlight)
}
