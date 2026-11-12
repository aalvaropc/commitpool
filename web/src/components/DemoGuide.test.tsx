import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import type { OwnerView } from '../api'
import DemoGuide from './DemoGuide'

const view = (over: Record<string, unknown> = {}) =>
  ({
    workshop: { status: 'CONFIRMED', timezone: 'America/New_York', minimum: 3, host_name: 'Iris Calder', demo: true },
    demo: { demo_mode: true, sandbox_mocks: false },
    proposal: { date_id: 'sat', payments_open: true, minimum: 3 },
    payout: { state: 'scheduled', locked: false, estimated: true, seats: 3 },
    act_as: [{ name: 'Priya Raman', participant_url: 'https://cp.test/p/PRI' }],
    dates: [],
    participants: [],
    availabilities: [],
    enrollments: [],
    suggestions: [],
    recommendation: { date_id: null, meets_minimum: true, tied: false, per_date: [], explanation: '', explanation_pending: false },
    ...over,
  }) as unknown as OwnerView

const render = (v: OwnerView) =>
  renderToStaticMarkup(
    <MemoryRouter>
      <DemoGuide token="OWN" view={v} onChange={async () => {}} />
    </MemoryRouter>,
  )

describe('DemoGuide', () => {
  it('keeps Close reservations now outside the collapsible steps, so a collapsed guide still shows it', () => {
    const html = render(view())
    expect(html).toContain('Close reservations now')
    expect(html.indexOf('Close reservations now')).toBeLessThan(html.indexOf('<details'))
  })

  it('keeps Start the class now outside the collapsible steps once reservations closed', () => {
    const html = render(view({ proposal: { date_id: 'sat', payments_open: false, minimum: 3 } }))
    expect(html).toContain('Start the class now')
    expect(html.indexOf('Start the class now')).toBeLessThan(html.indexOf('<details'))
    expect(html).not.toContain('Close reservations now')
  })

  it('opens the steps when the guide arrives with an action to take', () => {
    expect(render(view())).toMatch(/<details[^>]* open/)
  })

  it('shows no button once the payout went out, and links Try it as Iris back to the start', () => {
    const html = render(view({ proposal: { date_id: 'sat', payments_open: false, minimum: 3 }, payout: { state: 'paid', locked: true, estimated: false, seats: 3 } }))
    expect(html).not.toContain('Start the class now')
    expect(html).not.toContain('Close reservations now')
    expect(html).toMatch(/<details[^>]* open/)
    expect(html).toContain('That’s the whole loop: the chat read, the date found, the holds charged and Iris paid. To see the rescue, press <a')
    expect(html).toMatch(/<a [^>]*href="\/"[^>]*>Try it as Iris<\/a> again, reserve as two people, then Skip to the last 2 hours\./)
  })
})
