import { useState } from 'react'
import type { Suggestion } from '../api'
import { firstName, groupSuggestions, groupTitle, previewLabel } from '../lib/suggest'
import { Avatar, Button, Icon, Tile } from './ui'

function useCopy() {
  const [copied, setCopied] = useState<boolean | null>(null)
  async function copy(s: Suggestion) {
    try {
      await navigator.clipboard.writeText(s.message)
      setCopied(true)
    } catch {
      setCopied(false)
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
          {copied ? 'Copied. Paste it where you talk to them.' : copied === false ? 'Copying is blocked here; select the message and copy it.' : s.pending ? 'Claude is rewording this message…' : ''}
        </span>
      </div>
    </li>
  )
}

const personKey = (s: Suggestion) => s.participant_id ?? s.name ?? ''

function SuggestionGroup({ people, pending }: { people: Suggestion[]; pending: boolean }) {
  const [shown, setShown] = useState<string | null>(null)
  const [copied, setCopied] = useState<{ key: string; ok: boolean } | null>(null)
  const preview = people.find((p) => personKey(p) === shown) ?? people[0]
  const copiedPerson = copied ? people.find((p) => personKey(p) === copied.key) : undefined
  const many = people.length > 1

  async function copy(s: Suggestion) {
    setShown(personKey(s))
    try {
      await navigator.clipboard.writeText(s.message)
      setCopied({ key: personKey(s), ok: true })
    } catch {
      setCopied({ key: personKey(s), ok: false })
    }
  }

  return (
    <li className={card}>
      <p className="flex min-w-0 items-center gap-2.5 font-semibold">
        {many ? <Icon name="users" className="shrink-0 text-brand" /> : <Avatar name={people[0].name ?? '?'} size="sm" />}
        <span className="min-w-0 [overflow-wrap:anywhere]">{groupTitle(people)}</span>
      </p>
      <div className="flex flex-col gap-1.5">
        {many && <p className="text-sm font-semibold text-ink-soft [overflow-wrap:anywhere]">{previewLabel(preview)}</p>}
        <p className={bubble}>{preview.message}</p>
      </div>
      <div className="flex flex-col gap-2">
        <p aria-hidden="true" className="text-sm font-semibold text-ink-soft">
          Copy it for
        </p>
        <ul aria-label="Copy the message for each person" className="flex flex-wrap gap-2">
          {people.map((s) => (
            <li key={personKey(s)}>
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
        {copiedPerson && copied?.ok
          ? `Copied the message for ${copiedPerson.name}. Paste it where you talk to them.`
          : copiedPerson
            ? `Copying is blocked here; select the message for ${firstName(copiedPerson.name ?? '')} above and copy it.`
            : pending
              ? `Claude is rewording ${many ? 'these messages' : 'this message'}…`
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
          c.kind === 'single' ? <SuggestionItem key={c.key} s={c.suggestion} /> : <SuggestionGroup key={c.key} people={c.people} pending={c.pending} />,
        )}
      </ul>
    </Tile>
  )
}
