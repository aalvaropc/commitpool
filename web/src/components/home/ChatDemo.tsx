import { useEffect, useRef, useState, type FormEvent } from 'react'
import {
  ANSWERS,
  CROWD,
  HOST,
  NEEDED,
  PRICE,
  SESSION_COST,
  SPOTS,
  SUGGESTIONS,
  YOU,
  countedBy,
  crowdTally,
  leader,
  opening,
  outcome,
  reservers,
  tally,
  type Counted,
  type Read,
} from '../../lib/chatScript'
import { useInView } from '../../lib/inView'
import { earlierFrom, footnote, readLive, type Earlier, type ReadBy, type Reader } from '../../lib/liveReading'
import { sampleDates, type SampleDates } from '../../lib/sampleDates'
import type { Day, SampleReading } from '../../lib/sampleReader'
import TryDemo from '../TryDemo'
import { Button, ButtonLink, Icon } from '../ui'
import { Asks, CommitPoolSays, ItsOn, Notice, Reading, Said, SaidByYou, Typing } from './ChatBubbles'

type Phase = 'playing' | 'turn' | 'reading' | 'counting' | 'offer' | 'reserving' | 'done'

type Draft<T> = T extends unknown ? Omit<T, 'id'> : never

type Offer = { id: number; kind: 'offer'; key: Day; day: string }

type Item =
  | { id: number; kind: 'host'; text: string }
  | { id: number; kind: 'person'; name: string; text: string; you: boolean; by: Reader; reading: SampleReading | null; counted: Counted | null }
  | { id: number; kind: 'typing'; name: string }
  | { id: number; kind: 'tally'; text: string }
  | { id: number; kind: 'question'; by: ReadBy; text: string }
  | Offer
  | { id: number; kind: 'short' }
  | { id: number; kind: 'hold'; name: string }
  | { id: number; kind: 'charging' }
  | { id: number; kind: 'on'; text: string }

function reducedMotion(): boolean {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true
}

function pause(ms: number): Promise<void> {
  return new Promise((done) => setTimeout(done, reducedMotion() ? Math.min(ms, 20) : ms))
}

function readsOf(items: Item[]): Read[] {
  const people = items.flatMap((i) => (i.kind === 'person' && i.counted ? [{ name: i.name, counted: i.counted, you: i.you }] : []))
  const crowd = people.filter((p) => !p.you)
  const you = people.filter((p) => p.you).slice(-1)
  return [...crowd, ...you].map(({ name, counted }) => ({ name, counted }))
}

function placeholder(phase: Phase, answering: boolean): string {
  switch (phase) {
    case 'playing':
      return 'Everyone is answering…'
    case 'turn':
      return answering ? 'Answer the question' : 'When can you come? Write it like a text'
    case 'reading':
      return 'Claude is reading…'
    case 'counting':
      return 'CommitPool is counting…'
    default:
      return 'Your turn is over'
  }
}

function DayCard({ day, count, was, best, state }: { day: string; count: number; was: number; best: boolean; state: string | null }) {
  const moved = state === null && was !== count
  return (
    <div className={`flex min-w-0 flex-col rounded-2xl px-3 pt-2.5 pb-3 transition-[background-color,box-shadow] duration-300 ${best ? 'bg-brand-wash ring-1 ring-brand/45 ring-inset' : 'bg-surface-muted'}`}>
      <p className="text-[0.8125rem] font-semibold text-ink-soft">{day}</p>
      <p className="mt-0.5 flex items-center gap-1.5 text-[clamp(0.9375rem,0.85rem+0.4vw,1.0625rem)] leading-tight font-bold whitespace-nowrap tabular-nums">
        {moved && (
          <span aria-hidden="true" className="flex items-center gap-1.5">
            <span className="font-semibold text-ink-faint line-through decoration-2">{was}</span>
            <Icon name="arrowRight" size={14} strokeWidth={2.4} className="text-brand" />
          </span>
        )}
        {state ?? `${count} can come`}
      </p>
      <div aria-hidden="true" className="relative mt-2.5 h-1.5 rounded-full bg-line">
        <div className={`absolute inset-y-0 left-0 rounded-full transition-[width] duration-500 ease-(--ease-out) ${best ? 'bg-paid' : 'bg-ink-faint'}`} style={{ width: `${Math.min(count / SPOTS, 1) * 100}%` }} />
        <span className="absolute -inset-y-1 w-[3px] rounded-sm bg-ink" style={{ left: `calc(${(NEEDED / SPOTS) * 100}% - 1px)` }} />
      </div>
    </div>
  )
}

function Short({ demo }: { demo: boolean | undefined }) {
  return (
    <div className="rise flex flex-col gap-3 rounded-3xl bg-surface-muted p-4">
      <p className="font-display text-lead leading-tight font-bold">Iris still needs one more</p>
      <p className="text-ink-soft">Until one day reaches {NEEDED}, nothing opens and nobody pays. The real flow works the same way: Claude reads every answer, Iris decides, and the PayPal Sandbox holds the money.</p>
      {demo === false ? (
        <ButtonLink to="/new" variant="primary">
          Create a workshop
        </ButtonLink>
      ) : (
        <TryDemo size="md" pending={demo === undefined} />
      )}
    </div>
  )
}

export default function ChatDemo({ demo, noteClassName = '' }: { demo?: boolean; noteClassName?: string }) {
  const [stage, seen] = useInView<HTMLDivElement>()
  const [items, setItems] = useState<Item[]>([])
  const [phase, setPhase] = useState<Phase>('playing')
  const [draft, setDraft] = useState('')
  const [round, setRound] = useState(0)
  const [dates, setDates] = useState<SampleDates>(() => sampleDates(new Date()))
  const [earlier, setEarlier] = useState<Earlier | null>(null)
  const [lastBy, setLastBy] = useState<ReadBy | null>(null)
  const ids = useRef(0)
  const run = useRef(0)
  const interacted = useRef(false)
  const log = useRef<HTMLDivElement>(null)
  const reserveButton = useRef<HTMLButtonElement>(null)
  const againButton = useRef<HTMLButtonElement>(null)

  const reads = readsOf(items)
  const counts = tally(reads)
  const before = tally(reads.filter((r) => r.name !== YOU))
  const best = leader(counts)
  const holds = items.filter((i) => i.kind === 'hold').length
  const on = items.some((i) => i.kind === 'on')
  const offer = items.find((i): i is Offer => i.kind === 'offer')

  useEffect(() => {
    log.current?.scrollTo?.({ top: log.current.scrollHeight, behavior: reducedMotion() ? 'auto' : 'smooth' })
  }, [items, phase])

  useEffect(() => {
    if (!interacted.current) return
    const active = document.activeElement
    if (active && active !== document.body && !stage.current?.contains(active)) return
    if (phase === 'offer' && offer) reserveButton.current?.focus({ preventScroll: true })
    if (phase === 'done') againButton.current?.focus({ preventScroll: true })
  }, [phase, offer, stage])

  function push(item: Draft<Item>): number {
    const id = ++ids.current
    setItems((list) => [...list, { ...item, id } as Item])
    return id
  }

  function patch(id: number, change: Partial<Item>) {
    setItems((list) => list.map((i) => (i.id === id ? ({ ...i, ...change } as Item) : i)))
  }

  function drop(id: number) {
    setItems((list) => list.filter((i) => i.id !== id))
  }

  useEffect(() => {
    if (!seen) return
    const mine = ++run.current
    const live = () => run.current === mine
    async function play() {
      const local = sampleDates(new Date())
      setItems([])
      setDates(local)
      setEarlier(null)
      setLastBy(null)
      setPhase('playing')
      await pause(500)
      if (!live()) return
      push({ kind: 'host', text: opening(local) })
      for (const voice of CROWD) {
        await pause(900)
        if (!live()) return
        const typing = push({ kind: 'typing', name: voice.name })
        await pause(1100)
        if (!live()) return
        drop(typing)
        const said = push({ kind: 'person', name: voice.name, text: voice.message, you: false, by: 'prepared', reading: null, counted: null })
        await pause(600)
        if (!live()) return
        patch(said, { reading: voice.reading, counted: countedBy(voice.reading) })
      }
      await pause(800)
      if (!live()) return
      push({ kind: 'tally', text: crowdTally(local) })
      await pause(500)
      if (live()) setPhase('turn')
    }
    void play()
    return () => {
      run.current++
    }
  }, [round, seen])

  async function send(text: string) {
    const said = text.trim()
    if (!said || phase !== 'turn') return
    interacted.current = true
    const mine = run.current
    const live = () => run.current === mine
    const asked = earlier
    setDraft('')
    setPhase('reading')
    const id = push({ kind: 'person', name: YOU, text: said, you: true, by: 'claude', reading: null, counted: null })
    const [result] = await Promise.all([readLive({ message: said, earlier: asked }), pause(700)])
    if (!live()) return
    setLastBy(result.by)
    patch(id, { by: result.by, reading: result.reading, counted: result.counted })
    await pause(800)
    if (!live()) return
    if (result.question) {
      push({ kind: 'question', by: result.by, text: result.question })
      if (!asked) {
        setEarlier(earlierFrom(said, result))
        setPhase('turn')
        return
      }
      await pause(900)
      if (!live()) return
    }
    setPhase('counting')
    const end = outcome(result.counted, dates, result.question !== null)
    push({ kind: 'tally', text: end.text })
    await pause(1100)
    if (!live()) return
    if (end.key) {
      push({ kind: 'offer', key: end.key, day: dates[end.key].label })
      setPhase('offer')
      return
    }
    push({ kind: 'short' })
    setPhase('done')
  }

  function quickReply(text: string) {
    void send(text)
    log.current?.focus({ preventScroll: true })
  }

  async function reserve(chosen: Offer) {
    if (phase !== 'offer') return
    interacted.current = true
    const mine = run.current
    const live = () => run.current === mine
    setPhase('reserving')
    const names = reservers(reads, chosen.key).slice(0, NEEDED)
    for (const name of names) {
      await pause(850)
      if (!live()) return
      push({ kind: 'hold', name })
    }
    await pause(700)
    if (!live()) return
    const charging = push({ kind: 'charging' })
    await pause(1400)
    if (!live()) return
    drop(charging)
    push({ kind: 'on', text: `${names.length} paid · $${names.length * PRICE} before PayPal fees covers your $${SESSION_COST} session costs.` })
    setPhase('done')
  }

  function again() {
    interacted.current = true
    setRound((r) => r + 1)
    log.current?.focus({ preventScroll: true })
  }

  function submit(e: FormEvent) {
    e.preventDefault()
    void send(draft)
  }

  const turn = phase === 'turn'
  const replies = earlier ? ANSWERS : SUGGESTIONS
  const dayState = (key: Day) => (on && best === key ? 'It’s on' : holds > 0 && best === key ? `${holds} reserved` : null)

  return (
    <div ref={stage} className="flex min-w-0 flex-col gap-3">
      <section aria-label="Example: answers to Iris’s invite" className="kiln flex h-[44rem] max-h-[88svh] flex-col overflow-hidden rounded-tile border border-surface-muted bg-surface">
        <header className="flex flex-col gap-3 px-[1.125rem] pt-4 pb-1">
          <div className="flex min-w-0 flex-col">
            <p className="text-[1.0625rem] leading-tight font-bold">Sat or Sun?</p>
            <p className="text-[0.8125rem] text-ink-faint">Example chat · prepared answers · payments simulated</p>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <DayCard day={dates.sat.label} count={counts.sat} was={before.sat} best={reads.length > 0 && best === 'sat'} state={dayState('sat')} />
            <DayCard day={dates.sun.label} count={counts.sun} was={before.sun} best={reads.length > 0 && best === 'sun'} state={dayState('sun')} />
          </div>
        </header>

        <div ref={log} role="log" aria-live="polite" aria-label="Messages" tabIndex={0} className="flex flex-1 flex-col gap-3 overflow-y-auto px-[1.125rem] pt-3 pb-4 [mask-image:linear-gradient(to_bottom,transparent,black_1.25rem)]">
          {items.map((item) => {
            switch (item.kind) {
              case 'host':
                return <Said key={item.id} name={HOST} text={item.text} role="organizer" />
              case 'typing':
                return <Typing key={item.id} name={item.name} />
              case 'person':
                return (
                  <div key={item.id} className="flex flex-col gap-2">
                    {item.you ? <SaidByYou text={item.text} /> : <Said name={item.name} text={item.text} />}
                    <Reading reading={item.reading} by={item.by} live={item.you} />
                  </div>
                )
              case 'tally':
                return <CommitPoolSays key={item.id} text={item.text} />
              case 'question':
                return <Asks key={item.id} by={item.by} text={item.text} />
              case 'offer':
                return (
                  <div key={item.id} className="rise flex flex-col gap-3 rounded-3xl bg-surface-muted p-4">
                    <p className="font-display text-lead leading-tight font-bold">Reservations are open for {item.day}</p>
                    <p className="text-ink-soft">
                      Simulated here: ${PRICE} would be held on your PayPal and charged only if {NEEDED} people reserve.
                    </p>
                    <Button ref={reserveButton} onClick={() => void reserve(item)} disabled={phase !== 'offer'}>
                      Reserve · ${PRICE} hold, simulated
                      <Icon name="arrowRight" size={18} />
                    </Button>
                  </div>
                )
              case 'short':
                return <Short key={item.id} demo={demo} />
              case 'hold':
                return (
                  <Notice key={item.id} tone="hold">
                    {item.name === YOU ? 'You reserved' : `${item.name.split(' ')[0]} reserved`} · not charged yet
                  </Notice>
                )
              case 'charging':
                return (
                  <Notice key={item.id} tone="attention">
                    {NEEDED} reserved — charging the holds…
                  </Notice>
                )
              case 'on':
                return <ItsOn key={item.id} text={item.text} />
            }
          })}
        </div>

        <footer className="flex flex-col gap-2.5 bg-composer px-3.5 pt-3 pb-3.5">
          {turn && (
            <p id="quick-replies-hint" className="text-[0.8125rem] text-ink-soft">
              Pick one or write your own, in any language.
            </p>
          )}
          {turn && (
            <div className="rise -mx-3.5 flex gap-2 overflow-x-auto px-3.5 [mask-image:linear-gradient(to_right,black_calc(100%-2rem),transparent)] [scrollbar-width:none]" role="group" aria-label="Quick replies" aria-describedby="quick-replies-hint">
              {replies.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => quickReply(s)}
                  className="min-h-11 shrink-0 rounded-full bg-surface-muted px-4 text-sm font-medium whitespace-nowrap transition-colors duration-150 hover:bg-line"
                >
                  {s}
                </button>
              ))}
            </div>
          )}
          {phase === 'done' ? (
            <Button ref={againButton} variant="secondary" onClick={again}>
              Play it again
            </Button>
          ) : (
            <form onSubmit={submit} className="flex gap-2">
              <label htmlFor="chat-draft" className="sr-only">
                Your answer
              </label>
              <input
                id="chat-draft"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                readOnly={!turn}
                maxLength={160}
                autoComplete="off"
                placeholder={placeholder(phase, earlier !== null)}
                className="min-h-12 min-w-0 flex-1 rounded-full border border-line-strong bg-surface px-5 text-base text-ink transition-colors duration-150 placeholder:text-ink-faint read-only:opacity-60 hover:bg-canvas focus-visible:bg-canvas"
              />
              <button
                type="submit"
                disabled={!turn || !draft.trim()}
                aria-label="Send"
                className="grid size-12 shrink-0 place-items-center rounded-full bg-brand text-on-brand transition-opacity duration-150 disabled:opacity-40"
              >
                <Icon name="send" size={20} />
              </button>
            </form>
          )}
        </footer>
      </section>
      <p className={`text-sm leading-snug text-graphite ${noteClassName}`}>
        {footnote(lastBy)} <span className="font-semibold text-charcoal">Try it as Iris</span> runs the real flow on the PayPal Sandbox.
      </p>
    </div>
  )
}
