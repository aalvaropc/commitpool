import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { StatusLine } from './Decision'

describe('StatusLine', () => {
  it('says in one line that nobody answered yet and links to the invite', () => {
    const html = renderToStaticMarkup(<StatusLine />)
    expect(html.replace(/<[^>]+>/g, '')).toBe('No answers yet. Paste the group chat, or share the invite.')
    expect(html).toMatch(/aria-label="Workshop status"/)
    expect(html).toMatch(/role="status"/)
    expect(html).toMatch(/href="#share-url"/)
  })
})
