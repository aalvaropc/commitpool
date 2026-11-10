import { describe, expect, it } from 'vitest'
import type { Extension, Proposal } from '../api'
import { canSkipToLastHours, extendLink, latestDeadline, latestDeadlineNote, movedDeadline } from './extend'

const offer: Extension = { available: true, from: '2026-11-03T09:00:00Z', to: '2026-11-05T09:00:00Z', reason: 'You can move the deadline once.' }

const proposal: Proposal = {
  id: 'p1',
  version: 1,
  date_id: 'sat',
  price: { minor: 1000, currency: 'USD', value: '10.00' },
  minimum: 3,
  capacity: 8,
  opens_at: '2026-11-02T09:00:00Z',
  closes_at: '2026-11-03T09:00:00Z',
  starts_at: '2026-11-14T15:00:00Z',
  payments_open: true,
  extended_from: null,
  latest_closes_at: '2026-11-05T09:00:00Z',
}

describe('extendLink', () => {
  it('opens the dialog only for the option on offer, to the second', () => {
    const at = String(Date.parse('2026-11-05T09:00:00Z') / 1000)
    expect(extendLink(at, offer)).toBe('open')
    expect(extendLink(String(Number(at) + 900), offer)).toBe('stale')
    expect(extendLink('soon', offer)).toBe('stale')
    expect(extendLink(at, { ...offer, available: false, to: undefined, refusal: 'already_extended' })).toBe('stale')
    expect(extendLink(at, null)).toBe('stale')
    expect(extendLink(null, offer)).toBeNull()
  })
})

describe('movedDeadline', () => {
  it('names both deadlines while reservations are still funding', () => {
    expect(movedDeadline(proposal, 'FUNDING')).toBeNull()
    const moved = { ...proposal, extended_from: '2026-11-03T09:00:00Z', closes_at: '2026-11-05T09:00:00Z' }
    expect(movedDeadline(moved, 'FUNDING')).toEqual({ from: '2026-11-03T09:00:00Z', to: '2026-11-05T09:00:00Z' })
    expect(movedDeadline(moved, 'CONFIRMED')).toBeNull()
    expect(movedDeadline({ ...moved, closes_at: '2026-11-02T12:00:00Z' }, 'FUNDING')).toBeNull()
  })
})

describe('latestDeadline', () => {
  it('gives the latest possible deadline while reservations are open', () => {
    expect(latestDeadline(proposal, 'FUNDING')).toBe('2026-11-05T09:00:00Z')
    expect(latestDeadline({ ...proposal, payments_open: false }, 'FUNDING')).toBeNull()
    expect(latestDeadline(proposal, 'CONFIRMED')).toBeNull()
    expect(latestDeadline({ ...proposal, latest_closes_at: undefined }, 'FUNDING')).toBeNull()
  })
})

describe('latestDeadlineNote', () => {
  it('promises an extension only while one is still possible', () => {
    expect(latestDeadlineNote(proposal)).toBe('The organizer can extend the deadline once, up to then.')
    expect(latestDeadlineNote({ ...proposal, latest_closes_at: proposal.closes_at })).toBe('This deadline will not move.')
    expect(latestDeadlineNote({ ...proposal, extended_from: '2026-11-02T20:00:00Z', closes_at: '2026-11-05T09:00:00Z' })).toBe('It was already extended once.')
  })
})

describe('canSkipToLastHours', () => {
  it('offers the skip only while more than two hours are left and the deadline never moved', () => {
    const closes = Date.parse(proposal.closes_at)
    expect(canSkipToLastHours(proposal, closes - 3 * 3600_000)).toBe(true)
    expect(canSkipToLastHours(proposal, closes - 2 * 3600_000)).toBe(false)
    expect(canSkipToLastHours({ ...proposal, extended_from: '2026-11-02T20:00:00Z' }, closes - 3 * 3600_000)).toBe(false)
  })
})
