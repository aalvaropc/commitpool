import { afterEach, describe, expect, it, vi } from 'vitest'
import { joinedAs, rememberJoin, rememberParticipant, tokenFromParticipantURL } from './participant'

describe('tokenFromParticipantURL', () => {
  it('takes the token from the participant URL the server returns', () => {
    expect(tokenFromParticipantURL('http://localhost:8080/p/AbC_123-xyz')).toBe('AbC_123-xyz')
  })

  it('refuses any other address', () => {
    expect(tokenFromParticipantURL('http://localhost:8080/owner/AbC')).toBeNull()
    expect(tokenFromParticipantURL('not a url')).toBeNull()
  })
})

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

describe('joinedAs', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('remembers who joined this workshop in this tab', () => {
    vi.stubGlobal('sessionStorage', memoryStorage())
    rememberJoin('share-1', 'tok-1', 'Ana')
    expect(sessionStorage.getItem('cp.participant')).toBe('tok-1')
    expect(joinedAs('share-1')).toEqual({ token: 'tok-1', name: 'Ana' })
    expect(joinedAs('share-2')).toBeNull()
  })

  it('forgets the pairing once another personal link is opened', () => {
    vi.stubGlobal('sessionStorage', memoryStorage())
    rememberJoin('share-1', 'tok-1', 'Ana')
    rememberParticipant('tok-2')
    expect(joinedAs('share-1')).toBeNull()
  })

  it('is null without storage', () => {
    vi.stubGlobal('sessionStorage', undefined)
    expect(joinedAs('share-1')).toBeNull()
  })
})
