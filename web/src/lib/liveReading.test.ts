import { afterEach, describe, expect, it, vi } from 'vitest'
import { LIVE_PATH, READ_LABEL, earlierFrom, footnote, readLive, readingRequest, saysNothingAboutTheDates } from './liveReading'

const none = { type: 'none', time: '' }
const FALLBACK = 'Simple rules read this one — Claude wasn’t available'

function answer(readBy: string, extra: Record<string, unknown> = {}) {
  return {
    read_by: readBy,
    dates: [
      { key: 'sat', label: 'Sat, Dec 12', hours: '10 AM–12 PM' },
      { key: 'sun', label: 'Sun, Dec 13', hours: '10 AM–12 PM' },
    ],
    per_date: {
      sat: { verdict: 'conditional', condition: { type: 'end_before', time: '11:00' }, counted: false },
      sun: { verdict: 'available', condition: none, counted: true },
    },
    question: '',
    question_sig: '',
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
  it('uses Claude’s reading and its counts', async () => {
    const fetch = respond(200, answer('claude'))
    const got = await readLive({ message: 'Sunday works', earlier: null })
    expect(got.by).toBe('claude')
    expect(READ_LABEL[got.by]).toBe('Read by Claude')
    expect(got.counted).toEqual({ sat: false, sun: true })
    expect(got.reading).toEqual({ sat: 'conditional', sun: 'available', limits: { sat: { type: 'end_before', hour: 11 } }, ask: null })
    expect(got.question).toBeNull()
    expect(got.questionSig).toBe('')
    expect(fetch).toHaveBeenCalledWith(LIVE_PATH, expect.objectContaining({ method: 'POST', body: JSON.stringify({ message: 'Sunday works' }) }))
  })

  it('says when the server read it with simple rules', async () => {
    respond(200, answer('rules'))
    const got = await readLive({ message: 'Sunday works', earlier: null })
    expect(READ_LABEL[got.by]).toBe('Simple rules read this one')
  })

  it('keeps Claude’s question exactly as signed, with its signature', async () => {
    respond(200, answer('claude', { question: 'Can you make Saturday morning?', question_sig: 'sig-1' }))
    const got = await readLive({ message: 'afternoons', earlier: null })
    expect(got.question).toBe('Can you make Saturday morning?')
    expect(got.questionSig).toBe('sig-1')
    expect(got.reading.ask).toBe('Can you make Saturday morning?')
  })

  it.each([429, 503, 400])('falls back to the rules in the browser on a %i and says Claude wasn’t available', async (status) => {
    respond(status, { error: 'ai_capped', message: 'Claude has read a lot of messages this hour.' })
    const got = await readLive({ message: 'No problem — Saturday works!', earlier: null })
    expect(got.by).toBe('fallback')
    expect(READ_LABEL[got.by]).toBe(FALLBACK)
    expect(got.counted).toEqual({ sat: true, sun: false })
  })

  it('falls back when the network fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('offline'))))
    expect((await readLive({ message: 'Saturday works', earlier: null })).by).toBe('fallback')
  })

  it('falls back on an answer it does not understand', async () => {
    respond(200, { read_by: 'claude', per_date: { sat: { verdict: 'maybe' } } })
    expect((await readLive({ message: 'Saturday works', earlier: null })).by).toBe('fallback')
  })

  it('falls back once the timeout passes, even if the request never ends', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => undefined)))
    const pending = readLive({ message: 'Saturday works', earlier: null }, 13_000)
    await vi.advanceTimersByTimeAsync(13_000)
    const got = await pending
    expect(got.by).toBe('fallback')
    expect(got.question).toBeNull()
  })

  it('asks the rules’ question about the class hours when the fallback is vague', async () => {
    respond(503, {})
    const got = await readLive({ message: 'maybe? ideally in the afternoon', earlier: null })
    expect(got.question).toBe('The class runs 10 AM–12 PM on Saturday and Sunday — can you make either morning, and which?')
    expect(got.questionSig).toBe('')
  })
})

describe('readingRequest', () => {
  it('sends only the message on a first answer', () => {
    expect(readingRequest({ message: 'Saturday works', earlier: null })).toEqual({ message: 'Saturday works' })
  })

  it('sends the earlier answer, the question and its signature with an answer to it', () => {
    const earlier = { message: 'ideally afternoons', questions: ['Can you make a morning?'], question_sigs: ['sig-1'] }
    expect(readingRequest({ message: 'Saturday morning works', earlier })).toEqual({ message: 'Saturday morning works', earlier })
  })
})

describe('earlierFrom', () => {
  it('carries a signed question with its signature', async () => {
    respond(200, answer('claude', { question: 'Can you make a morning?', question_sig: 'sig-1' }))
    const read = await readLive({ message: 'afternoons', earlier: null })
    expect(earlierFrom('afternoons', read)).toEqual({ message: 'afternoons', questions: ['Can you make a morning?'], question_sigs: ['sig-1'] })
  })

  it('sends the rules’ question unsigned, so the server reads the earlier message alone', async () => {
    respond(503, {})
    const read = await readLive({ message: 'maybe? ideally in the afternoon', earlier: null })
    expect(earlierFrom('maybe? ideally in the afternoon', read).question_sigs).toEqual([])
  })
})

describe('readLive on an answer to a question', () => {
  it('posts the earlier answer as context', async () => {
    const fetch = respond(200, answer('claude'))
    const earlier = { message: 'ideally afternoons', questions: ['Can you make a morning?'], question_sigs: ['sig-1'] }
    await readLive({ message: 'Saturday morning works', earlier })
    expect(fetch).toHaveBeenCalledWith(LIVE_PATH, expect.objectContaining({ body: JSON.stringify({ message: 'Saturday morning works', earlier }) }))
  })
})

describe('labels', () => {
  it('never signs a prepared answer as Claude', () => {
    expect(READ_LABEL.prepared).toBe('Prepared reading')
  })

  it('says in the footnote who actually read the visitor’s answer', () => {
    expect(footnote(null)).toBe('The five answers are prepared; yours goes to Claude to read live. Payments in this chat are simulated.')
    expect(footnote('claude')).toContain('Claude read yours live.')
    expect(footnote('rules')).toContain('simple rules on the server read yours.')
    expect(footnote('fallback')).toContain('Claude wasn’t available, so simple rules in your browser read yours.')
    expect(footnote('fallback')).not.toContain('read live by Claude')
  })
})

describe('saysNothingAboutTheDates', () => {
  const unknown = { verdict: 'unknown', condition: none, counted: false }
  const offTopic = { per_date: { sat: unknown, sun: unknown } }

  it('is true when Claude finds nothing about either date and asks nothing', async () => {
    respond(200, answer('claude', offTopic))
    const got = await readLive({ message: 'What is the weather like?', earlier: null })
    expect(saysNothingAboutTheDates(got)).toBe(true)
    expect(got.counted).toEqual({ sat: false, sun: false })
  })

  it('is false when Claude asks a question instead', async () => {
    respond(200, answer('claude', { ...offTopic, question: 'The class runs 10 AM–12 PM — can you make either morning?', question_sig: 'sig-1' }))
    expect(saysNothingAboutTheDates(await readLive({ message: 'afternoons', earlier: null }))).toBe(false)
  })

  it('is false when one date has a verdict', async () => {
    respond(200, answer('claude', { per_date: { sat: unknown, sun: { verdict: 'unavailable', condition: none, counted: false } } }))
    expect(saysNothingAboutTheDates(await readLive({ message: 'Not Sunday', earlier: null }))).toBe(false)
  })

  it('is false for simple rules, which may just not understand the message', async () => {
    respond(200, answer('rules', offTopic))
    expect(saysNothingAboutTheDates(await readLive({ message: 'Samstag passt', earlier: null }))).toBe(false)
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('offline'))))
    const fallen = await readLive({ message: 'Samstag passt', earlier: null })
    expect(fallen.by).toBe('fallback')
    expect(fallen.reading).toMatchObject({ sat: 'unknown', sun: 'unknown' })
    expect(saysNothingAboutTheDates(fallen)).toBe(false)
  })
})
