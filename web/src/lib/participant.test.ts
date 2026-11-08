import { afterEach, describe, expect, it, vi } from 'vitest'
import { answerForm, joinedAs, rememberJoin, rememberParticipant, tokenFromParticipantURL } from './participant'
import type { Availability, Interpretation } from '../api'

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

describe('answerForm', () => {
  const none = { type: 'none' as const, time: '' }
  const reading = (questions: string[]): Interpretation => ({
    per_date: [{ candidate_date_id: 'sat', verdict: questions.length ? 'unknown' : 'available', condition: none, evidence: '' }],
    hard_constraints: [],
    preferences: [],
    ambiguities: questions.map((question) => ({ question, why: '' })),
    needs_clarification: questions.length > 0,
  })
  const availability = (r: Interpretation, corrected: Interpretation | null = null): Availability => ({
    id: 'a1',
    message: 'Count me in! Mornings are tricky, but I might swap one.',
    interpretation: r,
    corrected,
    needs_clarification: r.needs_clarification,
    created_at: '',
  })

  it('asks when they can come before they answered', () => {
    expect(answerForm(null)).toEqual({ question: null, label: 'When can you come?', initial: '', button: 'Send my availability', clarifying: false })
  })

  it('offers the sent message for an update once the reading is clear', () => {
    expect(answerForm(availability(reading([])))).toEqual({
      question: null,
      label: 'When can you come?',
      initial: 'Count me in! Mornings are tricky, but I might swap one.',
      button: 'Update my availability',
      clarifying: false,
    })
  })

  it('puts Claude’s question above an empty box while the reading needs clarifying', () => {
    const q = 'The class runs 10 AM–12 PM on Saturday and Sunday — can you make either morning, and which?'
    expect(answerForm(availability(reading([q])))).toEqual({ question: q, label: 'Your answer to Claude’s question', initial: '', button: 'Send my answer', clarifying: true })
    expect(answerForm(availability(reading([q, 'And which one?']))).question).toBe(`${q} And which one?`)
  })

  it('stops asking once the person corrected the reading themselves', () => {
    expect(answerForm(availability(reading(['Which?']), reading([]))).clarifying).toBe(false)
  })
})
