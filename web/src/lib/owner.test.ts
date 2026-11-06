import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import { cancellationDetail, cellNotes, dateColumns, nextStep, ownerRefresh, studioCovered } from './owner'

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
    suggestions: [],
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

describe('nextStep never gets ahead of the money', () => {
  const funding = (over: Partial<OwnerView['tally']>, open = true) =>
    view({
      workshop: { ...view().workshop, status: 'FUNDING' },
      proposal: { id: 'p', version: 1, date_id: 'sat', price: usd(4500), minimum: 3, capacity: 6, opens_at: '', closes_at: '2026-11-13T23:00:00Z', starts_at: '2026-11-14T15:00:00Z', payments_open: open },
      tally: { ...view().tally, ...over },
    })

  it('counts holds being charged as reserved', () => {
    expect(nextStep(funding({ authorized: 0, capturing: 3 })).title).toBe('3 reserved — confirming payments.')
  })

  it('says the class falls short once reservations closed below the minimum', () => {
    expect(nextStep(funding({ authorized: 1 }, false))).toEqual({
      title: 'Reservations closed with 1 of the 3 needed.',
      detail: 'The class is being cancelled: holds are released and any payment refunded.',
      action: { kind: 'none' },
    })
  })

  it('puts a payment that needs a check first', () => {
    const v = view({ workshop: { ...view().workshop, status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION' } })
    expect(nextStep(v).title).toBe('A payment needs your check.')
  })

  it('tells the organizer what is left once cancelled, without repeating the status', () => {
    const cancelled = (resolution: OwnerView['workshop']['resolution'], enrollments: OwnerView['enrollments']) =>
      nextStep(view({ workshop: { ...view().workshop, status: 'CANCELLED', resolution }, enrollments }))
    const e = (status: OwnerView['enrollments'][number]['status']) =>
      ({ id: status, status, status_label: '', seat_expires_at: '', approval_cancelled_at: null, payment: null }) as OwnerView['enrollments'][number]
    expect(cancelled('RESOLVING', [e('REFUNDING')])).toEqual({
      title: 'PayPal is returning the money.',
      detail: 'Holds are released and payments refunded on their own. This page updates as each one completes.',
      action: { kind: 'none' },
    })
    expect(cancelled('SETTLED', [e('REFUNDED'), e('VOIDED')])).toEqual({
      title: 'Everything is settled.',
      detail: 'Every hold is released and every payment refunded. There is nothing else to do.',
      action: { kind: 'none' },
    })
    expect(cancelled('SETTLED', [])).toEqual({ title: 'Nothing to settle.', detail: 'Nobody had reserved, so nothing was held or charged.', action: { kind: 'none' } })
  })

  it('says when nobody can make any date yet', () => {
    const v = view({
      availabilities: answered(['p1']),
      recommendation: { date_id: 'sat', meets_minimum: false, per_date: [{ date_id: 'sat', compatible: [], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v).title).toBe('Nobody can make a date yet.')
  })

  it('names at most three people who need to clarify', () => {
    const many = ['A', 'B', 'C', 'D', 'E'].map((n) => ({ ...answered([n])[0], display_name: n, needs_clarification: true }))
    const v = view({ availabilities: many, recommendation: { date_id: 'sat', meets_minimum: false, per_date: [{ date_id: 'sat', compatible: ['A'], blockers: null }], explanation: '', explanation_pending: false } })
    expect(nextStep(v).detail).toContain('A, B, C and 2 others need to clarify their message.')
  })

  it('tells two slots on the same day apart', () => {
    const v = view({
      dates: [
        { id: 'am', starts_at: '2026-11-14T15:00:00Z', ends_at: '2026-11-14T17:00:00Z', label: '' },
        { id: 'pm', starts_at: '2026-11-14T20:00:00Z', ends_at: '2026-11-14T22:00:00Z', label: '' },
      ],
      availabilities: answered(['p1', 'p2', 'p3']),
      recommendation: { date_id: 'pm', meets_minimum: true, per_date: [{ date_id: 'pm', compatible: ['p1', 'p2', 'p3'], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v).action).toEqual({ kind: 'open', dateId: 'pm', day: 'Sat, Nov 14 at 3 PM' })
  })
})

describe('studioCovered once cancelled', () => {
  it('stops claiming the studio is covered', () => {
    expect(studioCovered(view({ workshop: { ...view().workshop, status: 'CANCELLED' }, tally: { ...view().tally, paid: 3 } }))).toBeNull()
  })
})

describe('ownerRefresh outside collecting', () => {
  it('does not race for an explanation the page no longer shows', () => {
    expect(ownerRefresh({ status: 'CANCELLED', resolution: 'SETTLED', paymentsOpen: false, enrollments: [], explanationPending: true })).toBeNull()
  })
})

describe('dateColumns', () => {
  const ready = () =>
    view({
      availabilities: answered(['p1', 'p2', 'p3']),
      recommendation: {
        date_id: 'sat',
        meets_minimum: true,
        per_date: [
          { date_id: 'sat', compatible: ['p1', 'p2', 'p3'], blockers: null },
          { date_id: 'sun', compatible: ['p1', 'p2', 'p3'], blockers: null },
        ],
        explanation: '',
        explanation_pending: false,
      },
    })

  it('counts who can come and marks the recommended date', () => {
    expect(dateColumns(ready())).toEqual([
      { id: 'sat', name: 'Sat, Nov 14', time: '10 AM–12 PM', can: 3, mark: 'Recommended', canOpen: false },
      { id: 'sun', name: 'Sun, Nov 15', time: '10 AM–12 PM', can: 3, mark: null, canOpen: true },
    ])
  })

  it('marks no date before anyone answered', () => {
    expect(dateColumns(view({ recommendation: { ...view().recommendation, date_id: 'sat' } })).map((c) => c.mark)).toEqual([null, null])
  })

  it('marks the date that opened instead of the recommendation', () => {
    const proposal = { date_id: 'sun', price: usd(4500), minimum: 3, capacity: 6, opens_at: '', closes_at: '2026-11-13T15:00:00Z', payments_open: true }
    const open = dateColumns({ ...ready(), workshop: { ...ready().workshop, status: 'FUNDING' }, proposal } as OwnerView)
    expect(open.map((c) => [c.mark, c.canOpen])).toEqual([
      [null, false],
      ['Reservations open', false],
    ])
    const closed = dateColumns({ ...ready(), workshop: { ...ready().workshop, status: 'CONFIRMED' }, proposal: { ...proposal, payments_open: false } } as OwnerView)
    expect(closed.map((c) => c.mark)).toEqual([null, 'Chosen date'])
  })

  it('names two slots on the same day by their start time', () => {
    const v = view({
      dates: [
        { id: 'am', starts_at: '2026-11-14T15:00:00Z', ends_at: '2026-11-14T17:00:00Z', label: '' },
        { id: 'pm', starts_at: '2026-11-14T20:00:00Z', ends_at: '2026-11-14T22:00:00Z', label: '' },
      ],
    })
    expect(dateColumns(v).map((c) => c.name)).toEqual(['Sat, Nov 14 at 10 AM', 'Sat, Nov 14 at 3 PM'])
  })
})

describe('cellNotes', () => {
  const reading = (verdict: 'available' | 'conditional', time = '') => ({
    per_date: [{ candidate_date_id: 'sat', verdict, condition: { type: time ? ('end_before' as const) : ('none' as const), time }, evidence: '' }],
    hard_constraints: [],
    preferences: [],
    ambiguities: [],
    needs_clarification: false,
  })
  const summary = { date_id: 'sat', compatible: ['p1'], blockers: { p2: 'ends after 11:00' } }

  it('says whether a condition fits the slot', () => {
    const a = { ...answered(['p1'])[0], interpretation: reading('conditional', '12:00') }
    expect(cellNotes(a, 'sat', summary)).toEqual({ fit: 'Fits this time', claudeRead: null })
    const b = { ...answered(['p2'])[0], interpretation: reading('conditional', '11:00') }
    expect(cellNotes(b, 'sat', summary)).toEqual({ fit: 'Doesn’t fit this time', claudeRead: null })
  })

  it('shows what Claude read when the person corrected it', () => {
    const a = { ...answered(['p1'])[0], interpretation: reading('conditional', '11:00'), corrected: reading('available') }
    expect(cellNotes(a, 'sat', summary)).toEqual({ fit: null, claudeRead: 'Claude read: Only if it ends by 11 AM' })
  })

  it('stays quiet for a plain answer', () => {
    expect(cellNotes({ ...answered(['p1'])[0], interpretation: reading('available') }, 'sat', summary)).toEqual({ fit: null, claudeRead: null })
  })
})

describe('cancellationDetail', () => {
  const e = (status: OwnerView['enrollments'][number]['status'], over: Partial<OwnerView['enrollments'][number]> = {}) =>
    ({ id: status, status, status_label: '', seat_expires_at: '', approval_cancelled_at: null, payment: null, ...over }) as OwnerView['enrollments'][number]

  it('counts released holds, completed refunds and what is still moving', () => {
    const v = view({ enrollments: [e('VOIDED'), e('REFUNDED'), e('REFUNDED'), e('REFUNDING'), e('VOIDING')] })
    expect(cancellationDetail(v)).toBe('1 hold released, 2 refunds completed, 2 in progress.')
  })

  it('names what needs attention', () => {
    expect(cancellationDetail(view({ enrollments: [e('REFUND_FAILED')] }))).toBe('0 holds released, 0 refunds completed, 0 in progress, 1 needs attention.')
  })
})

describe('ownerRefresh for a demo filling in', () => {
  const base = { status: 'COLLECTING' as const, resolution: 'OPEN' as const, paymentsOpen: false, enrollments: [], explanationPending: false }
  it('follows the answers as they arrive', () => {
    expect(ownerRefresh({ ...base, demoFilling: true })).toBe(2000)
    expect(ownerRefresh({ ...base, demoFilling: false })).toBe(8000)
  })
})
