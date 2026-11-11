import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import { DemoChatUsed } from './Owner'

describe('a demo board emptied after its one chat', () => {
  it('says the demo already read the chat and links back to Try it as Iris', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <DemoChatUsed />
      </MemoryRouter>,
    )
    const plain = html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ')
    expect(plain).toContain('This demo already read Iris’s chat. Start a new demo to read it again.')
    expect(plain).toContain('Back to Try it as Iris')
    expect(html).toMatch(/<a[^>]*href="\/"/)
    expect(html).not.toContain('<textarea')
  })
})
