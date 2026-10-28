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
