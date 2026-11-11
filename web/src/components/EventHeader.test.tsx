import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { PublicView } from '../api'
import { EventDetails } from './EventHeader'

function view(tied: boolean): PublicView {
  return {
    workshop: { timezone: 'America/New_York', status: 'COLLECTING', duration_min: 120, capacity: 6, price: { minor: 4500, currency: 'USD', value: '45.00' }, demo: false },
    dates: [
      { id: 'sat', starts_at: '2026-11-14T15:00:00Z', ends_at: '2026-11-14T17:00:00Z', label: '' },
      { id: 'sun', starts_at: '2026-11-15T15:00:00Z', ends_at: '2026-11-15T17:00:00Z', label: '' },
    ],
    proposal: null,
    tally: { interested: 4, authorized: 0, paid: 0, charging: 0, minimum: 3, capacity: 6 },
    group: { answered: 4, can_come: { sat: 2, sun: 2 }, tied },
  } as unknown as PublicView
}

describe('EventDetails', () => {
  it('calls a tied date the one that comes first, not the best', () => {
    const html = renderToStaticMarkup(<EventDetails view={view(true)} />)
    expect(html).toContain('tied, comes first')
    expect(html).toContain('2 can come on the date that comes first.')
    expect(html).not.toContain('the best date')
  })

  it('calls a clear leader the best date', () => {
    const html = renderToStaticMarkup(<EventDetails view={view(false)} />)
    expect(html).toContain('most so far')
    expect(html).toContain('2 can come on the best date.')
  })
})
