import { afterEach, describe, expect, it, vi } from 'vitest'
import type { OwnerView } from '../api'
import { demoParticipants, demoStep, rememberDemo } from './demo'

function memoryStorage(): Storage {
  const data = new Map<string, string>()
  return {
    get length() {
      return data.size
    },
    clear: () => data.clear(),
    getItem: (key) => data.get(key) ?? null,
    key: (i) => [...data.keys()][i] ?? null,
    removeItem: (key) => void data.delete(key),
    setItem: (key, value) => void data.set(key, String(value)),
  }
}

describe('demo participants', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('remembers who the visitor can act as, for that demo workshop only', () => {
    vi.stubGlobal('sessionStorage', memoryStorage())
    rememberDemo('https://cp.test/owner/OWN', [
      { name: 'Priya Raman', participant_url: 'https://cp.test/p/PRI' },
      { name: 'Ben Whitaker', participant_url: 'https://cp.test/p/BEN' },
    ])
    expect(demoParticipants('OWN')).toEqual([
      { name: 'Priya Raman', path: '/p/PRI' },
      { name: 'Ben Whitaker', path: '/p/BEN' },
    ])
    expect(demoParticipants('OTHER')).toEqual([])
  })

  it('ignores anything that is not a participant link', () => {
    vi.stubGlobal('sessionStorage', memoryStorage())
    rememberDemo('https://cp.test/owner/OWN', [{ name: 'Mallory', participant_url: 'javascript:alert(1)' }])
    expect(demoParticipants('OWN')).toEqual([])
  })

  it('works without storage', () => {
    vi.stubGlobal('sessionStorage', undefined)
    rememberDemo('https://cp.test/owner/OWN', [{ name: 'Priya', participant_url: 'https://cp.test/p/PRI' }])
    expect(demoParticipants('OWN')).toEqual([])
  })
})

describe('demoStep', () => {
  const none = { type: 'none' as const, time: '' }
  const cast = ['Priya Raman', 'Ben Whitaker', 'Hannah Kim', 'Grace Liu']
  const ids: Record<string, string> = { 'Priya Raman': 'p1', 'Ben Whitaker': 'p2', 'Hannah Kim': 'p3', 'Grace Liu': 'p4', 'Joined Late': 'p9' }
  const answer = (name: string, clarify = false) => ({
    id: `a-${ids[name]}`,
    participant_id: ids[name],
    display_name: name,
    message: '',
    interpretation: { per_date: [{ candidate_date_id: 'sat', verdict: 'available' as const, condition: none, evidence: '' }], hard_constraints: [], preferences: [], ambiguities: [], needs_clarification: clarify },
    corrected: null,
    needs_clarification: clarify,
    created_at: '',
  })
  const view = (over: Partial<OwnerView> = {}): OwnerView =>
    ({
      workshop: { status: 'COLLECTING', timezone: 'America/New_York', minimum: 3, host_name: 'Maya Okafor', demo: true },
      dates: [
        { id: 'sat', starts_at: '2026-11-21T15:00:00Z', ends_at: '2026-11-21T17:00:00Z', label: '' },
        { id: 'sun', starts_at: '2026-11-22T15:00:00Z', ends_at: '2026-11-22T17:00:00Z', label: '' },
      ],
      proposal: null,
      participants: [...cast, 'Joined Late'].map((name) => ({ id: ids[name], display_name: name })),
      availabilities: [answer('Priya Raman'), answer('Ben Whitaker'), answer('Hannah Kim'), answer('Grace Liu', true)],
      recommendation: {
        date_id: 'sat',
        meets_minimum: false,
        per_date: [
          { date_id: 'sat', compatible: ['p1', 'p2'], blockers: { p3: 'ends after 11:30', p4: 'needs clarification' } },
          { date_id: 'sun', compatible: ['p3'], blockers: null },
        ],
        explanation: '',
        explanation_pending: false,
      },
      suggestions: [],
      ...over,
    }) as unknown as OwnerView

  it('sends the visitor to answer Claude’s question as Grace, with her reply from the scenario', () => {
    expect(demoStep(view(), cast)).toEqual({
      text: 'Saturday has 2 of the 3 Maya needs. Act as Grace Liu and answer Claude’s question — for example: “No problem — I swapped Saturday. Sunday I\'m still at the market.”',
      primary: 'Grace Liu',
    })
  })

  it('offers no example for anyone else who needs clarifying, and takes the day and numbers from the view', () => {
    const v = view()
    const ben = view({
      availabilities: [answer('Priya Raman'), answer('Ben Whitaker', true), answer('Hannah Kim'), answer('Grace Liu')],
      recommendation: { ...v.recommendation, date_id: 'sun' },
      workshop: { ...v.workshop, minimum: 4, host_name: 'Ana Ruiz' },
    })
    expect(demoStep(ben, cast)).toEqual({ text: 'Sunday has 1 of the 4 Ana needs. Act as Ben Whitaker and answer Claude’s question.', primary: 'Ben Whitaker' })
  })

  it('falls back to the first person who does not count yet when nobody has a question to answer', () => {
    const clear = view({ availabilities: [answer('Priya Raman'), answer('Ben Whitaker'), answer('Hannah Kim'), answer('Grace Liu')] })
    expect(demoStep(clear, cast)).toEqual({ text: 'Saturday has 2 of the 3 Maya needs. Act as Hannah Kim and answer as them, in your own words.', primary: 'Hannah Kim' })
  })

  it('says to open reservations only once enough people can come', () => {
    const v = view()
    expect(demoStep(view({ recommendation: { ...v.recommendation, meets_minimum: true } }), cast)).toEqual({ text: 'Open reservations, then reserve as someone who can come.', primary: null })
    expect(demoStep(v, cast)?.text).not.toContain('Open reservations')
  })

  it('waits for the cast only, not for someone who joined through the invite', () => {
    const v = view()
    expect(demoStep(v, cast)).not.toBeNull()
    expect(demoStep(view({ availabilities: v.availabilities.slice(0, 2) }), cast)).toBeNull()
    expect(demoStep(v, [])).toBeNull()
  })

  it('stays quiet once reservations open', () => {
    const v = view()
    expect(demoStep(view({ workshop: { ...v.workshop, status: 'FUNDING' } }), cast)).toBeNull()
  })
})
