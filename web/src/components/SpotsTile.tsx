import type { OwnerView } from '../api'
import { sessionCovered } from '../lib/owner'
import { count } from '../lib/plural'
import { Meter, Stat, Tile } from './ui'

export default function SpotsTile({ view }: { view: OwnerView }) {
  const t = view.tally
  const coverage = view.proposal ? sessionCovered(view) : null
  const reserved = t.authorized + t.capturing + t.pending
  const charging = t.capturing + t.pending
  return (
    <Tile as="section" aria-labelledby="counts-title" className="flex flex-col gap-4">
      <h2 id="counts-title" className="text-xl">
        Spots
      </h2>
      <div className="grid grid-cols-3 gap-3">
        <Stat label="Joined" value={t.interested} />
        <Stat label="Reserved" value={<span className="text-brand">{reserved}</span>} />
        <Stat label="Paid" value={<span className="text-paid">{t.paid}</span>} />
      </div>
      {view.proposal ? (
        <div className="flex flex-col gap-2">
          <Meter capacity={t.capacity} needed={t.minimum} paid={t.paid} reserved={reserved} caption={false} />
          <p className="text-sm text-ink-soft">
            {t.minimum} needed · {t.seats_taken} of {count(t.capacity, 'spot')} taken
            {charging ? ` · ${count(charging, 'charge')} in progress` : ''}
            {t.failed ? ` · ${t.failed} failed` : ''}
          </p>
        </div>
      ) : (
        <p className="text-sm text-ink-soft">
          People reserve once you open reservations on a date. {t.minimum} needed to happen, {count(t.capacity, 'spot')}.
        </p>
      )}
      {coverage && (
        <div className="flex flex-col gap-1.5 rounded-control bg-surface-muted p-3">
          <p className="text-sm font-semibold">{coverage.text}</p>
          <div aria-hidden="true" className="h-2 rounded-full bg-line">
            <div className="h-full rounded-full bg-paid" style={{ width: `${Math.min(100, (coverage.covered / Math.max(coverage.cost, 1)) * 100)}%` }} />
          </div>
        </div>
      )}
    </Tile>
  )
}
