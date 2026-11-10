import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Extension, OwnerView, Proposal, Suggestion } from '../api'
import { formatMoment } from '../lib/time'
import ClaudeSuggests from './ClaudeSuggests'
import DeadlineMoved from './DeadlineMoved'
import { ExtendDialog } from './RescueCard'

const tz = 'America/New_York'
const from = '2026-11-03T09:00:00Z'
const to = '2026-11-05T09:00:00Z'

const text = (html: string) => html.replace(/<[^>]+>/g, ' ').replaceAll('&#x27;', "'").replaceAll('&amp;', '&').replace(/\s+/g, ' ')

const rescue: Suggestion = {
  kind: 'rescue',
  reason: 'Reservations close Tue, Nov 3 at 4 AM and 1 more is needed. Extending the deadline once gives people more time.',
  message: 'Life drawing on Sat, Nov 14 has more time: reservations now stay open until Thu, Nov 5 at 4 AM.\nhttps://example.test/w/INVITE',
  pending: false,
}

const offer: Extension = { available: true, from, to, reason: 'You can move the deadline once.' }

const proposal: Proposal = {
  id: 'p1',
  version: 1,
  date_id: 'sat',
  price: { minor: 1000, currency: 'USD', value: '10.00' },
  minimum: 3,
  capacity: 8,
  opens_at: '2026-11-02T09:00:00Z',
  closes_at: from,
  starts_at: '2026-11-14T15:00:00Z',
  payments_open: true,
  extended_from: null,
  latest_closes_at: to,
}

describe('the rescue card', () => {
  it('offers the one extension, with the reason the server wrote and Claude’s message for afterwards', () => {
    const plain = text(renderToStaticMarkup(<ClaudeSuggests suggestions={[rescue]} rescue={{ extension: offer, timeZone: tz, onExtend: () => {} }} />))
    expect(plain).toContain(rescue.reason)
    expect(plain).toContain(`Extend to ${formatMoment(to, tz)}`)
    expect(plain).toContain('Claude’s message for the group, once you extend')
    expect(plain).toContain('reservations now stay open until Thu, Nov 5 at 4 AM.')
    expect(plain).toContain('Copy message for the group')
  })

  it('shows no button once the extension is no longer on offer', () => {
    const refused: Extension = { available: false, from, refusal: 'charging_started', reason: 'Charging has started.' }
    const plain = text(renderToStaticMarkup(<ClaudeSuggests suggestions={[rescue]} rescue={{ extension: refused, timeZone: tz, onExtend: () => {} }} />))
    expect(plain).not.toContain('Extend to')
    expect(plain).toContain(rescue.reason)
  })
})

describe('the extend dialog', () => {
  it('says it happens once, everyone is told, holds stay and anyone can still leave', () => {
    const view = { proposal, extension: offer, workshop: { timezone: tz } } as unknown as OwnerView
    const plain = text(renderToStaticMarkup(<ExtendDialog token="t" view={view} open onClose={() => {}} onExtended={async () => {}} />))
    expect(plain).toContain(`Extend the deadline to ${formatMoment(to, tz)}?`)
    expect(plain).toContain(`Reservations close ${formatMoment(from, tz)}.`)
    for (const line of [
      'You can do this only once.',
      'Everyone who left an email is told the new deadline.',
      'Holds stay in place: nobody reserves or pays again.',
      'Anyone can still leave and release their hold until charging starts.',
    ])
      expect(plain).toContain(line)
    expect(plain).toContain(`Extend to ${formatMoment(to, tz)}`)
  })
})

describe('the deadline banner', () => {
  it('tells a participant the deadline moved, from when to when', () => {
    const moved = { ...proposal, extended_from: from, closes_at: to }
    const plain = text(renderToStaticMarkup(<DeadlineMoved proposal={moved} status="FUNDING" timeZone={tz} audience="participant" />))
    expect(plain).toContain(`Deadline extended from ${formatMoment(from, tz)} to ${formatMoment(to, tz)}`)
    expect(plain).toContain('you can still leave until charging starts')
  })

  it('stays away until the deadline moves, and once the class is decided', () => {
    expect(renderToStaticMarkup(<DeadlineMoved proposal={proposal} status="FUNDING" timeZone={tz} audience="participant" />)).toBe('')
    const moved = { ...proposal, extended_from: from, closes_at: to }
    expect(renderToStaticMarkup(<DeadlineMoved proposal={moved} status="CONFIRMED" timeZone={tz} audience="owner" />)).toBe('')
  })
})
