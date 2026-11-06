import { afterEach, describe, expect, it, vi } from 'vitest'
import { demoParticipants, rememberDemo } from './demo'

function memoryStorage(): Storage {
  const data = new Map<string, string>()
  return {
    get length() {
      return data.size
    },
    clear: () => data.clear(),
    getItem: (key) => data.get(key) ?? null,
    key: (i) => [...data.keys()][i] ?? null,
    removeItem: (key) => void data.delete(key),
    setItem: (key, value) => void data.set(key, String(value)),
  }
}

describe('demo participants', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('remembers who the visitor can act as, for that demo workshop only', () => {
    vi.stubGlobal('sessionStorage', memoryStorage())
    rememberDemo('https://cp.test/owner/OWN', [
      { name: 'Priya Raman', participant_url: 'https://cp.test/p/PRI' },
      { name: 'Ben Whitaker', participant_url: 'https://cp.test/p/BEN' },
    ])
    expect(demoParticipants('OWN')).toEqual([
      { name: 'Priya Raman', path: '/p/PRI' },
      { name: 'Ben Whitaker', path: '/p/BEN' },
    ])
    expect(demoParticipants('OTHER')).toEqual([])
  })

  it('ignores anything that is not a participant link', () => {
    vi.stubGlobal('sessionStorage', memoryStorage())
    rememberDemo('https://cp.test/owner/OWN', [{ name: 'Mallory', participant_url: 'javascript:alert(1)' }])
    expect(demoParticipants('OWN')).toEqual([])
  })

  it('works without storage', () => {
    vi.stubGlobal('sessionStorage', undefined)
    rememberDemo('https://cp.test/owner/OWN', [{ name: 'Priya', participant_url: 'https://cp.test/p/PRI' }])
    expect(demoParticipants('OWN')).toEqual([])
  })
})
