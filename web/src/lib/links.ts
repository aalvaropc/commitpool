export const REPO_URL = 'https://github.com/aalvaropc/commitpool'

export function safeHttps(url: string | undefined): string | null {
  return url?.startsWith('https://') ? url : null
}
