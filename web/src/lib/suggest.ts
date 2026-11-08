import type { Suggestion } from '../api'

export type SuggestionCard =
  | { kind: 'single'; key: string; suggestion: Suggestion }
  | { kind: 'group'; key: string; people: Suggestion[]; reason: string | null; pending: boolean }

const PERSONAL: ReadonlySet<Suggestion['kind']> = new Set(['clarify', 'condition'])
const NAME = '\u0000'

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

function withoutName(text: string, name: string): string {
  const first = name.trim().split(/\s+/)[0] ?? ''
  return [name.trim(), first]
    .filter(Boolean)
    .reduce((t, n) => t.replace(new RegExp(`(?<![\\p{L}\\p{N}])${escape(n)}(?![\\p{L}\\p{N}])`, 'gu'), NAME), text)
}

const keyOf = (s: Suggestion) => `${s.kind}:${s.participant_id ?? ''}`

function sharedReason(people: Suggestion[]): string | null {
  const reasons = new Set(people.map((p) => withoutName(p.reason, p.name ?? '')))
  const [only] = reasons
  if (reasons.size !== 1 || !only.startsWith(NAME) || only.indexOf(NAME, 1) !== -1) return null
  return 'Each of them' + only.slice(NAME.length)
}

export function groupSuggestions(list: Suggestion[]): SuggestionCard[] {
  const groups = new Map<string, Suggestion[]>()
  const slot = (s: Suggestion) => (s.name && !PERSONAL.has(s.kind) ? `${s.kind}\n${withoutName(s.message, s.name)}` : null)
  for (const s of list) {
    const k = slot(s)
    if (k) groups.set(k, [...(groups.get(k) ?? []), s])
  }
  const cards: SuggestionCard[] = []
  for (const s of list) {
    const k = slot(s)
    const people = k ? (groups.get(k) ?? []) : []
    if (people.length < 2) {
      cards.push({ kind: 'single', key: keyOf(s), suggestion: s })
    } else if (people[0] === s) {
      cards.push({ kind: 'group', key: keyOf(s), people, reason: sharedReason(people), pending: people.some((p) => p.pending) })
    }
  }
  return cards
}
