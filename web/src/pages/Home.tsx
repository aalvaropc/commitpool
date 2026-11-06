import type { ReactNode } from 'react'
import TryDemo, { useDemoAvailable } from '../components/TryDemo'
import { Avatar, ButtonLink, Icon, Meter, Pill, Tile, VerdictChip, type IconName } from '../components/ui'

const chat = [
  { name: 'Priya Raman', text: 'Saturday works for me!' },
  { name: 'Ben Whitaker', text: 'either day, but Sat only if we finish before six' },
  { name: 'Grace Liu', text: 'maybe? ideally in the afternoon' },
]

const grid = [
  { name: 'Priya Raman', sat: { verdict: 'available', text: 'Can come' }, sun: { verdict: 'unavailable', text: 'Can’t come' } },
  { name: 'Ben Whitaker', sat: { verdict: 'conditional', text: 'Only if it ends by 6 PM' }, sun: { verdict: 'available', text: 'Can come' } },
  { name: 'Grace Liu', sat: { verdict: 'unknown', text: 'Not sure' }, sun: { verdict: 'unknown', text: 'Not sure' } },
] as const

const roles: { icon: IconName; title: string; body: string }[] = [
  { icon: 'quote', title: 'Claude reads', body: 'Each answer becomes a verdict per date, with the words it came from. Vague answers get a question, not a guess.' },
  { icon: 'calendar', title: 'CommitPool decides', body: 'Who can come, which date works, when to charge and when to release. The rules are code, not a model.' },
  { icon: 'lock', title: 'PayPal moves the money', body: 'Holds, charges, releases and refunds through PayPal’s own authorize-then-capture flow.' },
  { icon: 'check', title: 'People approve', body: 'Every hold is approved by the person at PayPal. Every date is opened by the organizer.' },
]

const faq = [
  {
    q: 'What is a hold?',
    a: 'Reserving places a temporary hold for the price on your PayPal account. Nothing is charged then. Holds are charged only once enough people have reserved for the class to happen.',
  },
  {
    q: 'What if not enough people reserve?',
    a: 'At the deadline the class is cancelled: every hold is released and any charge already made is refunded. Nobody pays for a class that does not happen.',
  },
  {
    q: 'Can I leave after reserving?',
    a: 'Yes, until you are charged: give up your spot and PayPal releases your hold. After that, payments are refunded only if the class is cancelled, so the group the organizer counted on stays whole.',
  },
  {
    q: 'Does anyone need an account?',
    a: 'No. The organizer gets a private link to their workshop; participants join from the invite link with a name and get a private link of their own.',
  },
  {
    q: 'Is this real money?',
    a: 'Not in this demo. Everything runs on the PayPal Sandbox, PayPal’s test environment, and the demo class and its participants are prepared data.',
  },
]

function Step({ n, title, body, children }: { n: number; title: string; body: string; children: ReactNode }) {
  return (
    <Tile as="article" className="flex flex-col gap-4 sm:p-6">
      <div className="min-h-44 rounded-control bg-surface-muted p-4">{children}</div>
      <h3 className="flex items-baseline gap-2 text-xl">
        <span aria-hidden="true" className="font-display text-brand tabular-nums">
          {n}
        </span>
        {title}
      </h3>
      <p className="text-ink-soft">{body}</p>
    </Tile>
  )
}

function Checks({ items, tone = '' }: { items: string[]; tone?: string }) {
  return (
    <ul className="flex flex-col gap-3 text-[1.0625rem]">
      {items.map((t) => (
        <li key={t} className="flex gap-2.5">
          <Icon name="check" className={`mt-1 ${tone}`} />
          {t}
        </li>
      ))}
    </ul>
  )
}

export default function Home() {
  const demo = useDemoAvailable()
  const origin = typeof window === 'undefined' ? 'https://your-commitpool' : window.location.origin
  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-16 px-4 py-8 sm:gap-24 sm:px-6 sm:py-14">
      <title>CommitPool – the class happens, or nobody pays</title>

      <section aria-labelledby="hero-title" className="grid items-center gap-10 lg:grid-cols-[minmax(0,1.05fr)_minmax(0,1fr)]">
        <div className="flex flex-col gap-6">
          <h1 id="hero-title" className="text-[2.75rem] leading-[1] font-extrabold sm:text-7xl">
            The class happens, or nobody pays.
          </h1>
          <p className="max-w-[36rem] text-lg text-ink-soft sm:text-xl">
            CommitPool finds the date your group can actually make, then lets everyone reserve with PayPal. Nobody is charged unless enough people
            reserve for the class to run.
          </p>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
            {demo && <TryDemo />}
            <ButtonLink to="/new" variant={demo ? 'secondary' : 'primary'} size="lg">
              Create a workshop
            </ButtonLink>
          </div>
          <p className="text-sm text-ink-soft">Doodle meets Kickstarter, for small paid classes. Runs on the PayPal Sandbox: no real money moves.</p>
        </div>

        <figure className="mx-auto w-full max-w-md">
          <div className="flex flex-col gap-3 rounded-[2rem] bg-surface-muted p-3 shadow-lift ring-1 ring-line">
            <Tile tone="brand" className="relative min-h-40 overflow-hidden">
              <span aria-hidden="true" className="absolute -top-10 -right-10 size-40 rounded-full border-[18px] border-white/15" />
              <div className="relative flex h-full flex-col justify-end gap-2 pt-8">
                <p className="font-display text-3xl leading-none font-extrabold">Wheel-throwing for beginners</p>
                <p className="flex items-center gap-2 font-semibold">
                  <Avatar name="Maya Okafor" size="sm" className="ring-2 ring-white/40" />
                  with Maya Okafor
                </p>
              </div>
            </Tile>
            <div className="grid grid-cols-2 gap-3">
              <Tile>
                <p className="text-xs font-semibold tracking-[0.06em] text-ink-soft uppercase">Price</p>
                <p className="mt-1 font-display text-2xl font-bold">$45</p>
                <p className="text-xs text-ink-soft">Charged only if it happens</p>
              </Tile>
              <Tile>
                <p className="text-xs font-semibold tracking-[0.06em] text-ink-soft uppercase">Studio</p>
                <p className="mt-1 font-display text-base leading-snug font-bold">Greenpoint, Brooklyn</p>
              </Tile>
            </div>
            <Tile className="flex flex-col gap-3">
              <div className="flex items-center justify-between">
                <p className="text-xs font-semibold tracking-[0.06em] text-ink-soft uppercase">Who’s in</p>
                <Pill tone="hold">Reservations open</Pill>
              </div>
              <Meter capacity={6} needed={3} paid={0} reserved={1} />
            </Tile>
          </div>
          <figcaption className="mt-3 text-center text-xs text-ink-soft">Example invite · prepared demo data</figcaption>
        </figure>
      </section>

      <section aria-labelledby="story-title" className="flex flex-col gap-6">
        <h2 id="story-title" className="max-w-[22ch] text-3xl sm:text-5xl">
          From “maybe” to “it’s on” in three steps
        </h2>
        <div className="grid gap-4 md:grid-cols-3">
          <Step n={1} title="Everyone answers in their own words" body="Share one link in the group chat. People join with a name and say when they can come, the way they already would.">
            <ul className="flex flex-col gap-2.5" aria-label="Example answers">
              {chat.map((m) => (
                <li key={m.name} className="flex items-end gap-2">
                  <Avatar name={m.name} size="sm" />
                  <span className="rounded-2xl rounded-bl-md bg-surface px-3 py-2 text-sm shadow-tile">{m.text}</span>
                </li>
              ))}
            </ul>
          </Step>
          <Step n={2} title="The date that works rises to the top" body="Claude reads each answer for every date; CommitPool counts who can come and recommends the date. Anyone can correct how theirs was read.">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Example readings</caption>
              <thead>
                <tr className="text-xs text-ink-soft">
                  <th scope="col" className="pb-2 font-semibold">
                    <span className="sr-only">Person</span>
                  </th>
                  <th scope="col" className="pb-2 font-semibold">
                    Sat
                  </th>
                  <th scope="col" className="pb-2 font-semibold">
                    Sun
                  </th>
                </tr>
              </thead>
              <tbody>
                {grid.map((r) => (
                  <tr key={r.name}>
                    <th scope="row" className="py-1 pr-2 font-normal">
                      <Avatar name={r.name} size="sm" />
                      <span className="sr-only">{r.name}</span>
                    </th>
                    <td className="py-1 pr-1">
                      <VerdictChip verdict={r.sat.verdict} className="text-xs">
                        {r.sat.text}
                      </VerdictChip>
                    </td>
                    <td className="py-1">
                      <VerdictChip verdict={r.sun.verdict} className="text-xs">
                        {r.sun.text}
                      </VerdictChip>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Step>
          <Step n={3} title="Reserve now, pay only if it happens" body="Each person reserves with a PayPal hold. Once enough reserve, the holds are charged and the class is on. Not enough? Every hold is released.">
            <div className="flex h-full flex-col justify-center gap-3">
              <p className="font-display text-3xl font-extrabold text-paid">It’s on</p>
              <Meter capacity={6} needed={3} paid={4} reserved={0} showCaption={false} />
              <p className="text-sm font-semibold">4 paid · $180 · the $120 studio is covered</p>
            </div>
          </Step>
        </div>
      </section>

      <section aria-labelledby="fair-title" className="flex flex-col gap-6">
        <h2 id="fair-title" className="text-3xl sm:text-5xl">
          Fair for both sides
        </h2>
        <div className="grid gap-4 md:grid-cols-2">
          <Tile as="article" tone="brand" className="flex flex-col gap-4 sm:p-8">
            <h3 className="text-2xl">For the organizer</h3>
            <Checks items={['No more chasing a chat full of maybes.', 'Know the room is covered before you commit to it.', 'One link to share. No accounts, no spreadsheet.']} />
          </Tile>
          <Tile as="article" className="flex flex-col gap-4 sm:p-8">
            <h3 className="text-2xl">For everyone who comes</h3>
            <Checks
              tone="text-paid"
              items={['Say when you can come in plain words.', 'Reserve with PayPal; you are charged only if it happens.', 'Change your mind before you are charged and your hold is released.']}
            />
          </Tile>
        </div>
      </section>

      <section aria-labelledby="roles-title" className="flex flex-col gap-6">
        <h2 id="roles-title" className="max-w-[24ch] text-3xl sm:text-5xl">
          Claude reads. CommitPool decides. PayPal moves the money.
        </h2>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {roles.map((r) => (
            <Tile as="article" key={r.title} className="flex flex-col gap-3">
              <span aria-hidden="true" className="grid size-10 place-items-center rounded-full bg-brand-wash text-brand">
                <Icon name={r.icon} size={20} />
              </span>
              <h3 className="text-lg">{r.title}</h3>
              <p className="text-sm text-ink-soft">{r.body}</p>
            </Tile>
          ))}
        </div>
      </section>

      <section aria-labelledby="mcp-title">
        <Tile className="grid gap-6 sm:p-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)] lg:items-center">
          <div className="flex flex-col gap-3">
            <h2 id="mcp-title" className="text-3xl">
              Run it from your AI assistant
            </h2>
            <p className="text-ink-soft">
              CommitPool is also an MCP server. An assistant can create the workshop, report who can come and open reservations once the
              organizer approves the exact terms. No tool moves money: every hold is still approved by a person at PayPal.
            </p>
          </div>
          <pre tabIndex={0} aria-label="Command to add CommitPool to Claude Code" className="overflow-x-auto rounded-control bg-ink p-4 text-sm leading-relaxed text-on-brand-soft">
            <code>{`claude mcp add --transport http commitpool \\\n  ${origin}/mcp`}</code>
          </pre>
        </Tile>
      </section>

      <section aria-labelledby="faq-title" className="grid gap-6 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
        <h2 id="faq-title" className="text-3xl sm:text-5xl">
          Questions people ask
        </h2>
        <div className="flex flex-col gap-3">
          {faq.map((f) => (
            <details key={f.q} className="group rounded-tile bg-surface p-5 shadow-tile">
              <summary className="flex cursor-pointer list-none items-center justify-between gap-3 font-display text-lg font-bold">
                {f.q}
                <Icon name="arrowRight" className="text-brand transition-transform duration-200 group-open:rotate-90" />
              </summary>
              <p className="mt-3 max-w-prose text-ink-soft">{f.a}</p>
            </details>
          ))}
        </div>
      </section>

      <section aria-labelledby="close-title">
        <Tile tone="brand" className="relative flex flex-col items-start gap-5 overflow-hidden sm:p-10">
          <span aria-hidden="true" className="absolute -right-16 -bottom-24 size-72 rounded-full border-[36px] border-white/10" />
          <h2 id="close-title" className="relative max-w-[18ch] text-3xl sm:text-5xl">
            Your next class, with a room that is paid for.
          </h2>
          <div className="relative flex flex-col gap-3 sm:flex-row">
            {demo && <TryDemo />}
            <ButtonLink to="/new" variant="secondary" size="lg">
              Create a workshop
            </ButtonLink>
          </div>
        </Tile>
      </section>
    </div>
  )
}
