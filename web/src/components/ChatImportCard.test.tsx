import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { ChatPreview, OwnerView } from '../api'
import { ZIP_ERROR } from '../lib/chat'
import ChatImportCard, { ChatImportPanel, ChatReading, chatCard, withFile, withPreview, type ChatCardState } from './ChatImportCard'

const noop = () => {}

function panel(state: ChatCardState): string {
  return renderToStaticMarkup(
    <ChatImportPanel state={state} busy={null} labelledBy="chat-title" onText={noop} onRead={noop} onFile={noop} onToggle={noop} onAdd={noop} />,
  )
}

const text = (html: string) => html.replace(/<[^>]+>/g, ' ').replaceAll('&#x27;', "'").replaceAll('&amp;', '&').replaceAll('&quot;', '"').replace(/\s+/g, ' ')

function row(html: string, name: string): string {
  const at = html.indexOf(`>${name}<`)
  expect(at, `${name} is in the preview`).toBeGreaterThan(-1)
  const start = html.lastIndexOf('<li>', at)
  return html.slice(start, html.indexOf('</li>', at))
}

const preview: ChatPreview = {
  people: [
    { name: 'Iris Calder', excerpt: 'Life drawing at The Long Room, Saturday or Sunday 10–12, $45. Who’s in?', messages: 2, host: true, joined: false },
    { name: 'Priya Raman', excerpt: 'Saturday works for me, Sunday I can’t.', messages: 1, host: false, joined: false },
    { name: 'Lucía Torres', excerpt: 'El sábado no puedo, pero el domingo me viene perfecto.', messages: 1, host: false, joined: true },
    { name: 'Grace Liu', excerpt: 'Count me in!', messages: 1, host: false, joined: false },
  ],
  skipped: 3,
}

const owner = (over: Partial<OwnerView>) => ({ workshop: { status: 'COLLECTING' }, participants: [], ...over }) as unknown as OwnerView

describe('ChatImportCard', () => {
  it('shows the preview with the host unticked and joined disabled', () => {
    const html = panel(withPreview(chatCard('Iris Calder: Who’s in?'), preview))
    const iris = row(html, 'Iris Calder')
    expect(iris).not.toMatch(/checked/)
    expect(text(iris)).toContain('Iris Calder · you')
    const lucia = row(html, 'Lucía Torres')
    expect(lucia).toMatch(/disabled/)
    expect(lucia).not.toMatch(/checked/)
    expect(text(lucia)).toContain('Lucía Torres · already here')
    for (const name of ['Priya Raman', 'Grace Liu']) {
      expect(row(html, name)).toMatch(/checked/)
      expect(row(html, name)).not.toMatch(/disabled/)
    }
    const plain = text(html)
    expect(plain).toContain('4 people in the chat')
    expect(html).toMatch(/<h3 id="chat-found" tabindex="-1"[^>]*>4 people in the chat<\/h3>/)
    expect(plain).toContain('“Saturday works for me, Sunday I can’t.”')
    expect(plain).toContain('3 lines left out')
    expect(plain).toContain('Add 2 people')
  })

  it('rejects a zip', async () => {
    const before = chatCard('Ana: Saturday works')
    const zip = new File([new Uint8Array([0x50, 0x4b, 0x03, 0x04])], 'WhatsApp Chat - Life drawing.zip', { type: 'application/zip' })
    const after = await withFile(before, zip)
    expect(after.error).toBe(ZIP_ERROR)
    expect(after.text).toBe('Ana: Saturday works')
    const html = panel(after)
    expect(html).toMatch(/role="alert"/)
    expect(text(html)).toContain('That’s a .zip. Export the chat without media to get a .txt, or paste the messages.')
    expect(html).toMatch(/aria-invalid="true"/)
  })

  it('reads a .txt into the textarea', async () => {
    const chat = '[11/10/26, 9:15:30 PM] Priya Raman: Saturday works for me\n[11/10/26, 9:16:44 PM] Ben Whitaker: Sunday afternoon is best'
    const stale = { ...withPreview(chatCard('Old: paste'), preview), error: 'Old error' }
    const after = await withFile(stale, new File([chat], 'WhatsApp Chat - Pottery.txt', { type: 'text/plain' }))
    expect(after).toEqual(chatCard(chat))
    const html = panel(after)
    expect(html).toContain('>[11/10/26, 9:15:30 PM] Priya Raman: Saturday works for me\n[11/10/26, 9:16:44 PM] Ben Whitaker: Sunday afternoon is best</textarea>')
    expect(text(html)).toContain(`${[...chat].length} of 20,000 characters`)
    expect(html).not.toMatch(/role="alert"/)
  })

  it('leads a new demo with Iris’s step, and a pasted chat of your own with the people', () => {
    const demo = text(renderToStaticMarkup(<ChatImportCard token="t" view={owner({ workshop: { status: 'COLLECTING', demo: true }, demo_chat: 'Iris Calder: Who’s in?' } as Partial<OwnerView>)} onImported={async () => {}} />))
    expect(demo).toContain('Next in the demo: This is Iris’s group chat. Press Read the chat, then Add 6 people.')
    const own = text(panel({ ...chatCard('Ana: Sunday'), preview: null }))
    expect(own).not.toContain('Next in the demo')
    const lead = renderToStaticMarkup(
      <ChatImportPanel state={chatCard('Ana: Sunday')} busy={null} labelledBy="chat-title" lead="Press Read the chat, then Add the people." onText={noop} onRead={noop} onFile={noop} onToggle={noop} onAdd={noop} />,
    )
    expect(text(lead)).toContain('Press Read the chat, then Add the people.')
    expect(text(renderToStaticMarkup(<ChatImportCard token="t" view={owner({ demo_chat: 'Iris Calder: Who’s in?' })} onImported={async () => {}} />))).not.toContain('Next in the demo')
  })

  it('keeps the textarea at 16 px on phones and still while a file is read', () => {
    const html = renderToStaticMarkup(
      <ChatImportPanel state={chatCard('Ana: Sunday')} busy="file" labelledBy="chat-title" onText={noop} onRead={noop} onFile={noop} onToggle={noop} onAdd={noop} />,
    )
    const area = html.slice(html.indexOf('<textarea'), html.indexOf('</textarea>'))
    expect(area).toMatch(/text-base/)
    expect(area).toMatch(/sm:text-sm/)
    expect(area).toMatch(/disabled=""/)
    expect(area).toMatch(/rows="6"/)
  })

  it('opens with the demo chat in a new demo and folds away once people are on the board', () => {
    const demo = renderToStaticMarkup(<ChatImportCard token="t" view={owner({ demo_chat: 'Iris Calder: Who’s in?' })} onImported={async () => {}} />)
    expect(text(demo)).toContain('Paste your group chat')
    expect(text(demo)).toContain("Paste the messages, or upload WhatsApp’s Export chat (.txt, without media). CommitPool finds who said what; Claude reads each person against your dates.")
    expect(demo).toContain('Iris Calder: Who’s in?</textarea>')
    expect(text(demo)).toContain('Read the chat')
    expect(text(demo)).toContain('Upload the export (.txt)')

    const later = renderToStaticMarkup(
      <ChatImportCard token="t" view={owner({ demo_chat: 'Iris Calder: Who’s in?', participants: [{ id: 'p1', display_name: 'Priya Raman' }] })} onImported={async () => {}} />,
    )
    expect(later).toMatch(/<details[^>]*>/)
    expect(later).not.toMatch(/<details[^>]*open/)
    expect(text(later)).toContain('Paste more of the chat')
    expect(later).not.toContain('Who’s in?</textarea>')
  })

  it('shows how far Claude got only while it is reading', () => {
    expect(text(renderToStaticMarkup(<ChatReading reading={{ pending: 2, total: 6, active: true }} />))).toContain('Claude is reading the chat… 4 of 6')
    expect(text(renderToStaticMarkup(<ChatReading reading={{ pending: 2, total: 6, active: false }} />)).trim()).toBe('')
  })
})
