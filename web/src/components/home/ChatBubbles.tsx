import type { ReactNode } from 'react'
import type { Day, SampleLimit, SampleReading, SampleVerdict } from '../../lib/sampleReader'
import { Avatar, Icon, Pill, VerdictChip } from '../ui'

const verdictWords: Record<SampleVerdict, string> = {
  available: 'Can come',
  conditional: 'Only if…',
  unavailable: 'Can’t',
  unknown: 'Not sure',
}

function clock(hour: number): string {
  const whole = Math.floor(hour)
  const minutes = Math.round((hour - whole) * 60)
  if (whole === 12 && minutes === 0) return 'noon'
  if ((whole === 24 || whole === 0) && minutes === 0) return 'midnight'
  const h = whole % 12 === 0 ? 12 : whole % 12
  return `${h}${minutes ? `:${String(minutes).padStart(2, '0')}` : ''} ${whole >= 12 && whole < 24 ? 'PM' : 'AM'}`
}

function limitWords(limit: SampleLimit): string {
  return limit.type === 'end_before' ? `If it ends by ${clock(limit.hour)}` : `If it starts after ${clock(limit.hour)}`
}

function dayWords(reading: SampleReading, day: Day): string {
  const limit = reading.limits[day]
  return reading[day] === 'conditional' && limit ? limitWords(limit) : verdictWords[reading[day]]
}

export function Dots() {
  return (
    <span aria-hidden="true" className="inline-flex items-center gap-1">
      {[0, 1, 2].map((i) => (
        <span key={i} className="size-1.5 rounded-full bg-ink-soft" style={{ animation: 'blink 1.1s infinite', animationDelay: `${i * 0.18}s` }} />
      ))}
    </span>
  )
}

export function ClaudeMark({ size = 'md' }: { size?: 'sm' | 'md' }) {
  return (
    <span aria-hidden="true" className={`inline-grid shrink-0 place-items-center rounded-full bg-brand-wash text-brand ${size === 'md' ? 'size-10' : 'size-8'}`}>
      <Icon name="spark" size={size === 'md' ? 20 : 16} />
    </span>
  )
}

export function Row({ side = 'left', hidden = false, children }: { side?: 'left' | 'right'; hidden?: boolean; children: ReactNode }) {
  return (
    <div aria-hidden={hidden || undefined} className={`rise flex gap-2.5 ${side === 'right' ? 'justify-end' : 'items-end'}`}>
      {children}
    </div>
  )
}

const bubble = 'rounded-3xl px-4 py-2.5 [overflow-wrap:anywhere]'

export function Typing({ name }: { name: string }) {
  return (
    <Row hidden>
      <Avatar name={name} />
      <span className="rounded-3xl rounded-bl-md bg-surface-muted px-4 py-3">
        <Dots />
      </span>
    </Row>
  )
}

export function Said({ name, text, role }: { name: string; text: string; role?: string }) {
  return (
    <Row>
      <Avatar name={name} />
      <div className="flex min-w-0 flex-col gap-1">
        <span className="px-1 text-sm text-ink-soft">
          {name}
          {role && <span className="text-brand"> · {role}</span>}
        </span>
        <p className={`max-w-[28rem] rounded-bl-md bg-surface-muted ${bubble}`}>{text}</p>
      </div>
    </Row>
  )
}

export function SaidByYou({ text }: { text: string }) {
  return (
    <Row side="right">
      <p className={`max-w-[80%] rounded-br-md bg-brand font-medium text-on-brand ${bubble}`}>
        <span className="sr-only">You: </span>
        {text}
      </p>
    </Row>
  )
}

export function Reading({ reading }: { reading: SampleReading | null }) {
  if (reading === null) {
    return (
      <div aria-hidden="true" className="rise flex items-center gap-2 pl-[3.125rem] text-sm text-ink-soft">
        <Icon name="spark" size={16} className="text-brand" />
        Claude is reading
        <Dots />
      </div>
    )
  }
  return (
    <div className="rise flex flex-col gap-1.5 pl-[3.125rem]">
      <span className="flex flex-wrap items-center gap-1.5">
        <Icon name="spark" size={16} className="text-brand" />
        <span className="sr-only">Read as:</span>
        <VerdictChip verdict={reading.sat}>{`Sat · ${dayWords(reading, 'sat')}`}</VerdictChip>
        <VerdictChip verdict={reading.sun}>{`Sun · ${dayWords(reading, 'sun')}`}</VerdictChip>
      </span>
      {reading.ask && (
        <span className="flex items-start gap-1.5 text-sm text-maybe">
          <Icon name="help" size={16} className="mt-0.5 shrink-0" />
          Needs clarifying: {reading.ask}
        </span>
      )}
    </div>
  )
}

export function ClaudeSays({ text }: { text: string }) {
  return (
    <Row>
      <ClaudeMark />
      <div className="flex min-w-0 flex-col gap-1">
        <span className="px-1 text-sm text-ink-soft">
          Claude <span className="text-brand">· reads for Maya</span>
        </span>
        <p className={`max-w-[28rem] rounded-bl-md bg-brand-wash ${bubble}`}>{text}</p>
      </div>
    </Row>
  )
}

export function Notice({ children, tone }: { children: ReactNode; tone: 'hold' | 'attention' }) {
  return (
    <div className="rise flex justify-center">
      <Pill tone={tone}>{children}</Pill>
    </div>
  )
}

export function ItsOn({ text }: { text: string }) {
  return (
    <div className="rise flex flex-col gap-1 rounded-3xl bg-brand p-5 text-on-brand">
      <p className="shout text-5xl leading-none">It’s on</p>
      <p className="text-lg font-semibold">{text}</p>
    </div>
  )
}
