import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import EnrollmentList from './EnrollmentList'

const usd = (minor: number) => ({ minor, currency: 'USD', value: (minor / 100).toFixed(2) })

function view(demoMode: boolean, withFees: boolean): OwnerView {
  return {
    workshop: { timezone: 'America/New_York' },
    proposal: { price: usd(4500), payments_open: false, closes_at: '2026-11-13T15:00:00Z' },
    demo: { demo_mode: demoMode, sandbox_mocks: false },
    enrollments: [
      {
        id: 'e1',
        display_name: 'Priya',
        status: 'PAID',
        status_label: 'Paid',
        seat_expires_at: '',
        payment: {
          attempt_id: 'a',
          attempt_no: 1,
          order_id: 'o',
          authorization_id: 'x',
          capture_id: 'c',
          refund_id: null,
          order_status: null,
          authorization_status: null,
          capture_status: null,
          capture_issue: null,
          refund_status: null,
          ...(withFees ? { fee: usd(273), net: usd(4227) } : {}),
        },
      },
    ],
  } as unknown as OwnerView
}

const note = 'The Sandbox merchant is registered outside the US, so PayPal charges its international rate.'

describe('EnrollmentList', () => {
  it('explains the fee rate once fees are shown on a Sandbox demo', () => {
    expect(renderToStaticMarkup(<EnrollmentList token="t" view={view(true, true)} onChange={async () => undefined} />)).toContain(note)
  })

  it('does not claim a Sandbox reason outside demo mode or without fees', () => {
    expect(renderToStaticMarkup(<EnrollmentList token="t" view={view(false, true)} onChange={async () => undefined} />)).not.toContain(note)
    expect(renderToStaticMarkup(<EnrollmentList token="t" view={view(true, false)} onChange={async () => undefined} />)).not.toContain(note)
  })
})
