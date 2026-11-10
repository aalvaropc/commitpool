import { describe, expect, it } from 'vitest'
import type { ApprovalView } from '../api'
import { approvalMessage, approvalOutcome, approvalTerms, approveFailure, confirmDeadline, refusedState } from './approval'

const view: ApprovalView = {
  approve_url: 'https://commitpool.example/owner/t/approve/a',
  approval: {
    id: 'a',
    state: 'pending',
    date_id: 'd',
    date_label: 'Sat, Nov 14',
    starts_at: '2026-11-14T15:00:00Z',
    ends_at: '2026-11-14T17:00:00Z',
    price: { minor: 4500, currency: 'USD', value: '45.00' },
    minimum: 3,
    capacity: 8,
    payment_window_min: 1440,
    expires_at: '2026-11-08T16:30:00Z',
    approved_at: null,
    used_at: null,
  },
  workshop: {
    title: 'Life drawing for beginners',
    description: '',
    duration_min: 120,
    timezone: 'America/New_York',
    minimum: 3,
    capacity: 8,
    price: { minor: 4500, currency: 'USD', value: '45.00' },
    currency: 'USD',
    payment_window_min: 1440,
    status: 'COLLECTING',
    resolution: 'OPEN',
    host_name: 'Iris',
    location: '',
  },
}

describe('approvalOutcome', () => {
  it('asks while pending and shows every other state as a result', () => {
    expect(approvalOutcome('pending', false)).toBeNull()
    expect(approvalOutcome('pending', true)).toBe('declined')
    for (const state of ['approved', 'used', 'expired', 'changed', 'closed'] as const) {
      expect(approvalOutcome(state, false)).toBe(state)
      expect(approvalOutcome(state, true)).toBe(state)
    }
  })
})

describe('approvalMessage', () => {
  it('tells the organizer the assistant can open reservations once approved', () => {
    expect(approvalMessage('approved').title).toBe('Approved. Your assistant can now open reservations.')
    expect(approvalMessage('approved').tone).toBe('paid')
  })

  it('sends the organizer back to the assistant when the request no longer holds', () => {
    expect(approvalMessage('expired').body).toContain('prepare the terms again')
    expect(approvalMessage('changed').body).toContain('prepare the terms again')
    expect(approvalMessage('declined').body).toContain('cannot open reservations unless you approve')
  })
})

describe('refusedState', () => {
  it('maps a refused approval to the state it reveals', () => {
    expect(refusedState('approval_expired')).toBe('expired')
    expect(refusedState('approval_used')).toBe('used')
    expect(refusedState('terms_changed')).toBe('changed')
    expect(refusedState('approval_closed')).toBe('closed')
    expect(refusedState('approval_needs_page')).toBeNull()
    expect(refusedState('network_error')).toBeNull()
  })
})

describe('approvalTerms', () => {
  it('lists the approved terms in the workshop time zone', () => {
    expect(approvalTerms(view).map((t) => [t.label, t.value])).toEqual([
      ['Date', 'Sat, Nov 14 · 10 AM–12 PM'],
      ['Price', '$45'],
      ['Duration', '2 hours'],
      ['Time to reserve', '24 hours'],
      ['Needed to happen', '3'],
      ['Spots', '8'],
      ['Paid out at the start to', 'No PayPal email given'],
    ])
  })

  it('names the PayPal email the money is paid out to', () => {
    const withPayout = { ...view, approval: { ...view.approval, payout_email: 'iris@example.com' } }
    expect(approvalTerms(withPayout).at(-1)).toEqual({ label: 'Paid out at the start to', value: 'iris@example.com', kind: 'text', span: true })
  })
})

describe('confirmDeadline', () => {
  it('tells the organizer until when the assistant can open reservations', () => {
    const approved = { ...view, approval: { ...view.approval, state: 'approved' as const, expires_at: '2026-11-08T17:00:00Z' } }
    expect(confirmDeadline(approved)).toBe('Your assistant has until Sun, Nov 8 · 12 PM to open them; after that it needs your approval again.')
  })
})

describe('approveFailure', () => {
  it('refreshes the page once when the approval needs the page again', () => {
    expect(approveFailure(403, 'approval_needs_page', 'x', false)).toEqual({ refresh: true, message: 'This page had to refresh before approving. Tap Approve again.' })
  })

  it('stops refreshing when the browser still cannot approve', () => {
    const second = approveFailure(403, 'approval_needs_page', 'x', true)
    expect(second.refresh).toBe(false)
    expect(second.message).toContain('This browser could not confirm the tap came from this page')
  })

  it('shows any other failure as it came', () => {
    expect(approveFailure(0, 'network_error', 'CommitPool could not be reached.', false)).toEqual({ refresh: false, message: 'CommitPool could not be reached.' })
  })
})
