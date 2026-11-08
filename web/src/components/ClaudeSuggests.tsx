import { useState } from 'react'
import type { Suggestion } from '../api'
import { groupSuggestions } from '../lib/suggest'
import { Avatar, Button, Icon, Tile } from './ui'

type Copied = { who: string; ok: boolean } | null

function useCopy() {
  const [copied, setCopied] = useState<Copied>(null)
  async function copy(s: Suggestion) {
    const who = s.name ?? 'the group'
    try {
      await navigator.clipboard.writeText(s.message)
      setCopied({ who, ok: true })
    } catch {
      setCopied({ who, ok: false })
    }
  }
  return { copied, copy }
}

const card = 'flex flex-col gap-3 rounded-control p-4 ring-1 ring-line ring-inset'
const bubble = 'rounded-control bg-surface-muted px-4 py-3 whitespace-pre-line [overflow-wrap:anywhere]'

function SuggestionItem({ s }: { s: Suggestion }) {
  const { copied, copy } = useCopy()
  return (
    <li className={card}>
      <p className="flex min-w-0 items-center gap-2.5 font-semibold">
        {s.name ? <Avatar name={s.name} size="sm" /> : <Icon name="users" className="text-brand" />}
        <span className="min-w-0 [overflow-wrap:anywhere]">{s.reason}</span>
      </p>
      <p className={bubble}>{s.message}</p>
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="secondary" size="sm" onClick={() => void copy(s)}>
          <Icon name="copy" size={16} />
          Copy message<span className="sr-only"> for {s.name ?? 'the group'}</span>
        </Button>
        <span role="status" className="text-sm text-ink-soft">
          {copied?.ok ? 'Copied. Paste it where you talk to them.' : copied ? 'Copying is blocked here; select the message and copy it.' : s.pending ? 'Claude is rewording this message…' : ''}
        </span>
      </div>
    </li>
  )
}

function SuggestionGroup({ people, reason, pending }: { people: Suggestion[]; reason: string | null; pending: boolean }) {
  const { copied, copy } = useCopy()
  const first = people[0]
  const firstName = (first.name ?? '').trim().split(/\s+/)[0]
  return (
    <li className={card}>
      <div className="flex flex-col gap-1">
        <p className="flex items-center gap-2.5 font-semibold">
          <Icon name="users" className="shrink-0 text-brand" />
          One message for {people.length} people
        </p>
        {reason && <p className="text-sm text-ink-soft [overflow-wrap:anywhere]">{reason}</p>}
      </div>
      <p className={bubble}>{first.message}</p>
      <p className="text-sm text-ink-soft">Shown with {firstName}’s name; each copy carries that person’s own name.</p>
      <div className="flex flex-col gap-2">
        <p aria-hidden="true" className="text-sm font-semibold text-ink-soft">
          Copy it for
        </p>
        <ul aria-label="Copy the message for each person" className="flex flex-wrap gap-2">
          {people.map((s) => (
            <li key={s.participant_id ?? s.name}>
              <Button variant="secondary" size="sm" onClick={() => void copy(s)} className="pl-2">
                <Avatar name={s.name ?? '?'} size="sm" />
                <span className="sr-only">Copy message for </span>
                {s.name}
                <Icon name="copy" size={16} className="text-ink-soft" />
              </Button>
            </li>
          ))}
        </ul>
      </div>
      <span role="status" className="text-sm text-ink-soft">
        {copied?.ok
          ? `Copied the message for ${copied.who}. Paste it where you talk to them.`
          : copied
            ? 'Copying is blocked here; select the message and copy it.'
            : pending
              ? 'Claude is rewording these messages…'
              : ''}
      </span>
    </li>
  )
}

export default function ClaudeSuggests({ suggestions }: { suggestions: Suggestion[] }) {
  if (suggestions.length === 0) return null
  return (
    <Tile as="section" aria-labelledby="suggests-title" className="flex flex-col gap-4 sm:p-6">
      <div className="flex flex-col gap-1">
        <h2 id="suggests-title" className="flex items-center gap-2 text-xl">
          <Icon name="quote" className="text-brand" />
          Claude suggests
        </h2>
        <p className="text-sm text-ink-soft">CommitPool picks who to nudge; Claude drafts the words. You decide whether to send them.</p>
      </div>
      <ul className="flex flex-col gap-3">
        {groupSuggestions(suggestions).map((c) =>
          c.kind === 'single' ? <SuggestionItem key={c.key} s={c.suggestion} /> : <SuggestionGroup key={c.key} people={c.people} reason={c.reason} pending={c.pending} />,
        )}
      </ul>
    </Tile>
  )
}
