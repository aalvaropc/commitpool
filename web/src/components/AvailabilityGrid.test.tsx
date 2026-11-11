import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { OwnerParticipant, OwnerView } from '../api'
import AvailabilityGrid from './AvailabilityGrid'

const text = (html: string) => html.replace(/<[^>]+>/g, ' ').replaceAll('&#x27;', "'").replaceAll('&amp;', '&').replace(/\s+/g, ' ')

const none = { type: 'none' as const, time: '' }

function view(people: OwnerParticipant[], over: Partial<OwnerView> = {}): OwnerView {
  return {
    workshop: { status: 'COLLECTING', timezone: 'America/New_York', minimum: 3, capacity: 6 },
    dates: [{ id: 'sat', starts_at: '2026-11-21T15:00:00Z', ends_at: '2026-11-21T17:00:00Z', label: '' }],
    proposal: null,
    participants: people,
    availabilities: [
      {
        id: 'a1',
        participant_id: 'p1',
        display_name: 'Priya Raman',
        message: 'Saturday works for me, Sunday I can’t.',
        interpretation: { per_date: [{ candidate_date_id: 'sat', verdict: 'available', condition: none, evidence: '' }], hard_constraints: [], preferences: [], ambiguities: [], needs_clarification: false },
        corrected: null,
        needs_clarification: false,
        created_at: '',
      },
    ],
    recommendation: { date_id: 'sat', meets_minimum: false, per_date: [{ date_id: 'sat', compatible: ['p1'], blockers: null }], explanation: '', explanation_pending: false },
    enrollments: [],
    reading: { pending: 1, total: 2, active: false },
    ...over,
  } as unknown as OwnerView
}

const priya: OwnerParticipant = { id: 'p1', display_name: 'Priya Raman', source: 'chat', chat_message: 'Saturday works for me', unread: false, participant_url: 'https://cp.test/p/PRI' }
const grace: OwnerParticipant = { id: 'p2', display_name: 'Grace Liu', source: 'chat', chat_message: 'Count me in!', unread: true, participant_url: 'https://cp.test/p/GRA' }
const ada: OwnerParticipant = { id: 'p3', display_name: 'Ada', source: 'link', chat_message: null, unread: false, participant_url: null }

const render = (v: OwnerView) => text(renderToStaticMarkup(<AvailabilityGrid token="t" view={v} onOpen={() => {}} onChange={async () => {}} />))

describe('AvailabilityGrid with people from the chat', () => {
  it('offers one Read again for everyone Claude could not read, and shows what they said', () => {
    const lucia: OwnerParticipant = { ...grace, id: 'p4', display_name: 'Lucía Torres', chat_message: 'El sábado no puedo.', participant_url: 'https://cp.test/p/LUC' }
    const one = render(view([priya, grace]))
    expect(one).toContain('Couldn’t read yet')
    expect(one).toContain('“Count me in!”')
    expect(one.match(/Read Grace Liu’s message again/g)).toHaveLength(1)
    const two = render(view([priya, grace, lucia], { reading: { pending: 2, total: 3, active: false } }))
    expect(two.match(/Read the 2 people Claude couldn’t read yet/g)).toHaveLength(1)
    expect(two).not.toContain('Read Grace Liu’s message again')
  })

  it('says Claude is reading while it still is', () => {
    const plain = render(view([priya, grace], { reading: { pending: 1, total: 2, active: true } }))
    expect(plain).toContain('Claude is reading…')
    expect(plain).toContain('“Count me in!”')
    expect(plain).not.toContain('Couldn’t read yet')
    expect(plain).not.toContain('Claude couldn’t read')
    expect(plain).not.toContain('Remove Grace Liu')
  })

  it('copies the link of each person from the chat and removes them only while collecting, never someone who joined by link', () => {
    const plain = render(view([priya, grace, ada]))
    expect(plain).toContain('Copy their link for Priya Raman')
    expect(plain).toContain('Copy their link for Grace Liu')
    expect(plain).not.toContain('Copy their link for Ada')
    expect(plain).toContain('Remove Priya Raman')
    expect(plain).toContain('Remove Grace Liu')
    expect(plain).not.toContain('Remove Ada')

    const funding = render(view([priya, ada], { workshop: { status: 'FUNDING', timezone: 'America/New_York', minimum: 3, capacity: 6 } } as unknown as Partial<OwnerView>))
    expect(funding).toContain('Copy their link for Priya Raman')
    expect(funding).not.toContain('Remove Priya Raman')
  })

  it('points to the chat card before anyone is on the board', () => {
    expect(render(view([], { availabilities: [] }))).toContain('Paste your group chat above, or share the invite. Each person appears here with their answer for every date.')
  })
})

function tieView(tied: boolean): OwnerView {
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
    enrollments: [],
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
    const html = renderToStaticMarkup(<AvailabilityGrid token="t" view={tieView(true)} onOpen={() => undefined} onChange={async () => {}} />)
    expect(html).toContain('Tied')
    expect(html).not.toContain('Recommended')
  })

  it('chips the lead as Recommended when no other date shares the most', () => {
    const html = renderToStaticMarkup(<AvailabilityGrid token="t" view={tieView(false)} onOpen={() => undefined} onChange={async () => {}} />)
    expect(html).toContain('Recommended')
    expect(html).not.toContain('Tied')
  })
})
