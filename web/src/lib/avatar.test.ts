import { describe, expect, it } from 'vitest'
import { AVATAR_TONES, avatarTone, initials } from './avatar'

describe('initials', () => {
  it('takes the first letter of the first two words', () => {
    expect(initials('Iris Calder')).toBe('IC')
    expect(initials('  priya   raman shah ')).toBe('PR')
    expect(initials('Lucía')).toBe('L')
    expect(initials('Ólafur Arnalds')).toBe('ÓA')
  })

  it('falls back to a question mark when there is no letter', () => {
    expect(initials('   ')).toBe('?')
    expect(initials('🎨')).toBe('?')
  })

  it('keeps whole letters however the accent was typed', () => {
    expect(initials('O\u0301lafur Arnalds')).toBe('ÓA')
  })

  it('skips symbols and emoji to the first letter of each word', () => {
    expect(initials('🇵🇪 Ana Ruiz')).toBe('AR')
    expect(initials('(Ana) Ruiz')).toBe('AR')
    expect(initials('👩🏽‍🎨 Sketch')).toBe('S')
  })
})

describe('avatarTone', () => {
  it('gives the same person the same colour every time', () => {
    expect(avatarTone('Iris Calder')).toBe(avatarTone('Iris Calder'))
    expect(avatarTone('O\u0301lafur')).toBe(avatarTone('Ólafur'))
  })

  it('spreads names across the palette', () => {
    const names = ['Priya Raman', 'Ben Whitaker', 'Lucía Torres', 'Hannah Kim', 'Tomás Silva', 'Grace Liu', 'Sam Okoro', 'Noor Haddad']
    const used = new Set(names.map(avatarTone))
    expect(used.size).toBeGreaterThanOrEqual(4)
    for (const t of used) expect(AVATAR_TONES).toContain(t)
  })
})
