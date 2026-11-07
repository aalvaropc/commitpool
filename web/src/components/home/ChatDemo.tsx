import { useEffect, useRef, useState, type FormEvent } from 'react'
import { CROWD, HOST, NEEDED, OPENING, PRICE, STUDIO, SUGGESTIONS, fits, recommend, reservers, tally, type Read } from '../../lib/chatScript'
import { useInView } from '../../lib/inView'
import { readSample, type Day, type SampleReading } from '../../lib/sampleReader'
import { Button, Icon } from '../ui'
import { ClaudeSays, ItsOn, Notice, Reading, Said, SaidByYou, Typing } from './ChatBubbles'

type Phase = 'playing' | 'turn' | 'offer' | 'reserving' | 'done'

type Draft<T> = T extends unknown ? Omit<T, 'id'> : never

type Offer = { id: number; kind: 'offer'; key: Day; day: string; counted: boolean }

type Item =
  | { id: number; kind: 'host'; text: string }
  | { id: number; kind: 'person'; name: string; text: string; you: boolean; reading: SampleReading | null }
  | { id: number; kind: 'typing'; name: string }
  | { id: number; kind: 'claude'; text: string }
  | Offer
  | { id: number; kind: 'hold'; name: string }
  | { id: number; kind: 'charging' }
  | { id: number; kind: 'on'; text: string }

const CROWD_READS: Read[] = CROWD.map((v) => ({ name: v.name, reading: readSample(v.message) }))

function reducedMotion(): boolean {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true
}

function pause(ms: number): Promise<void> {
  return new Promise((done) => setTimeout(done, reducedMotion() ? Math.min(ms, 20) : ms))
}

function readsOf(items: Item[]): Read[] {
  return items.flatMap((i) => (i.kind === 'person' && i.reading ? [{ name: i.name, reading: i.reading }] : []))
}

function claudeReply(reading: SampleReading): Draft<Item> {
  const next = recommend(tally([...CROWD_READS, { name: 'You', reading }]))
  const counted = fits(reading, next.key)
  const you = counted
    ? 'You’re counted.'
    : reading.ask
      ? 'I asked you a question, so you aren’t counted yet.'
      : `From what you wrote, you aren’t counted for ${next.day}.`
  return { kind: 'claude', text: `${next.text} ${you} Maya can open reservations.` }
}

function DayCard({ day, count, best, state }: { day: string; count: number; best: boolean; state: string | null }) {
  return (
    <div className={`flex flex-col gap-1.5 rounded-2xl px-3.5 py-2.5 transition-colors duration-300 ${best ? 'bg-brand-wash' : 'bg-surface-muted'}`}>
      <p className="text-sm font-semibold">{day}</p>
      <p className="font-display text-lg leading-none font-bold tabular-nums">{state ?? `${count} can come`}</p>
      <div aria-hidden="true" className="h-1.5 rounded-full bg-canvas/60">
        <div className="h-full rounded-full bg-paid transition-[width] duration-500 ease-(--ease-out)" style={{ width: `${Math.min(count / NEEDED, 1) * 100}%` }} />
      </div>
    </div>
  )
}

export default function ChatDemo() {
  const [stage, seen] = useInView<HTMLDivElement>()
  const [items, setItems] = useState<Item[]>([])
  const [phase, setPhase] = useState<Phase>('playing')
  const [draft, setDraft] = useState('')
  const [round, setRound] = useState(0)
  const ids = useRef(0)
  const run = useRef(0)
  const interacted = useRef(false)
  const log = useRef<HTMLDivElement>(null)
  const reserveButton = useRef<HTMLButtonElement>(null)
  const againButton = useRef<HTMLButtonElement>(null)

  const reads = readsOf(items)
  const counts = tally(reads)
  const rec = recommend(counts)
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
      setItems([])
      setPhase('playing')
      await pause(500)
      if (!live()) return
      push({ kind: 'host', text: OPENING })
      for (const voice of CROWD) {
        await pause(900)
        if (!live()) return
        const typing = push({ kind: 'typing', name: voice.name })
        await pause(1100)
        if (!live()) return
        drop(typing)
        const said = push({ kind: 'person', name: voice.name, text: voice.message, you: false, reading: null })
        await pause(800)
        if (!live()) return
        patch(said, { reading: readSample(voice.message) })
      }
      await pause(700)
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
    const reading = readSample(said)
    setDraft('')
    setPhase('offer')
    const id = push({ kind: 'person', name: 'You', text: said, you: true, reading: null })
    await pause(900)
    if (!live()) return
    patch(id, { reading })
    await pause(900)
    if (!live()) return
    push(claudeReply(reading))
    await pause(1100)
    if (!live()) return
    const next = recommend(tally([...CROWD_READS, { name: 'You', reading }]))
    push({ kind: 'offer', key: next.key, day: next.day, counted: fits(reading, next.key) })
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
    const others = reservers(CROWD_READS, chosen.key)
    const names = (chosen.counted ? ['You', ...others] : others).slice(0, NEEDED)
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
    push({ kind: 'on', text: `${names.length} paid · $${names.length * PRICE} covers the $${STUDIO} studio.` })
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
  const dayState = (key: Day) => (on && rec.key === key ? 'It’s on' : holds > 0 && rec.key === key ? `${holds} reserved` : null)

  return (
    <div ref={stage} className="flex min-w-0 flex-col gap-3">
      <section aria-label="Example: answers to Maya’s invite" className="flex h-[40rem] max-h-[88svh] flex-col overflow-hidden rounded-[2rem] bg-surface">
        <header className="flex flex-col gap-3 p-4 pb-3">
          <div className="flex items-center justify-between gap-3">
            <div className="flex min-w-0 flex-col">
              <p className="font-display text-lg leading-tight font-bold">Sat or Sun?</p>
              <p className="truncate text-sm text-ink-soft">Answers to {HOST.split(' ')[0]}’s wheel-throwing invite</p>
            </div>
            <span className="shrink-0 rounded-full bg-surface-muted px-2.5 py-1 text-sm font-medium text-ink-soft">Example</span>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <DayCard day="Sat, Nov 14" count={counts.sat} best={reads.length > 0 && rec.key === 'sat'} state={dayState('sat')} />
            <DayCard day="Sun, Nov 15" count={counts.sun} best={reads.length > 0 && rec.key === 'sun'} state={dayState('sun')} />
          </div>
        </header>

        <div ref={log} role="log" aria-live="polite" aria-label="Messages" tabIndex={0} className="flex flex-1 flex-col gap-3 overflow-y-auto px-4 pt-3 pb-4 [mask-image:linear-gradient(to_bottom,transparent,black_1.25rem)]">
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
                    <Reading reading={item.reading} />
                  </div>
                )
              case 'claude':
                return <ClaudeSays key={item.id} text={item.text} />
              case 'offer':
                return (
                  <div key={item.id} className="rise flex flex-col gap-3 rounded-3xl bg-surface-muted p-4">
                    <p className="font-display text-lead leading-tight font-bold">Reservations are open for {item.day}</p>
                    {item.counted ? (
                      <>
                        <p className="text-ink-soft">
                          ${PRICE} is held on your PayPal and charged only if {NEEDED} people reserve.
                        </p>
                        <Button ref={reserveButton} onClick={() => void reserve(item)} disabled={phase !== 'offer'}>
                          Reserve with PayPal · ${PRICE} hold
                          <Icon name="arrowRight" size={18} />
                        </Button>
                      </>
                    ) : (
                      <>
                        <p className="text-ink-soft">You aren’t counted for this date, so there is nothing for you to reserve. Everyone who can come reserves with a ${PRICE} hold.</p>
                        <Button ref={reserveButton} variant="secondary" onClick={() => void reserve(item)} disabled={phase !== 'offer'}>
                          Watch the others reserve
                          <Icon name="arrowRight" size={18} />
                        </Button>
                      </>
                    )}
                  </div>
                )
              case 'hold':
                return (
                  <Notice key={item.id} tone="hold">
                    {item.name === 'You' ? 'You reserved' : `${item.name.split(' ')[0]} reserved`} · not charged yet
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

        <footer className="flex flex-col gap-2.5 bg-surface-muted p-3">
          {turn && (
            <div className="rise -mx-3 flex gap-2 overflow-x-auto px-3 sm:flex-wrap" role="group" aria-label="Quick replies">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => quickReply(s)}
                  className="min-h-11 shrink-0 rounded-full bg-surface px-4 text-sm font-medium whitespace-nowrap transition-colors duration-150 hover:bg-line"
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
                placeholder={turn ? 'When can you come? Write it like a text' : phase === 'playing' ? 'Everyone is answering…' : 'Claude is on it…'}
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
      <p className="px-2 text-sm text-ink-soft">
        A scripted example: here, simple rules in your browser stand in for Claude. <span className="font-semibold text-ink">Try it as Maya</span> shows the real reading.
      </p>
    </div>
  )
}
