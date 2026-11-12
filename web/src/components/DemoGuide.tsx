import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import type { OwnerView } from '../api'
import { demoParticipants, demoStep, type DemoParticipant, type DemoStep } from '../lib/demo'
import { DEMO_ACTIONS, useDemoAction } from '../lib/demoActions'
import { Button, ButtonLink, DemoTag, Icon, Notice, Pill, Tile } from './ui'

function StepText({ step }: { step: DemoStep }) {
  const at = step.link ? step.text.indexOf(step.link.label) : -1
  if (!step.link || at < 0) return <>{step.text}</>
  return (
    <>
      {step.text.slice(0, at)}
      <Link to={step.link.to} className="underline">
        {step.link.label}
      </Link>
      {step.text.slice(at + step.link.label.length)}
    </>
  )
}

interface GuideProps {
  token: string
  step: DemoStep | null
  people: DemoParticipant[]
  guided: boolean
  onChange: () => Promise<unknown>
}

function Guide({ token, step, people, guided, onChange }: GuideProps) {
  const { busy, result, run } = useDemoAction(token, onChange)
  const action = step?.action
  const [open, setOpen] = useState(guided || Boolean(action))
  useEffect(() => {
    if (action) setOpen(true)
  }, [action])
  return (
    <Tile as="section" aria-labelledby="demo-guide-title" className="flex flex-col gap-2 ring-2 ring-brand/40 sm:p-6">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <h2 id="demo-guide-title" className="text-xl">
          You are Iris now
        </h2>
        <Pill tone="attention">Demo workshop · prepared data</Pill>
      </div>
      {action && (
        <div>
          <Button variant="demo" onClick={() => void run(action)} busy={busy} busyLabel={DEMO_ACTIONS[action].busy}>
            <DemoTag /> {DEMO_ACTIONS[action].label}
          </Button>
        </div>
      )}
      {result && result.action === action && (
        <Notice tone={result.ok ? 'neutral' : 'attention'} role={result.ok ? 'status' : 'alert'}>
          {result.text}
        </Notice>
      )}
      <details open={open} onToggle={(e) => setOpen(e.currentTarget.open)} className="group">
        <summary className="-mx-2 flex min-h-11 w-fit cursor-pointer list-none items-center gap-2 rounded-control px-2 font-semibold text-brand [&::-webkit-details-marker]:hidden">
          <Icon name="arrowRight" size={18} className="shrink-0 transition-transform duration-200 group-open:rotate-90" />
          Demo steps
        </summary>
        <div className="mt-2 flex flex-col gap-3">
          <div aria-live="polite">
            {step && (
              <p className="flex max-w-prose items-start gap-2 rounded-control bg-brand-wash px-4 py-3 font-semibold text-ink">
                <Icon name="spark" size={20} className="mt-0.5 shrink-0 text-brand" />
                <span>
                  <span className="sr-only">Next in the demo: </span>
                  <StepText step={step} />
                </span>
              </p>
            )}
          </div>
          {people.length > 0 && (
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
          )}
          <p className="max-w-prose text-sm text-ink-soft">
            A prepared class: Iris’s group chat and the six people in it were written for the demo, and Claude reads each person’s message when you add them.
            Open reservations, then reserve as a participant with a PayPal Sandbox account.
          </p>
        </div>
      </details>
    </Tile>
  )
}

export default function DemoGuide({ token, view, onChange }: { token: string; view: OwnerView; onChange: () => Promise<unknown> }) {
  if (!view.workshop.demo) return null
  const people = demoParticipants(view)
  const step = demoStep(view, people.map((p) => p.name))
  if (people.length === 0 && step === null) {
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
  return <Guide token={token} step={step} people={people} guided={guided} onChange={onChange} />
}
