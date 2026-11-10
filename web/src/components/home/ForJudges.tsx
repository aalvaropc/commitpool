import { useEffect, useRef, useState, type Ref } from 'react'
import type { DemoInfo, JudgeLogin } from '../../api'
import { embeddableVideo } from '../../lib/demoInfo'
import { Icon, Pill } from '../ui'
import type { Tone } from '../../lib/status'
import { SectionHead } from './Section'

const truth: { what: string; tone: Tone; label: string; note?: string }[] = [
  { what: 'PayPal Sandbox', tone: 'paid', label: 'Real Sandbox calls', note: 'Orders, holds, charges, refunds, payouts, webhooks' },
  { what: 'Claude on Amazon Bedrock', tone: 'paid', label: 'Real', note: 'Reads every answer live' },
  { what: 'Demo participants and their answers', tone: 'attention', label: 'Prepared', note: 'Six people and what they wrote' },
  { what: '“Close reservations now”, “Start the class now”, “Skip to the last 2 hours”', tone: 'hold', label: 'Accelerated clock' },
  { what: '“Fail next charge”', tone: 'cancel', label: 'Simulated failure', note: 'A PayPal Sandbox mock response' },
  { what: 'Emails', tone: 'neutral', label: 'Logged, not sent' },
]

function CopyRow({ label, value, shown }: { label: string; value: string; shown: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setState('copied')
    } catch {
      setState('failed')
    }
  }
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2.5 rounded-[0.875rem] bg-surface-muted py-1.5 pr-1.5 pl-3.5">
      <div className="min-w-0">
        <p className="text-xs text-ink-faint">{label}</p>
        <p className={`font-mono text-[0.9375rem] ${state === 'failed' ? 'select-all [overflow-wrap:anywhere]' : 'truncate'}`}>{state === 'failed' ? value : shown}</p>
      </div>
      <button
        type="button"
        onClick={() => void copy()}
        aria-label={`Copy the Sandbox buyer ${label.toLowerCase()}`}
        className="inline-flex min-h-11 min-w-11 items-center gap-1.5 rounded-full bg-canvas px-3.5 text-sm font-semibold transition-colors duration-150 hover:bg-surface"
      >
        <Icon name={state === 'copied' ? 'check' : 'copy'} size={15} />
        {state === 'copied' ? 'Copied' : 'Copy'}
      </button>
      <p role="status" className="sr-only">
        {state === 'copied' ? `${label} copied.` : state === 'failed' ? `Copying is blocked here, so the ${label.toLowerCase()} is shown to copy by hand.` : ''}
      </p>
    </div>
  )
}

export function SandboxBuyer({ login }: { login: JudgeLogin | undefined }) {
  if (!login) {
    return <p className="mt-7 max-w-[44ch] text-ink-soft">The PayPal Sandbox buyer login for step 3 is in the testing instructions on Devpost.</p>
  }
  return (
    <section aria-labelledby="buyer-title" className="mt-7 grid gap-2.5 rounded-[1.25rem] bg-surface p-[1.125rem]">
      <div className="flex flex-wrap items-center justify-between gap-2.5">
        <h3 id="buyer-title" className="text-base">
          PayPal Sandbox buyer
        </h3>
        <Pill tone="hold">Test account</Pill>
      </div>
      <CopyRow label="Email" value={login.email} shown={login.email} />
      <CopyRow label="Password" value={login.password} shown={'•'.repeat(Math.min(Math.max(login.password.length, 8), 16))} />
    </section>
  )
}

export function VideoFrame({ src, autoplay = false, frameRef }: { src: string; autoplay?: boolean; frameRef?: Ref<HTMLIFrameElement> }) {
  return (
    <iframe
      ref={frameRef}
      src={autoplay ? `${src}?autoplay=1` : src}
      title="CommitPool demo video: Iris fills her class on the PayPal Sandbox"
      loading="lazy"
      referrerPolicy="strict-origin-when-cross-origin"
      allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
      allowFullScreen
      className="block h-full w-full border-0"
    />
  )
}

export function DemoVideo({ url }: { url: string | undefined }) {
  const src = embeddableVideo(url)
  const [playing, setPlaying] = useState(false)
  const frame = useRef<HTMLIFrameElement>(null)
  useEffect(() => {
    if (playing) frame.current?.focus()
  }, [playing])
  if (!src) return null
  const id = src.slice(src.lastIndexOf('/') + 1)
  return (
    <div className="mb-14 aspect-video overflow-hidden rounded-tile bg-surface">
      {playing ? (
        <VideoFrame src={src} autoplay frameRef={frame} />
      ) : (
        <button type="button" onClick={() => setPlaying(true)} aria-label="Watch the demo video" className="group relative block h-full w-full rounded-tile">
          <img src={`https://i.ytimg.com/vi/${id}/hqdefault.jpg`} alt="" loading="lazy" decoding="async" className="block h-full w-full object-cover" />
          <span className="absolute bottom-[clamp(1rem,3vw,1.75rem)] left-[clamp(1rem,3vw,1.75rem)] inline-flex min-h-14 items-center gap-3 rounded-full bg-charcoal pr-[1.375rem] pl-2 font-bold text-ink transition-colors duration-200 group-hover:bg-surface-muted">
            <span className="grid size-[2.625rem] place-items-center rounded-full bg-brand text-on-brand">
              <Icon name="play" size={16} fill="currentColor" />
            </span>
            Watch the demo
          </span>
        </button>
      )}
    </div>
  )
}

export default function ForJudges({ info }: { info: DemoInfo | null | undefined }) {
  const login = info?.judge_login
  return (
    <section id="judges" aria-labelledby="judges-title" className="section-pad scroll-mt-4">
      <div className="wrap">
        <SectionHead id="judges-title" title="For judges.">
          Three steps in the running demo, nothing to install. Everything runs on the PayPal Sandbox.
        </SectionHead>
        <div className="grid gap-12 lg:grid-cols-12 lg:gap-x-8">
          <div className="lg:col-span-6">
            <ol className="grid border-t border-line">
              {[
                { title: 'Open Iris’s board', text: 'Press Try it as Iris. The class is set up and six answers arrive in seconds, each read by Claude.' },
                { title: 'Answer as Grace', text: 'Act as Grace and reply to Claude’s question. Saturday reaches 3; open reservations.' },
                {
                  title: 'Reserve with PayPal',
                  text: `Act as Priya, Ben and Grace and reserve with the Sandbox buyer${login ? ' below' : ''}. The third reservation charges all three; then press Close reservations now and Start the class now.`,
                },
              ].map((s, i) => (
                <li key={s.title} className="grid grid-cols-[3.5rem_minmax(0,1fr)] gap-4 border-b border-line py-5">
                  <span aria-hidden="true" className="shout text-5xl leading-[0.85] text-brand">
                    {i + 1}
                  </span>
                  <div>
                    <h3 className="text-xl">{s.title}</h3>
                    <p className="mt-1.5 max-w-[44ch] text-ink-soft">{s.text}</p>
                  </div>
                </li>
              ))}
            </ol>
            <SandboxBuyer login={login} />
          </div>
          <div className="lg:col-span-5 lg:col-start-8">
            <DemoVideo url={info?.video_url} />
            <h3 className="mb-4 text-2xl">What’s real, what’s prepared</h3>
            <table className="w-full border-collapse text-[0.9375rem]">
              <tbody>
                {truth.map((t) => (
                  <tr key={t.what}>
                    <th scope="row" className="w-[46%] border-t border-line py-3.5 pr-3 text-left align-top font-bold">
                      {t.what}
                    </th>
                    <td className="border-t border-line py-3.5 align-top text-ink-soft">
                      <Pill tone={t.tone}>{t.label}</Pill>
                      {t.note && <span className="mt-1.5 block text-[0.8125rem] text-ink-faint">{t.note}</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </section>
  )
}
