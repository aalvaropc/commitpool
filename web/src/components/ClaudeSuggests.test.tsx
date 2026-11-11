import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Suggestion } from '../api'
import ClaudeSuggests from './ClaudeSuggests'

const reserve = (name: string, id: string, message: string): Suggestion => ({
  kind: 'reserve',
  participant_id: id,
  name,
  reason: `${name} can come Sat, Nov 14 and hasn’t reserved yet.`,
  message,
  pending: false,
})

const text = (html: string) => html.replace(/<[^>]+>/g, '').replaceAll('&#x27;', "'").replaceAll('&amp;', '&')

describe('ClaudeSuggests', () => {
  it('shows the reserve nudges as one card: everyone named, one person’s own draft as the preview, a copy button per person', () => {
    const people = [
      reserve('José Núñez', 'p1', 'José, Saturday at 10 is yours if you reserve today.'),
      reserve('Ana (Ani) Ruiz', 'p2', 'Hi Ana! You said mornings work — reserve before Monday?'),
      reserve('Grace Liu', 'p3', 'Grace, glad the swap worked. Your spot is waiting.'),
    ]
    const plain = text(renderToStaticMarkup(<ClaudeSuggests suggestions={people} />))
    expect(plain).toContain('3 people can come but haven’t reserved: José, Ana and Grace')
    expect(plain).toContain('Message for José — each one is written for that person')
    expect(plain).toContain('José, Saturday at 10 is yours if you reserve today.')
    expect(plain).not.toContain('Hi Ana!')
    expect(plain).not.toContain('Grace, glad the swap worked.')
    for (const name of ['José Núñez', 'Ana (Ani) Ruiz', 'Grace Liu']) expect(plain).toContain(`Copy message for ${name}`)
    expect(plain.match(/CommitPool picks who to nudge/g)).toHaveLength(1)
  })

  it('keeps a clarifying question on its own card next to the reserve card', () => {
    const clarify: Suggestion = { kind: 'clarify', participant_id: 'p3', name: 'Grace Liu', reason: 'Grace Liu’s message needs clarifying.', message: 'Hi Grace! Which morning?', pending: false }
    const plain = text(renderToStaticMarkup(<ClaudeSuggests suggestions={[clarify, reserve('Priya Raman', 'p1', 'Hi Priya!')]} />))
    expect(plain).toContain('Grace Liu’s message needs clarifying.')
    expect(plain).toContain('Copy message for Grace Liu')
    expect(plain).toContain('Priya Raman can come Sat, Nov 14 and hasn’t reserved yet.')
    expect(plain).toContain('Copy message for Priya Raman')
    expect(plain).not.toContain('people can come but')
  })

  it('shows the group reply above personal nudges', () => {
    const clarify: Suggestion = { kind: 'clarify', participant_id: 'p3', name: 'Grace Liu', reason: 'Grace Liu’s message needs clarifying.', message: 'Hi Grace! Which morning?', pending: false }
    const condition: Suggestion = { kind: 'condition', participant_id: 'p4', name: 'Hannah Kim', reason: 'Hannah Kim said only if it ends by 11:30 AM.', message: 'Hi Hannah!', pending: false }
    const reply: Suggestion = {
      kind: 'group_reply',
      reason: '6 people have answered. Tell the group where things stand.',
      message: 'Life drawing for beginners: thanks for answering, everyone! Sat, Nov 21 and Sun, Nov 22 are tied.\nhttps://example.test/w/INVITE',
      pending: true,
    }
    const plain = text(renderToStaticMarkup(<ClaudeSuggests suggestions={[clarify, reply, condition]} />))
    const at = (s: string) => plain.indexOf(s)
    expect(at('Claude’s reply for the group')).toBeGreaterThan(-1)
    expect(at('Claude’s reply for the group')).toBeLessThan(at('Grace Liu’s message needs clarifying.'))
    expect(at('Sat, Nov 21 and Sun, Nov 22 are tied.')).toBeLessThan(at('Hannah Kim said only if it ends by 11:30 AM.'))
    expect(plain).toContain('6 people have answered. Tell the group where things stand.')
    expect(plain).toContain('https://example.test/w/INVITE')
    expect(plain).toContain('Copy message for the group')
    expect(plain).toContain('Claude is rewording this message…')
  })
})
