import AgentFeed from '../components/home/AgentFeed'
import ChatDemo from '../components/home/ChatDemo'
import Faq from '../components/home/Faq'
import McpTranscript from '../components/home/McpTranscript'
import MoneyFlow from '../components/home/MoneyFlow'
import StartSentence from '../components/home/StartSentence'
import TryDemo, { useDemoAvailable } from '../components/TryDemo'
import { ButtonLink } from '../components/ui'

const organizer = ['No more chasing a chat full of maybes.', 'Know the room is covered before you commit to it.', 'One link to share. No accounts, no spreadsheet.']
const everyone = ['Say when you can come in plain words.', 'Reserve with PayPal; you are charged only if it happens.', 'Give up your spot until charging starts and you pay nothing.']

function Pledge({ title, items }: { title: string; items: string[] }) {
  return (
    <div className="flex flex-col gap-4">
      <h3 className="text-lead">{title}</h3>
      <ul className="flex flex-col gap-3 text-lg text-ink-soft">
        {items.map((t) => (
          <li key={t} className="flex gap-3">
            <span aria-hidden="true" className="mt-[0.6em] h-0.5 w-4 shrink-0 rounded-full bg-brand" />
            {t}
          </li>
        ))}
      </ul>
    </div>
  )
}

export default function Home() {
  const demo = useDemoAvailable()
  const origin = typeof window === 'undefined' ? 'https://your-commitpool' : window.location.origin
  return (
    <>
      <title>CommitPool – the class happens, or nobody pays</title>
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-28 px-4 py-6 sm:gap-36 sm:px-6 sm:py-12">
        <section aria-labelledby="hero-title" className="grid grid-cols-[minmax(0,1fr)] items-center gap-10 lg:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)] lg:gap-14">
          <div className="flex flex-col gap-7">
            <h1 id="hero-title" className="shout text-display">
              The class happens, or nobody pays.
            </h1>
            <p className="max-w-[34rem] text-lead text-ink-soft">
              Share one link in the group chat. Claude reads everyone’s “maybe”, you see which date works, and PayPal only charges if enough people reserve.
            </p>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
              {demo !== false && <TryDemo pending={demo === undefined} />}
              <ButtonLink to="/new" variant={demo === false ? 'primary' : 'secondary'} size="lg">
                Create a workshop
              </ButtonLink>
            </div>
            <p className="text-sm text-ink-soft">Doodle meets Kickstarter, for small paid classes. Runs on the PayPal Sandbox: no real money moves.</p>
          </div>
          <ChatDemo />
        </section>

        <section aria-labelledby="agent-title" className="grid grid-cols-[minmax(0,1fr)] gap-10 lg:grid-cols-2 lg:gap-16">
          <div className="flex flex-col gap-5 lg:sticky lg:top-8 lg:self-start">
            <h2 id="agent-title" className="shout text-head">
              Claude chases the maybes.
            </h2>
            <p className="max-w-[34rem] text-lead text-ink-soft">
              Claude reads each message against every date, asks when something is vague, and drafts the nudge. Maya keeps the decision, and the server, not the model, decides
              which date works.
            </p>
          </div>
          <AgentFeed />
        </section>

        <section aria-labelledby="money-title" className="grid grid-cols-[minmax(0,1fr)] gap-10 lg:grid-cols-2 lg:gap-16">
          <div className="flex flex-col gap-8 lg:order-2">
            <div className="flex flex-col gap-5">
              <h2 id="money-title" className="shout text-head">
                The money comes last.
              </h2>
              <p className="max-w-[34rem] text-lead text-ink-soft">
                Reserving is a hold on PayPal and nothing is charged. When enough people have reserved, every hold is charged. Not enough by the deadline? Every hold is released and any charge refunded.
              </p>
            </div>
            <div className="grid gap-8 sm:grid-cols-2">
              <Pledge title="For the organizer" items={organizer} />
              <Pledge title="For everyone who comes" items={everyone} />
            </div>
          </div>
          <MoneyFlow />
        </section>

        <section aria-labelledby="mcp-title" className="flex flex-col gap-8">
          <div className="grid grid-cols-[minmax(0,1fr)] gap-6 lg:grid-cols-2 lg:gap-16">
            <h2 id="mcp-title" className="shout text-head">
              Run it from your AI assistant.
            </h2>
            <p className="max-w-[34rem] text-lead text-ink-soft">
              CommitPool is also an MCP server. An assistant can report who can come and open reservations once the organizer approves the exact terms. No tool moves money:
              every reservation is still approved by a person at PayPal.
            </p>
          </div>
          <div className="grid grid-cols-[minmax(0,1fr)] gap-4 lg:grid-cols-2 lg:gap-16">
            <McpTranscript />
            <figure className="flex min-w-0 flex-col gap-2 self-start">
              <figcaption id="mcp-command" className="font-semibold">
                Add it to Claude Code
              </figcaption>
              <pre role="region" aria-labelledby="mcp-command" tabIndex={0} className="overflow-x-auto rounded-3xl bg-surface p-5 font-mono text-sm leading-relaxed text-ink">
                <code>{`claude mcp add --transport http commitpool \\\n  ${origin}/mcp`}</code>
              </pre>
            </figure>
          </div>
        </section>

        <Faq />

        <StartSentence demo={demo} />
      </div>
    </>
  )
}
