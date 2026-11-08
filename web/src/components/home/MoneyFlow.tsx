import { useEffect, useState } from 'react'
import { useInView } from '../../lib/inView'
import { Avatar, Icon, Pill, SegmentedControl } from '../ui'

type Scenario = 'enough' | 'short'

const options = [
  { value: 'enough' as const, label: '3 people reserve' },
  { value: 'short' as const, label: 'Only 2 reserve' },
]

const people: Record<Scenario, string[]> = {
  enough: ['Priya Raman', 'Ben Whitaker', 'Grace Liu'],
  short: ['Priya Raman', 'Ben Whitaker'],
}

export default function MoneyFlow() {
  const [ref, seen] = useInView<HTMLDivElement>()
  const [scenario, setScenario] = useState<Scenario>('enough')
  const [settled, setSettled] = useState(false)
  useEffect(() => {
    setSettled(false)
    if (!seen) return
    const timer = setTimeout(() => setSettled(true), 1400)
    return () => clearTimeout(timer)
  }, [scenario, seen])
  const enough = scenario === 'enough'
  const total = people[scenario].length * 45
  return (
    <div ref={ref} className="flex min-w-0 flex-col gap-6 rounded-[2rem] bg-surface p-5 sm:p-8">
      <SegmentedControl legend="How many reserve before the deadline" value={scenario} options={options} onChange={setScenario} />
      <ul className="flex flex-col gap-2" aria-label="Reservations">
        {people[scenario].map((name) => (
          <li key={`${scenario}-${name}`} className="rise flex flex-wrap items-center justify-between gap-x-3 gap-y-2 rounded-2xl bg-surface-muted px-4 py-3">
            <span className="flex min-w-0 items-center gap-3 font-semibold">
              <Avatar name={name} size="sm" />
              {name}
            </span>
            {!settled ? (
              <Pill tone="hold">$45 on hold</Pill>
            ) : enough ? (
              <Pill tone="paid">$45 charged</Pill>
            ) : (
              <Pill tone="neutral">Released · never charged</Pill>
            )}
          </li>
        ))}
      </ul>
      <p aria-live="polite" className="flex items-start gap-3 font-display text-lead leading-tight font-bold">
        <Icon name={enough ? 'check' : 'x'} size={26} className={`mt-0.5 ${enough ? 'text-paid' : 'text-no'}`} />
        {!settled ? 'Holding…' : enough ? `It’s on. $${total} collected covers the $120 studio.` : 'Not enough by the deadline. Every hold is released. Nobody paid.'}
      </p>
    </div>
  )
}
