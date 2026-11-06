import type { OwnerView } from '../api'
import { formatMoney } from './money'
import { shouldPollOwner } from './status'
import { formatDay, formatMoment } from './time'

export type NextAction = { kind: 'share' } | { kind: 'open'; dateId: string; day: string } | { kind: 'none' }

export interface NextStep {
  title: string
  detail: string
  action: NextAction
}

const usd = (minor: number, currency: string) => formatMoney({ minor, currency })

function clarifying(v: OwnerView): string {
  const names = v.availabilities.filter((a) => a.needs_clarification).map((a) => a.display_name ?? 'Someone')
  if (names.length === 0) return ''
  const who = names.length === 1 ? `${names[0]}’s message needs` : `${names.slice(0, -1).join(', ')} and ${names.at(-1)}’s messages need`
  return `${who} clarifying. `
}

function collecting(v: OwnerView): NextStep {
  const { recommendation: rec, workshop: w } = v
  if (v.availabilities.length === 0 || !rec.date_id) {
    return { title: 'Share the invite to get answers', detail: 'Answers show up here as people reply.', action: { kind: 'share' } }
  }
  const date = v.dates.find((d) => d.id === rec.date_id)
  const day = date ? formatDay(date.starts_at, w.timezone) : 'The recommended date'
  const can = rec.per_date?.find((d) => d.date_id === rec.date_id)?.compatible?.length ?? 0
  if (rec.meets_minimum) {
    return {
      title: `${day} works for ${can} — you need ${w.minimum}.`,
      detail: `${clarifying(v)}Open reservations and people can reserve their spot with PayPal.`,
      action: { kind: 'open', dateId: rec.date_id, day },
    }
  }
  return {
    title: `Best so far: ${day}, with ${can} of the ${w.minimum} needed.`,
    detail: `${clarifying(v)}Share the invite to reach more people.`,
    action: { kind: 'share' },
  }
}

export function nextStep(v: OwnerView): NextStep {
  const { workshop: w, tally: t, proposal: p } = v
  switch (w.status) {
    case 'COLLECTING':
      return collecting(v)
    case 'FUNDING': {
      const committed = t.paid + t.authorized
      const more = Math.max(t.minimum - committed, 0)
      const closes = p ? `reservations close ${formatMoment(p.closes_at, w.timezone)}` : 'reservations are open'
      return {
        title: more > 0 ? `${committed} of ${t.minimum} reserved — ${more} more and it’s on.` : `${committed} reserved — confirming payments.`,
        detail: p?.payments_open ? `Share the invite again; ${closes}.` : 'Reservations have closed; the outcome follows in a moment.',
        action: p?.payments_open ? { kind: 'share' } : { kind: 'none' },
      }
    }
    case 'CONFIRMED': {
      const price = p?.price ?? w.price
      return {
        title: `It’s on — ${t.paid} paid, ${usd(t.paid * price.minor, price.currency)} collected.`,
        detail: p?.payments_open ? 'Spots still open can be reserved until reservations close.' : 'Everyone who paid got their spot.',
        action: p?.payments_open ? { kind: 'share' } : { kind: 'none' },
      }
    }
    case 'CANCELLED':
      return { title: 'This workshop is cancelled.', detail: 'Every hold is released and every payment refunded; the status above follows it to the end.', action: { kind: 'none' } }
  }
}

export interface Coverage {
  covered: number
  cost: number
  text: string
}

export function studioCovered(v: OwnerView): Coverage | null {
  const cost = v.workshop.venue_cost
  if (!cost) return null
  const price = v.proposal?.price ?? v.workshop.price
  const covered = v.tally.paid * price.minor
  const money = (minor: number) => usd(minor, cost.currency)
  const text = covered >= cost.minor ? `Your ${money(cost.minor)} studio is covered, with ${money(covered - cost.minor)} to spare` : `${money(covered)} of your ${money(cost.minor)} studio covered`
  return { covered, cost: cost.minor, text }
}

interface RefreshInput {
  status: OwnerView['workshop']['status']
  resolution: OwnerView['workshop']['resolution']
  paymentsOpen: boolean
  enrollments: OwnerView['enrollments'][number]['status'][]
  explanationPending: boolean
}

export function ownerRefresh(s: RefreshInput): number | null {
  if (s.explanationPending) return 3000
  if (shouldPollOwner(s)) return 5000
  if (s.status === 'COLLECTING') return 8000
  return null
}
