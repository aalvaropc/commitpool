import { describe, expect, it } from 'vitest'
import { lockedTerms, openReservationsTerms, workshopTerms } from './terms'

const price = { minor: 1000, currency: 'USD', value: '10.00' }

describe('workshopTerms', () => {
  it('lists price, minimum, capacity and duration', () => {
    expect(workshopTerms({ price, minimum: 3, capacity: 8, duration_min: 90 })).toEqual([
      { label: 'Price', value: '$10', kind: 'money' },
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
      ['Price', '$10'],
      ['Time to reserve', 'Demo window: 15 minutes'],
      ['Needed to happen', '3'],
      ['Spots', '8'],
      ['Reservations would close about', 'Tue, Oct 27 · 8 PM'],
    ])
    expect(items[0].span).toBe(true)
  })

  it('names the payout receiver last, only when there is one', () => {
    const base = { timeZone: 'UTC', minimum: 3, capacity: 8, closesAbout: '2026-10-28T01:00:00Z' }
    expect(lockedTerms({ ...base, payoutTo: 'iris@example.com' }).at(-1)).toEqual({
      label: 'Paid out at the start to',
      value: 'iris@example.com',
      kind: 'text',
      span: true,
    })
    expect(lockedTerms(base).map((i) => i.label)).not.toContain('Paid out at the start to')
    expect(lockedTerms({ ...base, payoutTo: '' }).map((i) => i.label)).not.toContain('Paid out at the start to')
  })

  it('names an open or a closed window', () => {
    const base = { timeZone: 'UTC', minimum: 1, capacity: 2, opensAt: '2026-10-27T10:00:00Z', closesAt: '2026-10-28T10:00:00Z' }
    expect(lockedTerms({ ...base, paymentsOpen: true }).map((i) => i.label)).toEqual(['Needed to happen', 'Spots', 'Reservations opened', 'Reservations close'])
    expect(lockedTerms({ ...base, paymentsOpen: false }).map((i) => i.label)).toContain('Reservations closed')
  })
})

describe('openReservationsTerms', () => {
  const workshop = { timezone: 'America/Lima', price, payment_window_min: 1440, minimum: 3, capacity: 8 }
  const date = { starts_at: '2026-11-14T20:00:00Z', ends_at: '2026-11-14T22:00:00Z' }
  const now = Date.parse('2026-11-10T15:00:00Z')

  it('names who is paid out at the start when the workshop has a payout email', () => {
    expect(openReservationsTerms({ ...workshop, payout_email: 'iris@example.com' }, date, now).map((i) => [i.label, i.value])).toEqual([
      ['Date', 'Sat, Nov 14 · 3 PM–5 PM'],
      ['Price', '$10'],
      ['Time to reserve', '24 hours'],
      ['Needed to happen', '3'],
      ['Spots', '8'],
      ['Reservations would close about', 'Wed, Nov 11 · 10 AM'],
      ['Paid out at the start to', 'iris@example.com'],
    ])
  })

  it('leaves the payout row out when there is no payout email', () => {
    expect(openReservationsTerms(workshop, date, now).map((i) => i.label)).not.toContain('Paid out at the start to')
  })
})
