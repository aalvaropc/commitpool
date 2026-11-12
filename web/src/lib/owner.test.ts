import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import { cancellationDetail, cellNotes, cellStatus, dateColumns, countFacts, decisionCounts, foldName, nextStep, ownerRefresh, possibleDuplicates, sessionCovered, sessionPlan } from './owner'

const usd = (minor: number) => ({ minor, currency: 'USD', value: (minor / 100).toFixed(2) })

function view(over: Partial<OwnerView> = {}): OwnerView {
  return {
    share_url: 'https://x/w/share',
    workshop: {
      title: 'Life drawing for beginners',
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
      host_name: 'Iris Calder',
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
    recommendation: { date_id: '', meets_minimum: false, tied: false, per_date: [], explanation: '', explanation_pending: false },
    enrollments: [],
    suggestions: [],
    payout: null,
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
      recommendation: { date_id: 'sat', meets_minimum: true, tied: false, per_date: [{ date_id: 'sat', compatible: ['p1', 'p2', 'p3', 'p4'], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v)).toEqual({ title: 'Sat, Nov 14 works for 4 — you need 3.', detail: 'Open reservations and people can reserve their spot with PayPal.', action: { kind: 'open', dateId: 'sat', day: 'Sat, Nov 14' } })
  })

  it('says tied instead of best so far when two dates share the most', () => {
    const v = view({
      availabilities: answered(['p1', 'p2', 'p3', 'p4']),
      recommendation: { date_id: 'sat', meets_minimum: false, tied: true, per_date: [{ date_id: 'sat', compatible: ['p1', 'p2'], blockers: null }, { date_id: 'sun', compatible: ['p3', 'p4'], blockers: null }], explanation: '', explanation_pending: false },
    })
    const step = nextStep(v)
    expect(step.title).toBe('Tied: Sat, Nov 14 comes first')
    expect(step.detail).toBe('2 can come on Sat, Nov 14, and as many on at least one later date. You need 3. Share the invite to reach more people.')
    expect(step.action).toEqual({ kind: 'share' })
  })

  it('still offers to open reservations on a tied date that reaches the minimum', () => {
    const v = view({
      availabilities: answered(['p1', 'p2', 'p3']),
      recommendation: { date_id: 'sat', meets_minimum: true, tied: true, per_date: [{ date_id: 'sat', compatible: ['p1', 'p2', 'p3'], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v)).toMatchObject({ title: 'Tied: Sat, Nov 14 comes first', action: { kind: 'open', dateId: 'sat', day: 'Sat, Nov 14' } })
  })

  it('names who still has to clarify', () => {
    const v = view({
      participants: [{ id: 'p5', display_name: 'Fay' }],
      availabilities: [...answered(['p1', 'p2']), { ...answered(['p5'])[0], display_name: 'Fay', needs_clarification: true }],
      recommendation: { date_id: 'sat', meets_minimum: false, tied: false, per_date: [{ date_id: 'sat', compatible: ['p1', 'p2'], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v)).toEqual({ title: 'Best so far: Sat, Nov 14, with 2 of the 3 needed.', detail: 'Fay’s message needs clarifying. Share the invite to reach more people.', action: { kind: 'share' } })
  })

  it('counts down while reservations are open', () => {
    const v = view({
      workshop: { ...view().workshop, status: 'FUNDING' },
      proposal: { id: 'p', version: 1, date_id: 'sat', price: usd(4500), minimum: 3, capacity: 6, opens_at: '', closes_at: '2026-11-13T23:00:00Z', starts_at: '2026-11-14T15:00:00Z', payments_open: true },
      tally: { ...view().tally, authorized: 1, paid: 1 },
    })
    expect(nextStep(v)).toEqual({ title: '2 of 3 reserved — 1 more and it’s on.', detail: 'Share the invite again.', action: { kind: 'share' } })
  })

  it('celebrates once it is on', () => {
    const v = view({ workshop: { ...view().workshop, status: 'CONFIRMED' }, tally: { ...view().tally, paid: 4 } })
    expect(nextStep(v)).toEqual({ title: '4 paid — $180 collected.', detail: 'Everyone who paid got their spot.', action: { kind: 'none' } })
  })
})

describe('decisionCounts', () => {
  const proposal = { id: 'p', version: 1, date_id: 'sat', price: usd(4500), minimum: 3, capacity: 6, opens_at: '', closes_at: '2026-11-13T23:00:00Z', starts_at: '2026-11-14T15:00:00Z', payments_open: true }

  it('still counts who joined before anyone answered', () => {
    expect(decisionCounts(view({ tally: { ...view().tally, interested: 2 } }))).toEqual({ bar: null, needed: 3, capacity: 6, joined: 2, charging: 0, failed: 0 })
  })

  it('counts who can come on the best date while the date is open', () => {
    const v = view({
      availabilities: answered(['p1', 'p2']),
      recommendation: { date_id: 'sat', meets_minimum: false, tied: false, per_date: [{ date_id: 'sat', compatible: ['p1', 'p2'], blockers: null }], explanation: '', explanation_pending: false },
      tally: { ...view().tally, interested: 6 },
    })
    expect(decisionCounts(v)).toEqual({ bar: { kind: 'interest', can: 2 }, needed: 3, capacity: 6, joined: 6, charging: 0, failed: 0 })
  })

  it('counts holds, payments, charges in progress and failures once reservations open, with the terms people agreed to', () => {
    const v = view({
      workshop: { ...view().workshop, status: 'FUNDING' },
      proposal: { ...proposal, minimum: 4, capacity: 8 },
      tally: { ...view().tally, interested: 7, authorized: 1, capturing: 1, pending: 1, paid: 1, failed: 1, seats_taken: 5, minimum: 4, capacity: 8 },
    })
    expect(decisionCounts(v)).toEqual({ bar: { kind: 'seats', paid: 1, reserved: 3, taken: 5 }, needed: 4, capacity: 8, joined: 7, charging: 2, failed: 1 })
  })

  it('drops the bar once cancelled but keeps the counts', () => {
    const v = view({ workshop: { ...view().workshop, status: 'CANCELLED' }, proposal: { ...proposal, payments_open: false }, tally: { ...view().tally, interested: 4, pending: 1 } })
    expect(decisionCounts(v)).toEqual({ bar: null, needed: 3, capacity: 6, joined: 4, charging: 1, failed: 0 })
  })
})

describe('countFacts', () => {
  it('lists who joined and the spots taken, and charges in progress or failures only when there are some', () => {
    const base = { needed: 3, capacity: 6, joined: 6, charging: 0, failed: 0 }
    expect(countFacts({ ...base, bar: { kind: 'interest', can: 2 } })).toEqual(['6 joined'])
    expect(countFacts({ ...base, bar: { kind: 'seats', paid: 1, reserved: 1, taken: 2 } })).toEqual(['6 joined', '2 of 6 spots taken'])
    expect(countFacts({ ...base, joined: 1, charging: 1, failed: 2, bar: { kind: 'seats', paid: 0, reserved: 1, taken: 1 } })).toEqual(['1 joined', '1 of 6 spots taken', '1 charge in progress', '2 failed'])
  })
})

describe('sessionPlan', () => {
  it('says how many payments cover the session costs while the date is still open', () => {
    expect(sessionPlan(view())).toBe('Your session costs $120 — 3 × $45 covers it before fees')
    expect(sessionPlan(view({ workshop: { ...view().workshop, break_even_seats: undefined, price: usd(2500) } }))).toBe('Your session costs $120 — 5 × $25 covers it before fees')
  })

  it('says when even a full class falls short', () => {
    expect(sessionPlan(view({ workshop: { ...view().workshop, capacity: 2, break_even_seats: 3 } }))).toBe('Your session costs $120 — even 2 × $45 falls short before fees')
  })

  it('is absent without a session cost, without a price, or once a date is chosen', () => {
    expect(sessionPlan(view({ workshop: { ...view().workshop, venue_cost: undefined } }))).toBeNull()
    expect(sessionPlan(view({ workshop: { ...view().workshop, price: usd(0), break_even_seats: undefined } }))).toBeNull()
    expect(sessionPlan(view({ workshop: { ...view().workshop, status: 'FUNDING' }, proposal: { id: 'p', version: 1, date_id: 'sat', price: usd(4500), minimum: 3, capacity: 6, opens_at: '', closes_at: '', starts_at: '', payments_open: true } }))).toBeNull()
  })
})

describe('sessionCovered', () => {
  const paidWithFees = (n: number, fee: number | null) =>
    Array.from({ length: n }, (_, i) => ({
      id: `e${i}`,
      status: 'PAID' as const,
      status_label: 'Paid',
      seat_expires_at: '',
      approval_cancelled_at: null,
      payment: { attempt_id: 'a', attempt_no: 1, order_id: null, authorization_id: null, capture_id: 'c', refund_id: null, order_status: null, authorization_status: null, capture_status: null, capture_issue: null, refund_status: null, ...(fee === null ? {} : { fee: usd(fee), net: usd(4500 - fee) }) },
    }))

  it('says how much of the session costs the payments cover before fees are known', () => {
    expect(sessionCovered(view({ tally: { ...view().tally, paid: 2 } }))).toEqual({ covered: 9000, cost: 12000, text: '$90 toward your $120 session costs (before fees)' })
    expect(sessionCovered(view({ tally: { ...view().tally, paid: 3 } }))?.text).toBe('Your $120 session costs are covered before fees')
  })

  it('shows the spare only after PayPal fees', () => {
    const v = view({ tally: { ...view().tally, paid: 3 }, enrollments: paidWithFees(3, 273) })
    expect(sessionCovered(v)).toEqual({ covered: 12681, cost: 12000, text: 'Your $120 session costs are covered, with $6.81 to spare after PayPal fees' })
  })

  it('hides the spare when the fees leave the costs uncovered', () => {
    const v = view({ tally: { ...view().tally, paid: 3 }, enrollments: paidWithFees(3, 600) })
    expect(sessionCovered(v)?.text).toBe('$117 toward your $120 session costs (after PayPal fees)')
  })

  it('falls back to the figure before fees when one paid seat has no fee yet', () => {
    const enrollments = [...paidWithFees(2, 273), ...paidWithFees(1, null)]
    expect(sessionCovered(view({ tally: { ...view().tally, paid: 3 }, enrollments }))?.text).toBe('Your $120 session costs are covered before fees')
  })

  it('leaves a refunded or flagged seat out of the spare', () => {
    const seats = paidWithFees(4, 273)
    const refunded = { ...seats[3], payment: { ...seats[3].payment, refund_id: 'r1' } }
    const flagged = { ...seats[2], needs_attention: true }
    const base = { tally: { ...view().tally, paid: 4 } }
    expect(sessionCovered(view({ ...base, enrollments: [...seats.slice(0, 3), refunded] }))?.text).toBe('Your $120 session costs are covered, with $6.81 to spare after PayPal fees')
    expect(sessionCovered(view({ ...base, enrollments: [seats[0], seats[1], flagged, seats[3]] }))?.text).toBe('Your $120 session costs are covered, with $6.81 to spare after PayPal fees')
    expect(sessionCovered(view({ ...base, enrollments: [seats[0], seats[1], flagged, refunded] }))?.text).toBe('$84.54 toward your $120 session costs (after PayPal fees)')
  })

  it('is absent without a session cost', () => {
    expect(sessionCovered(view({ workshop: { ...view().workshop, venue_cost: undefined } }))).toBeNull()
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

  it('refreshes quickly while Claude reads people pasted from the chat', () => {
    expect(ownerRefresh({ status: 'COLLECTING', resolution: 'OPEN', paymentsOpen: false, enrollments: [], explanationPending: false, chatReading: true })).toBe(2000)
    expect(ownerRefresh({ status: 'FUNDING', resolution: 'OPEN', paymentsOpen: true, enrollments: [], explanationPending: false, chatReading: true })).toBe(5000)
  })

  it('keeps refreshing while the organizer’s payout is on its way', () => {
    expect(ownerRefresh({ status: 'CONFIRMED', resolution: 'OPEN', paymentsOpen: false, enrollments: ['PAID'], explanationPending: false, payoutMoving: true })).toBe(5000)
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

  it('says the class falls short once reservations closed below the minimum, without repeating that they closed', () => {
    expect(nextStep(funding({ authorized: 1 }, false))).toEqual({
      title: 'Only 1 of the 3 needed reserved.',
      detail: 'The class is being cancelled: holds are released and any payment refunded.',
      action: { kind: 'none' },
    })
    expect(nextStep(funding({ authorized: 3 }, false))).toEqual({ title: '3 reserved — confirming payments.', detail: 'The result follows in a moment.', action: { kind: 'none' } })
  })

  it('puts a payment that needs a check first', () => {
    const v = view({ workshop: { ...view().workshop, status: 'CONFIRMED', resolution: 'NEEDS_ATTENTION' } })
    expect(nextStep(v)).toEqual({ title: 'PayPal left a payment unsettled.', detail: 'The check below says what happened and offers the fix.', action: { kind: 'none' } })
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
      detail: 'There is nothing else to do.',
      action: { kind: 'none' },
    })
    expect(cancelled('SETTLED', [])).toEqual({ title: 'Nothing to settle.', detail: 'Nobody had reserved, so nothing was held or charged.', action: { kind: 'none' } })
    expect(cancelled('RESOLVING', [])).toEqual({ title: 'Nothing to settle.', detail: 'Nobody had reserved, so nothing was held or charged.', action: { kind: 'none' } })
    expect(cancelled('RESOLVING', [e('SEAT_RESERVED'), e('RELEASED')])).toEqual({
      title: 'Nothing to settle.',
      detail: 'No hold or payment went through, so nothing was charged.',
      action: { kind: 'none' },
    })
  })

  it('says when nobody can make any date yet', () => {
    const v = view({
      availabilities: answered(['p1']),
      recommendation: { date_id: 'sat', meets_minimum: false, tied: false, per_date: [{ date_id: 'sat', compatible: [], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v).title).toBe('Nobody can make a date yet.')
  })

  it('names at most three people who need to clarify', () => {
    const many = ['A', 'B', 'C', 'D', 'E'].map((n) => ({ ...answered([n])[0], display_name: n, needs_clarification: true }))
    const v = view({ availabilities: many, recommendation: { date_id: 'sat', meets_minimum: false, tied: false, per_date: [{ date_id: 'sat', compatible: ['A'], blockers: null }], explanation: '', explanation_pending: false } })
    expect(nextStep(v).detail).toContain('A, B, C and 2 others need to clarify their message.')
  })

  it('tells two slots on the same day apart', () => {
    const v = view({
      dates: [
        { id: 'am', starts_at: '2026-11-14T15:00:00Z', ends_at: '2026-11-14T17:00:00Z', label: '' },
        { id: 'pm', starts_at: '2026-11-14T20:00:00Z', ends_at: '2026-11-14T22:00:00Z', label: '' },
      ],
      availabilities: answered(['p1', 'p2', 'p3']),
      recommendation: { date_id: 'pm', meets_minimum: true, tied: false, per_date: [{ date_id: 'pm', compatible: ['p1', 'p2', 'p3'], blockers: null }], explanation: '', explanation_pending: false },
    })
    expect(nextStep(v).action).toEqual({ kind: 'open', dateId: 'pm', day: 'Sat, Nov 14 at 3 PM' })
  })
})

describe('sessionCovered once cancelled', () => {
  it('stops claiming the session costs are covered', () => {
    expect(sessionCovered(view({ workshop: { ...view().workshop, status: 'CANCELLED' }, tally: { ...view().tally, paid: 3 } }))).toBeNull()
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
        tied: true,
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
      { id: 'sat', name: 'Sat, Nov 14', time: '10 AM–12 PM', can: 3, mark: 'Tied', canOpen: false },
      { id: 'sun', name: 'Sun, Nov 15', time: '10 AM–12 PM', can: 3, mark: null, canOpen: true },
    ])
  })

  it('marks no date when nobody can come on the leading one', () => {
    const v = ready()
    const empty = { ...v.recommendation, tied: false, per_date: v.recommendation.per_date?.map((d) => ({ ...d, compatible: [] })) ?? [] }
    expect(dateColumns({ ...v, recommendation: empty }).map((c) => c.mark)).toEqual([null, null])
  })

  it('marks the lead as Recommended only when no other date shares the most', () => {
    const v = ready()
    const alone = dateColumns({ ...v, recommendation: { ...v.recommendation, tied: false } })
    expect(alone.map((c) => c.mark)).toEqual(['Recommended', null])
  })

  it('marks no date before anyone answered', () => {
    expect(dateColumns(view({ recommendation: { ...view().recommendation, date_id: 'sat' } })).map((c) => c.mark)).toEqual([null, null])
  })

  it('marks the chosen date without repeating whether reservations are open', () => {
    const proposal = { date_id: 'sun', price: usd(4500), minimum: 3, capacity: 6, opens_at: '', closes_at: '2026-11-13T15:00:00Z', payments_open: true }
    const open = dateColumns({ ...ready(), workshop: { ...ready().workshop, status: 'FUNDING' }, proposal } as OwnerView)
    expect(open.map((c) => [c.mark, c.canOpen])).toEqual([
      [null, false],
      ['Chosen date', false],
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

describe('ownerRefresh while Claude reads a pasted chat', () => {
  const base = { status: 'COLLECTING' as const, resolution: 'OPEN' as const, paymentsOpen: false, enrollments: [], explanationPending: false }
  it('follows the readings as they land, then falls back once none is in flight', () => {
    expect(ownerRefresh({ ...base, chatReading: true })).toBe(2000)
    expect(ownerRefresh({ ...base, chatReading: false })).toBe(8000)
  })
})

describe('nextStep while Claude reads the chat', () => {
  it('says Claude is reading instead of asking to share the invite', () => {
    const reading = view({ participants: [{ id: 'p1', display_name: 'Priya Raman' }], availabilities: [], reading: { pending: 1, total: 1, active: true } })
    expect(nextStep(reading)).toEqual({ title: 'Claude is reading the chat.', detail: 'Each person shows up below as Claude reads what they said.', action: { kind: 'none' } })
    expect(nextStep({ ...reading, reading: { pending: 1, total: 1, active: false } }).title).toBe('Share the invite to get answers')
  })
})

describe('cellStatus', () => {
  const none = { type: 'none' as const, time: '' }
  const reading = (needsClarification: boolean) => ({
    per_date: [
      { candidate_date_id: 'sat', verdict: 'available' as const, condition: none, evidence: 'Saturday works' },
      { candidate_date_id: 'sun', verdict: 'unknown' as const, condition: none, evidence: '' },
    ],
    hard_constraints: [],
    preferences: [],
    ambiguities: needsClarification ? [{ question: 'The class runs 10 AM–12 PM. Can you make it?', why: 'vague' }] : [],
    needs_clarification: needsClarification,
  })

  it('shows the verdict of a clear reading', () => {
    const a = { ...answered(['p1'])[0], interpretation: reading(false) }
    expect(cellStatus(a, 'sat')).toEqual({ chip: { verdict: 'available', text: 'Can come' }, pill: null, readAs: null })
    expect(cellStatus(a, 'gone')).toEqual({ chip: null, pill: null, readAs: null })
  })

  it('says a reading that needs clarifying is not counted, whatever it read', () => {
    const a = { ...answered(['p1'])[0], interpretation: reading(true), needs_clarification: true }
    expect(cellStatus(a, 'sat')).toEqual({ chip: null, pill: 'Not counted — needs clarifying', readAs: 'Read as: Can come' })
    expect(cellStatus(a, 'sun')).toEqual({ chip: null, pill: 'Not counted — needs clarifying', readAs: null })
  })

  it('follows the corrected reading', () => {
    const a = { ...answered(['p1'])[0], interpretation: reading(true), needs_clarification: true, corrected: reading(false) }
    expect(cellStatus(a, 'sat').chip).toEqual({ verdict: 'available', text: 'Can come' })
  })
})

describe('cellNotes while a reading needs clarifying', () => {
  it('does not say whether a condition fits, since the person is not counted', () => {
    const interpretation = {
      per_date: [{ candidate_date_id: 'sat', verdict: 'conditional' as const, condition: { type: 'start_after' as const, time: '12:00' }, evidence: '' }],
      hard_constraints: [],
      preferences: [],
      ambiguities: [{ question: 'Can you make 10 AM?', why: '' }],
      needs_clarification: true,
    }
    const a = { ...answered(['p2'])[0], interpretation, needs_clarification: true }
    expect(cellNotes(a, 'sat', { date_id: 'sat', compatible: [], blockers: { p2: 'needs clarification' } }).fit).toBeNull()
  })
})

describe('foldName', () => {
  it('folds case, accents and spaces like the server', () => {
    const same = (a: string, b: string) => foldName(a) === foldName(b)
    expect(same('Lucia', 'Lucía')).toBe(true)
    expect(same('lucía', 'LUCÍA')).toBe(true)
    expect(same('José  María', 'jose maria')).toBe(true)
    expect(same(' Grace Liu ', 'grace liu')).toBe(true)
    expect(same('Ana', 'Anna')).toBe(false)
    expect(same('Ana', 'Ana Ruiz')).toBe(false)
  })
})

describe('possibleDuplicates', () => {
  it('marks everyone whose name another person shares', () => {
    const people = [
      { id: 'chat', display_name: 'Grace Liu' },
      { id: 'ben', display_name: 'Ben' },
      { id: 'link', display_name: 'grace liu' },
      { id: 'lucia', display_name: 'Lucía' },
      { id: 'lucia2', display_name: 'Lucia' },
      { id: 'ana', display_name: 'Ana Ruiz' },
    ]
    expect([...possibleDuplicates(people)].sort()).toEqual(['chat', 'link', 'lucia', 'lucia2'])
    expect(possibleDuplicates([{ id: 'ben', display_name: 'Ben' }]).size).toBe(0)
  })
})
