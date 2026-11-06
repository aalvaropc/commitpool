import type { DemoWorkshop } from '../api'
import { tokenFromParticipantURL } from './participant'
import { readSession, writeSession } from './session'

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
