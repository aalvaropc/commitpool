import type { Tally, WorkshopStatus } from '../api'
import { storedParticipant } from './participant'
import type { Tone } from './status'

export const RESULTS = [
  'authorized',
  'pending',
  'mismatch',
  'seat_expired',
  'closed',
  'expired',
  'declined',
  'error',
  'cancelled',
] as const

export type ReturnResult = (typeof RESULTS)[number]

export interface ReturnedMessage {
  title: string
  body: string
  tone: Tone
}

const MESSAGES: Record<ReturnResult, ReturnedMessage> = {
  authorized: {
    title: 'Spot reserved',
    body: 'Your spot is reserved and nothing is charged yet. You are charged only if enough people reserve for the workshop to happen.',
    tone: 'hold',
  },
  pending: {
    title: 'Checking your payment',
    body: 'PayPal has not confirmed your hold yet. Your page keeps checking and updates on its own.',
    tone: 'hold',
  },
  mismatch: {
    title: 'This checkout does not match',
    body: 'This PayPal checkout is not the one for your current spot, so nothing was reserved. Start again from your page.',
    tone: 'attention',
  },
  seat_expired: {
    title: 'Reservation expired',
    body: 'Your reservation ran out before PayPal confirmed it and no spot was free to renew it. Nothing was charged.',
    tone: 'cancel',
  },
  closed: {
    title: 'Reservations have closed',
    body: 'Reservations for this workshop closed before your hold could count. Nothing will be charged and any hold will be released.',
    tone: 'cancel',
  },
  expired: {
    title: 'Checkout expired',
    body: 'PayPal no longer has this checkout, so nothing was charged. Start again from your page while reservations are open.',
    tone: 'cancel',
  },
  declined: {
    title: 'Hold declined',
    body: 'PayPal declined the hold and nothing was charged. You can try again from your page while reservations are open.',
    tone: 'attention',
  },
  error: {
    title: 'Checkout not recorded',
    body: 'We could not record your checkout. Open your page to see your current status before trying again.',
    tone: 'attention',
  },
  cancelled: {
    title: 'Checkout cancelled',
    body: 'You cancelled at PayPal, so nothing was reserved. Your spot is kept for a short while if you want to try again.',
    tone: 'neutral',
  },
}

const UNKNOWN: ReturnedMessage = {
  title: 'Checkout result unknown',
  body: 'We could not read the result of your checkout. Open your page to see your current status.',
  tone: 'neutral',
}

export function isReturnResult(value: string | null): value is ReturnResult {
  return value !== null && (RESULTS as readonly string[]).includes(value)
}

export interface ReturnedClass {
  workshop: { status: WorkshopStatus }
  tally: Tally
}

function reachedByThisReservation({ workshop, tally }: ReturnedClass): boolean {
  return workshop.status === 'FUNDING' && tally.minimum > 0 && tally.paid + tally.authorized + tally.charging === tally.minimum
}

export function returnedMessage(result: string | null, state?: ReturnedClass | null): ReturnedMessage {
  if (!isReturnResult(result)) return UNKNOWN
  if (result === 'authorized' && state) {
    if (reachedByThisReservation(state)) return { ...MESSAGES.authorized, body: `That made ${state.tally.minimum}. Charging everyone now.` }
    if (state.workshop.status === 'CONFIRMED') {
      return { ...MESSAGES.authorized, body: 'Your spot is reserved. The class is on, so your hold is charged straight away. Your page shows where it stands.' }
    }
    if (state.tally.paid + state.tally.authorized + state.tally.charging > state.tally.minimum) {
      return { ...MESSAGES.authorized, body: 'Your spot is reserved. The class already has its minimum. Your page shows whether your hold has been charged.' }
    }
  }
  return MESSAGES[result]
}

export function returnPath(result: string | null): string | null {
  const token = storedParticipant()
  if (!token) return null
  const path = `/p/${encodeURIComponent(token)}`
  return result === 'authorized' || result === 'pending' ? `${path}?paid=pending` : path
}
