import { describe, expect, it } from 'vitest'
import {
  canStartCheckout,
  enrollmentTone,
  headline,
  heldAny,
  isTerminal,
  refundsAny,
  releasesAny,
  reservedSeatNote,
  returnCheckSettled,
  seatHeld,
  shouldPollOwner,
  shouldPollParticipant,
  tone,
} from './status'

describe('headline', () => {
  it('names a refund still in progress in a cancelled workshop', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING', enrollment: 'REFUND_PENDING' })).toBe(
      'Cancelled — your refund is on its way',
    )
  })

  it('names a completed refund in a cancelled workshop', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'REFUNDED' })).toBe(
      'Cancelled — you were refunded',
    )
  })

  it('confirms a paid seat in a confirmed workshop', () => {
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'PAID' })).toBe('You’re in — it’s on')
  })

  it('tells the owner how a cancellation is resolving', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING' })).toBe('Cancelled — refunds on their way')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED' })).toBe('Cancelled — every payment was refunded')
  })

  it('puts a workshop that needs attention first', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'NEEDS_ATTENTION' })).toBe('Needs a check')
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION' })).toBe('Needs a check')
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION', enrollment: 'NEEDS_ATTENTION' })).toBe('Needs a check')
  })

  it('has a sensible default for every other state', () => {
    expect(headline({ status: 'COLLECTING', resolution: 'OPEN' })).toBe('Finding the date')
    expect(headline({ status: 'FUNDING', resolution: 'OPEN' })).toBe('Reservations open')
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'AUTHORIZED' })).toBe('Spot reserved — not charged yet')
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN' })).toBe('It’s on')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'VOIDED' })).toBe(
      'Cancelled — your hold was released',
    )
  })
})

describe('headline next to the server’s own label', () => {
  it('does not call a confirmed workshop good news for a failed payment', () => {
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED' })).toBe(
      'It’s on — your payment didn’t go through, try again',
    )
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED', paymentsOpen: false })).toBe(
      'It’s on — you weren’t charged',
    )
    expect(tone('CONFIRMED', 'OPEN', 'CAPTURE_FAILED')).toBe('attention')
  })

  it('says a lapsed seat expired, as its label does', () => {
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false })).toBe('Your reservation expired')
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false })).toBe('Your reservation expired')
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: true })).toBe(
      'Finish reserving at PayPal',
    )
  })

  it('treats a failed charge as neutral once reservations closed', () => {
    expect(enrollmentTone('CAPTURE_FAILED', { paymentsOpen: false })).toBe('neutral')
    expect(enrollmentTone('CAPTURE_FAILED', { paymentsOpen: true })).toBe('attention')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'CAPTURE_FAILED' })).toBe(
      'Cancelled — you weren’t charged',
    )
  })

  it('never says a hold was released when there was none', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'RELEASED' })).toBe('Cancelled — you weren’t charged')
    expect(enrollmentTone('SEAT_RESERVED', { seatHeld: false })).toBe('neutral')
  })
})

describe('a seat kept for a pending authorization', () => {
  it('says the hold is pending, not that the seat expired, in funding and confirmed workshops', () => {
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false, authPending: true })).toBe(
      'PayPal is reviewing your hold',
    )
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false, authPending: true })).toBe(
      'PayPal is reviewing your hold',
    )
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'SEAT_RESERVED', seatHeld: false })).toBe('Your reservation expired')
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
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION', enrollment: 'PAID' })).toBe('You’re in — it’s on')
    for (const [enrollment, expected] of [
      ['AUTHORIZED', 'Spot reserved — not charged yet'],
      ['PAID', 'Paid — you’re in'],
      ['SEAT_RESERVED', 'Finish reserving at PayPal'],
    ] as const) {
      expect(headline({ status: 'FUNDING', resolution: 'NEEDS_ATTENTION', enrollment })).toBe(expected)
    }
  })

  it('shows the flag to the owner and to the seat that needs attention, not to someone who never enrolled', () => {
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION' })).toBe('Needs a check')
    expect(headline({ status: 'CANCELLED', resolution: 'NEEDS_ATTENTION', enrollment: 'REFUND_FAILED' })).toBe('Needs a check')
    expect(tone('CANCELLED', 'NEEDS_ATTENTION', 'REFUND_FAILED')).toBe('attention')
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION', enrollment: null })).toBe('It’s on')
    expect(headline({ status: 'FUNDING', resolution: 'NEEDS_ATTENTION', enrollment: null })).toBe('Reservations open')
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
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING', refunds: false })).toBe('Cancelled — releasing holds')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: false })).toBe('Cancelled — every hold was released')
  })

  it('says refund when any refund happened', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING', refunds: true })).toBe('Cancelled — refunds on their way')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: true })).toBe('Cancelled — every payment was refunded')
  })

  it('says nothing was charged when nobody ever held a seat', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'RESOLVING', refunds: false, held: false })).toBe('Cancelled — nobody was charged')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: false, held: false })).toBe('Cancelled — nobody was charged')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: false, held: true })).toBe('Cancelled — every hold was released')
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
      'Cancelled — your hold was released',
    )
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', enrollment: 'RELEASED' })).toBe('Cancelled — you weren’t charged')
  })
})

describe('a failed charge once payments closed', () => {
  it('invites another try only while payments are open', () => {
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED' })).toBe('Payment didn’t go through — try again')
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED', paymentsOpen: true })).toBe(
      'Payment didn’t go through — try again',
    )
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED', paymentsOpen: false })).toBe(
      'Not charged — PayPal releases your hold',
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

describe('reservedSeatNote', () => {
  it('says the seat is kept while PayPal reviews the authorization, even past its expiry', () => {
    expect(reservedSeatNote({ authPending: true, held: false, until: '10:00' })).toBe('Spot kept while PayPal reviews the hold')
    expect(reservedSeatNote({ authPending: true, held: true, until: '10:00' })).toBe('Spot kept while PayPal reviews the hold')
  })

  it('names the time a live seat is held until', () => {
    expect(reservedSeatNote({ authPending: false, held: true, until: '10:00' })).toBe('Spot held for you until 10:00')
  })

  it('calls a lapsed seat expired', () => {
    expect(reservedSeatNote({ authPending: false, held: false, until: '10:00' })).toBe('Your reservation expired')
  })
})

describe('returnCheckSettled', () => {
  it('ends the check once PayPal holds the authorization for review', () => {
    expect(returnCheckSettled({ enrollment: 'SEAT_RESERVED', polling: true, authPending: true })).toBe(true)
  })

  it('keeps checking a reserved seat while the page still polls', () => {
    expect(returnCheckSettled({ enrollment: 'SEAT_RESERVED', polling: true, authPending: false })).toBe(false)
    expect(returnCheckSettled({ enrollment: null, polling: true, authPending: false })).toBe(false)
  })

  it('ends the check once the payment moved past the checkout or nothing more will change', () => {
    expect(returnCheckSettled({ enrollment: 'AUTHORIZED', polling: true, authPending: false })).toBe(true)
    expect(returnCheckSettled({ enrollment: 'SEAT_RESERVED', polling: false, authPending: false })).toBe(true)
  })
})

describe('headlines that never get ahead of the money', () => {
  it('names refunds and released holds together once a cancellation settles', () => {
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: true, releases: true })).toBe('Cancelled — payments refunded, holds released')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: true, releases: false })).toBe('Cancelled — every payment was refunded')
    expect(headline({ status: 'CANCELLED', resolution: 'SETTLED', refunds: false, releases: true })).toBe('Cancelled — every hold was released')
  })

  it('says a failed charge’s hold is still being released', () => {
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'CAPTURE_FAILED', paymentsOpen: false })).toBe(
      'Not charged — PayPal releases your hold',
    )
  })

  it('does not call a hold reserved once reservations closed', () => {
    expect(headline({ status: 'FUNDING', resolution: 'OPEN', enrollment: 'AUTHORIZED', paymentsOpen: false })).toBe(
      'Reservations closed — not charged yet',
    )
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'AUTHORIZED', paymentsOpen: false })).toBe(
      'It’s on — your hold is being released, you weren’t charged',
    )
    expect(headline({ status: 'CONFIRMED', resolution: 'OPEN', enrollment: 'AUTHORIZED', paymentsOpen: true })).toBe('It’s on — charging your spot')
  })

  it('counts a hold being released, not only a released one', () => {
    expect(releasesAny([{ status: 'PAID' }, { status: 'VOIDING' }])).toBe(true)
    expect(releasesAny([{ status: 'CAPTURE_FAILED' }])).toBe(true)
    expect(releasesAny([{ status: 'PAID' }, { status: 'REFUNDED' }])).toBe(false)
  })
})
