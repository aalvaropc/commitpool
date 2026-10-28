import type { OwnerView } from '../api'
import { count } from '../lib/plural'
import { blockerText } from '../lib/reading'
import OpenPayments from './OpenPayments'
import { Button, Tag } from './ui'

interface Props {
  token: string
  view: OwnerView
  explanationStalled: boolean
  onChange: () => Promise<unknown>
}

/** Which date fits the most people, why, and the button to open payments on it. */
export default function RecommendationPanel({ token, view, explanationStalled, onChange }: Props) {
  const { recommendation: rec, workshop, dates } = view
  const names = new Map(view.participants.map((p) => [p.id, p.display_name]))
  const name = (id: string) => names.get(id) ?? 'Someone'
  const perDate = new Map((rec.per_date ?? []).map((d) => [d.date_id, d]))
  const anyone = view.availabilities.length > 0
  const collecting = workshop.status === 'COLLECTING'

  return (
    <section aria-labelledby="recommendation-title" className="flex flex-col gap-5">
      <div>
        <h2 id="recommendation-title" className="text-2xl font-bold">
          Dates
        </h2>
        <p className="mt-1 text-ink-soft">
          {anyone
            ? rec.meets_minimum
              ? 'The recommended date has enough people who can come to reach the minimum.'
              : `No date has ${count(workshop.minimum, 'person', 'people')} who can come yet.`
            : 'No availability yet. Share the link so participants can say when they can come.'}
        </p>
      </div>

      {anyone && (
        <div aria-live="polite" className="rounded-sm bg-sheet px-4 py-3">
          {rec.explanation_pending ? (
            <p className="text-ink-soft">Writing the explanation…</p>
          ) : rec.explanation ? (
            <p className="max-w-prose">{rec.explanation}</p>
          ) : null}
          {rec.explanation_pending && explanationStalled && (
            <div className="mt-2 flex flex-wrap items-center gap-3 text-sm text-ink-soft">
              <span>The explanation is taking longer than usual.</span>
              <Button variant="secondary" onClick={() => void onChange()} className="px-3 py-1.5 text-sm">
                Check again
              </Button>
            </div>
          )}
        </div>
      )}

      <ol className="flex flex-col">
        {dates.map((d) => {
          const s = perDate.get(d.id)
          const compatible = s?.compatible ?? []
          const blockers = Object.entries(s?.blockers ?? {})
          const recommended = anyone && rec.date_id === d.id
          const enough = compatible.length >= workshop.minimum
          return (
            <li key={d.id} className="flex flex-col gap-3 border-t border-rule py-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-lg font-bold">{d.label}</h3>
                {recommended && <Tag tone={rec.meets_minimum ? 'paid' : 'neutral'}>Recommended</Tag>}
              </div>
              <p>
                <span className="semiwide font-semibold tabular-nums">
                  {compatible.length > 0 ? `${compatible.length} can come` : 'Nobody can come yet'}
                </span>{' '}
                ({workshop.minimum} needed){compatible.length > 0 ? `: ${compatible.map(name).join(', ')}` : ''}.
              </p>
              {blockers.length > 0 && (
                <ul className="flex flex-col gap-0.5 text-sm text-ink-soft">
                  {blockers.map(([id, why]) => (
                    <li key={id}>
                      {name(id)}: {blockerText(why)}
                    </li>
                  ))}
                </ul>
              )}
              {collecting &&
                (enough ? (
                  <div>
                    <OpenPayments token={token} date={d} workshop={workshop} onOpened={onChange} />
                  </div>
                ) : (
                  anyone && (
                    <p className="text-sm text-ink-soft">
                      Payments can open on this date once {workshop.minimum - compatible.length} more can come.
                    </p>
                  )
                ))}
            </li>
          )
        })}
      </ol>
    </section>
  )
}
