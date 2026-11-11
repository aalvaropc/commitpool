import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import AvailabilityGrid from './AvailabilityGrid'

const none = { type: 'none', time: '' } as const

function view(tied: boolean): OwnerView {
  const answer = (id: string, sat: 'available' | 'unavailable', sun: 'available' | 'unavailable') => ({
    id: `a-${id}`,
    participant_id: id,
    display_name: id,
    message: 'ok',
    interpretation: {
      per_date: [
        { candidate_date_id: 'sat', verdict: sat, condition: none, evidence: '' },
        { candidate_date_id: 'sun', verdict: sun, condition: none, evidence: '' },
      ],
      hard_constraints: [],
      preferences: [],
      ambiguities: [],
      needs_clarification: false,
    },
    corrected: null,
    needs_clarification: false,
    created_at: '',
  })
  return {
    workshop: { timezone: 'America/New_York', minimum: 3, status: 'COLLECTING' },
    dates: [
      { id: 'sat', starts_at: '2026-11-14T15:00:00Z', ends_at: '2026-11-14T17:00:00Z', label: '' },
      { id: 'sun', starts_at: '2026-11-15T15:00:00Z', ends_at: '2026-11-15T17:00:00Z', label: '' },
    ],
    proposal: null,
    participants: [{ id: 'p1', display_name: 'Priya' }, { id: 'p2', display_name: 'Ben' }],
    availabilities: [answer('p1', 'available', 'unavailable'), answer('p2', 'unavailable', 'available')],
    recommendation: {
      date_id: 'sat',
      meets_minimum: false,
      tied,
      per_date: [
        { date_id: 'sat', compatible: ['p1'], blockers: null },
        { date_id: 'sun', compatible: ['p2'], blockers: null },
      ],
      explanation: '',
      explanation_pending: false,
    },
  } as unknown as OwnerView
}

describe('AvailabilityGrid', () => {
  it('chips the earliest date as Tied when the server says two dates share the most', () => {
    const html = renderToStaticMarkup(<AvailabilityGrid view={view(true)} onOpen={() => undefined} />)
    expect(html).toContain('Tied')
    expect(html).not.toContain('Recommended')
  })

  it('chips the lead as Recommended when no other date shares the most', () => {
    const html = renderToStaticMarkup(<AvailabilityGrid view={view(false)} onOpen={() => undefined} />)
    expect(html).toContain('Recommended')
    expect(html).not.toContain('Tied')
  })
})
