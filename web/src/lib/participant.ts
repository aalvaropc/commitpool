// The participant token lives only in the URL /p/:token and in this tab's
// sessionStorage, so the PayPal return page can link back without carrying it.

export const PARTICIPANT_KEY = 'cp.participant'

export function rememberParticipant(token: string): void {
  try {
    globalThis.sessionStorage?.setItem(PARTICIPANT_KEY, token)
  } catch {
    // Storage can be blocked; the personal link still works on its own.
  }
}

export function storedParticipant(): string | null {
  try {
    return globalThis.sessionStorage?.getItem(PARTICIPANT_KEY) ?? null
  } catch {
    return null
  }
}

// Who joined which workshop in this tab, so the share page can offer to
// continue; it counts only while that token is still this tab's participant.
const JOIN_KEY = 'cp.participant.join'

export function rememberJoin(share: string, token: string, name: string): void {
  rememberParticipant(token)
  try {
    globalThis.sessionStorage?.setItem(JOIN_KEY, JSON.stringify({ share, token, name }))
  } catch {
    // As above: the personal link is enough.
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

/** The token in a participant URL from the server, or null for any other address. */
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
