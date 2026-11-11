export const REPO_URL = 'https://github.com/aalvaropc/commitpool'

export function safeHttps(url: string | undefined): string | null {
  if (!url) return null
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' && parsed.hostname !== '' && parsed.username === '' && parsed.password === '' ? parsed.href : null
  } catch {
    return null
  }
}
