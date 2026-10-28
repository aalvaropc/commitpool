import { describe, expect, it } from 'vitest'
import { ApiError } from '../api'
import { enrollErrorMessage, isSafeRedirect } from './enroll'

describe('enrollErrorMessage', () => {
  it('names each refusal the enrollment can answer', () => {
    expect(enrollErrorMessage(new ApiError(502, 'paypal_unavailable', 'x'))).toBe(
      'PayPal is not answering — try again in a moment',
    )
    expect(enrollErrorMessage(new ApiError(409, 'capacity_full', 'x'))).toBe('Every seat is taken.')
    for (const code of ['proposal_not_open', 'consent_version_mismatch', 'seat_expired', 'checkout_in_progress', 'workshop_closed']) {
      expect(enrollErrorMessage(new ApiError(409, code, 'server text'))).not.toBe('server text')
    }
  })

  it('falls back to the server’s sentence', () => {
    expect(enrollErrorMessage(new ApiError(400, 'invalid_json', 'The request body is not valid.'))).toBe(
      'The request body is not valid.',
    )
  })
})

describe('isSafeRedirect', () => {
  it('only follows web addresses', () => {
    expect(isSafeRedirect('https://www.sandbox.paypal.com/checkoutnow?token=1')).toBe(true)
    expect(isSafeRedirect('http://127.0.0.1:43210/sandbox/approve?order=1')).toBe(true)
    expect(isSafeRedirect('javascript:alert(1)')).toBe(false)
    expect(isSafeRedirect('')).toBe(false)
  })
})
