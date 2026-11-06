import { useState } from 'react'
import type { Suggestion } from '../api'
import { Avatar, Button, Icon, Tile } from './ui'

function SuggestionItem({ s }: { s: Suggestion }) {
  const [copied, setCopied] = useState<'idle' | 'copied' | 'failed'>('idle')
  const who = s.name ?? 'the group'

  async function copy() {
    try {
      await navigator.clipboard.writeText(s.message)
      setCopied('copied')
    } catch {
      setCopied('failed')
    }
  }

  return (
    <li className="flex flex-col gap-3 rounded-control p-4 ring-1 ring-line ring-inset">
      <p className="flex min-w-0 items-center gap-2.5 font-semibold">
        {s.name ? <Avatar name={s.name} size="sm" /> : <Icon name="users" className="text-brand" />}
        <span className="min-w-0 [overflow-wrap:anywhere]">{s.reason}</span>
      </p>
      <p className="rounded-control bg-surface-muted px-4 py-3 whitespace-pre-line [overflow-wrap:anywhere]">{s.message}</p>
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="secondary" size="sm" onClick={copy}>
          <Icon name="copy" size={16} />
          Copy message<span className="sr-only"> for {who}</span>
        </Button>
        <span role="status" className="text-sm text-ink-soft">
          {copied === 'copied' ? 'Copied. Paste it where you talk to them.' : copied === 'failed' ? 'Copying is blocked here; select the message and copy it.' : s.pending ? 'Claude is rewording this message…' : ''}
        </span>
      </div>
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
        {suggestions.map((s) => (
          <SuggestionItem key={`${s.kind}:${s.participant_id ?? ''}`} s={s} />
        ))}
      </ul>
    </Tile>
  )
}
