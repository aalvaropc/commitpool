import type { ChatImport, ChatPerson, Reading } from '../api'
import { count } from './plural'

export const MAX_CHAT_CHARS = 20000

export const ZIP_ERROR = "That's a .zip. Export the chat without media to get a .txt, or paste the messages."
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

export async function readChatFile(file: File): Promise<string> {
  if (/\.zip$/i.test(file.name) || ZIP_TYPES.has(file.type) || (await startsLikeZip(file))) throw new Error(ZIP_ERROR)
  if (file.size > 4 * MAX_CHAT_CHARS) throw new Error(TOO_LONG_ERROR)
  const text = await file.text()
  if (text.includes('\u0000')) throw new Error(NOT_TEXT_ERROR)
  if (chatLength(text) > MAX_CHAT_CHARS) throw new Error(TOO_LONG_ERROR)
  return text
}

export function chatProblem(text: string): string | null {
  if (!text.trim()) return EMPTY_ERROR
  if (chatLength(text) > MAX_CHAT_CHARS) return TOO_LONG_ERROR
  return null
}

export function ticked(people: ChatPerson[]): string[] {
  return people.filter((p) => !p.host && !p.joined).map((p) => p.name)
}

export function addLabel(n: number): string {
  return `Add ${count(n, 'person', 'people')}`
}

export function personTag(p: ChatPerson): string | null {
  if (p.host) return 'you'
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
  const parts = [res.imported.length > 0 ? `Added ${count(res.imported.length, 'person', 'people')}.` : 'Nobody new to add.']
  const here = res.skipped.filter((s) => s.reason === 'already_here').map((s) => s.name)
  if (here.length > 0) parts.push(`${names(here)} ${here.length === 1 ? 'was' : 'were'} already here.`)
  return parts.join(' ')
}
