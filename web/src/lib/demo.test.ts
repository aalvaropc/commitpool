import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import { chatLead, demoAction, demoControl, demoParticipants, demoStep } from './demo'

describe('chatLead', () => {
  const demo = '[11/10/26, 7:05:14\u202fPM] Iris Calder: Who’s in?'
  it('points at Iris’s chat while it is the one in the box, and at the people otherwise', () => {
    expect(chatLead(demo, demo)).toBe('This is Iris’s group chat. Press Read the chat, then Add 6 people.')
    expect(chatLead(`${demo}\n`, demo)).toBe('This is Iris’s group chat. Press Read the chat, then Add 6 people.')
    expect(chatLead('Ana: Sunday works', demo)).toBe('Press Read the chat, then Add the people.')
    expect(chatLead('Ana: Sunday works', null)).toBeNull()
  })
})

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
        tied: false,
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

  it('says Claude is reading while the cast’s answers are still being read', () => {
    const v = view()
    const reading = view({ availabilities: v.availabilities.slice(0, 2), reading: { pending: 2, total: 4, active: true } })
    expect(demoStep(reading, cast)).toEqual({ text: 'Claude is reading each person’s messages against Iris’s dates.', primary: null })
    expect(demoStep(view({ availabilities: v.availabilities.slice(0, 2), reading: { pending: 2, total: 4, active: false } }), cast)).toBeNull()
  })

  it('once Claude drafted the group reply, says to copy it, then to answer as Grace', () => {
    const reply = { kind: 'group_reply' as const, reason: '', message: 'Life drawing: thanks, everyone!', pending: false }
    expect(demoStep(view({ suggestions: [reply] }), cast)).toEqual({
      text: 'Copy Claude’s reply for the group, then act as Grace to answer her question. For example: “No problem — I swapped Saturday. Sunday I\'m still at the market.”',
      primary: 'Grace Liu',
    })
    const reading = view({ suggestions: [reply], availabilities: view().availabilities.slice(0, 2), reading: { pending: 2, total: 4, active: true } })
    expect(demoStep(reading, cast)?.text).toBe('Claude is reading each person’s messages against Iris’s dates.')
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
      expect(demoStep(view({ workshop: { ...v.workshop, status: 'CONFIRMED' }, proposal: { date_id: 'sat', payments_open: true }, demo: { demo_mode: false } } as unknown as Partial<OwnerView>), cast)).toBeNull()
    })
  })

  describe('once the class is on', () => {
    const v = view()
    const payout = (state: string) => ({ state, locked: false, estimated: true, seats: 3 })
    const confirmed = (over: Record<string, unknown> = {}) =>
      view({
        workshop: { ...v.workshop, status: 'CONFIRMED' },
        proposal: { date_id: 'sat', payments_open: true, minimum: 3 },
        demo: { demo_mode: true, sandbox_mocks: false },
        payout: payout('scheduled'),
        ...over,
      } as unknown as Partial<OwnerView>)

    it('demoStep after it is on points to Close reservations now', () => {
      expect(demoStep(confirmed(), cast)).toEqual({ text: 'Close reservations now to end the reservation window.', primary: null, action: 'close' })
    })

    it('demoStep after closing points to Start the class now', () => {
      const closed = confirmed({ proposal: { date_id: 'sat', payments_open: false, minimum: 3 } })
      expect(demoStep(closed, cast)).toEqual({
        text: 'Start the class now. The demo clock jumps to the start and CommitPool pays Iris.',
        primary: null,
        action: 'start',
      })
      expect(demoStep(confirmed({ proposal: { date_id: 'sat', payments_open: false, minimum: 3 }, payout: payout('waiting') }), cast)?.action).toBe('start')
    })

    it('stays quiet once the payout has gone out, or when the server is not in demo mode', () => {
      expect(demoStep(confirmed({ proposal: { date_id: 'sat', payments_open: false, minimum: 3 }, payout: payout('paid') }), cast)).toBeNull()
      expect(demoStep(confirmed({ proposal: { date_id: 'sat', payments_open: false, minimum: 3 }, payout: payout('no_email') }), cast)).toBeNull()
      expect(demoStep(confirmed({ demo: { demo_mode: false, sandbox_mocks: false } }), cast)).toBeNull()
    })

    it('hands the controls to the guide only for a demo workshop', () => {
      expect(demoAction(confirmed())).toBe('close')
      expect(demoControl(confirmed())).toBe('close')
      const plain = confirmed({ workshop: { ...v.workshop, status: 'CONFIRMED', demo: false } })
      expect(demoAction(plain)).toBeNull()
      expect(demoControl(plain)).toBe('close')
      const funding = confirmed({ workshop: { ...v.workshop, status: 'FUNDING' } })
      expect(demoAction(funding)).toBeNull()
      expect(demoControl(funding)).toBe('close')
    })
  })
})
