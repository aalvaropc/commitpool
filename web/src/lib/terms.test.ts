import { describe, expect, it } from 'vitest'
import { lockedTerms, workshopTerms } from './terms'

const price = { minor: 1000, currency: 'USD', value: '10.00' }

describe('workshopTerms', () => {
  it('lists price, minimum, capacity and duration', () => {
    expect(workshopTerms({ price, minimum: 3, capacity: 8, duration_min: 90 })).toEqual([
      { label: 'Price', value: '$10.00', kind: 'money' },
      { label: 'Needed to happen', value: '3', kind: 'number' },
      { label: 'Spots', value: '8', kind: 'number' },
      { label: 'Duration', value: '1 hour 30 min', kind: 'number' },
    ])
  })
})

describe('lockedTerms', () => {
  it('lists the terms that lock, in a fixed order, leaving out what is not given', () => {
    const items = lockedTerms({
      timeZone: 'America/Lima',
      dateLabel: 'Sat, Nov 14 · 3 PM–5 PM',
      price,
      minimum: 3,
      capacity: 8,
      windowMin: 15,
      closesAbout: '2026-10-28T01:00:00Z',
    })
    expect(items.map((i) => [i.label, i.value])).toEqual([
      ['Date', 'Sat, Nov 14 · 3 PM–5 PM'],
      ['Price', '$10.00'],
      ['Time to reserve', 'Demo window: 15 minutes'],
      ['Needed to happen', '3'],
      ['Spots', '8'],
      ['Reservations would close about', 'Tue, Oct 27 · 8 PM'],
    ])
    expect(items[0].span).toBe(true)
  })

  it('names an open or a closed window', () => {
    const base = { timeZone: 'UTC', minimum: 1, capacity: 2, opensAt: '2026-10-27T10:00:00Z', closesAt: '2026-10-28T10:00:00Z' }
    expect(lockedTerms({ ...base, paymentsOpen: true }).map((i) => i.label)).toEqual(['Needed to happen', 'Spots', 'Reservations opened', 'Reservations close'])
    expect(lockedTerms({ ...base, paymentsOpen: false }).map((i) => i.label)).toContain('Reservations closed')
  })
})
