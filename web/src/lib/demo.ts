import type { OwnerView } from '../api'
import { tokenFromParticipantURL } from './participant'
import { freeToReserveAgain } from './status'
import { formatWeekday } from './time'

export interface DemoParticipant {
  name: string
  path: string
}

export function demoParticipants(view: Pick<OwnerView, 'act_as'>): DemoParticipant[] {
  return (view.act_as ?? []).flatMap((p) => {
    const token = tokenFromParticipantURL(p.participant_url)
    return token ? [{ name: p.name, path: `/p/${token}` }] : []
  })
}

export interface DemoStep {
  text: string
  primary: string | null
}

const GRACE = 'Grace Liu'
const GRACE_REPLY = "No problem — I swapped Saturday. Sunday I'm still at the market."

function reserveStep(v: OwnerView, cast: string[]): DemoStep | null {
  const { tally: t, proposal: p } = v
  if (!p?.payments_open || t.paid + t.authorized + t.capturing + t.pending >= t.minimum) return null
  const taken = new Set(v.enrollments.filter((e) => !freeToReserveAgain(e)).map((e) => e.participant_id))
  const compatible = new Set(v.recommendation.per_date?.find((d) => d.date_id === p.date_id)?.compatible ?? [])
  const next = v.participants.find((x) => compatible.has(x.id) && !taken.has(x.id) && cast.includes(x.display_name))
  if (!next) return null
  return { text: `Reserve as someone who can come: act as ${next.display_name} and reserve with PayPal.`, primary: next.display_name }
}

export function demoStep(v: OwnerView, cast: string[]): DemoStep | null {
  const { workshop: w, recommendation: rec } = v
  if (w.status === 'FUNDING') return reserveStep(v, cast)
  if (w.status !== 'COLLECTING' || v.proposal || !rec.date_id) return null
  const members = v.participants.filter((p) => cast.includes(p.display_name))
  const answers = new Map(v.availabilities.map((a) => [a.participant_id, a]))
  if (members.length === 0 || members.some((p) => !answers.has(p.id))) return null
  if (rec.meets_minimum) return { text: 'Open reservations, then reserve as someone who can come.', primary: null }
  const best = v.dates.find((d) => d.id === rec.date_id)
  if (!best) return null
  const compatible = rec.per_date?.find((d) => d.date_id === rec.date_id)?.compatible ?? []
  const host = w.host_name?.trim().split(/\s+/)[0] || 'the organizer'
  const short = `${formatWeekday(best.starts_at, w.timezone)} has ${compatible.length} of the ${w.minimum} ${host} needs.`
  const unclear = members.find((p) => {
    const a = answers.get(p.id)
    return a ? (a.corrected ?? a.interpretation).needs_clarification : false
  })
  if (unclear) {
    const name = unclear.display_name
    const example = name === GRACE ? ` — for example: “${GRACE_REPLY}”` : '.'
    return { text: `${short} Act as ${name} and answer Claude’s question${example}`, primary: name }
  }
  const out = members.find((p) => !compatible.includes(p.id))
  if (out) return { text: `${short} Act as ${out.display_name} and answer as them, in your own words.`, primary: out.display_name }
  return { text: short, primary: null }
}
