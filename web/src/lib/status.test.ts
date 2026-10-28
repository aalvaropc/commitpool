import { describe, expect, it } from 'vitest'
import { headline, isTerminal, shouldPollOwner, shouldPollParticipant, tone } from './status'

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
    expect(headline({ status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION', enrollment: 'PAID' })).toBe('Needs attention')
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
