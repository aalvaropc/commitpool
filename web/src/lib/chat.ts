import type { ChatImport, ChatPerson, Reading } from '../api'
import { count } from './plural'

export const MAX_CHAT_CHARS = 20000

export const ZIP_ERROR = 'That’s a .zip. Export the chat without media to get a .txt, or paste the messages.'
export const NOT_UTF8_ERROR = 'This file isn’t UTF-8 text. Export it again from WhatsApp, or paste the messages.'
export const TOO_LONG_ERROR = 'The chat is longer than 20,000 characters. Paste only the part where people answered.'
export const NOT_TEXT_ERROR = 'That file is not a text export. Export the chat without media to get a .txt, or paste the messages.'
export const EMPTY_ERROR = 'Paste the group chat first.'

const ZIP_MAGIC = [0x50, 0x4b, 0x03, 0x04]
const ZIP_TYPES = new Set(['application/zip', 'application/x-zip-compressed', 'application/x-zip'])

export function chatLength(text: string): number {
  return [...text].length
}

async function startsLikeZip(file: File): Promise<boolean> {
  const head = new Uint8Array(await file.slice(0, ZIP_MAGIC.length).arrayBuffer())
  return head.length === ZIP_MAGIC.length && ZIP_MAGIC.every((b, i) => head[i] === b)
}

function encodingOf(bytes: Uint8Array): 'utf-8' | 'utf-16le' | 'utf-16be' {
  if (bytes[0] === 0xff && bytes[1] === 0xfe) return 'utf-16le'
  if (bytes[0] === 0xfe && bytes[1] === 0xff) return 'utf-16be'
  return 'utf-8'
}

function decode(bytes: Uint8Array): string {
  let text: string
  try {
    text = new TextDecoder(encodingOf(bytes), { fatal: true }).decode(bytes)
  } catch {
    throw new Error(NOT_UTF8_ERROR)
  }
  if (text.includes('\ufffd')) throw new Error(NOT_UTF8_ERROR)
  return text
}

export async function readChatFile(file: File): Promise<string> {
  if (/\.zip$/i.test(file.name) || ZIP_TYPES.has(file.type) || (await startsLikeZip(file))) throw new Error(ZIP_ERROR)
  if (file.size > 4 * MAX_CHAT_CHARS + 4) throw new Error(TOO_LONG_ERROR)
  const bytes = new Uint8Array(await file.arrayBuffer())
  if (bytes.includes(0) && encodingOf(bytes) === 'utf-8') throw new Error(NOT_TEXT_ERROR)
  const text = decode(bytes)
  if (chatLength(text) > MAX_CHAT_CHARS) throw new Error(TOO_LONG_ERROR)
  return text
}

export function chatProblem(text: string): string | null {
  if (!text.trim()) return EMPTY_ERROR
  if (chatLength(text) > MAX_CHAT_CHARS) return TOO_LONG_ERROR
  return null
}

export function hasNewMessages(p: ChatPerson): boolean {
  return !p.host && p.joined && (p.new_messages ?? 0) > 0
}

export function isLocked(p: ChatPerson): boolean {
  return p.host || (p.joined && !hasNewMessages(p))
}

export function ticked(people: ChatPerson[]): string[] {
  return people.filter((p) => !isLocked(p)).map((p) => p.name)
}

export function addLabel(n: number, updates = 0): string {
  const add = `Add ${count(n, 'person', 'people')}`
  if (updates === 0) return add
  if (n === 0) return `Read new messages from ${count(updates, 'person', 'people')}`
  return `${add}, read new messages from ${updates}`
}

export function chosenCounts(people: ChatPerson[], chosen: string[]): { add: number; update: number } {
  const picked = people.filter((p) => chosen.includes(p.name))
  const update = picked.filter(hasNewMessages).length
  return { add: picked.length - update, update }
}

export function personTag(p: ChatPerson): string | null {
  if (p.host) return 'you'
  if (hasNewMessages(p)) return 'new messages'
  if (p.joined) return 'already here'
  return null
}

export function readingNow(r: Reading | undefined): boolean {
  return !!r && r.active && r.pending > 0
}

export function progressText(r: Reading): string {
  return `Claude is reading the chat… ${r.total - r.pending} of ${r.total}`
}

export function hostFirstName(hostName: string | undefined): string {
  return hostName?.trim().split(/\s+/)[0] || 'The organizer'
}

function names(list: string[]): string {
  if (list.length <= 1) return list.join('')
  return `${list.slice(0, -1).join(', ')} and ${list.at(-1)}`
}

export function importedNote(res: ChatImport): string {
  const updated = (res.updated ?? []).map((u) => u.name)
  const parts: string[] = []
  if (res.imported.length > 0) parts.push(`Added ${count(res.imported.length, 'person', 'people')}.`)
  if (updated.length > 0) parts.push(`Claude is reading the new messages from ${names(updated)}.`)
  if (parts.length === 0) parts.push('Nobody new to add.')
  const here = res.skipped.filter((s) => s.reason === 'already_here').map((s) => s.name)
  if (here.length > 0) parts.push(`${names(here)} ${here.length === 1 ? 'was' : 'were'} already here.`)
  const busy = res.skipped.filter((s) => s.reason === 'still_reading').map((s) => s.name)
  if (busy.length > 0) parts.push(`Claude is still reading what ${names(busy)} said before. Paste the chat again in a moment for the new messages.`)
  return parts.join(' ')
}
