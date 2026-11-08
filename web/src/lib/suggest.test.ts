import { describe, expect, it } from 'vitest'
import type { Suggestion } from '../api'
import { groupSuggestions } from './suggest'

const reserve = (name: string, id: string, pending = false): Suggestion => ({
  kind: 'reserve',
  participant_id: id,
  name,
  reason: `${name} can come Sat, Nov 14 and hasn’t reserved yet.`,
  message: `Hi ${name.split(' ')[0]}! Reservations for Wheel-throwing on Sat, Nov 14 are open until Mon, Nov 9 · 10 AM, and you can make it. Nothing is charged unless 3 people reserve.`,
  pending,
})

const clarify = (name: string, id: string): Suggestion => ({
  kind: 'clarify',
  participant_id: id,
  name,
  reason: `${name}’s message needs clarifying.`,
  message: `Hi ${name.split(' ')[0]}! Can you make one of the dates, and which? Answer on your CommitPool page.`,
  pending: false,
})

describe('groupSuggestions', () => {
  it('puts messages that differ only by the person’s name into one card', () => {
    const list = [reserve('Priya Raman', 'p1'), reserve('Ben Whitaker', 'p2'), reserve('Grace Liu', 'p3')]
    expect(groupSuggestions(list)).toEqual([
      { kind: 'group', key: 'reserve:p1', people: list, reason: 'Each of them can come Sat, Nov 14 and hasn’t reserved yet.', pending: false },
    ])
  })

  it('keeps clarify and condition suggestions individual even when they read the same', () => {
    const list = [clarify('Grace Liu', 'p3'), clarify('Lucía Torres', 'p4')]
    expect(groupSuggestions(list)).toEqual([
      { kind: 'single', key: 'clarify:p3', suggestion: list[0] },
      { kind: 'single', key: 'clarify:p4', suggestion: list[1] },
    ])
  })

  it('keeps a message that says more than the name on its own card', () => {
    const odd = { ...reserve('Ben Whitaker', 'p2'), message: 'Hi Ben! You said Saturday works if we finish before 6, and it ends at noon. Reserve your spot.' }
    const list = [reserve('Priya Raman', 'p1'), odd, reserve('Grace Liu', 'p3')]
    expect(groupSuggestions(list).map((c) => [c.kind, c.key])).toEqual([
      ['group', 'reserve:p1'],
      ['single', 'reserve:p2'],
    ])
    const group = groupSuggestions(list)[0]
    expect(group.kind === 'group' && group.people.map((p) => p.name)).toEqual(['Priya Raman', 'Grace Liu'])
  })

  it('leaves a lone suggestion of its kind as it is', () => {
    const list = [clarify('Grace Liu', 'p3'), reserve('Priya Raman', 'p1')]
    expect(groupSuggestions(list).map((c) => c.kind)).toEqual(['single', 'single'])
  })

  it('places the group where its first person was and keeps the order of the rest', () => {
    const share: Suggestion = { kind: 'share', reason: 'Reservations close soon, and 2 more are needed.', message: 'Know someone?', pending: false }
    const list = [share, reserve('Priya Raman', 'p1'), clarify('Grace Liu', 'p3'), reserve('Ben Whitaker', 'p2')]
    expect(groupSuggestions(list).map((c) => c.key)).toEqual(['share:', 'reserve:p1', 'clarify:p3'])
  })

  it('treats names as plain text, accents and symbols included', () => {
    const list = [reserve('Lucía Torres', 'p4'), reserve('Ana (Ani) Ruiz', 'p5')]
    expect(groupSuggestions(list).map((c) => c.kind)).toEqual(['group'])
  })

  it('does not take a name inside a longer word for the name', () => {
    const al = { ...reserve('Al Kim', 'p6'), message: 'Hi Al! Reservations for Pottery Almanac are open.' }
    const bo = { ...reserve('Bo Lee', 'p7'), message: 'Hi Bo! Reservations for Pottery Almanac are open.' }
    expect(groupSuggestions([al, bo]).map((c) => c.kind)).toEqual(['group'])
  })

  it('says the group is still being reworded while any message is pending', () => {
    const [group] = groupSuggestions([reserve('Priya Raman', 'p1'), reserve('Ben Whitaker', 'p2', true)])
    expect(group.kind === 'group' && group.pending).toBe(true)
  })

  it('drops the shared reason when the reasons do not start with the name', () => {
    const a = { ...reserve('Priya Raman', 'p1'), reason: 'Saturday works for Priya Raman.' }
    const b = { ...reserve('Ben Whitaker', 'p2'), reason: 'Saturday works for Ben Whitaker.' }
    const [group] = groupSuggestions([a, b])
    expect(group.kind === 'group' && group.reason).toBeNull()
  })
})
