import { afterEach, describe, expect, it, vi } from 'vitest'
import { LIVE_PATH, READ_LABEL, readLive, readingRequest } from './liveReading'
import { sampleDates } from './sampleDates'

const local = sampleDates(new Date('2026-12-01T12:00:00Z'))
const none = { type: 'none', time: '' }

function answer(readBy: string, extra: Record<string, unknown> = {}) {
  return {
    read_by: readBy,
    dates: [
      { key: 'sat', label: 'Sat, Dec 12', hours: '10 AM–12 PM' },
      { key: 'sun', label: 'Sun, Dec 13', hours: '10 AM–12 PM' },
    ],
    per_date: {
      sat: { verdict: 'conditional', condition: { type: 'end_before', time: '11:00' }, evidence: 'done by 11', counted: false },
      sun: { verdict: 'available', condition: none, evidence: 'Sunday works', counted: true },
    },
    question: '',
    ...extra,
  }
}

function respond(status: number, body: unknown) {
  const fetch = vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', fetch)
  return fetch
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('readLive', () => {
  it('uses Claude’s reading, its counts and the dates the server returned', async () => {
    const fetch = respond(200, answer('claude'))
    const got = await readLive({ message: 'Sunday works', earlier: null }, local)
    expect(got.by).toBe('claude')
    expect(READ_LABEL[got.by]).toBe('Read by Claude')
    expect(got.counted).toEqual({ sat: false, sun: true })
    expect(got.reading).toEqual({ sat: 'conditional', sun: 'available', limits: { sat: { type: 'end_before', hour: 11 } }, ask: null })
    expect(got.dates.sat.label).toBe('Sat, Dec 12')
    expect(got.dates.sun.weekday).toBe('Sunday')
    expect(got.question).toBeNull()
    expect(fetch).toHaveBeenCalledWith(LIVE_PATH, expect.objectContaining({ method: 'POST', body: JSON.stringify({ message: 'Sunday works' }) }))
  })

  it('says when the server read it with simple rules', async () => {
    respond(200, answer('rules'))
    const got = await readLive({ message: 'Sunday works', earlier: null }, local)
    expect(READ_LABEL[got.by]).toBe('Simple rules read this one')
  })

  it('passes Claude’s question on', async () => {
    respond(200, answer('claude', { question: ' Can you make Saturday morning? ' }))
    const got = await readLive({ message: 'afternoons', earlier: null }, local)
    expect(got.question).toBe('Can you make Saturday morning?')
    expect(got.reading.ask).toBe('Can you make Saturday morning?')
  })

  it.each([429, 503, 400])('falls back to the rules in the browser on a %i and says Claude is busy', async (status) => {
    respond(status, { error: 'ai_capped', message: 'Claude has read a lot of messages this hour.' })
    const got = await readLive({ message: 'No problem — Saturday works!', earlier: null }, local)
    expect(got.by).toBe('fallback')
    expect(READ_LABEL[got.by]).toBe('Claude is busy — simple rules read this one')
    expect(got.counted).toEqual({ sat: true, sun: false })
    expect(got.dates).toEqual(local)
  })

  it('falls back when the network fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('offline'))))
    expect((await readLive({ message: 'Saturday works', earlier: null }, local)).by).toBe('fallback')
  })

  it('falls back on an answer it does not understand', async () => {
    respond(200, { read_by: 'claude', dates: [], per_date: { sat: { verdict: 'maybe' } } })
    expect((await readLive({ message: 'Saturday works', earlier: null }, local)).by).toBe('fallback')
  })

  it('falls back once the timeout passes, even if the request never ends', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => undefined)))
    const pending = readLive({ message: 'Saturday works', earlier: null }, local, 13_000)
    await vi.advanceTimersByTimeAsync(13_000)
    const got = await pending
    expect(got.by).toBe('fallback')
    expect(got.question).toBeNull()
  })

  it('asks the rules’ question about the class hours when the fallback is vague', async () => {
    respond(503, {})
    const got = await readLive({ message: 'maybe? ideally in the afternoon', earlier: null }, local)
    expect(got.question).toBe('The class runs 10 AM–12 PM on Saturday and Sunday — can you make either morning, and which?')
  })
})

describe('readingRequest', () => {
  it('sends only the message on a first answer', () => {
    expect(readingRequest({ message: 'Saturday works', earlier: null })).toEqual({ message: 'Saturday works' })
  })

  it('sends the earlier answer and the question with an answer to it', () => {
    const earlier = { message: 'ideally afternoons', questions: ['Can you make a morning?'] }
    expect(readingRequest({ message: 'Saturday morning works', earlier })).toEqual({ message: 'Saturday morning works', earlier })
  })
})

describe('readLive on an answer to a question', () => {
  it('posts the earlier answer as context', async () => {
    const fetch = respond(200, answer('claude'))
    const earlier = { message: 'ideally afternoons', questions: ['Can you make a morning?'] }
    await readLive({ message: 'Saturday morning works', earlier }, local)
    expect(fetch).toHaveBeenCalledWith(LIVE_PATH, expect.objectContaining({ body: JSON.stringify({ message: 'Saturday morning works', earlier }) }))
  })
})

describe('READ_LABEL', () => {
  it('never signs a prepared answer as Claude', () => {
    expect(READ_LABEL.prepared).toBe('Prepared reading')
  })
})
