import type { OwnerView } from '../api'
import { tokenFromParticipantURL } from './participant'
import { canStartClass } from './payout'
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

export type DemoAction = 'close' | 'start'

export interface DemoLink {
  label: string
  to: string
}

export interface DemoStep {
  text: string
  primary: string | null
  action?: DemoAction
  link?: DemoLink
}

export const CHAT_STEP = 'This is Iris’s group chat. Press Read the chat, then Add 6 people.'
export const OWN_CHAT_STEP = 'Press Read the chat, then Add the people.'
export const REPLY_STEP = 'Copy Claude’s reply for the group, then act as Grace to answer her question.'
export const ANSWERED_STEP = 'Grace answered in the chat. Paste the updated chat, or act as Grace to answer on her page.'
export const UPDATE_STEP = 'Grace’s answer is now the last line of Iris’s chat. Press Read the chat, then Read new messages from 1 person.'
export const DONE_STEP =
  'That’s the whole loop: the chat read, the date found, the holds charged and Iris paid. To see the rescue, press Try it as Iris again, reserve as two people, then Skip to the last 2 hours.'
const TRY_AGAIN: DemoLink = { label: 'Try it as Iris', to: '/' }

export function chatLead(text: string, demoChat: string | null | undefined): string | null {
  if (!demoChat) return null
  return text.trim() === demoChat.trim() ? CHAT_STEP : OWN_CHAT_STEP
}

export function updateLead(text: string, demoUpdate: string | null | undefined): string | null {
  if (!demoUpdate) return null
  return text.trim() === demoUpdate.trim() ? UPDATE_STEP : null
}

const firstName = (name: string | undefined) => name?.trim().split(/\s+/)[0] || 'the organizer'

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

export function demoControl(v: OwnerView): DemoAction | null {
  if (v.demo.demo_mode && v.proposal?.payments_open) return 'close'
  return canStartClass(v) ? 'start' : null
}

export function demoAction(v: OwnerView): DemoAction | null {
  if (!v.workshop.demo || !v.demo?.demo_mode) return null
  if (v.workshop.status !== 'CONFIRMED') return null
  return demoControl(v)
}

function paidOut(v: OwnerView): boolean {
  return Boolean(v.workshop.demo && v.demo?.demo_mode) && v.workshop.status === 'CONFIRMED' && v.payout?.state === 'paid'
}

function finishStep(action: DemoAction, v: OwnerView): DemoStep {
  if (action === 'close') return { text: 'Close reservations now to end the reservation window.', primary: null, action }
  return { text: `Start the class now. The demo clock jumps to the start and CommitPool pays ${firstName(v.workshop.host_name)}.`, primary: null, action }
}

export function demoStep(v: OwnerView, cast: string[]): DemoStep | null {
  const { workshop: w, recommendation: rec } = v
  const action = demoAction(v)
  if (action) return finishStep(action, v)
  if (paidOut(v)) return { text: DONE_STEP, primary: null, link: TRY_AGAIN }
  if (w.status === 'FUNDING') return reserveStep(v, cast)
  if (w.status !== 'COLLECTING' || v.proposal) return null
  const members = v.participants.filter((p) => cast.includes(p.display_name))
  const answers = new Map(v.availabilities.map((a) => [a.participant_id, a]))
  const waiting = members.some((p) => !answers.has(p.id))
  if (members.length > 0 && v.reading?.active) return { text: `Claude is reading each person’s messages against ${firstName(w.host_name)}’s dates.`, primary: null }
  if (!rec.date_id || members.length === 0 || waiting) return null
  if (rec.meets_minimum) return { text: 'Open reservations, then reserve as someone who can come.', primary: null }
  const best = v.dates.find((d) => d.id === rec.date_id)
  if (!best) return null
  const compatible = rec.per_date?.find((d) => d.date_id === rec.date_id)?.compatible ?? []
  const host = firstName(w.host_name)
  const short = `${formatWeekday(best.starts_at, w.timezone)} has ${compatible.length} of the ${w.minimum} ${host} needs.`
  const unclear = members.find((p) => {
    const a = answers.get(p.id)
    return a ? (a.corrected ?? a.interpretation).needs_clarification : false
  })
  if (unclear) {
    const name = unclear.display_name
    const replied = v.suggestions.some((s) => s.kind === 'group_reply')
    if (name === GRACE && v.demo_update) {
      return { text: `${replied ? 'Copy Claude’s reply for the group.' : short} ${ANSWERED_STEP} She wrote: “${GRACE_REPLY}”`, primary: name }
    }
    if (name === GRACE && replied) {
      return { text: `${REPLY_STEP} For example: “${GRACE_REPLY}”`, primary: name }
    }
    const example = name === GRACE ? ` — for example: “${GRACE_REPLY}”` : '.'
    return { text: `${short} Act as ${name} and answer Claude’s question${example}`, primary: name }
  }
  const out = members.find((p) => !compatible.includes(p.id))
  if (out) return { text: `${short} Act as ${out.display_name} and answer as them, in your own words.`, primary: out.display_name }
  return { text: short, primary: null }
}
