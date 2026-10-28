import { describe, expect, it } from 'vitest'
import type { Interpretation } from '../api'
import { blockerText, correctedReading, saveButton, verdictText } from './reading'

const none = { type: 'none', time: '' } as const

describe('verdictText', () => {
  it('says each verdict in plain words', () => {
    expect(verdictText({ candidate_date_id: 'a', verdict: 'available', condition: none, evidence: '' })).toBe('Can come')
    expect(verdictText({ candidate_date_id: 'a', verdict: 'unavailable', condition: none, evidence: '' })).toBe('Can’t come')
    expect(verdictText({ candidate_date_id: 'a', verdict: 'unknown', condition: none, evidence: '' })).toBe('Not sure')
    expect(
      verdictText({ candidate_date_id: 'a', verdict: 'conditional', condition: { type: 'end_before', time: '18:00' }, evidence: '' }),
    ).toBe('Only if it ends by 18:00')
    expect(
      verdictText({ candidate_date_id: 'a', verdict: 'conditional', condition: { type: 'start_after', time: '10:30' }, evidence: '' }),
    ).toBe('Only if it starts at 10:30 or later')
  })
})

describe('blockerText', () => {
  it('turns the server’s blockers into plain words', () => {
    expect(blockerText('unavailable')).toBe('can’t come')
    expect(blockerText('needs clarification')).toBe('message needs clarifying')
    expect(blockerText('ends after 18:00')).toBe('only if it ends by 18:00')
    expect(blockerText('starts before 10:00')).toBe('only if it starts at 10:00 or later')
    expect(blockerText('something new')).toBe('something new')
  })
})

describe('correctedReading', () => {
  const reading: Interpretation = {
    per_date: [{ candidate_date_id: 'd1', verdict: 'unknown', condition: none, evidence: 'afternoon' }],
    hard_constraints: [],
    preferences: ['mornings'],
    ambiguities: [{ question: 'Which afternoon?', why: 'vague' }],
    needs_clarification: true,
  }

  it('answers the open questions with the participant’s choices', () => {
    const out = correctedReading(reading, [
      { candidate_date_id: 'd1', verdict: 'conditional', condition: { type: 'start_after', time: '14:00' }, evidence: 'afternoon' },
    ])
    expect(out.needs_clarification).toBe(false)
    expect(out.ambiguities).toEqual([])
    expect(out.preferences).toEqual(['mornings'])
    expect(out.per_date[0].condition).toEqual({ type: 'start_after', time: '14:00' })
  })

  it('drops the condition of a verdict that has none', () => {
    const out = correctedReading(reading, [
      { candidate_date_id: 'd1', verdict: 'available', condition: { type: 'end_before', time: '18:00' }, evidence: '' },
    ])
    expect(out.per_date[0].condition).toEqual({ type: 'none', time: '' })
  })
})

describe('saveButton', () => {
  it('lets a reading that needs clarification be confirmed as it is', () => {
    expect(saveButton(false, true)).toEqual({ enabled: true, label: 'Confirm reading' })
  })

  it('saves corrections once something changed', () => {
    expect(saveButton(true, false)).toEqual({ enabled: true, label: 'Save corrections' })
    expect(saveButton(true, true)).toEqual({ enabled: true, label: 'Save corrections' })
  })

  it('has nothing to save for an unchanged, clear reading', () => {
    expect(saveButton(false, false)).toEqual({ enabled: false, label: 'Save corrections' })
  })
})
