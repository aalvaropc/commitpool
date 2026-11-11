import TryDemo from '../TryDemo'
import { ButtonLink } from '../ui'

export default function Closing({ demo }: { demo: boolean | undefined }) {
  return (
    <section aria-labelledby="band-title" className="m-[clamp(0.5rem,1.4vw,1.25rem)] rounded-sheet bg-brand text-on-brand [&_:focus-visible]:outline-on-brand [&_::selection]:bg-on-brand [&_::selection]:text-brand">
      <div className="wrap grid gap-7 py-[clamp(3.5rem,8vw,6.5rem)] lg:grid-cols-[1fr_auto] lg:items-end">
        <div>
          <h2 id="band-title" className="shout max-w-[13ch] text-[clamp(2.5rem,8vw,5.5rem)] leading-[0.9]">
            The class happens, or nobody pays.
          </h2>
          <p className="mt-[1.125rem] max-w-[40ch] text-[clamp(1.125rem,1rem+0.4vw,1.375rem)] font-medium">Paste your group chat. Let Claude chase the maybes. Get paid when the class starts.</p>
        </div>
        <div className="flex flex-wrap items-start gap-3">
          {demo !== false && <TryDemo variant="ink" pending={demo === undefined} />}
          <ButtonLink to="/new" variant={demo === false ? 'ink' : 'outline'} size="lg">
            Create a workshop
          </ButtonLink>
        </div>
      </div>
    </section>
  )
}
