import { useInView } from '../../lib/inView'
import { Icon, type IconName } from '../ui'

type StepState = 'done' | 'ask' | 'work'

const steps: { state: StepState; title: string; detail: string }[] = [
  { state: 'done', title: 'Read 6 messages', detail: 'In English and Spanish, the way people text.' },
  { state: 'done', title: 'Matched every answer to every date', detail: 'Priya and Ben can come Saturday; Lucía and Tomás, Sunday. Hannah has to leave by 11:30, before the class ends.' },
  { state: 'ask', title: 'Grace said “I might be able to swap one”', detail: 'She didn’t say which morning, and the class runs 10 AM–12 PM, so Claude asked her: can she make either morning, and which? Saturday has 2 of the 3 Iris needs.' },
  { state: 'done', title: 'Grace swapped Saturday', detail: 'Claude read her answer together with her first message. Saturday reached 3, so the server let Iris open reservations.' },
  { state: 'work', title: 'Lining up the nudges', detail: 'While reservations are open, Claude drafts a reminder for anyone who can come but hasn’t reserved. Iris decides what to send.' },
]

const marks: Record<StepState, { icon: IconName; tone: string }> = {
  done: { icon: 'check', tone: 'bg-paid-wash text-paid' },
  ask: { icon: 'help', tone: 'bg-maybe-wash text-maybe' },
  work: { icon: 'spark', tone: 'bg-brand-wash text-brand' },
}

export default function AgentFeed() {
  const [ref, seen] = useInView<HTMLOListElement>()
  return (
    <ol ref={ref} aria-label="What Claude did for Iris" className="flex flex-col gap-3">
      {steps.map((s, i) => (
        <li
          key={s.title}
          className={`flex items-start gap-4 rounded-3xl bg-surface p-4 sm:p-5 ${seen ? 'rise' : 'opacity-0'}`}
          style={seen ? { animationDelay: `${i * 0.5}s` } : undefined}
        >
          <span aria-hidden="true" className={`grid size-10 shrink-0 place-items-center rounded-full ${marks[s.state].tone}`}>
            <Icon name={marks[s.state].icon} size={20} />
          </span>
          <div className="flex min-w-0 flex-col gap-0.5">
            <p className="font-display text-lead leading-tight font-bold">{s.title}</p>
            <p className="text-ink-soft">{s.detail}</p>
          </div>
        </li>
      ))}
    </ol>
  )
}
