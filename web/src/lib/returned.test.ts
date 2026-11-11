import { afterEach, describe, expect, it, vi } from 'vitest'
import { RESULTS, returnPath, returnedMessage } from './returned'

function memoryStorage(entries: Record<string, string> = {}): Storage {
  const data = new Map(Object.entries(entries))
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

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('returnPath', () => {
  it('reads the participant token from sessionStorage and links to the pending check', () => {
    vi.stubGlobal('sessionStorage', memoryStorage({ 'cp.participant': 'tok_123-abc' }))
    expect(returnPath('authorized')).toBe('/p/tok_123-abc?paid=pending')
    expect(returnPath('pending')).toBe('/p/tok_123-abc?paid=pending')
  })

  it('links to the plain participant page when no payment is being checked', () => {
    vi.stubGlobal('sessionStorage', memoryStorage({ 'cp.participant': 'tok' }))
    expect(returnPath('cancelled')).toBe('/p/tok')
    expect(returnPath('declined')).toBe('/p/tok')
  })

  it('has no link without a stored token', () => {
    vi.stubGlobal('sessionStorage', memoryStorage())
    expect(returnPath('authorized')).toBeNull()
  })

  it('has no link when storage is unavailable', () => {
    vi.stubGlobal('sessionStorage', undefined)
    expect(returnPath('authorized')).toBeNull()
  })
})

describe('returnedMessage', () => {
  it('promises a charge only if the minimum is reached', () => {
    expect(returnedMessage('authorized').body).toBe(
      'Your spot is reserved and nothing is charged yet. You are charged only if enough people reserve for the workshop to happen.',
    )
  })

  it('tells the reserver who reaches the minimum that everyone is being charged now', () => {
    const tally = { interested: 5, authorized: 3, paid: 0, charging: 0, minimum: 3, capacity: 6 }
    expect(returnedMessage('authorized', tally).body).toBe('That made 3. Charging everyone now.')
    expect(returnedMessage('authorized', { ...tally, authorized: 2 }).body).toContain('nothing is charged yet')
    expect(returnedMessage('declined', tally).body).toBe(returnedMessage('declined').body)
  })

  it('has a distinct message for every result the server sends', () => {
    expect(RESULTS).toEqual([
      'authorized',
      'pending',
      'mismatch',
      'seat_expired',
      'closed',
      'expired',
      'declined',
      'error',
      'cancelled',
    ])
    const bodies = new Set(RESULTS.map((r) => returnedMessage(r).body))
    expect(bodies.size).toBe(RESULTS.length)
  })

  it('falls back for a missing or unknown result', () => {
    expect(returnedMessage(null).title).toBe('Checkout result unknown')
    expect(returnedMessage('surprise').title).toBe('Checkout result unknown')
  })
})
