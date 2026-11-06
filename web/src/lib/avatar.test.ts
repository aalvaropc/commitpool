import { describe, expect, it } from 'vitest'
import { AVATAR_TONES, avatarTone, initials } from './avatar'

describe('initials', () => {
  it('takes the first letter of the first two words', () => {
    expect(initials('Maya Okafor')).toBe('MO')
    expect(initials('  priya   raman shah ')).toBe('PR')
    expect(initials('Lucía')).toBe('L')
    expect(initials('Ólafur Arnalds')).toBe('ÓA')
  })

  it('falls back to a question mark for an empty name', () => {
    expect(initials('   ')).toBe('?')
  })
})

describe('avatarTone', () => {
  it('gives the same person the same colour every time', () => {
    expect(avatarTone('Maya Okafor')).toBe(avatarTone('Maya Okafor'))
  })

  it('spreads names across the palette', () => {
    const names = ['Priya Raman', 'Ben Whitaker', 'Lucía Torres', 'Hannah Kim', 'Tomás Silva', 'Grace Liu', 'Sam Okoro', 'Noor Haddad']
    const used = new Set(names.map(avatarTone))
    expect(used.size).toBeGreaterThanOrEqual(4)
    for (const t of used) expect(AVATAR_TONES).toContain(t)
  })
})
