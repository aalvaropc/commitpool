import { useInView } from '../../lib/inView'
import { Icon, type IconName } from '../ui'

type StepState = 'done' | 'ask' | 'work'

const steps: { state: StepState; title: string; detail: string }[] = [
  { state: 'done', title: 'Read 6 messages', detail: 'In plain English, the way people text.' },
  { state: 'done', title: 'Matched every answer to every date', detail: 'Priya, Hannah and Tomás can come Saturday. Ben can if it ends by 6 PM.' },
  { state: 'ask', title: 'Grace said “ideally in the afternoon”', detail: 'Not enough to count her. Claude drafted a question for Maya to send.' },
  { state: 'done', title: 'Recommended Sat, Nov 14', detail: '4 can come. You need 3. The server decided; Claude explained.' },
  { state: 'work', title: 'Watching the reservations', detail: 'Charges only start once 3 people have reserved.' },
]

const marks: Record<StepState, { icon: IconName; tone: string }> = {
  done: { icon: 'check', tone: 'bg-paid-wash text-paid' },
  ask: { icon: 'help', tone: 'bg-maybe-wash text-maybe' },
  work: { icon: 'spark', tone: 'bg-brand-wash text-brand' },
}

export default function AgentFeed() {
  const [ref, seen] = useInView<HTMLOListElement>()
  return (
    <ol ref={ref} aria-label="What Claude did for Maya" className="flex flex-col gap-3">
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
