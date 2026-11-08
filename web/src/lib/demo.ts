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

export function demoStep(v: OwnerView): DemoStep | null {
  const { workshop: w, recommendation: rec } = v
  if (w.status !== 'COLLECTING' || v.proposal || !rec.date_id || v.availabilities.length < v.participants.length) return null
  if (rec.meets_minimum) return { text: 'Open reservations, then reserve as someone who can come.', primary: null }
  const ask = v.suggestions.find((s) => s.kind === 'clarify' && s.name)
  const best = v.dates.find((d) => d.id === rec.date_id)
  if (!ask?.name || !best) return null
  const day = formatWeekday(best.starts_at, w.timezone)
  const other = v.dates.find((d) => d.id !== rec.date_id)
  const can = rec.per_date?.find((d) => d.date_id === rec.date_id)?.compatible?.length ?? 0
  const host = w.host_name?.trim().split(/\s+/)[0] || 'the organizer'
  const example = other ? `No problem — I swapped ${day}. ${formatWeekday(other.starts_at, w.timezone)} I’m still at the market.` : `No problem — I swapped ${day}.`
  return { text: `${day} has ${can} of the ${w.minimum} ${host} needs. Act as ${ask.name} and answer Claude’s question — for example: “${example}”`, primary: ask.name }
}
