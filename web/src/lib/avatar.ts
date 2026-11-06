export const AVATAR_TONES = ['amber', 'sky', 'pink', 'mint', 'lilac', 'peach'] as const

export type AvatarTone = (typeof AVATAR_TONES)[number]

const letter = /\p{L}/u
const graphemes = new Intl.Segmenter(undefined, { granularity: 'grapheme' })

function firstLetter(word: string): string | null {
  for (const { segment } of graphemes.segment(word)) {
    if (letter.test(segment)) return segment.toLocaleUpperCase()
  }
  return null
}

export function initials(name: string): string {
  const letters = name
    .normalize('NFC')
    .trim()
    .split(/\s+/)
    .map(firstLetter)
    .filter((l): l is string => l !== null)
    .slice(0, 2)
  return letters.length ? letters.join('') : '?'
}

export function avatarTone(name: string): AvatarTone {
  let hash = 2166136261
  for (const ch of name.normalize('NFC').trim().toLocaleLowerCase()) {
    hash ^= ch.codePointAt(0) ?? 0
    hash = Math.imul(hash, 16777619)
  }
  return AVATAR_TONES[(hash >>> 0) % AVATAR_TONES.length]
}
