import type { OwnerView } from '../api'
import { Button, Icon, Tile } from './ui'

interface Props {
  view: OwnerView
  stalled: boolean
  onCheck: () => void
}

export default function ClaudeRead({ view, stalled, onCheck }: Props) {
  const rec = view.recommendation
  if (view.availabilities.length === 0 || view.workshop.status !== 'COLLECTING') return null
  return (
    <Tile as="section" aria-labelledby="claude-title" tone="muted" className="flex flex-col gap-2 shadow-none ring-1 ring-line">
      <h2 id="claude-title" className="flex items-center gap-2 text-sm font-semibold tracking-[0.06em] text-ink-soft uppercase">
        <Icon name="quote" className="text-brand" />
        Claude’s read
      </h2>
      <div aria-live="polite">
        {rec.explanation_pending ? <p className="text-ink-soft">Claude is writing why this date works…</p> : rec.explanation ? <p className="max-w-prose text-[1.0625rem]">{rec.explanation}</p> : null}
      </div>
      <p className="text-xs text-ink-soft">CommitPool picks the date from everyone’s answers; Claude only puts the reason into words.</p>
      {rec.explanation_pending && stalled && (
        <div className="flex flex-wrap items-center gap-3 text-sm text-ink-soft">
          <span>This is taking longer than usual.</span>
          <Button variant="secondary" size="sm" onClick={onCheck}>
            Check again
          </Button>
        </div>
      )}
    </Tile>
  )
}
