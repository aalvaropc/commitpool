import type { ReactNode } from 'react'
import { READ_LABEL, type ReadBy, type Reader } from '../../lib/liveReading'
import type { Day, SampleLimit, SampleReading, SampleVerdict } from '../../lib/sampleReader'
import { Avatar, Icon, Pill, VerdictChip, type IconName } from '../ui'

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

export function Mark({ icon }: { icon: IconName }) {
  return (
    <span aria-hidden="true" className="inline-grid size-8 shrink-0 place-items-center rounded-full bg-brand-wash text-brand">
      <Icon name={icon} size={17} />
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

const bubble = 'rounded-[1.25rem] px-3.5 py-2.5 text-[0.9375rem] leading-[1.42] [overflow-wrap:anywhere]'
const signature = 'px-0.5 text-[0.8125rem] text-ink-faint'

export function Typing({ name }: { name: string }) {
  return (
    <Row hidden>
      <Avatar name={name} size="sm" />
      <span className="rounded-[1.25rem] rounded-bl-[0.35rem] bg-surface-muted px-4 py-3">
        <Dots />
      </span>
    </Row>
  )
}

export function Said({ name, text, role }: { name: string; text: string; role?: string }) {
  return (
    <Row>
      <Avatar name={name} size="sm" />
      <div className="flex min-w-0 flex-col gap-1">
        <span className={signature}>
          {name}
          {role && <span className="font-semibold text-brand"> · {role}</span>}
        </span>
        <p className={`max-w-[28rem] rounded-bl-[0.35rem] bg-surface-muted ${bubble}`}>{text}</p>
      </div>
    </Row>
  )
}

export function SaidByYou({ text }: { text: string }) {
  return (
    <Row side="right">
      <p className={`max-w-[80%] rounded-br-[0.35rem] bg-brand font-medium text-on-brand ${bubble}`}>
        <span className="sr-only">You: </span>
        {text}
      </p>
    </Row>
  )
}

export function Reading({ reading, by, live = false }: { reading: SampleReading | null; by: Reader; live?: boolean }) {
  const indent = live ? 'justify-end pl-10' : 'pl-[2.625rem]'
  if (reading === null) {
    if (!live) return null
    return (
      <div className={`rise flex items-center gap-2 text-sm text-ink-soft ${indent}`}>
        <Icon name="spark" size={16} className="text-brand" />
        Claude is reading…
        <Dots />
      </div>
    )
  }
  return (
    <div className={`rise flex flex-wrap items-center gap-1.5 ${indent}`}>
      <span className={`mr-0.5 text-xs ${by === 'claude' ? 'font-semibold text-brand' : 'text-ink-faint'}`}>{READ_LABEL[by]}</span>
      <VerdictChip verdict={reading.sat} size="sm">{`Sat · ${dayWords(reading, 'sat')}`}</VerdictChip>
      <VerdictChip verdict={reading.sun} size="sm">{`Sun · ${dayWords(reading, 'sun')}`}</VerdictChip>
    </div>
  )
}

function Signed({ name, role, icon, wash, children }: { name: string; role: string; icon: IconName; wash: boolean; children: string }) {
  return (
    <Row>
      <Mark icon={icon} />
      <div className="flex min-w-0 flex-col gap-1">
        <span className={signature}>
          {name} <span className="font-semibold text-brand">· {role}</span>
        </span>
        <p className={`max-w-[28rem] rounded-bl-[0.35rem] ${wash ? 'bg-brand-wash' : 'bg-surface-muted'} ${bubble}`}>{children}</p>
      </div>
    </Row>
  )
}

export function CommitPoolSays({ text }: { text: string }) {
  return (
    <Signed name="CommitPool" role="counts for Iris" icon="users" wash>
      {text}
    </Signed>
  )
}

export function Asks({ by, text }: { by: ReadBy; text: string }) {
  return (
    <Signed name={by === 'claude' ? 'Claude' : 'Simple rules'} role="asks you" icon={by === 'claude' ? 'spark' : 'help'} wash={false}>
      {text}
    </Signed>
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
      <p className="text-sm">Simulated here: no money moved.</p>
    </div>
  )
}
