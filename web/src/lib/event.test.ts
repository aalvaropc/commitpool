import { describe, expect, it } from 'vitest'
import { dateRows, groupSummary, offersCalendar } from './event'

const dates = [
  { id: 'sat', starts_at: '2026-11-14T15:00:00Z', ends_at: '2026-11-14T17:00:00Z', label: '' },
  { id: 'sun', starts_at: '2026-11-15T15:00:00Z', ends_at: '2026-11-15T17:00:00Z', label: '' },
]

describe('dateRows', () => {
  it('lists every candidate with how many can come and marks the one most can make', () => {
    expect(dateRows(dates, { answered: 4, can_come: { sat: 4, sun: 3 } }, null, 'America/New_York')).toEqual([
      { id: 'sat', day: 'Sat, Nov 14', time: '10 AM–12 PM', label: 'Sat, Nov 14 · 10 AM–12 PM', canCome: 4, best: true, chosen: false },
      { id: 'sun', day: 'Sun, Nov 15', time: '10 AM–12 PM', label: 'Sun, Nov 15 · 10 AM–12 PM', canCome: 3, best: false, chosen: false },
    ])
  })

  it('marks no best date before anyone answered', () => {
    expect(dateRows(dates, { answered: 0, can_come: { sat: 0, sun: 0 } }, null, 'America/New_York').some((r) => r.best)).toBe(false)
  })

  it('shows only the chosen date once reservations open', () => {
    const rows = dateRows(dates, { answered: 4, can_come: { sat: 4, sun: 3 } }, 'sun', 'America/New_York')
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ id: 'sun', chosen: true })
  })
})

describe('groupSummary', () => {
  it('invites the first answer without a zero', () => {
    expect(groupSummary({ answered: 0, can_come: {} }, 3)).toEqual({ count: null, text: 'Be the first to say when you can come. 3 needed to happen.' })
  })

  it('counts the answers so far', () => {
    expect(groupSummary({ answered: 1, can_come: {} }, 3)).toEqual({ count: 1, text: 'person answered so far. 3 needed to happen.' })
    expect(groupSummary({ answered: 4, can_come: {} }, 3)).toEqual({ count: 4, text: 'people answered so far. 3 needed to happen.' })
  })
})

describe('offersCalendar', () => {
  it('offers the date to people still going, never once it is cancelled', () => {
    expect(offersCalendar('FUNDING', null)).toBe(true)
    expect(offersCalendar('CONFIRMED', { status: 'PAID' })).toBe(true)
    expect(offersCalendar('FUNDING', { status: 'RELEASED' })).toBe(false)
    expect(offersCalendar('FUNDING', { status: 'CAPTURE_FAILED' })).toBe(false)
    expect(offersCalendar('CANCELLED', { status: 'PAID' })).toBe(false)
  })
})
