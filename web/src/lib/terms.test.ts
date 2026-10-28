import { describe, expect, it } from 'vitest'
import { lockedTerms, workshopTerms } from './terms'

const price = { minor: 1000, currency: 'USD', value: '10.00' }

describe('workshopTerms', () => {
  it('lists price, minimum, capacity and duration', () => {
    expect(workshopTerms({ price, minimum: 3, capacity: 8, duration_min: 90 })).toEqual([
      { label: 'Price per seat', value: '$10.00', kind: 'money' },
      { label: 'Minimum paid seats', value: '3', kind: 'number' },
      { label: 'Capacity', value: '8', kind: 'number' },
      { label: 'Duration', value: '1 h 30 min', kind: 'number' },
    ])
  })
})

describe('lockedTerms', () => {
  it('lists the terms that lock, in a fixed order, leaving out what is not given', () => {
    const items = lockedTerms({
      timeZone: 'America/Lima',
      dateLabel: 'Sat 14 Nov 2026, 15:00–17:00 America/Lima',
      price,
      minimum: 3,
      capacity: 8,
      windowMin: 15,
      closesAbout: '2026-10-28T01:00:00Z',
    })
    expect(items.map((i) => [i.label, i.value])).toEqual([
      ['Date', 'Sat 14 Nov 2026, 15:00–17:00 America/Lima'],
      ['Price per seat', '$10.00'],
      ['Payment window', 'Demo window: 15 minutes'],
      ['Minimum paid seats', '3'],
      ['Capacity', '8'],
      ['Payments close about', 'Tue 27 Oct 2026, 20:00 America/Lima'],
    ])
    expect(items[0].span).toBe(true)
  })

  it('names an open or a closed window', () => {
    const base = { timeZone: 'UTC', minimum: 1, capacity: 2, opensAt: '2026-10-27T10:00:00Z', closesAt: '2026-10-28T10:00:00Z' }
    expect(lockedTerms({ ...base, paymentsOpen: true }).map((i) => i.label)).toEqual(['Minimum paid seats', 'Capacity', 'Opened', 'Closes'])
    expect(lockedTerms({ ...base, paymentsOpen: false }).map((i) => i.label)).toContain('Closed')
  })
})
