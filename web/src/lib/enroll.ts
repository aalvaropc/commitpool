import { ApiError } from '../api'

const MESSAGES: Record<string, string> = {
  proposal_not_open: 'Payments are not open for this workshop any more.',
  consent_version_mismatch: 'The terms have changed. Read them again below before authorizing.',
  capacity_full: 'Every seat is taken.',
  seat_expired: 'Your seat reservation expired and no seat was free to renew it. Nothing was charged.',
  checkout_in_progress: 'Your checkout is still being prepared in another window. Try again in a moment.',
  workshop_closed: 'This workshop no longer takes new participants or changes.',
  paypal_unavailable: 'PayPal is not answering — try again in a moment',
}

export const REFRESH_AFTER = new Set(['proposal_not_open', 'consent_version_mismatch', 'seat_expired', 'workshop_closed', 'capacity_full'])

export function enrollErrorMessage(e: unknown): string {
  if (e instanceof ApiError) return MESSAGES[e.code] ?? e.message
  return 'Something went wrong. Please try again.'
}

export function isSafeRedirect(url: string): boolean {
  try {
    const u = new URL(url)
    return u.protocol === 'https:' || u.protocol === 'http:'
  } catch {
    return false
  }
}
