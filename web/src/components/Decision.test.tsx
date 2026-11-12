import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import Decision, { StatusLine } from './Decision'

describe('StatusLine', () => {
  it('says in one line that nobody answered yet and links to the invite', () => {
    const html = renderToStaticMarkup(<StatusLine />)
    expect(html.replace(/<[^>]+>/g, '')).toBe('No answers yet. Paste the group chat, or share the invite.')
    expect(html).toMatch(/aria-label="Workshop status"/)
    expect(html).toMatch(/role="status"/)
    expect(html).toMatch(/href="#share-url"/)
  })
  it('drops the paste when a demo already read its chat', () => {
    expect(renderToStaticMarkup(<StatusLine canPaste={false} />).replace(/<[^>]+>/g, '')).toBe('No answers yet. Share the invite.')
  })
})

describe('Decision after the payout', () => {
  const usd = (minor: number) => ({ minor, currency: 'USD', value: (minor / 100).toFixed(2) })
  const view = (payout: Record<string, unknown>) =>
    ({
      workshop: { status: 'CONFIRMED', resolution: 'NONE', timezone: 'America/New_York', host_name: 'Iris Calder', minimum: 3, capacity: 8, price: usd(1000) },
      proposal: { date_id: 'sat', payments_open: false, minimum: 3, capacity: 8, price: usd(1000), closes_at: '2026-11-20T15:00:00Z' },
      dates: [{ id: 'sat', starts_at: '2026-11-21T15:00:00Z', ends_at: '2026-11-21T17:00:00Z', label: '' }],
      tally: { minimum: 3, paid: 3, authorized: 0, capturing: 0, pending: 0, seats_taken: 3 },
      enrollments: [],
      participants: [],
      availabilities: [],
      recommendation: { date_id: 'sat', meets_minimum: true, tied: false, per_date: [], explanation: '', explanation_pending: false },
      payout,
    }) as unknown as OwnerView
  const step = { title: '3 paid', detail: '', action: { kind: 'none' as const } }
  const text = (html: string) => html.replace(/<[^>]+>/g, '|').replace(/\|+/g, '|')

  it('the status banner shows Class started after the payout', () => {
    const paid = { state: 'paid', locked: true, estimated: false, seats: 3, amount: usd(2748), payout_batch_id: 'BATCH42', batch_status: 'SUCCESS' }
    const html = text(renderToStaticMarkup(<Decision view={view(paid)} step={step} onOpen={() => {}} />))
    expect(html).toContain('|Class started · $27.48 paid out to Iris|')
    expect(html).toContain('|PayPal batch BATCH42 · SUCCESS|')
  })

  it('keeps It’s on while the payout is still ahead', () => {
    const html = text(renderToStaticMarkup(<Decision view={view({ state: 'scheduled', locked: false, estimated: true, seats: 3 })} step={step} onOpen={() => {}} />))
    expect(html).toContain('|It’s on|')
    expect(html).not.toContain('Class started')
  })
})
