import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Availability, Interpretation, ParticipantView } from '../api'
import AvailabilityPanel from './AvailabilityPanel'

const none = { type: 'none', time: '' } as const
const question = 'The class runs 10 AM–12 PM on Saturday and Sunday — can you make either morning, and which?'

function reading(clarify: boolean): Interpretation {
  return {
    per_date: [{ candidate_date_id: 'sat', verdict: clarify ? 'unknown' : 'available', condition: none, evidence: '' }],
    hard_constraints: [],
    preferences: [],
    ambiguities: clarify ? [{ question, why: 'vague' }] : [],
    needs_clarification: clarify,
  }
}

function render(clarify: boolean): string {
  const availability: Availability = {
    id: 'a1',
    message: 'Count me in if it’s an afternoon thing',
    interpretation: reading(clarify),
    corrected: null,
    needs_clarification: clarify,
    created_at: '2026-11-08T12:00:00Z',
  }
  const view = {
    availability,
    workshop: { status: 'COLLECTING', timezone: 'America/New_York' },
    proposal: null,
    dates: [{ id: 'sat', starts_at: '2026-11-14T15:00:00Z', ends_at: '2026-11-14T17:00:00Z', label: '' }],
  } as unknown as ParticipantView
  return renderToStaticMarkup(<AvailabilityPanel token="t" view={view} onSaved={async () => undefined} />)
}

describe('AvailabilityPanel', () => {
  it('puts Claude’s question above an empty answer box', () => {
    const html = render(true)
    expect(html).toContain(question)
    expect(html).toContain('Your answer to Claude’s question')
    expect(html).toContain('Send my answer')
    expect(html).toMatch(/<textarea[^>]*><\/textarea>/)
    expect(html.indexOf(question)).toBeLessThan(html.indexOf('Your answer to Claude’s question'))
  })

  it('keeps the sent message ready to update once the reading is clear', () => {
    const html = render(false)
    expect(html).not.toContain('Claude asks you')
    expect(html).toContain('Update my availability')
    expect(html).toMatch(/<textarea[^>]*>Count me in if it’s an afternoon thing<\/textarea>/)
  })
})
