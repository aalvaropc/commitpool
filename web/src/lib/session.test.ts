import { afterEach, describe, expect, it, vi } from 'vitest'
import { readSession, writeSession } from './session'

function blockedStorage(): Storage {
  const refuse = () => {
    throw new DOMException('blocked', 'SecurityError')
  }
  return {
    length: 0,
    clear: refuse,
    getItem: refuse,
    key: refuse,
    removeItem: refuse,
    setItem: refuse,
  }
}

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

describe('session storage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('stores and reads a value', () => {
    vi.stubGlobal('sessionStorage', memoryStorage())
    expect(writeSession('k', 'v')).toBe(true)
    expect(readSession('k')).toBe('v')
  })

  it('reports a blocked storage instead of throwing', () => {
    vi.stubGlobal('sessionStorage', blockedStorage())
    expect(writeSession('k', 'v')).toBe(false)
    expect(readSession('k')).toBeNull()
  })

  it('treats a missing storage as empty', () => {
    vi.stubGlobal('sessionStorage', undefined)
    expect(writeSession('k', 'v')).toBe(false)
    expect(readSession('k')).toBeNull()
  })
})
