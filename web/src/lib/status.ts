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

const IN_FLIGHT: ReadonlySet<EnrollmentStatus> = new Set([
  'CAPTURING',
  'CAPTURE_PENDING',
  'VOIDING',
  'REFUNDING',
  'REFUND_PENDING',
])

const REFUND_STATES: ReadonlySet<EnrollmentStatus> = new Set(['PAID', 'REFUNDING', 'REFUND_PENDING', 'REFUNDED', 'REFUND_FAILED'])

const NEVER_HELD: ReadonlySet<string> = new Set(['INTERESTED', 'SEAT_RESERVED', 'RELEASED'])

export function refundsAny(enrollments: readonly { status: EnrollmentStatus }[]): boolean {
  return enrollments.some((e) => REFUND_STATES.has(e.status))
}

export function heldAny(enrollments: readonly { status: EnrollmentStatus }[]): boolean {
  return enrollments.some((e) => !NEVER_HELD.has(e.status))
}

export function isTerminal(s: EnrollmentStatus): boolean {
  return TERMINAL.has(s)
}

export function isInFlight(s: EnrollmentStatus): boolean {
  return IN_FLIGHT.has(s)
}

export function seatHeld(
  e: { status: EnrollmentStatus; seat_expires_at: string } | null | undefined,
  paymentsOpen: boolean,
  now: number,
): boolean {
  return !!e && e.status === 'SEAT_RESERVED' && paymentsOpen && Date.parse(e.seat_expires_at) > now
}

export function authorizationPending(
  e: { payment: { authorization_status: string | null } | null } | null | undefined,
): boolean {
  return e?.payment?.authorization_status === 'PENDING'
}

export function reservedSeatNote(input: { authPending: boolean; held: boolean; until: string }): string {
  if (input.authPending) return 'Seat kept while PayPal reviews the authorization'
  if (input.held) return `Seat held until ${input.until}`
  return 'Seat expired'
}

export function returnCheckSettled(input: { enrollment: EnrollmentStatus | null; polling: boolean; authPending: boolean }): boolean {
  if (input.authPending) return true
  if (input.enrollment && input.enrollment !== 'SEAT_RESERVED') return true
  return !input.polling
}

const RETRYABLE: ReadonlySet<EnrollmentStatus> = new Set(['SEAT_RESERVED', 'RELEASED', 'CAPTURE_FAILED'])

export function canStartCheckout(paymentsOpen: boolean, status: EnrollmentStatus | null | undefined, authPending = false): boolean {
  return paymentsOpen && !authPending && (!status || RETRYABLE.has(status))
}

export interface HeadlineInput {
  status: WorkshopStatus
  resolution: Resolution
  enrollment?: EnrollmentStatus | null
  seatHeld?: boolean
  authPending?: boolean
  paymentsOpen?: boolean
  refunds?: boolean
  held?: boolean
}

function flagged(resolution: Resolution, enrollment?: EnrollmentStatus | null): boolean {
  if (resolution !== 'NEEDS_ATTENTION') return false
  return enrollment === undefined || enrollment === 'NEEDS_ATTENTION' || enrollment === 'REFUND_FAILED'
}

export function headline({
  status,
  resolution,
  enrollment,
  seatHeld = true,
  authPending = false,
  paymentsOpen = true,
  refunds = true,
  held = true,
}: HeadlineInput): string {
  if (flagged(resolution, enrollment)) return 'Needs attention'
  if ((status === 'FUNDING' || status === 'CONFIRMED') && enrollment === 'SEAT_RESERVED' && authPending) return 'Hold pending verification'
  if ((status === 'FUNDING' || status === 'CONFIRMED') && enrollment === 'SEAT_RESERVED' && !seatHeld) return 'Seat expired'
  switch (status) {
    case 'COLLECTING':
      return 'Collecting availability'
    case 'FUNDING':
      return fundingHeadline(enrollment, paymentsOpen)
    case 'CONFIRMED':
      return confirmedHeadline(enrollment, paymentsOpen)
    case 'CANCELLED':
      return cancelledHeadline(resolution, enrollment, refunds, held)
  }
}

function fundingHeadline(e: EnrollmentStatus | null | undefined, paymentsOpen: boolean): string {
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
      return paymentsOpen ? 'Payment failed — you can try again' : 'Hold released — nothing was charged'
    case 'RELEASED':
      return 'Seat expired'
    case 'NEEDS_ATTENTION':
      return 'Needs attention'
  }
  return 'Payments are open'
}

function confirmedHeadline(e: EnrollmentStatus | null | undefined, paymentsOpen: boolean): string {
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
    case 'CAPTURE_FAILED':
      return paymentsOpen
        ? 'Workshop confirmed — payment failed, you can try again'
        : 'Workshop confirmed — your hold was released, nothing was charged'
    case 'RELEASED':
      return 'Seat expired'
    case 'NEEDS_ATTENTION':
    case 'REFUND_FAILED':
      return 'Needs attention'
  }
  return 'Workshop confirmed'
}

function cancelledHeadline(r: Resolution, e: EnrollmentStatus | null | undefined, refunds: boolean, held: boolean): string {
  if (e === undefined) {
    if (!refunds && !held && (r === 'RESOLVING' || r === 'SETTLED')) return 'Workshop cancelled — nothing was charged'
    if (r === 'RESOLVING') return refunds ? 'Workshop cancelled — refund in progress' : 'Workshop cancelled — releasing holds'
    if (r === 'SETTLED') return refunds ? 'Workshop cancelled — refund completed' : 'Workshop cancelled — holds released'
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
    case 'CAPTURE_FAILED':
      return 'Workshop cancelled — your hold was released'
    case 'SEAT_RESERVED':
    case 'RELEASED':
      return 'Workshop cancelled — nothing was charged'
    case 'REFUND_FAILED':
    case 'NEEDS_ATTENTION':
      return 'Workshop cancelled — refund needs attention'
  }
  return 'Workshop cancelled'
}

export function tone(status: WorkshopStatus, resolution: Resolution, enrollment?: EnrollmentStatus | null): Tone {
  if (flagged(resolution, enrollment)) return 'attention'
  if (status === 'CONFIRMED' && (enrollment === 'CAPTURE_FAILED' || enrollment === 'NEEDS_ATTENTION' || enrollment === 'REFUND_FAILED')) {
    return 'attention'
  }
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

export function enrollmentTone(s: EnrollmentStatus, ctx: { paymentsOpen?: boolean; seatHeld?: boolean; authPending?: boolean } = {}): Tone {
  const { paymentsOpen = true, seatHeld = true, authPending = false } = ctx
  if (s === 'CAPTURE_FAILED') return paymentsOpen ? 'attention' : 'neutral'
  if (s === 'SEAT_RESERVED' && authPending) return 'hold'
  if (s === 'SEAT_RESERVED' && !seatHeld) return 'neutral'
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
    case 'REFUND_FAILED':
    case 'NEEDS_ATTENTION':
      return 'attention'
  }
  return 'neutral'
}

interface PollState {
  status: WorkshopStatus
  resolution: Resolution
  paymentsOpen: boolean
}

export function shouldPollParticipant(s: PollState & { enrollment: EnrollmentStatus | null }): boolean {
  if (s.enrollment && isTerminal(s.enrollment)) return false
  if (s.status === 'FUNDING' || (s.status === 'CONFIRMED' && s.paymentsOpen)) return true
  if (s.resolution === 'RESOLVING') return true
  return s.enrollment !== null && isInFlight(s.enrollment)
}

export function shouldPollOwner(s: PollState & { enrollments: EnrollmentStatus[] }): boolean {
  if (s.status === 'FUNDING' || s.resolution === 'RESOLVING' || s.paymentsOpen) return true
  return s.enrollments.some(isInFlight)
}
