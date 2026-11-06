import { readSession, writeSession } from './session'

export const PARTICIPANT_KEY = 'cp.participant'

export function rememberParticipant(token: string): void {
  writeSession(PARTICIPANT_KEY, token)
}

export function storedParticipant(): string | null {
  return readSession(PARTICIPANT_KEY)
}

const JOIN_KEY = 'cp.participant.join'

export function rememberJoin(share: string, token: string, name: string): void {
  rememberParticipant(token)
  writeSession(JOIN_KEY, JSON.stringify({ share, token, name }))
}

export function joinedAs(share: string): { token: string; name: string } | null {
  const raw = readSession(JOIN_KEY)
  if (!raw) return null
  let j: { share?: unknown; token?: unknown; name?: unknown }
  try {
    j = JSON.parse(raw) as typeof j
  } catch {
    return null
  }
  if (j.share !== share || typeof j.token !== 'string' || typeof j.name !== 'string') return null
  return j.token === storedParticipant() ? { token: j.token, name: j.name } : null
}

export function tokenFromParticipantURL(url: string): string | null {
  let path: string
  try {
    path = new URL(url).pathname
  } catch {
    return null
  }
  const m = /^\/p\/([A-Za-z0-9_-]+)$/.exec(path)
  return m ? m[1] : null
}
