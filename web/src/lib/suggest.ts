import type { Suggestion } from '../api'

export type SuggestionCard = { kind: 'single'; key: string; suggestion: Suggestion } | { kind: 'group'; key: string; people: Suggestion[]; pending: boolean }

const GROUPED: ReadonlySet<Suggestion['kind']> = new Set(['reserve'])

const groupable = (s: Suggestion) => GROUPED.has(s.kind) && !!s.name

export function groupSuggestions(list: Suggestion[]): SuggestionCard[] {
  const groups = new Map<string, Suggestion[]>()
  for (const s of list) if (groupable(s)) groups.set(s.kind, [...(groups.get(s.kind) ?? []), s])
  const cards: SuggestionCard[] = []
  for (const s of list) {
    const people = groupable(s) ? groups.get(s.kind) : undefined
    if (!people) cards.push({ kind: 'single', key: `${s.kind}:${s.participant_id ?? ''}`, suggestion: s })
    else if (people[0] === s) cards.push({ kind: 'group', key: s.kind, people, pending: people.some((p) => p.pending) })
  }
  return cards
}

export function firstName(name: string): string {
  return name.trim().split(/\s+/)[0] || name
}

function nameList(names: string[]): string {
  return names.length < 2 ? names.join('') : `${names.slice(0, -1).join(', ')} and ${names.at(-1)}`
}

export function groupTitle(people: Suggestion[]): string {
  if (people.length === 1) return people[0].reason
  return `${people.length} people can come but haven’t reserved: ${nameList(people.map((p) => firstName(p.name ?? '')))}`
}

export function previewLabel(person: Suggestion): string {
  return `Message for ${firstName(person.name ?? '')} — each one is written for that person`
}
