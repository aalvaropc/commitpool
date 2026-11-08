import { useState } from 'react'
import type { OwnerView } from '../api'
import { demoParticipants, demoStep, type DemoParticipant, type DemoStep } from '../lib/demo'
import { ButtonLink, Icon, Pill, Tile } from './ui'

function Guide({ step, people, guided }: { step: DemoStep | null; people: DemoParticipant[]; guided: boolean }) {
  const [open, setOpen] = useState(guided)
  const [was, setWas] = useState(guided)
  if (was !== guided) {
    setWas(guided)
    setOpen(guided)
  }
  return (
    <Tile as="section" aria-labelledby="demo-guide-title" className="ring-2 ring-brand/40 sm:p-6">
      <details open={open} onToggle={(e) => setOpen(e.currentTarget.open)} className="group">
        <summary className="-m-2 flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 rounded-control p-2 [&::-webkit-details-marker]:hidden">
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
            <h2 id="demo-guide-title" className="text-xl">
              You are Maya now
            </h2>
            <Pill tone="attention">Demo workshop · prepared data</Pill>
          </span>
          <Icon name="arrowRight" className="shrink-0 text-brand transition-transform duration-200 group-open:rotate-90" />
        </summary>
        <div className="mt-4 flex flex-col gap-3">
          <div aria-live="polite">
            {step && (
              <p className="flex max-w-prose items-start gap-2 rounded-control bg-brand-wash px-4 py-3 font-semibold text-ink">
                <Icon name="spark" size={20} className="mt-0.5 shrink-0 text-brand" />
                <span>
                  <span className="sr-only">Next in the demo: </span>
                  {step.text}
                </span>
              </p>
            )}
          </div>
          <div className="flex flex-col gap-2">
            <p aria-hidden="true" className="text-sm font-semibold text-ink-soft">
              Act as
            </p>
            <ul className="flex flex-wrap gap-2" aria-label="Open the page of a participant">
              {people.map((p) => (
                <li key={p.path}>
                  <ButtonLink to={p.path} target="_blank" rel="noopener" variant={step?.primary === p.name ? 'primary' : 'secondary'} size="sm">
                    <span className="sr-only">Act as </span>
                    {p.name}
                    <span className="sr-only"> (opens in a new tab)</span>
                    <Icon name="arrowRight" size={16} />
                  </ButtonLink>
                </li>
              ))}
            </ul>
          </div>
          <p className="max-w-prose text-sm text-ink-soft">
            A prepared class: its six participants and their answers were written for the demo, and Claude reads each answer as it arrives. Open
            reservations, then reserve as a participant with a PayPal Sandbox account.
          </p>
        </div>
      </details>
    </Tile>
  )
}

export default function DemoGuide({ view }: { view: OwnerView }) {
  if (!view.workshop.demo) return null
  const people = demoParticipants(view)
  const step = demoStep(view, people.map((p) => p.name))
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
  const guided = step !== null || (view.workshop.status === 'COLLECTING' && !view.proposal)
  return <Guide step={step} people={people} guided={guided} />
}
