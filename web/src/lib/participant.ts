export const PARTICIPANT_KEY = 'cp.participant'

export function rememberParticipant(token: string): void {
  try {
    globalThis.sessionStorage?.setItem(PARTICIPANT_KEY, token)
  } catch {
  }
}

export function storedParticipant(): string | null {
  try {
    return globalThis.sessionStorage?.getItem(PARTICIPANT_KEY) ?? null
  } catch {
    return null
  }
}

const JOIN_KEY = 'cp.participant.join'

export function rememberJoin(share: string, token: string, name: string): void {
  rememberParticipant(token)
  try {
    globalThis.sessionStorage?.setItem(JOIN_KEY, JSON.stringify({ share, token, name }))
  } catch {
  }
}

export function joinedAs(share: string): { token: string; name: string } | null {
  try {
    const raw = globalThis.sessionStorage?.getItem(JOIN_KEY)
    if (!raw) return null
    const j = JSON.parse(raw) as { share?: unknown; token?: unknown; name?: unknown }
    if (j.share !== share || typeof j.token !== 'string' || typeof j.name !== 'string') return null
    return j.token === storedParticipant() ? { token: j.token, name: j.name } : null
  } catch {
    return null
  }
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
