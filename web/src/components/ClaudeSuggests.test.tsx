import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Suggestion } from '../api'
import ClaudeSuggests from './ClaudeSuggests'

const reserve = (name: string, id: string): Suggestion => ({
  kind: 'reserve',
  participant_id: id,
  name,
  reason: `${name} can come Sat, Nov 14 and hasn’t reserved yet.`,
  message: `Hi ${name.split(' ')[0]}! Reservations are open, and you can make it.`,
  pending: false,
})

const text = (html: string) => html.replace(/<[^>]+>/g, '')

describe('ClaudeSuggests', () => {
  it('shows one message for people who would get the same words, with a copy button for each person', () => {
    const html = renderToStaticMarkup(<ClaudeSuggests suggestions={[reserve('Priya Raman', 'p1'), reserve('Ben Whitaker', 'p2'), reserve('Grace Liu', 'p3')]} />)
    const plain = text(html)
    expect(plain).toContain('One message for 3 people')
    expect(plain).toContain('Each of them can come Sat, Nov 14 and hasn’t reserved yet.')
    expect(plain.match(/Reservations are open, and you can make it\./g)).toHaveLength(1)
    expect(plain).toContain('Hi Priya!')
    for (const name of ['Priya Raman', 'Ben Whitaker', 'Grace Liu']) expect(plain).toContain(`Copy message for ${name}`)
    expect(plain.match(/CommitPool picks who to nudge/g)).toHaveLength(1)
  })

  it('keeps a clarifying question on its own card', () => {
    const clarify: Suggestion = { kind: 'clarify', participant_id: 'p3', name: 'Grace Liu', reason: 'Grace Liu’s message needs clarifying.', message: 'Hi Grace! Which morning?', pending: false }
    const plain = text(renderToStaticMarkup(<ClaudeSuggests suggestions={[clarify, reserve('Priya Raman', 'p1')]} />))
    expect(plain).not.toContain('One message for')
    expect(plain).toContain('Copy message for Grace Liu')
    expect(plain).toContain('Copy message for Priya Raman')
  })
})
