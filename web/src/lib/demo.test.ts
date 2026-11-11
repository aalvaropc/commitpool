import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import { demoParticipants, demoStep } from './demo'

describe('demo participants', () => {
  it('offers the people the organizer view lists', () => {
    const act_as = [
      { name: 'Priya Raman', participant_url: 'https://cp.test/p/PRI' },
      { name: 'Ben Whitaker', participant_url: 'https://cp.test/p/BEN' },
    ]
    expect(demoParticipants({ act_as })).toEqual([
      { name: 'Priya Raman', path: '/p/PRI' },
      { name: 'Ben Whitaker', path: '/p/BEN' },
    ])
  })

  it('ignores anything that is not a participant link', () => {
    expect(demoParticipants({ act_as: [{ name: 'Mallory', participant_url: 'javascript:alert(1)' }] })).toEqual([])
    expect(demoParticipants({ act_as: [{ name: 'Mallory', participant_url: 'https://cp.test/owner/OWN' }] })).toEqual([])
  })

  it('offers nobody outside a demo workshop', () => {
    expect(demoParticipants({})).toEqual([])
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
      workshop: { status: 'COLLECTING', timezone: 'America/New_York', minimum: 3, host_name: 'Iris Calder', demo: true },
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

  it('starts from Iris’s group chat while nobody is on the board yet', () => {
    const fresh = view({ participants: [], availabilities: [], demo_chat: 'Iris Calder: Who’s in?', recommendation: { ...view().recommendation, date_id: '', per_date: null } })
    expect(demoStep(fresh, [])).toEqual({ text: "This is Iris's group chat. Press Read the chat, then Add 6 people.", primary: null })
    expect(demoStep(view({ participants: [], availabilities: [], demo_chat: null }), [])).toBeNull()
    expect(demoStep({ ...fresh, proposal: { date_id: 'sat' } } as unknown as OwnerView, [])).toBeNull()
  })

  it('sends the visitor to answer Claude’s question as Grace, with her reply from the scenario', () => {
    expect(demoStep(view(), cast)).toEqual({
      text: 'Saturday has 2 of the 3 Iris needs. Act as Grace Liu and answer Claude’s question — for example: “No problem — I swapped Saturday. Sunday I\'m still at the market.”',
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
    expect(demoStep(clear, cast)).toEqual({ text: 'Saturday has 2 of the 3 Iris needs. Act as Hannah Kim and answer as them, in your own words.', primary: 'Hannah Kim' })
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

  describe('once reservations open', () => {
    const v = view()
    const seat = (participant_id: string, status: string, needs_attention = false) => ({ id: `e-${participant_id}`, participant_id, status, needs_attention })
    const funding = (enrollments: ReturnType<typeof seat>[], over: Partial<OwnerView> = {}) =>
      view({
        workshop: { ...v.workshop, status: 'FUNDING' },
        proposal: { date_id: 'sat', payments_open: true, minimum: 3 },
        tally: { minimum: 3, paid: 0, authorized: enrollments.length, capturing: 0, pending: 0 },
        enrollments,
        ...over,
      } as unknown as Partial<OwnerView>)

    it('sends the visitor to reserve as the first person who can come and has not reserved', () => {
      expect(demoStep(funding([]), cast)).toEqual({ text: 'Reserve as someone who can come: act as Priya Raman and reserve with PayPal.', primary: 'Priya Raman' })
      expect(demoStep(funding([seat('p1', 'AUTHORIZED')]), cast)).toEqual({ text: 'Reserve as someone who can come: act as Ben Whitaker and reserve with PayPal.', primary: 'Ben Whitaker' })
    })

    it('follows the order people are listed in, not the order the date summary names them', () => {
      const shuffled = { ...v.recommendation, per_date: [{ date_id: 'sat', compatible: ['p2', 'p1'], blockers: null }] }
      expect(demoStep(funding([], { recommendation: shuffled }), cast)?.primary).toBe('Priya Raman')
    })

    it('points again at someone who can reserve again, as the nudges do', () => {
      expect(demoStep(funding([seat('p1', 'RELEASED')]), cast)?.primary).toBe('Priya Raman')
      expect(demoStep(funding([seat('p1', 'CAPTURE_FAILED')]), cast)?.primary).toBe('Priya Raman')
    })

    it('skips someone who withdrew, holds a spot, or has a seat flagged for a check', () => {
      expect(demoStep(funding([seat('p1', 'VOIDED')]), cast)?.primary).toBe('Ben Whitaker')
      expect(demoStep(funding([seat('p1', 'SEAT_RESERVED')]), cast)?.primary).toBe('Ben Whitaker')
      expect(demoStep(funding([seat('p1', 'CAPTURE_FAILED', true)]), cast)?.primary).toBe('Ben Whitaker')
    })

    it('stays quiet once enough people reserved, once reservations close, or when nobody is left to act as', () => {
      expect(demoStep(funding([seat('p1', 'AUTHORIZED'), seat('p2', 'AUTHORIZED'), seat('p9', 'AUTHORIZED')]), cast)).toBeNull()
      expect(demoStep(funding([], { proposal: { date_id: 'sat', payments_open: false, minimum: 3 } } as unknown as Partial<OwnerView>), cast)).toBeNull()
      expect(demoStep(funding([seat('p1', 'PAID'), seat('p2', 'SEAT_RESERVED')], { tally: { minimum: 3, paid: 1, authorized: 0, capturing: 0, pending: 0 } } as unknown as Partial<OwnerView>), cast)).toBeNull()
      expect(demoStep(view({ workshop: { ...v.workshop, status: 'CONFIRMED' }, proposal: { date_id: 'sat', payments_open: true } } as unknown as Partial<OwnerView>), cast)).toBeNull()
    })
  })
})
