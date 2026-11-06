export const AVATAR_TONES = ['amber', 'sky', 'pink', 'mint', 'lilac', 'peach'] as const

export type AvatarTone = (typeof AVATAR_TONES)[number]

export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return '?'
  return words
    .slice(0, 2)
    .map((w) => Array.from(w)[0].toLocaleUpperCase())
    .join('')
}

export function avatarTone(name: string): AvatarTone {
  let hash = 2166136261
  for (const ch of name.trim().toLocaleLowerCase()) {
    hash ^= ch.codePointAt(0) ?? 0
    hash = Math.imul(hash, 16777619)
  }
  return AVATAR_TONES[(hash >>> 0) % AVATAR_TONES.length]
}
