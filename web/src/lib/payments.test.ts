import { describe, expect, it } from 'vitest'
import type { Enrollment, EnrollmentStatus, Payment, VerifyResult } from '../api'
import { moneySummary, verifyDifferences, verifyHeadline, verifySkipped } from './payments'

const usd = (minor: number) => ({ minor, currency: 'USD', value: (minor / 100).toFixed(2) })

function seat(status: EnrollmentStatus, fees?: { fee: number; net: number }, money_status?: EnrollmentStatus): Enrollment {
  const payment: Payment = {
    attempt_id: 'a',
    attempt_no: 1,
    order_id: 'ORDER',
    authorization_id: 'AUTH',
    capture_id: status === 'PAID' ? 'CAPTURE' : null,
    refund_id: null,
    order_status: 'COMPLETED',
    authorization_status: 'CREATED',
    capture_status: status === 'PAID' ? 'COMPLETED' : null,
    capture_issue: null,
    refund_status: null,
    ...(fees ? { fee: usd(fees.fee), net: usd(fees.net) } : {}),
  }
  return { id: 'e', status, money_status, status_label: '', seat_expires_at: '', approval_cancelled_at: null, payment }
}

describe('moneySummary', () => {
  it('adds up what the paid spots collected, PayPal kept and the organizer nets', () => {
    const paid = { fee: 206, net: 4294 }
    expect(moneySummary([seat('PAID', paid), seat('PAID', paid), seat('PAID', paid), seat('AUTHORIZED')], usd(4500))).toBe(
      '$135.00 collected · $6.18 PayPal fees · $128.82 net',
    )
  })

  it('leaves the fees out while any paid spot has none recorded', () => {
    expect(moneySummary([seat('PAID', { fee: 84, net: 916 }), seat('PAID')], usd(1000))).toBe('$20.00 collected')
  })

  it('counts a flagged spot by where its money is', () => {
    expect(moneySummary([seat('NEEDS_ATTENTION', { fee: 84, net: 916 }, 'PAID')], usd(1000))).toBe('$10.00 collected · $0.84 PayPal fees · $9.16 net')
  })

  it('says nothing before anyone paid', () => {
    expect(moneySummary([seat('AUTHORIZED'), seat('REFUNDED', { fee: 84, net: 916 })], usd(1000))).toBeNull()
  })
})

function result(over: Partial<VerifyResult> = {}): VerifyResult {
  return {
    checked_at: '2026-11-10T15:42:00Z',
    summary: { records: 4, matching: 4, seats: 2, skipped: 0 },
    seats: [],
    ...over,
  }
}

describe('verifyHeadline', () => {
  it('counts the records that match and says when, in the workshop’s time zone', () => {
    expect(verifyHeadline(result(), 'America/New_York')).toBe('4 of 4 records match PayPal · checked 10:42 AM')
  })

  it('agrees in number', () => {
    expect(verifyHeadline(result({ summary: { records: 1, matching: 1, seats: 1, skipped: 0 } }), 'UTC')).toBe('1 of 1 record matches PayPal · checked 3:42 PM')
    expect(verifyHeadline(result({ summary: { records: 3, matching: 1, seats: 1, skipped: 0 } }), 'UTC')).toBe('1 of 3 records matches PayPal · checked 3:42 PM')
  })

  it('says when nothing has reached PayPal', () => {
    expect(verifyHeadline(result({ summary: { records: 0, matching: 0, seats: 0, skipped: 0 } }), 'UTC')).toBe('No payment has reached PayPal yet · checked 3:42 PM')
  })
})

describe('verifySkipped', () => {
  it('says how many spots were left for another check', () => {
    expect(verifySkipped(result())).toBeNull()
    expect(verifySkipped(result({ summary: { records: 36, matching: 36, seats: 12, skipped: 3 } }))).toBe('Checked the first 12 spots; 3 more spots not checked this time.')
    expect(verifySkipped(result({ summary: { records: 36, matching: 36, seats: 12, skipped: 1 } }))).toBe('Checked the first 12 spots; 1 more spot not checked this time.')
  })
})

describe('verifyDifferences', () => {
  it('lists each record that differs, in plain words, with PayPal’s debug id', () => {
    const r = result({
      summary: { records: 3, matching: 1, seats: 1, skipped: 0 },
      seats: [
        {
          enrollment_id: 'e1',
          seat: 'Ada',
          debug_id: 'f00d',
          checks: [
            { resource: 'order', id: 'O1', local: 'COMPLETED, 10.00 USD', paypal: 'COMPLETED, 10.00 USD', match: true },
            { resource: 'authorization', id: 'A1', local: 'CAPTURED, 10.00 USD', paypal: 'error INTERNAL_SERVER_ERROR', match: false },
            { resource: 'capture', id: 'C1', local: 'COMPLETED, 10.00 USD', paypal: 'REFUNDED, 10.00 USD', match: false },
          ],
        },
      ],
    })
    expect(verifyDifferences(r)).toEqual([
      { key: 'e1-authorization', text: 'Ada — Hold A1: CommitPool has CAPTURED, 10.00 USD; PayPal has error INTERNAL_SERVER_ERROR.', debugId: 'f00d' },
      { key: 'e1-capture', text: 'Ada — Charge C1: CommitPool has COMPLETED, 10.00 USD; PayPal has REFUNDED, 10.00 USD.', debugId: 'f00d' },
    ])
  })
})
