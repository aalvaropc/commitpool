import { describe, expect, it } from 'vitest'
import { addLabel, chatLength, chatProblem, chosenCounts, hostFirstName, importedNote, isLocked, MAX_CHAT_CHARS, NOT_TEXT_ERROR, NOT_UTF8_ERROR, personTag, progressText, readChatFile, readingNow, ticked, TOO_LONG_ERROR, ZIP_ERROR } from './chat'

const txt = (text: string, name = 'WhatsApp Chat - Life drawing.txt') => new File([text], name, { type: 'text/plain' })

describe('readChatFile', () => {
  it('reads a WhatsApp .txt export as it is', async () => {
    const text = '[11/10/26, 9:14:03 PM] Priya Raman: Saturday works for me, Sunday I can’t.\n[11/10/26, 9:16:44 PM] Lucía Torres: El domingo me viene perfecto 🎨'
    await expect(readChatFile(txt(text))).resolves.toBe(text)
  })

  it('rejects files over 20000 characters', async () => {
    await expect(readChatFile(txt('a'.repeat(MAX_CHAT_CHARS + 1)))).rejects.toThrow(TOO_LONG_ERROR)
    await expect(readChatFile(txt('🎨'.repeat(MAX_CHAT_CHARS + 1)))).rejects.toThrow(TOO_LONG_ERROR)
    await expect(readChatFile(txt('🎨'.repeat(MAX_CHAT_CHARS)))).resolves.toHaveLength(2 * MAX_CHAT_CHARS)
  })

  it('rejects a .zip by its name, its type or its first bytes', async () => {
    await expect(readChatFile(txt('Ana: hi', 'WhatsApp Chat - Life drawing.zip'))).rejects.toThrow(ZIP_ERROR)
    await expect(readChatFile(new File(['Ana: hi'], 'chat', { type: 'application/zip' }))).rejects.toThrow(ZIP_ERROR)
    await expect(readChatFile(new File([new Uint8Array([0x50, 0x4b, 0x03, 0x04, 0x14, 0x00])], 'chat.txt'))).rejects.toThrow(ZIP_ERROR)
  })

  it('refuses text that is not UTF-8 instead of filling the box with replacement marks', async () => {
    const latin1 = new Uint8Array([...'Luc'].map((c) => c.charCodeAt(0)).concat([0xed], [...'a: El s'].map((c) => c.charCodeAt(0)), [0xe1], [...'bado s'].map((c) => c.charCodeAt(0)), [0xed]))
    await expect(readChatFile(new File([latin1], 'chat.txt', { type: 'text/plain' }))).rejects.toThrow(NOT_UTF8_ERROR)
    await expect(readChatFile(txt('Ana: \ufffd broken'))).rejects.toThrow('This file isn’t UTF-8 text. Export it again from WhatsApp, or paste the messages.')
  })

  it('reads a UTF-8 export with a byte order mark, and UTF-16 with one', async () => {
    const bom = new Uint8Array([0xef, 0xbb, 0xbf, ...new TextEncoder().encode('Lucía: Sí puedo')])
    await expect(readChatFile(new File([bom], 'chat.txt'))).resolves.toBe('Lucía: Sí puedo')
    const utf16 = new Uint8Array([0xff, 0xfe, ...[...'Lucía: Sí'].flatMap((c) => [c.charCodeAt(0), 0])])
    await expect(readChatFile(new File([utf16], 'chat.txt'))).resolves.toBe('Lucía: Sí')
  })

  it('rejects a file that is not text', async () => {
    await expect(readChatFile(new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x00, 0x1a])], 'photo.txt'))).rejects.toThrow(NOT_TEXT_ERROR)
  })
})

describe('chat helpers', () => {
  it('counts characters the way the server does, an emoji as one', () => {
    expect(chatLength('Thanks all! 🎨')).toBe(13)
    expect(chatProblem('  \n ')).toBe('Paste the group chat first.')
    expect(chatProblem('🎨'.repeat(MAX_CHAT_CHARS))).toBeNull()
    expect(chatProblem('🎨'.repeat(MAX_CHAT_CHARS + 1))).toBe(TOO_LONG_ERROR)
  })

  it('ticks everyone but the host and people already here', () => {
    const people = [
      { name: 'Iris Calder', excerpt: '', messages: 2, host: true, joined: false },
      { name: 'Priya Raman', excerpt: '', messages: 1, host: false, joined: false },
      { name: 'Lucía Torres', excerpt: '', messages: 1, host: false, joined: true },
      { name: 'Grace Liu', excerpt: '', messages: 1, host: false, joined: false },
    ]
    expect(ticked(people)).toEqual(['Priya Raman', 'Grace Liu'])
    expect(people.map(isLocked)).toEqual([false, false, true, false])
    expect(addLabel(6)).toBe('Add 6 people')
    expect(addLabel(1)).toBe('Add 1 person')
  })

  it('ticks someone already here who has new messages, and names what the button reads', () => {
    const people = [
      { name: 'Iris Calder', excerpt: '', messages: 3, new_messages: 1, host: true, joined: false },
      { name: 'Priya Raman', excerpt: '', messages: 1, new_messages: 0, host: false, joined: true },
      { name: 'Grace Liu', excerpt: '', messages: 2, new_messages: 1, host: false, joined: true },
      { name: 'Ana', excerpt: '', messages: 1, host: false, joined: false },
    ]
    expect(ticked(people)).toEqual(['Grace Liu', 'Ana'])
    expect(people.map(personTag)).toEqual(['you', 'already here', 'new messages', null])
    expect(chosenCounts(people, ['Grace Liu', 'Ana'])).toEqual({ add: 1, update: 1 })
    expect(addLabel(0, 1)).toBe('Read new messages from 1 person')
    expect(addLabel(0, 2)).toBe('Read new messages from 2 people')
    expect(addLabel(2, 1)).toBe('Add 2 people, read new messages from 1')
  })

  it('says how far Claude got, only while it is still reading', () => {
    expect(progressText({ pending: 2, total: 6, active: true })).toBe('Claude is reading the chat… 4 of 6')
    expect(readingNow({ pending: 2, total: 6, active: true })).toBe(true)
    expect(readingNow({ pending: 2, total: 6, active: false })).toBe(false)
    expect(readingNow({ pending: 0, total: 6, active: true })).toBe(false)
    expect(readingNow(undefined)).toBe(false)
  })

  it('names the host by their first name', () => {
    expect(hostFirstName('Iris Calder')).toBe('Iris')
    expect(hostFirstName('  ')).toBe('The organizer')
  })
})

describe('importedNote', () => {
  it('says how many were added and who was already here', () => {
    expect(importedNote({ imported: [{ name: 'Priya Raman', participant_url: 'https://cp.test/p/P' }], skipped: [] })).toBe('Added 1 person.')
    expect(
      importedNote({
        imported: [],
        skipped: [
          { name: 'Iris Calder', reason: 'host' },
          { name: 'Lucía Torres', reason: 'already_here' },
          { name: 'Ben Whitaker', reason: 'already_here' },
        ],
      }),
    ).toBe('Nobody new to add. Lucía Torres and Ben Whitaker were already here.')
  })

  it('says whose new messages Claude is reading, and who to paste again for', () => {
    expect(
      importedNote({
        imported: [],
        updated: [{ name: 'Grace Liu', participant_url: 'https://cp.test/p/G' }],
        skipped: [{ name: 'Priya Raman', reason: 'already_here' }],
      }),
    ).toBe('Claude is reading the new messages from Grace Liu. Priya Raman was already here.')
    expect(
      importedNote({
        imported: [{ name: 'Ana', participant_url: 'https://cp.test/p/A' }],
        updated: [],
        skipped: [{ name: 'Grace Liu', reason: 'still_reading' }],
      }),
    ).toBe('Added 1 person. Claude is still reading what Grace Liu said before. Paste the chat again in a moment for the new messages.')
  })
})
