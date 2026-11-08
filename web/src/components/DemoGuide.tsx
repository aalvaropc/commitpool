import type { OwnerView } from '../api'
import { demoParticipants } from '../lib/demo'
import { ButtonLink, Icon, Pill, Tile } from './ui'

export default function DemoGuide({ view }: { view: OwnerView }) {
  if (!view.workshop.demo) return null
  const people = demoParticipants(view)
  if (people.length === 0) {
    return (
      <Tile as="section" aria-labelledby="demo-guide-title" tone="muted" className="flex flex-col gap-2 shadow-none ring-1 ring-line">
        <div className="flex flex-wrap items-center gap-2">
          <h2 id="demo-guide-title" className="text-lg">
            Demo workshop
          </h2>
          <Pill tone="attention">Demo workshop · prepared data</Pill>
        </div>
        <p className="max-w-prose text-sm text-ink-soft">This class and its participants were prepared for the demo. Payments run on the PayPal Sandbox.</p>
      </Tile>
    )
  }
  return (
    <Tile as="section" aria-labelledby="demo-guide-title" className="flex flex-col gap-3 ring-2 ring-brand/40 sm:p-6">
      <div className="flex flex-wrap items-center gap-2">
        <h2 id="demo-guide-title" className="text-xl">
          You are Maya now
        </h2>
        <Pill tone="attention">Demo workshop · prepared data</Pill>
      </div>
      <p className="max-w-prose text-ink-soft">
        This is the organizer page of a prepared class. Its six participants and their answers were written for the demo; watch Claude read each
        answer as it arrives. Open reservations, then reserve as a participant with a PayPal Sandbox account.
      </p>
      <ul className="flex flex-wrap gap-2" aria-label="Open the page of a participant">
        {people.map((p) => (
          <li key={p.path}>
            <ButtonLink to={p.path} target="_blank" rel="noopener" variant="secondary" size="sm">
              Act as {p.name}
              <span className="sr-only"> (opens in a new tab)</span>
              <Icon name="arrowRight" size={16} />
            </ButtonLink>
          </li>
        ))}
      </ul>
    </Tile>
  )
}
