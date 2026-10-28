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
    title: 'Hold authorized',
    body: 'Your hold is authorized. You will only be charged if the group reaches the minimum.',
    tone: 'hold',
  },
  pending: {
    title: 'Checking your payment',
    body: 'PayPal has not confirmed your hold yet. Your page keeps checking and updates on its own.',
    tone: 'hold',
  },
  mismatch: {
    title: 'This checkout does not match',
    body: 'This PayPal checkout is not the one for your current seat, so nothing was authorized. Start the checkout again from your page.',
    tone: 'attention',
  },
  seat_expired: {
    title: 'Seat reservation expired',
    body: 'Your seat reservation ran out before PayPal confirmed it and no seat was free to renew it. Nothing was charged.',
    tone: 'cancel',
  },
  closed: {
    title: 'Payments have closed',
    body: 'The payment window for this workshop closed before your hold could count. Nothing will be charged and any hold will be released.',
    tone: 'cancel',
  },
  expired: {
    title: 'Checkout expired',
    body: 'PayPal no longer has this checkout, so nothing was charged. Start a new checkout from your page while payments are open.',
    tone: 'cancel',
  },
  declined: {
    title: 'Hold declined',
    body: 'PayPal declined the hold and nothing was charged. You can try again from your page while payments are open.',
    tone: 'attention',
  },
  error: {
    title: 'Checkout not recorded',
    body: 'We could not record your checkout. Open your page to see your current status before trying again.',
    tone: 'attention',
  },
  cancelled: {
    title: 'Checkout cancelled',
    body: 'You cancelled at PayPal, so nothing was authorized. Your seat stays reserved for a short while if you want to try again.',
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

export function returnedMessage(result: string | null): ReturnedMessage {
  return isReturnResult(result) ? MESSAGES[result] : UNKNOWN
}

/**
 * The way back to the participant page, from the token this tab stored; it
 * asks the page to check the payment when a hold may have been placed. Null
 * when this tab has no token.
 */
export function returnPath(result: string | null): string | null {
  const token = storedParticipant()
  if (!token) return null
  const path = `/p/${encodeURIComponent(token)}`
  return result === 'authorized' || result === 'pending' ? `${path}?paid=pending` : path
}
