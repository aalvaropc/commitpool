import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Availability, CandidateDate, Interpretation } from '../api'
import ReadingEditor from './ReadingEditor'

const dates: CandidateDate[] = [
  { id: 'sat', starts_at: '2026-11-14T20:00:00Z', ends_at: '2026-11-14T23:00:00Z', label: 'Sat 14 Nov 2026, 15:00–18:00 America/Lima' },
  { id: 'sun', starts_at: '2026-11-15T20:00:00Z', ends_at: '2026-11-15T23:00:00Z', label: 'Sun 15 Nov 2026, 15:00–18:00 America/Lima' },
]

function availability(reading: Interpretation): Availability {
  return { id: 'a1', message: 'Saturday works, Sunday afternoon maybe', interpretation: reading, corrected: null, needs_clarification: reading.needs_clarification, created_at: '2026-10-27T12:00:00Z' }
}

const none = { type: 'none', time: '' } as const
const reading = (needsClarification: boolean): Interpretation => ({
  per_date: [
    { candidate_date_id: 'sat', verdict: 'available', condition: none, evidence: 'Saturday works' },
    { candidate_date_id: 'sun', verdict: 'unknown', condition: none, evidence: 'Sunday afternoon maybe' },
  ],
  hard_constraints: [],
  preferences: [],
  ambiguities: needsClarification ? [{ question: 'Which hours on Sunday afternoon?', why: 'vague' }] : [],
  needs_clarification: needsClarification,
})

function saveButton(html: string): string {
  const m = /<button[^>]*>(Confirm reading|Save corrections)<\/button>/.exec(html)
  if (!m) throw new Error('no save button in:\n' + html)
  return m[0]
}

const render = (r: Interpretation, editable = true) =>
  renderToStaticMarkup(<ReadingEditor token="t" availability={availability(r)} timeZone="America/New_York" dates={dates} editable={editable} onSaved={async () => undefined} />)

function disclosure(html: string): string {
  const m = /<details[^>]*>/.exec(html)
  if (!m) throw new Error('no Edit reading disclosure in:\n' + html)
  return m[0]
}

describe('ReadingEditor', () => {
  it('lets a correct reading that needs clarification be confirmed without changes', () => {
    const button = saveButton(render(reading(true)))
    expect(button).toContain('Confirm reading')
    expect(button).not.toMatch(/\sdisabled=/)
  })

  it('shows the reading per date and keeps the corrections behind a closed Edit reading', () => {
    const html = render(reading(false))
    expect(html).toContain('Saturday works')
    expect(html.indexOf('Saturday works')).toBeLessThan(html.indexOf('Edit reading'))
    expect(html).toMatch(/<summary[^>]*>Edit reading/)
    expect(disclosure(html)).not.toMatch(/\sopen/)
    expect(html.indexOf('Edit reading')).toBeLessThan(html.indexOf('Can you come on'))
  })

  it('opens Edit reading when the reading needs clarification', () => {
    expect(disclosure(render(reading(true)))).toMatch(/\sopen/)
  })

  it('offers no corrections once answers are closed, but still shows the reading', () => {
    const html = render(reading(false), false)
    expect(html).toContain('Saturday works')
    expect(html).not.toContain('Edit reading')
  })

  it('keeps the button disabled for a clear, unchanged reading', () => {
    const button = saveButton(render(reading(false)))
    expect(button).toContain('Save corrections')
    expect(button).toMatch(/\sdisabled=/)
  })
})
