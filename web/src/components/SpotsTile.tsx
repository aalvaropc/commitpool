import type { OwnerView } from '../api'
import { studioCovered } from '../lib/owner'
import { count } from '../lib/plural'
import { Meter, Stat, Tile } from './ui'

export default function SpotsTile({ view }: { view: OwnerView }) {
  const t = view.tally
  const coverage = studioCovered(view)
  return (
    <Tile as="section" aria-labelledby="counts-title" className="flex flex-col gap-4">
      <h2 id="counts-title" className="text-xl">
        Spots
      </h2>
      <div className="grid grid-cols-3 gap-3">
        <Stat label="Joined" value={t.interested} />
        <Stat label="Reserved" value={<span className="text-brand">{t.authorized + t.capturing + t.pending}</span>} />
        <Stat label="Paid" value={<span className="text-paid">{t.paid}</span>} />
      </div>
      {view.proposal ? (
        <Meter capacity={t.capacity} needed={t.minimum} paid={t.paid} reserved={t.authorized + t.capturing + t.pending} />
      ) : (
        <p className="text-sm text-ink-soft">People reserve once you open reservations on a date. {t.minimum} needed to happen.</p>
      )}
      {coverage && (
        <div className="flex flex-col gap-1.5 rounded-control bg-surface-muted p-3">
          <p className="text-sm font-semibold">{coverage.text}</p>
          <div aria-hidden="true" className="h-2 rounded-full bg-line">
            <div className="h-full rounded-full bg-paid" style={{ width: `${Math.min(100, (coverage.covered / Math.max(coverage.cost, 1)) * 100)}%` }} />
          </div>
        </div>
      )}
      <p className="text-sm text-ink-soft">
        {t.seats_taken} of {count(t.capacity, 'spot')} taken · {count(t.capturing + t.pending, 'charge')} in progress
        {t.failed ? ` · ${t.failed} failed` : ''}
      </p>
    </Tile>
  )
}
