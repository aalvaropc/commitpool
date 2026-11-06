import SeatMeter from '../components/SeatMeter'
import { ButtonLink } from '../components/ui'

const STEPS = [
  {
    title: 'Propose dates',
    body: 'Set the price, how many people it takes to happen and how many spots there are, and offer a few candidate dates.',
  },
  {
    title: 'Participants say when they can come',
    body: 'In their own words. Each message is read date by date, and everyone can correct how theirs was read.',
  },
  {
    title: 'You pick the date and open reservations',
    body: 'The terms lock and people have 24 hours by default to reserve.',
  },
  {
    title: 'Holds now, charges only if the group is real',
    body: 'Each person reserves with a PayPal hold. Holds are charged once enough people have reserved. If not enough people pay by the deadline, the workshop is cancelled, holds are released and any charge is refunded.',
  },
]

export default function Home() {
  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-12 sm:px-6 sm:py-20">
      <title>CommitPool – book a workshop only when enough people pay</title>
      <div className="grid gap-12 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] lg:items-end">
        <div className="flex flex-col gap-6">
          <h1 className="wide text-4xl leading-[1.05] font-bold sm:text-6xl">Run the workshop when the group has paid.</h1>
          <p className="max-w-[34rem] text-lg text-ink-soft">
            People reserve with a PayPal hold. Nobody is charged until enough of them reserve, and if the group falls short, every hold
            is released.
          </p>
          <div>
            <ButtonLink to="/new" className="px-6 py-3 text-lg">
              Create a workshop
            </ButtonLink>
          </div>
        </div>
        <div className="rounded-sm border border-rule bg-sheet p-5 sm:p-7">
          <p className="semiwide font-bold">Saturday clay workshop</p>
          <p className="mb-5 text-sm text-ink-soft">Example: 3 of 4 paid, 2 more reserved.</p>
          <SeatMeter capacity={8} minimum={4} paid={3} authorized={2} label="Example" size="lg" />
        </div>
      </div>

      <section aria-labelledby="how-title" className="mt-20 flex flex-col gap-8">
        <h2 id="how-title" className="text-2xl font-bold">
          How it works
        </h2>
        <ol className="grid gap-x-10 gap-y-8 sm:grid-cols-2">
          {STEPS.map((s, i) => (
            <li key={s.title} className="flex gap-4 border-t border-rule pt-4">
              <span aria-hidden="true" className="wide text-2xl font-bold text-ink-soft tabular-nums">
                {i + 1}
              </span>
              <div className="flex flex-col gap-1">
                <h3 className="text-lg font-bold">{s.title}</h3>
                <p className="max-w-prose text-ink-soft">{s.body}</p>
              </div>
            </li>
          ))}
        </ol>
      </section>
    </div>
  )
}
