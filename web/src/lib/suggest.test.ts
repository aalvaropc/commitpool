import { describe, expect, it } from 'vitest'
import type { Suggestion } from '../api'
import { groupSuggestions, groupTitle, previewLabel } from './suggest'

const reserve = (name: string, id: string, message = `Hi ${name.split(' ')[0]}! Reserve your spot before Monday.`, pending = false): Suggestion => ({
  kind: 'reserve',
  participant_id: id,
  name,
  reason: `${name} can come Sat, Nov 14 and hasn’t reserved yet.`,
  message,
  pending,
})

const clarify = (name: string, id: string): Suggestion => ({
  kind: 'clarify',
  participant_id: id,
  name,
  reason: `${name}’s message needs clarifying.`,
  message: `Hi ${name.split(' ')[0]}! Can you make one of the dates, and which?`,
  pending: false,
})

const share: Suggestion = { kind: 'share', reason: 'Reservations close soon, and 2 more are needed.', message: 'Know someone?', pending: false }

describe('groupSuggestions', () => {
  it('puts every reserve nudge in one card, even when Claude wrote each one differently', () => {
    const list = [
      reserve('Priya Raman', 'p1', 'Hi Priya! Saturday morning is yours if you reserve today.'),
      reserve('Ben Whitaker', 'p2', 'Ben, you said before 6 works — the class ends at noon. Grab your spot?'),
      reserve('Grace Liu', 'p3', 'Grace, glad the swap worked! Reserve whenever you are ready.'),
    ]
    expect(groupSuggestions(list)).toEqual([{ kind: 'group', key: 'reserve', people: list, pending: false }])
  })

  it('keeps the card’s key when someone drops out, down to one person', () => {
    const [a, b] = [reserve('Priya Raman', 'p1'), reserve('Ben Whitaker', 'p2')]
    expect(groupSuggestions([a, b]).map((c) => c.key)).toEqual(['reserve'])
    expect(groupSuggestions([b])).toEqual([{ kind: 'group', key: 'reserve', people: [b], pending: false }])
  })

  it('keeps clarify, condition and share suggestions individual', () => {
    const condition: Suggestion = { ...clarify('Hannah Kim', 'p5'), kind: 'condition', reason: 'Hannah Kim said only if it ends by 11:30 AM.' }
    const list = [share, clarify('Grace Liu', 'p3'), clarify('Lucía Torres', 'p4'), condition]
    expect(groupSuggestions(list)).toEqual([
      { kind: 'single', key: 'share:', suggestion: share },
      { kind: 'single', key: 'clarify:p3', suggestion: list[1] },
      { kind: 'single', key: 'clarify:p4', suggestion: list[2] },
      { kind: 'single', key: 'condition:p5', suggestion: condition },
    ])
  })

  it('places the group where its first person was and keeps the order of the rest', () => {
    const list = [share, reserve('Priya Raman', 'p1'), clarify('Grace Liu', 'p3'), reserve('Ben Whitaker', 'p2')]
    expect(groupSuggestions(list).map((c) => c.key)).toEqual(['share:', 'reserve', 'clarify:p3'])
  })

  it('says the group is still being written while any message is pending', () => {
    const [group] = groupSuggestions([reserve('Priya Raman', 'p1'), reserve('Ben Whitaker', 'p2', 'Hi Ben!', true)])
    expect(group.kind === 'group' && group.pending).toBe(true)
  })
})

describe('groupTitle and previewLabel', () => {
  it('names everyone by first name, accents and symbols as written', () => {
    const people = [reserve('José', 'p1'), reserve('Ana (Ani) Ruiz', 'p2'), reserve('Zoë [Z] O’Neil+', 'p3')]
    expect(groupTitle(people)).toBe('3 people can come but haven’t reserved: José, Ana and Zoë')
    expect(groupTitle(people.slice(0, 2))).toBe('2 people can come but haven’t reserved: José and Ana')
    expect(previewLabel(people[0])).toBe('Message for José — each one is written for that person')
    expect(previewLabel(people[1])).toBe('Message for Ana — each one is written for that person')
  })

  it('uses CommitPool’s own reason when only one person is left', () => {
    const one = reserve('Grace Liu', 'p3')
    expect(groupTitle([one])).toBe('Grace Liu can come Sat, Nov 14 and hasn’t reserved yet.')
  })
})
