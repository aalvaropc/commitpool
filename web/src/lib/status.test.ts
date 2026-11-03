import { describe, expect, it } from 'vitest'
import { canStartCheckout, enrollmentTone, headline, heldAny, isTerminal, refundsAny, seatHeld, shouldPollOwner, shouldPollParticipant, tone } from './status'

describe('headline', () => {
  it('names a refund still in progress in a cancelled workshop', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING', enrollment: 'REFUND_PENDING' })).toBe(
      'Workshop cancelled — refund in progress',
    )
  })

  it('names a completed refund in a cancelled workshop', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'REFUNDED' })).toBe(
      'Workshop cancelled — refund completed',
    )
  })

  it('confirms a paid seat in a confirmed workshop', () => {
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'PAID' })).toBe('Seat confirmed')
  })

  it('tells the owner how a cancellation is resolving', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING' })).toBe('Workshop cancelled — refund in progress')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED' })).toBe('Workshop cancelled — refund completed')
  })

  it('puts a workshop that needs attention first', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'NEEDS_ATTENTION' })).toBe('Needs attention')
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION' })).toBe('Needs attention')
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION', enrollment: 'NEEDS_ATTENTION' })).toBe('Needs attention')
  })

  it('has a sensible default for every other state', () => {
    expect(headline({ status: 'COLLECTING', resolution: 'OPEN' })).toBe('Collecting availability')
    expect(headline({ status: 'FUNDING', resolution: 'OPEN' })).toBe('Payments are open')
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'AUTHORIZED' })).toBe('Hold authorized')
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN' })).toBe('Workshop confirmed')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'VOIDED' })).toBe(
      'Workshop cancelled — your hold was released',
    )
  })
})

describe('headline next to the server’s own label', () => {
  it('does not call a confirmed workshop good news for a failed payment', () => {
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED' })).toBe(
      'Workshop confirmed — payment failed, you can try again',
    )
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED', paymentsOpen: false })).toBe(
      'Workshop confirmed — your hold was released, nothing was charged',
    )
    expect(tone('CONFIRMED', 'OPEN', 'CAPTURE_FAILED')).toBe('attention')
  })

  it('says a lapsed seat expired, as its label does', () => {
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false })).toBe('Seat expired')
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false })).toBe('Seat expired')
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: true })).toBe(
      'Seat reserved — authorize your hold',
    )
  })

  it('treats a failed charge labelled “Hold released” as neutral once payments closed', () => {
    expect(enrollmentTone('CAPTURE_FAILED', { paymentsOpen: false })).toBe('neutral')
    expect(enrollmentTone('CAPTURE_FAILED', { paymentsOpen: true })).toBe('attention')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'CAPTURE_FAILED' })).toBe(
      'Workshop cancelled — your hold was released',
    )
  })

  it('never says a hold was released when there was none', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'RELEASED' })).toBe('Workshop cancelled — nothing was charged')
    expect(enrollmentTone('SEAT_RESERVED', { seatHeld: false })).toBe('neutral')
  })
})

describe('a seat kept for a pending authorization', () => {
  it('says the hold is pending, not that the seat expired, in funding and confirmed workshops', () => {
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false, authPending: true })).toBe(
      'Hold pending verification',
    )
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false, authPending: true })).toBe(
      'Hold pending verification',
    )
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false })).toBe('Seat expired')
  })

  it('offers no new checkout while the authorization is pending', () => {
    expect(canStartCheckout(true, 'SEAT_RESERVED', true)).toBe(false)
    expect(canStartCheckout(true, 'SEAT_RESERVED', false)).toBe(true)
    expect(canStartCheckout(true, 'SEAT_RESERVED')).toBe(true)
  })

  it('colours the tag as a hold', () => {
    expect(enrollmentTone('SEAT_RESERVED', { paymentsOpen: true, seatHeld: false, authPending: true })).toBe('hold')
    expect(enrollmentTone('SEAT_RESERVED', { paymentsOpen: true, seatHeld: false })).toBe('neutral')
  })
})

describe('headline for a participant while the workshop is flagged for the organizer', () => {
  it('describes the participant’s own seat, not the operator flag', () => {
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION', enrollment: 'PAID' })).toBe('Seat confirmed')
    for (const [enrollment, expected] of [
      ['AUTHORIZED', 'Hold authorized'],
      ['PAID', 'Seat paid'],
      ['SEAT_RESERVED', 'Seat reserved — authorize your hold'],
    ] as const) {
      expect(headline({ status: 'FUNDING', resolution: 'NEEDS_ATTENTION', enrollment })).toBe(expected)
    }
  })

  it('shows the flag to the owner and to the seat that needs attention, not to someone who never enrolled', () => {
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION' })).toBe('Needs attention')
    expect(headline({ status: 'CANCELLED', resolution: 'NEEDS_ATTENTION', enrollment: 'REFUND_FAILED' })).toBe('Needs attention')
    expect(tone('CANCELLED', 'NEEDS_ATTENTION', 'REFUND_FAILED')).toBe('attention')
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION', enrollment: null })).toBe('Workshop confirmed')
    expect(headline({ status: 'FUNDING', resolution: 'NEEDS_ATTENTION', enrollment: null })).toBe('Payments are open')
    expect(tone('CONFIRMED', 'NEEDS_ATTENTION', null)).toBe('paid')
  })

  it('colours the banner by the participant’s own seat, and the owner’s by the flag', () => {
    expect(tone('CONFIRMED', 'NEEDS_ATTENTION', 'PAID')).toBe('paid')
    expect(tone('FUNDING', 'NEEDS_ATTENTION', 'AUTHORIZED')).toBe('hold')
    expect(tone('CONFIRMED', 'NEEDS_ATTENTION', 'NEEDS_ATTENTION')).toBe('attention')
    expect(tone('CONFIRMED', 'NEEDS_ATTENTION')).toBe('attention')
  })
})

describe('the owner’s cancelled headline', () => {
  it('says holds are released when no refund happened', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING', refunds: false })).toBe('Workshop cancelled — releasing holds')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: false })).toBe('Workshop cancelled — holds released')
  })

  it('says refund when any refund happened', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING', refunds: true })).toBe('Workshop cancelled — refund in progress')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: true })).toBe('Workshop cancelled — refund completed')
  })

  it('says nothing was charged when nobody ever held a seat', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING', refunds: false, held: false })).toBe('Workshop cancelled — nothing was charged')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: false, held: false })).toBe('Workshop cancelled — nothing was charged')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: false, held: true })).toBe('Workshop cancelled — holds released')
  })

  it('derives refunds and holds from the enrollments', () => {
    expect(refundsAny([{ status: 'VOIDED' }, { status: 'PAID' }])).toBe(true)
    expect(refundsAny([{ status: 'VOIDED' }, { status: 'CAPTURE_FAILED' }])).toBe(false)
    expect(heldAny([])).toBe(false)
    expect(heldAny([{ status: 'SEAT_RESERVED' }, { status: 'RELEASED' }])).toBe(false)
    expect(heldAny([{ status: 'RELEASED' }, { status: 'VOIDED' }])).toBe(true)
  })

  it('leaves a participant’s own cancelled copy alone', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'VOIDED', refunds: false })).toBe(
      'Workshop cancelled — your hold was released',
    )
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'RELEASED' })).toBe('Workshop cancelled — nothing was charged')
  })
})

describe('a failed charge once payments closed', () => {
  it('invites another try only while payments are open', () => {
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED' })).toBe('Payment failed — you can try again')
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED', paymentsOpen: true })).toBe(
      'Payment failed — you can try again',
    )
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED', paymentsOpen: false })).toBe(
      'Hold released — nothing was charged',
    )
  })
})

describe('seatHeld', () => {
  const now = Date.parse('2026-10-27T20:00:00Z')
  const seat = (status: 'SEAT_RESERVED' | 'AUTHORIZED', at: string) => ({ status, seat_expires_at: at })

  it('holds a reserved seat until it expires while payments are open', () => {
    expect(seatHeld(seat('SEAT_RESERVED', '2026-10-27T20:10:00Z'), true, now)).toBe(true)
    expect(seatHeld(seat('SEAT_RESERVED', '2026-10-27T19:59:00Z'), true, now)).toBe(false)
    expect(seatHeld(seat('SEAT_RESERVED', '2026-10-27T20:10:00Z'), false, now)).toBe(false)
    expect(seatHeld(seat('AUTHORIZED', '2026-10-27T20:10:00Z'), true, now)).toBe(false)
    expect(seatHeld(null, true, now)).toBe(false)
  })
})

describe('canStartCheckout', () => {
  it('offers a checkout without an enrollment, for a reserved seat and after the retryable outcomes', () => {
    expect(canStartCheckout(true, null)).toBe(true)
    expect(canStartCheckout(true, 'SEAT_RESERVED')).toBe(true)
    expect(canStartCheckout(true, 'RELEASED')).toBe(true)
    expect(canStartCheckout(true, 'CAPTURE_FAILED')).toBe(true)
  })

  it('never offers one from money that is placed, moving or needs a person', () => {
    for (const s of ['AUTHORIZED', 'CAPTURING', 'PAID', 'NEEDS_ATTENTION', 'REFUND_FAILED', 'REFUNDED', 'VOIDED'] as const) {
      expect(canStartCheckout(true, s)).toBe(false)
    }
  })

  it('never offers one once payments closed', () => {
    expect(canStartCheckout(false, null)).toBe(false)
    expect(canStartCheckout(false, 'RELEASED')).toBe(false)
  })
})

describe('tone', () => {
  it('follows the workshop state', () => {
    expect(tone('COLLECTING', 'OPEN')).toBe('neutral')
    expect(tone('FUNDING', 'OPEN')).toBe('hold')
    expect(tone('CONFIRMED', 'OPEN')).toBe('paid')
    expect(tone('CANCELLED', 'RESOLVING')).toBe('cancel')
    expect(tone('FUNDING', 'NEEDS_ATTENTION')).toBe('attention')
  })
})

describe('isTerminal', () => {
  it('matches the server’s terminal enrollment states', () => {
    for (const s of ['VOIDED', 'REFUNDED', 'REFUND_FAILED', 'NEEDS_ATTENTION', 'RELEASED', 'CAPTURE_FAILED'] as const) {
      expect(isTerminal(s)).toBe(true)
    }
    for (const s of ['SEAT_RESERVED', 'AUTHORIZED', 'CAPTURING', 'PAID', 'REFUND_PENDING'] as const) {
      expect(isTerminal(s)).toBe(false)
    }
  })
})

describe('shouldPollParticipant', () => {
  const funding = { status: 'FUNDING', resolution: 'OPEN', paymentsOpen: true } as const

  it('polls while payments are open and the own status can still change', () => {
    expect(shouldPollParticipant({ ...funding, enrollment: null })).toBe(true)
    expect(shouldPollParticipant({ ...funding, enrollment: 'AUTHORIZED' })).toBe(true)
    expect(shouldPollParticipant({ ...funding, status: 'CONFIRMED', enrollment: 'SEAT_RESERVED' })).toBe(true)
  })

  it('stops on a terminal own status', () => {
    expect(shouldPollParticipant({ ...funding, enrollment: 'RELEASED' })).toBe(false)
  })

  it('stops before payments open and after a confirmed window closes', () => {
    expect(shouldPollParticipant({ status: 'COLLECTING', resolution: 'OPEN', paymentsOpen: false, enrollment: null })).toBe(false)
    expect(shouldPollParticipant({ status: 'CONFIRMED', resolution: 'OPEN', paymentsOpen: false, enrollment: 'PAID' })).toBe(false)
  })

  it('keeps polling while money is moving after the window', () => {
    expect(shouldPollParticipant({ status: 'CANCELLED', resolution: 'RESOLVING', paymentsOpen: false, enrollment: 'PAID' })).toBe(true)
    expect(shouldPollParticipant({ status: 'CONFIRMED', resolution: 'OPEN', paymentsOpen: false, enrollment: 'CAPTURING' })).toBe(true)
    expect(shouldPollParticipant({ status: 'CANCELLED', resolution: 'SETTLED', paymentsOpen: false, enrollment: 'REFUNDED' })).toBe(false)
  })
})

describe('shouldPollOwner', () => {
  it('polls while funding or resolving', () => {
    expect(shouldPollOwner({ status: 'FUNDING', resolution: 'OPEN', paymentsOpen: true, enrollments: [] })).toBe(true)
    expect(shouldPollOwner({ status: 'CANCELLED', resolution: 'RESOLVING', paymentsOpen: false, enrollments: [] })).toBe(true)
  })

  it('polls a confirmed workshop while seats are still sold or captures are in flight', () => {
    expect(shouldPollOwner({ status: 'CONFIRMED', resolution: 'OPEN', paymentsOpen: true, enrollments: [] })).toBe(true)
    expect(shouldPollOwner({ status: 'CONFIRMED', resolution: 'OPEN', paymentsOpen: false, enrollments: ['CAPTURING'] })).toBe(true)
  })

  it('stops when nothing can change on its own', () => {
    expect(shouldPollOwner({ status: 'COLLECTING', resolution: 'OPEN', paymentsOpen: false, enrollments: [] })).toBe(false)
    expect(shouldPollOwner({ status: 'CONFIRMED', resolution: 'OPEN', paymentsOpen: false, enrollments: ['PAID'] })).toBe(false)
    expect(shouldPollOwner({ status: 'CANCELLED', resolution: 'SETTLED', paymentsOpen: false, enrollments: ['VOIDED'] })).toBe(false)
  })
})
