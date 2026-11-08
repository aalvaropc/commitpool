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

const RELEASE_STATES: ReadonlySet<EnrollmentStatus> = new Set(['VOIDING', 'VOIDED', 'CAPTURE_FAILED'])

export function releasesAny(enrollments: readonly { status: EnrollmentStatus }[]): boolean {
  return enrollments.some((e) => RELEASE_STATES.has(e.status))
}

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
  if (input.authPending) return 'Spot kept while PayPal reviews the hold'
  if (input.held) return `Spot held for you until ${input.until}`
  return 'Your reservation expired'
}

export function returnCheckSettled(input: { enrollment: EnrollmentStatus | null; polling: boolean; authPending: boolean }): boolean {
  if (input.authPending) return true
  if (input.enrollment && input.enrollment !== 'SEAT_RESERVED') return true
  return !input.polling
}

const RETRYABLE: ReadonlySet<EnrollmentStatus> = new Set(['SEAT_RESERVED', 'RELEASED', 'CAPTURE_FAILED'])

export function freeToReserveAgain(e: { status: EnrollmentStatus; needs_attention?: boolean }): boolean {
  return RETRYABLE.has(e.status) && e.status !== 'SEAT_RESERVED' && !e.needs_attention
}

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
  releases?: boolean
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
  releases = false,
}: HeadlineInput): string {
  if (flagged(resolution, enrollment)) return 'Needs a check'
  if ((status === 'FUNDING' || status === 'CONFIRMED') && enrollment === 'SEAT_RESERVED' && authPending) return 'PayPal is reviewing your hold'
  if ((status === 'FUNDING' || status === 'CONFIRMED') && enrollment === 'SEAT_RESERVED' && !seatHeld) return 'Your reservation expired'
  switch (status) {
    case 'COLLECTING':
      return 'Finding the date'
    case 'FUNDING':
      return fundingHeadline(enrollment, paymentsOpen)
    case 'CONFIRMED':
      return confirmedHeadline(enrollment, paymentsOpen)
    case 'CANCELLED':
      return cancelledHeadline(resolution, enrollment, refunds, held, releases)
  }
}

function fundingHeadline(e: EnrollmentStatus | null | undefined, paymentsOpen: boolean): string {
  switch (e) {
    case 'SEAT_RESERVED':
      return 'Finish reserving at PayPal'
    case 'AUTHORIZED':
      return paymentsOpen ? 'Spot reserved — not charged yet' : 'Reservations closed — not charged yet'
    case 'CAPTURING':
      return 'Charging your spot…'
    case 'CAPTURE_PENDING':
      return 'PayPal is verifying your payment'
    case 'PAID':
      return 'Paid — you’re in'
    case 'CAPTURE_FAILED':
      return paymentsOpen ? 'Payment didn’t go through — try again' : 'Not charged — PayPal releases your hold'
    case 'RELEASED':
      return 'Your reservation expired'
    case 'NEEDS_ATTENTION':
      return 'Needs a check'
  }
  return paymentsOpen ? 'Reservations open' : 'Reservations closed'
}

function confirmedHeadline(e: EnrollmentStatus | null | undefined, paymentsOpen: boolean): string {
  switch (e) {
    case 'PAID':
      return 'You’re in — it’s on'
    case 'AUTHORIZED':
      return paymentsOpen ? 'It’s on — charging your spot' : 'It’s on — your hold is being released, you weren’t charged'
    case 'CAPTURING':
    case 'CAPTURE_PENDING':
      return 'It’s on — charging your spot'
    case 'VOIDING':
      return 'It’s on — releasing your hold'
    case 'VOIDED':
      return 'It’s on — your hold was released'
    case 'REFUNDING':
    case 'REFUND_PENDING':
      return 'It’s on — your refund is on its way'
    case 'REFUNDED':
      return 'It’s on — you were refunded'
    case 'CAPTURE_FAILED':
      return paymentsOpen
        ? 'It’s on — your payment didn’t go through, try again'
        : 'It’s on — you weren’t charged'
    case 'RELEASED':
      return 'Your reservation expired'
    case 'NEEDS_ATTENTION':
    case 'REFUND_FAILED':
      return 'Needs a check'
  }
  return 'It’s on'
}

function settledCancellation(refunds: boolean, releases: boolean): string {
  if (refunds && releases) return 'Cancelled — payments refunded, holds released'
  return refunds ? 'Cancelled — every payment was refunded' : 'Cancelled — every hold was released'
}

function cancelledHeadline(r: Resolution, e: EnrollmentStatus | null | undefined, refunds: boolean, held: boolean, releases: boolean): string {
  if (e === undefined) {
    if (!refunds && !held && (r === 'RESOLVING' || r === 'SETTLED')) return 'Cancelled — nobody was charged'
    if (r === 'RESOLVING') return refunds ? 'Cancelled — refunds on their way' : 'Cancelled — releasing holds'
    if (r === 'SETTLED') return settledCancellation(refunds, releases)
    return 'Cancelled'
  }
  switch (e) {
    case 'PAID':
    case 'REFUNDING':
    case 'REFUND_PENDING':
      return 'Cancelled — your refund is on its way'
    case 'REFUNDED':
      return 'Cancelled — you were refunded'
    case 'AUTHORIZED':
    case 'VOIDING':
      return 'Cancelled — releasing your hold'
    case 'VOIDED':
      return 'Cancelled — your hold was released'
    case 'CAPTURE_FAILED':
    case 'SEAT_RESERVED':
    case 'RELEASED':
      return 'Cancelled — you weren’t charged'
    case 'REFUND_FAILED':
    case 'NEEDS_ATTENTION':
      return 'Cancelled — your refund needs a check'
  }
  return 'Cancelled'
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
