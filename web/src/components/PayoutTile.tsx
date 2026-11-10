import { useState } from 'react'
import { api, type OwnerView } from '../api'
import { payoutBreakdown, payoutCheckable, payoutIds, payoutSummary } from '../lib/payout'
import { Button, Notice, Pill, Tile } from './ui'

const PILL: Record<string, string> = {
  no_email: 'Not set up',
  off: 'Off',
  scheduled: 'Scheduled',
  waiting: 'Waiting',
  sending: 'Sending',
  paid: 'Paid out',
  unclaimed: 'Not claimed yet',
  failed: 'Failed',
  unknown: 'Needs a check',
}

interface Props {
  token: string
  view: OwnerView
  onChange: () => Promise<unknown>
}

export default function PayoutTile({ token, view, onChange }: Props) {
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const p = view.payout
  if (!p) return null
  const summary = payoutSummary(p, view.workshop.timezone)
  const breakdown = payoutBreakdown(p)
  const ids = payoutIds(p)

  async function check() {
    setChecking(true)
    setError(null)
    try {
      await api.operatorCheckPayout(token)
      await onChange()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'PayPal could not be checked right now.')
    } finally {
      setChecking(false)
    }
  }

  return (
    <Tile as="section" aria-labelledby="payout-title" className="flex flex-col gap-3 sm:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="payout-title" className="text-2xl">
          Your payout
        </h2>
        <Pill tone={summary.tone}>{PILL[p.state] ?? PILL.unknown}</Pill>
      </div>
      <p role="status" className="font-semibold [overflow-wrap:anywhere]">
        {summary.title}
      </p>
      <p className="max-w-prose text-sm text-ink-soft [overflow-wrap:anywhere]">{summary.detail}</p>
      {breakdown && (
        <p className="text-sm font-semibold tabular-nums">
          {p.estimated ? 'So far: ' : ''}
          {breakdown}
        </p>
      )}
      {ids.length > 0 && (
        <details className="text-sm text-ink-soft">
          <summary className="-my-2 w-fit cursor-pointer rounded-sm py-3 font-medium text-ink-soft hover:text-ink">PayPal details</summary>
          <dl className="mt-1 flex flex-col gap-1">
            {ids.map(([label, value]) => (
              <div key={label} className="flex flex-wrap items-baseline gap-x-3">
                <dt className="w-28 shrink-0">{label}</dt>
                <dd>
                  <code className="font-mono text-sm text-ink [overflow-wrap:anywhere]">{value}</code>
                </dd>
              </div>
            ))}
          </dl>
        </details>
      )}
      {payoutCheckable(p) && (
        <div>
          <Button variant="secondary" size="sm" onClick={() => void check()} busy={checking} busyLabel="Checking…">
            Check the payout with PayPal
          </Button>
        </div>
      )}
      {error && (
        <Notice tone="attention" role="alert">
          {error}
        </Notice>
      )}
    </Tile>
  )
}
