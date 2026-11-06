import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import { nextStep, ownerRefresh, studioCovered } from './owner'

const usd = (minor: number) => ({ minor, currency: 'USD', value: (minor / 100).toFixed(2) })

function view(over: Partial<OwnerView> = {}): OwnerView {
  return {
    share_url: 'https://x/w/share',
    workshop: {
      title: 'Wheel-throwing for beginners',
      description: '',
      duration_min: 120,
      timezone: 'America/New_York',
      minimum: 3,
      capacity: 6,
      price: usd(4500),
      currency: 'USD',
      payment_window_min: 1440,
      status: 'COLLECTING',
      resolution: 'OPEN',
      host_name: 'Maya Okafor',
      location: '',
      venue_cost: usd(12000),
      break_even_seats: 3,
    },
    dates: [
      { id: 'sat', starts_at: '2026-11-14T15:00:00Z', ends_at: '2026-11-14T17:00:00Z', label: '' },
      { id: 'sun', starts_at: '2026-11-15T15:00:00Z', ends_at: '2026-11-15T17:00:00Z', label: '' },
    ],
    proposal: null,
    tally: { interested: 0, authorized: 0, paid: 0, charging: 0, minimum: 3, capacity: 6, reserved: 0, capturing: 0, pending: 0, failed: 0, seats_taken: 0 },
    participants: [],
    availabilities: [],
    recommendation: { date_id: '', meets_minimum: false, per_date: [], explanation: '', explanation_pending: false },
    enrollments: [],
    demo: { demo_mode: false, sandbox_mocks: false },
    ...over,
  }
}

const answered = (ids: string[]) => ids.map((id) => ({ id: `a-${id}`, participant_id: id, display_name: id, message: '', interpretation: { per_date: [], hard_constraints: [], preferences: [], ambiguities: [], needs_clarification: false }, corrected: null, needs_clarification: false, created_at: '' }))

describe('nextStep', () => {
  it('asks to share the invite before anyone answered', () => {
    expect(nextStep(view())).toEqual({ title: 'Share the invite to get answers', detail: 'Answers show up here as people reply.', action: { kind: 'share' } })
  })

  it('offers to open reservations once a date reaches the minimum', () => {
    const v = view({
      availabilities: answered(['p1', 'p2', 'p3', 'p4']),
      recommendation: { date_id: 'sat', meets_minimum: true, per_date: [{ date_id: 'sat', compatible: ['p1', 'p2', 'p3', 'p4'], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v)).toEqual({ title: 'Sat, Nov 14 works for 4 — you need 3.', detail: 'Open reservations and people can reserve their spot with PayPal.', action: { kind: 'open', dateId: 'sat', day: 'Sat, Nov 14' } })
  })

  it('names who still has to clarify', () => {
    const v = view({
      participants: [{ id: 'p5', display_name: 'Fay' }],
      availabilities: [...answered(['p1', 'p2']), { ...answered(['p5'])[0], display_name: 'Fay', needs_clarification: true }],
      recommendation: { date_id: 'sat', meets_minimum: false, per_date: [{ date_id: 'sat', compatible: ['p1', 'p2'], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v)).toEqual({ title: 'Best so far: Sat, Nov 14, with 2 of the 3 needed.', detail: 'Fay’s message needs clarifying. Share the invite to reach more people.', action: { kind: 'share' } })
  })

  it('counts down while reservations are open', () => {
    const v = view({
      workshop: { ...view().workshop, status: 'FUNDING' },
      proposal: { id: 'p', version: 1, date_id: 'sat', price: usd(4500), minimum: 3, capacity: 6, opens_at: '', closes_at: '2026-11-13T23:00:00Z', starts_at: '2026-11-14T15:00:00Z', payments_open: true },
      tally: { ...view().tally, authorized: 1, paid: 1 },
    })
    expect(nextStep(v)).toEqual({ title: '2 of 3 reserved — 1 more and it’s on.', detail: 'Share the invite again; reservations close Fri, Nov 13 · 6 PM.', action: { kind: 'share' } })
  })

  it('celebrates once it is on', () => {
    const v = view({ workshop: { ...view().workshop, status: 'CONFIRMED' }, tally: { ...view().tally, paid: 4 } })
    expect(nextStep(v)).toEqual({ title: 'It’s on — 4 paid, $180 collected.', detail: 'Everyone who paid got their spot.', action: { kind: 'none' } })
  })
})

describe('studioCovered', () => {
  it('says how much of the studio the payments cover', () => {
    expect(studioCovered(view({ tally: { ...view().tally, paid: 2 } }))).toEqual({ covered: 9000, cost: 12000, text: '$90 of your $120 studio covered' })
    expect(studioCovered(view({ tally: { ...view().tally, paid: 3 } }))?.text).toBe('Your $120 studio is covered, with $15 to spare')
  })

  it('is absent without a studio cost', () => {
    expect(studioCovered(view({ workshop: { ...view().workshop, venue_cost: undefined } }))).toBeNull()
  })
})

describe('ownerRefresh', () => {
  it('refreshes fast while Claude writes, steadily while answers arrive and while money moves', () => {
    expect(ownerRefresh({ status: 'COLLECTING', resolution: 'OPEN', paymentsOpen: false, enrollments: [], explanationPending: true })).toBe(3000)
    expect(ownerRefresh({ status: 'COLLECTING', resolution: 'OPEN', paymentsOpen: false, enrollments: [], explanationPending: false })).toBe(8000)
    expect(ownerRefresh({ status: 'FUNDING', resolution: 'OPEN', paymentsOpen: true, enrollments: [], explanationPending: false })).toBe(5000)
  })

  it('stops once nothing can change on its own', () => {
    expect(ownerRefresh({ status: 'CANCELLED', resolution: 'SETTLED', paymentsOpen: false, enrollments: ['REFUNDED'], explanationPending: false })).toBeNull()
    expect(ownerRefresh({ status: 'CONFIRMED', resolution: 'SETTLED', paymentsOpen: false, enrollments: ['PAID'], explanationPending: false })).toBeNull()
  })
})
