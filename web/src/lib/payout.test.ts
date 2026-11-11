import { describe, expect, it } from 'vitest'
import type { Payout } from '../api'
import { canStartClass, classStarted, payoutBreakdown, payoutCheckable, payoutIds, payoutSummary } from './payout'

const usd = (minor: number) => ({ minor, currency: 'USD', value: (minor / 100).toFixed(2) })
const zone = 'America/New_York'

function payout(over: Partial<Payout> = {}): Payout {
  return {
    state: 'scheduled',
    locked: false,
    receiver: 'iris@example.com',
    scheduled_at: '2026-11-14T15:00:00Z',
    estimated: true,
    amount: usd(12882),
    gross: usd(13500),
    fees: usd(618),
    seats: 3,
    ...over,
  }
}

describe('payoutSummary', () => {
  it('says when and how much before the class starts', () => {
    expect(payoutSummary(payout(), zone)).toEqual({
      tone: 'hold',
      title: 'Paid out when the class starts, Sat, Nov 14 · 10 AM',
      detail: 'CommitPool sends $128.82 to iris@example.com through PayPal. The amount is final once the class starts.',
    })
  })

  it('does not invent an amount while a fee is unknown', () => {
    expect(payoutSummary(payout({ amount: undefined, fees: undefined }), zone).detail).toBe(
      'CommitPool sends what people paid, less PayPal’s fees, to iris@example.com through PayPal.',
    )
  })

  it('names the paid out amount, the receiver and the PayPal batch', () => {
    const s = payoutSummary(payout({ state: 'paid', estimated: false, payout_batch_id: '5UXD2E8A7EBQJ' }), zone)
    expect(s.tone).toBe('paid')
    expect(s.title).toBe('Paid out $128.82 to iris@example.com · PayPal batch 5UXD2E8A7EBQJ')
  })

  it('covers every other state', () => {
    const titles = (['no_email', 'off', 'waiting', 'sending', 'unclaimed', 'failed', 'unknown'] as const).map((state) => payoutSummary(payout({ state, waiting: 'payments_in_flight', issue: 'INSUFFICIENT_FUNDS' }), zone))
    expect(titles.map((t) => t.title)).toEqual([
      'No payout email',
      'Payouts are off on this server',
      'Waiting for payments to settle',
      'Sending $128.82 to iris@example.com',
      'Waiting for iris@example.com to claim $128.82',
      'The payout didn’t go through',
      'The payout needs a check',
    ])
    expect(titles[2].detail).toBe('A payment is still moving at PayPal. Your payout to iris@example.com goes out on its own once it does.')
    expect(titles[5].detail).toBe('PayPal answered INSUFFICIENT_FUNDS. Quote the debug ID below to PayPal support.')
    expect(titles[5].tone).toBe('attention')
  })
})

describe('payoutSummary never promises a payout that waits for a person', () => {
  it('tells apart a frozen payout PayPal has not confirmed and one parked for an operator', () => {
    expect(payoutSummary(payout({ state: 'created', locked: true }), zone)).toEqual({
      tone: 'hold',
      title: 'Getting $128.82 ready for iris@example.com',
      detail: 'The amount is final. PayPal has not confirmed the payout yet.',
    })
    const parked = payoutSummary(payout({ state: 'parked', locked: true }), zone)
    expect(parked.tone).toBe('attention')
    expect(parked.detail).not.toMatch(/sent to|on its own/)
  })

  it('asks for the check a flagged payment needs, and says when there is nothing to pay', () => {
    for (const waiting of ['payment_flagged', 'needs_attention']) {
      const s = payoutSummary(payout({ state: 'waiting', waiting }), zone)
      expect(s.title).toBe('Your payout waits for a check')
      expect(s.detail).not.toMatch(/on its own/)
    }
    expect(payoutSummary(payout({ state: 'waiting', waiting: 'nothing_to_pay' }), zone).title).toBe('Nothing to pay out')
    expect(payoutSummary(payout({ state: 'waiting', waiting: 'fees_missing' }), zone).detail).toBe(
      'PayPal has not reported every fee yet. Your payout to iris@example.com goes out on its own once it does.',
    )
  })
})

describe('payoutBreakdown', () => {
  it('adds up what was paid, what PayPal kept and what the organizer gets', () => {
    expect(payoutBreakdown(payout())).toBe('$135.00 paid by 3 people · $6.18 PayPal fees · $128.82 to you')
    expect(payoutBreakdown(payout({ amount: undefined, fees: undefined, seats: 1 }))).toBe('$135.00 paid by 1 person')
    expect(payoutBreakdown(payout({ seats: 0 }))).toBeNull()
  })
})

describe('payoutIds', () => {
  it('lists only the ids PayPal gave', () => {
    expect(payoutIds(payout({ sender_batch_id: 'cp-w', payout_batch_id: 'B', transaction_id: 'T' }))).toEqual([
      ['Sender batch', 'cp-w'],
      ['PayPal batch', 'B'],
      ['Transaction', 'T'],
    ])
  })
})

describe('payoutCheckable', () => {
  it('offers a check only while PayPal may still change its answer', () => {
    expect(payoutCheckable(payout({ state: 'sending', payout_batch_id: 'B' }))).toBe(true)
    expect(payoutCheckable(payout({ state: 'unclaimed', payout_batch_id: 'B' }))).toBe(true)
    expect(payoutCheckable(payout({ state: 'paid', payout_batch_id: 'B' }))).toBe(false)
    expect(payoutCheckable(payout({ state: 'unknown' }))).toBe(false)
    expect(payoutCheckable(null)).toBe(false)
  })
})

describe('canStartClass', () => {
  const v = (over: Partial<Parameters<typeof canStartClass>[0]> = {}) => ({
    demo: { demo_mode: true },
    workshop: { status: 'CONFIRMED' },
    proposal: { payments_open: false },
    payout: payout(),
    ...over,
  })
  it('appears in a demo once reservations closed and before any payout', () => {
    expect(canStartClass(v())).toBe(true)
    expect(canStartClass(v({ payout: payout({ state: 'waiting' }) }))).toBe(true)
    expect(canStartClass(v({ demo: { demo_mode: false } }))).toBe(false)
    expect(canStartClass(v({ proposal: { payments_open: true } }))).toBe(false)
    expect(canStartClass(v({ workshop: { status: 'CANCELLED' } }))).toBe(false)
    expect(canStartClass(v({ payout: payout({ state: 'paid' }) }))).toBe(false)
    expect(canStartClass(v({ payout: payout({ state: 'no_email' }) }))).toBe(false)
  })
})

describe('classStarted', () => {
  const v = (over: Partial<Parameters<typeof classStarted>[0]> = {}, p: Partial<Payout> = {}) => ({
    workshop: { status: 'CONFIRMED', host_name: 'Iris Calder' },
    payout: payout({ state: 'paid', estimated: false, amount: usd(2748), payout_batch_id: 'BATCH42', batch_status: 'SUCCESS', ...p }),
    ...over,
  }) as Parameters<typeof classStarted>[0]

  it('names the amount, the PayPal batch and its status once the payout succeeded', () => {
    expect(classStarted(v())).toEqual({ headline: 'Class started · Iris paid $27.48', sub: 'PayPal batch BATCH42 · SUCCESS' })
    expect(classStarted(v({}, { batch_status: undefined, item_status: 'SUCCESS' }))?.sub).toBe('PayPal batch BATCH42 · SUCCESS')
    expect(classStarted(v({}, { payout_batch_id: undefined }))?.sub).toBeNull()
  })

  it('leaves the usual headline for every other payout state', () => {
    for (const state of ['scheduled', 'waiting', 'created', 'sending', 'unclaimed', 'failed', 'parked', 'unknown', 'no_email', 'off'] as const) {
      expect(classStarted(v({}, { state }))).toBeNull()
    }
    expect(classStarted(v({ payout: null }))).toBeNull()
    expect(classStarted(v({}, { amount: undefined }))).toBeNull()
    expect(classStarted(v({ workshop: { status: 'CANCELLED', host_name: 'Iris Calder' } } as Partial<Parameters<typeof classStarted>[0]>))).toBeNull()
    expect(classStarted(v({ workshop: { status: 'CONFIRMED', host_name: '' } } as Partial<Parameters<typeof classStarted>[0]>))).toBeNull()
  })
})
