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
  const answer = (id: string, clarify = false) => ({
    id: `a-${id}`,
    participant_id: id,
    display_name: id,
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
      participants: ['p1', 'p2', 'p3', 'grace'].map((id) => ({ id, display_name: id })),
      availabilities: [answer('p1'), answer('p2'), answer('p3'), answer('grace', true)],
      recommendation: {
        date_id: 'sat',
        meets_minimum: false,
        per_date: [
          { date_id: 'sat', compatible: ['p1', 'p2'], blockers: { grace: 'needs clarification' } },
          { date_id: 'sun', compatible: ['p3'], blockers: null },
        ],
        explanation: '',
        explanation_pending: false,
      },
      suggestions: [
        { kind: 'condition', participant_id: 'p3', name: 'Hannah Kim', reason: '', message: '', pending: false },
        { kind: 'clarify', participant_id: 'grace', name: 'Grace Liu', reason: '', message: '', pending: false },
      ],
      ...over,
    }) as unknown as OwnerView

  it('sends the visitor to answer Claude’s question while the best date is short', () => {
    expect(demoStep(view())).toEqual({
      text: 'Saturday has 2 of the 3 Maya needs. Act as Grace Liu and answer Claude’s question — for example: “No problem — I swapped Saturday. Sunday I’m still at the market.”',
      primary: 'Grace Liu',
    })
  })

  it('takes the day and the numbers from the view', () => {
    const v = view()
    const sunday = view({ recommendation: { ...v.recommendation, date_id: 'sun' }, workshop: { ...v.workshop, minimum: 4, host_name: 'Ana' } })
    expect(demoStep(sunday)?.text).toBe('Sunday has 1 of the 4 Ana needs. Act as Grace Liu and answer Claude’s question — for example: “No problem — I swapped Sunday. Saturday I’m still at the market.”')
  })

  it('says to open reservations once enough people can come', () => {
    const v = view()
    expect(demoStep(view({ recommendation: { ...v.recommendation, meets_minimum: true }, suggestions: [] }))).toEqual({
      text: 'Open reservations, then reserve as someone who can come.',
      primary: null,
    })
  })

  it('stays quiet while answers arrive, without a question to answer, and once reservations open', () => {
    const v = view()
    expect(demoStep(view({ availabilities: v.availabilities.slice(0, 2) }))).toBeNull()
    expect(demoStep(view({ suggestions: [] }))).toBeNull()
    expect(demoStep(view({ workshop: { ...v.workshop, status: 'FUNDING' } }))).toBeNull()
  })
})
