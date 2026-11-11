import { LogoMark } from '../ui/Logo'
import TryDemo from '../TryDemo'
import { ButtonLink } from '../ui'
import ChatDemo from './ChatDemo'
import { SeatedFigure, Underline } from './Drawings'

export default function Hero({ demo }: { demo: boolean | undefined }) {
  return (
    <section aria-labelledby="hero-title" className="pt-3 lg:pt-7">
      <div className="wrap grid gap-10 lg:grid-cols-12 lg:items-start lg:gap-x-8 lg:gap-y-0">
        <div className="lg:col-span-6 lg:self-center lg:pb-6">
          <h1 id="hero-title" className="shout text-hero">
            Find the date that <span className="text-brand">fills the class.</span>
          </h1>
          <p className="mt-7 max-w-[34ch] text-landing-lead text-ink-soft">
            Paste your group chat. Claude reads everyone’s maybes and asks when it matters. CommitPool picks the date that reaches your minimum, and PayPal charges only once enough people reserve.
          </p>
          <div className="mt-8 flex flex-wrap items-start gap-3">
            {demo !== false && <TryDemo pending={demo === undefined} />}
            <ButtonLink to="/new" variant={demo === false ? 'primary' : 'secondary'} size="lg">
              Create a workshop
            </ButtonLink>
          </div>
          <div className="mt-9 flex max-w-[30rem] items-center gap-3.5 border-t border-line pt-[1.375rem] lg:mt-12">
            <LogoMark size={52} className="size-11 lg:size-[3.75rem]" />
            <p className="shout text-[clamp(1.375rem,1rem+1.4vw,2.25rem)] leading-[1.05] tracking-[-0.01em]">
              The class happens, <span className="text-brand">or nobody pays.</span>
            </p>
          </div>
          <p className="mt-3.5 max-w-[30rem] text-sm text-ink-faint">Runs on the PayPal Sandbox: no real money moves. Iris Calder and her class are prepared demo data.</p>
        </div>

        <div className="-mx-(--gutter) lg:col-span-6 lg:mr-[calc(-1*var(--gutter))] lg:ml-0">
          <div className="paper grain grid grid-cols-[minmax(0,1fr)_40%] gap-x-3 overflow-hidden px-(--gutter) pt-6 pb-7 lg:block lg:min-h-[45rem] lg:overflow-visible lg:rounded-l-sheet lg:pt-[1.875rem] lg:pr-10 lg:pb-10 lg:pl-0">
            <div className="col-start-1 row-start-1 max-w-[18rem] lg:ml-10">
              <p className="text-[1.375rem] leading-[1.15] font-extrabold tracking-[-0.02em] [font-stretch:80%]">Life drawing for beginners</p>
              <Underline className="mt-0.5 block h-2.5 w-[min(13.125rem,100%)]" />
              <p className="mt-2 text-sm leading-[1.45] text-graphite">
                The Long Room · Red Hook, Brooklyn
                <br />
                Sat or Sun, 10 AM–12 PM · $45 a spot
                <br />
                $120 room and model · needs 3 of 6 easels
              </p>
            </div>
            <SeatedFigure className="col-start-2 row-span-2 row-start-1 block h-auto w-full self-start lg:absolute lg:top-[1.875rem] lg:right-[1.375rem] lg:w-[clamp(11rem,17vw,15.625rem)]" />
            <div className="relative z-[2] col-span-full row-start-3 mt-[1.125rem] lg:mt-[2.125rem] lg:w-[26.875rem] lg:-ml-9">
              <ChatDemo demo={demo} noteClassName="lg:pr-4 lg:pl-[4.75rem]" />
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
