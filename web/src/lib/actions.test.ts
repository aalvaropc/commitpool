import { describe, expect, it } from 'vitest'
import type { EnrollmentStatus } from '../api'
import { canCancel, cancelEffect, operatorActions, withdrawKind } from './actions'

const now = Date.parse('2026-11-10T12:00:00Z')
const en = (status: EnrollmentStatus, extra: { needs_attention?: boolean; auth?: string | null } = {}) => ({
  status,
  needs_attention: extra.needs_attention ?? false,
  payment: extra.auth === undefined ? null : { authorization_status: extra.auth },
})

describe('canCancel', () => {
  it('allows a workshop that is collecting, funding or confirmed before it starts', () => {
    expect(canCancel('COLLECTING', null, now)).toBe(true)
    expect(canCancel('FUNDING', '2026-11-12T10:00:00Z', now)).toBe(true)
    expect(canCancel('CONFIRMED', '2026-11-12T10:00:00Z', now)).toBe(true)
  })
  it('refuses once cancelled or once the date has started', () => {
    expect(canCancel('CANCELLED', null, now)).toBe(false)
    expect(canCancel('CONFIRMED', '2026-11-10T12:00:00Z', now)).toBe(false)
    expect(canCancel('CONFIRMED', '2026-11-09T10:00:00Z', now)).toBe(false)
  })
})

describe('cancelEffect', () => {
  it('says no money moves when nothing is held or charged', () => {
    expect(cancelEffect([])).toBe('Nothing is held or charged yet, so no money moves.')
    expect(cancelEffect([en('SEAT_RESERVED'), en('RELEASED'), en('VOIDED')])).toBe('Nothing is held or charged yet, so no money moves.')
  })
  it('counts the holds released and the payments refunded', () => {
    expect(cancelEffect([en('AUTHORIZED'), en('AUTHORIZED'), en('PAID')])).toBe('PayPal releases 2 holds and refunds 1 payment.')
    expect(cancelEffect([en('PAID'), en('PAID')])).toBe('PayPal refunds 2 payments.')
    expect(cancelEffect([en('AUTHORIZED')])).toBe('PayPal releases 1 hold.')
  })
  it('mentions charges still in flight, refunded once they complete', () => {
    expect(cancelEffect([en('AUTHORIZED'), en('CAPTURING'), en('CAPTURE_PENDING')])).toBe(
      'PayPal releases 1 hold. 2 charges in progress are refunded once they complete.',
    )
  })
})

describe('withdrawKind', () => {
  it('releases a held seat and voids an authorized hold', () => {
    expect(withdrawKind(en('SEAT_RESERVED'), 'FUNDING')).toBe('release')
    expect(withdrawKind(en('AUTHORIZED', { auth: 'CREATED' }), 'FUNDING')).toBe('void')
    expect(withdrawKind(en('AUTHORIZED'), 'CONFIRMED')).toBe('void')
  })
  it('offers nothing while PayPal reviews the hold, once charged, when flagged or after a cancel', () => {
    expect(withdrawKind(en('SEAT_RESERVED', { auth: 'PENDING' }), 'FUNDING')).toBeNull()
    expect(withdrawKind(en('CAPTURING'), 'FUNDING')).toBeNull()
    expect(withdrawKind(en('PAID'), 'CONFIRMED')).toBeNull()
    expect(withdrawKind(en('AUTHORIZED', { needs_attention: true }), 'FUNDING')).toBeNull()
    expect(withdrawKind(en('AUTHORIZED'), 'CANCELLED')).toBeNull()
    expect(withdrawKind(null, 'FUNDING')).toBeNull()
  })
})

describe('operatorActions', () => {
  it('offers nothing for a seat that needs no check', () => {
    expect(operatorActions(en('PAID'))).toEqual([])
    expect(operatorActions(en('VOIDED'))).toEqual([])
  })
  it('offers a PayPal check and clearing for a flagged seat', () => {
    expect(operatorActions(en('VOIDING', { needs_attention: true }))).toEqual(['reconcile', 'clear'])
  })
  it('offers a refund retry for a failed refund, flagged or not', () => {
    expect(operatorActions(en('REFUND_FAILED'))).toEqual(['retry_refund'])
    expect(operatorActions(en('REFUND_FAILED', { needs_attention: true }))).toEqual(['reconcile', 'retry_refund', 'clear'])
  })
})
