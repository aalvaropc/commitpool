import type { DemoWorkshop, OwnerView } from '../api'
import { tokenFromParticipantURL } from './participant'
import { readSession, writeSession } from './session'
import { formatWeekday } from './time'

const KEY = 'cp.demo'

export interface DemoParticipant {
  name: string
  path: string
}

function ownerToken(ownerURL: string): string | null {
  try {
    const m = /^\/owner\/([A-Za-z0-9_-]+)$/.exec(new URL(ownerURL).pathname)
    return m ? m[1] : null
  } catch {
    return null
  }
}

export function rememberDemo(ownerURL: string, participants: DemoWorkshop['participants']): void {
  const owner = ownerToken(ownerURL)
  if (!owner) return
  const people = participants.flatMap((p) => {
    const token = tokenFromParticipantURL(p.participant_url)
    return token ? [{ name: p.name, path: `/p/${token}` }] : []
  })
  writeSession(KEY, JSON.stringify({ owner, people }))
}

export function demoParticipants(owner: string): DemoParticipant[] {
  const raw = readSession(KEY)
  if (!raw) return []
  try {
    const saved = JSON.parse(raw) as { owner?: unknown; people?: unknown }
    if (saved.owner !== owner || !Array.isArray(saved.people)) return []
    return saved.people.filter((p): p is DemoParticipant => typeof p?.name === 'string' && typeof p?.path === 'string' && /^\/p\/[A-Za-z0-9_-]+$/.test(p.path))
  } catch {
    return []
  }
}

export interface DemoStep {
  text: string
  primary: string | null
}

const GRACE = 'Grace Liu'
const GRACE_REPLY = "No problem — I swapped Saturday. Sunday I'm still at the market."

export function demoStep(v: OwnerView, cast: string[]): DemoStep | null {
  const { workshop: w, recommendation: rec } = v
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
