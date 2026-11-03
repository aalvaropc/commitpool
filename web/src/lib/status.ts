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

// A seat still paid in a cancelled workshop is about to be refunded.
const REFUND_STATES: ReadonlySet<EnrollmentStatus> = new Set(['PAID', 'REFUNDING', 'REFUND_PENDING', 'REFUNDED', 'REFUND_FAILED'])

// Enrollments that never placed a hold: nothing of theirs was ever at PayPal.
const NEVER_HELD: ReadonlySet<string> = new Set(['INTERESTED', 'SEAT_RESERVED', 'RELEASED'])

/** Whether any enrollment was refunded, is being refunded or is still paid. */
export function refundsAny(enrollments: readonly { status: EnrollmentStatus }[]): boolean {
  return enrollments.some((e) => REFUND_STATES.has(e.status))
}

/** Whether any enrollment ever placed a hold at PayPal. */
export function heldAny(enrollments: readonly { status: EnrollmentStatus }[]): boolean {
  return enrollments.some((e) => !NEVER_HELD.has(e.status))
}

export function isTerminal(s: EnrollmentStatus): boolean {
  return TERMINAL.has(s)
}

export function isInFlight(s: EnrollmentStatus): boolean {
  return IN_FLIGHT.has(s)
}

/**
 * The own seat is still held for checkout: reserved, payments open and its
 * expiry ahead. The server's "Seat reserved" label means the same.
 */
export function seatHeld(
  e: { status: EnrollmentStatus; seat_expires_at: string } | null | undefined,
  paymentsOpen: boolean,
  now: number,
): boolean {
  return !!e && e.status === 'SEAT_RESERVED' && paymentsOpen && Date.parse(e.seat_expires_at) > now
}

/** PayPal holds the enrollment's latest authorization PENDING (under review); the server keeps the seat while it does. */
export function authorizationPending(
  e: { payment: { authorization_status: string | null } | null } | null | undefined,
): boolean {
  return e?.payment?.authorization_status === 'PENDING'
}

/** The line under a reserved seat in the organizer's list; `until` is the formatted end of a live hold. */
export function reservedSeatNote(input: { authPending: boolean; held: boolean; until: string }): string {
  if (input.authPending) return 'Seat kept while PayPal reviews the authorization'
  if (input.held) return `Seat held until ${input.until}`
  return 'Seat expired'
}

/**
 * Whether the check a PayPal return asked for is over: PayPal holds the
 * authorization for review, the payment moved past the checkout, or nothing
 * more will change on its own.
 */
export function returnCheckSettled(input: { enrollment: EnrollmentStatus | null; polling: boolean; authPending: boolean }): boolean {
  if (input.authPending) return true
  if (input.enrollment && input.enrollment !== 'SEAT_RESERVED') return true
  return !input.polling
}

// After these the server takes a new checkout: a new enrollment, or a renewed seat.
const RETRYABLE: ReadonlySet<EnrollmentStatus> = new Set(['SEAT_RESERVED', 'RELEASED', 'CAPTURE_FAILED'])

/**
 * Whether to offer "Authorize with PayPal": no enrollment yet, a reserved seat,
 * or a retryable outcome; never while PayPal is still reviewing an authorization.
 */
export function canStartCheckout(paymentsOpen: boolean, status: EnrollmentStatus | null | undefined, authPending = false): boolean {
  return paymentsOpen && !authPending && (!status || RETRYABLE.has(status))
}

export interface HeadlineInput {
  status: WorkshopStatus
  resolution: Resolution
  /** The viewer's own enrollment status; absent for the owner. */
  enrollment?: EnrollmentStatus | null
  /** For a SEAT_RESERVED enrollment, whether the seat is still held (seatHeld); a lapsed one reads as expired. */
  seatHeld?: boolean
  /** For a SEAT_RESERVED enrollment, whether PayPal holds its authorization PENDING (authorizationPending); the seat is kept meanwhile. */
  authPending?: boolean
  /** The proposal's payments_open; a failed charge only invites another try while it is true. */
  paymentsOpen?: boolean
  /** For the owner of a cancelled workshop: whether any enrollment was refunded or is being refunded (refundsAny). */
  refunds?: boolean
  /** For the owner of a cancelled workshop: whether any enrollment ever placed a hold (heldAny). */
  held?: boolean
}

/**
 * The workshop-level NEEDS_ATTENTION flag is an operator signal: it shows to
 * the owner (enrollment undefined) and to a participant whose own seat needs
 * attention; any other participant, enrolled (a status) or not (null), reads
 * the workshop and their own seat.
 */
function flagged(resolution: Resolution, enrollment?: EnrollmentStatus | null): boolean {
  if (resolution !== 'NEEDS_ATTENTION') return false
  return enrollment === undefined || enrollment === 'NEEDS_ATTENTION' || enrollment === 'REFUND_FAILED'
}

/** The one-line state of a workshop for whoever is looking at it. */
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

/** The banner's tone; a viewer whose payment failed in a confirmed workshop is not shown good news. */
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

/**
 * An enrollment tag's tone, matching its server label: a failed charge is
 * "Payment failed" (attention) only while payments are open and "Hold
 * released" (neutral) after; a lapsed reserved seat is "Seat expired" unless
 * PayPal is reviewing its authorization, which is a hold.
 */
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
