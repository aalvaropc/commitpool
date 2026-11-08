import type { Day } from './sampleReader'

export const SAMPLE_ZONE = 'America/New_York'
export const WINDOW_MIN = 1440
export const START_HOUR = 10
export const DURATION_MIN = 120

export interface SampleDate {
  key: Day
  weekday: string
  label: string
  hours: string
}

export type SampleDates = Record<Day, SampleDate>

const MINUTE = 60_000
const DAY_MS = 24 * 60 * MINUTE
const SHORT_DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const SHORT_MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const WEEKDAYS: Record<Day, string> = { sat: 'Saturday', sun: 'Sunday' }

const wallClock = new Intl.DateTimeFormat('en-US', {
  timeZone: SAMPLE_ZONE,
  year: 'numeric',
  month: 'numeric',
  day: 'numeric',
  hour: 'numeric',
  minute: 'numeric',
  second: 'numeric',
  hourCycle: 'h23',
})

function local(at: Date): { civil: number; millis: number } {
  const parts = wallClock.formatToParts(at)
  const part = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((p) => p.type === type)?.value ?? 0)
  const civil = Date.UTC(part('year'), part('month') - 1, part('day'))
  const millis = ((part('hour') * 60 + part('minute')) * 60 + part('second')) * 1000 + at.getUTCMilliseconds()
  return { civil, millis }
}

export function clockWords(minutes: number): string {
  const hour = Math.floor(minutes / 60) % 24
  const minute = minutes % 60
  const h = hour % 12 === 0 ? 12 : hour % 12
  return `${h}${minute ? `:${String(minute).padStart(2, '0')}` : ''} ${hour < 12 ? 'AM' : 'PM'}`
}

export const CLASS_HOURS_WORDS = `${clockWords(START_HOUR * 60)}–${clockWords(START_HOUR * 60 + DURATION_MIN)}`

function dayLabel(civil: number): string {
  const d = new Date(civil)
  return `${SHORT_DAYS[d.getUTCDay()]}, ${SHORT_MONTHS[d.getUTCMonth()]} ${d.getUTCDate()}`
}

export function firstSaturday(now: Date): number {
  const earliest = local(new Date(now.getTime() + (WINDOW_MIN + 3 * 24 * 60) * MINUTE))
  let day = earliest.civil
  if (earliest.millis > START_HOUR * 60 * MINUTE) day += DAY_MS
  while (new Date(day).getUTCDay() !== 6) day += DAY_MS
  return day
}

export function sampleDates(now: Date): SampleDates {
  const saturday = firstSaturday(now)
  const date = (key: Day, civil: number): SampleDate => ({ key, weekday: WEEKDAYS[key], label: dayLabel(civil), hours: CLASS_HOURS_WORDS })
  return { sat: date('sat', saturday), sun: date('sun', saturday + DAY_MS) }
}

export function withLabels(dates: SampleDates, returned: { key: string; label: string; hours: string }[]): SampleDates {
  const out = { ...dates }
  for (const d of returned) {
    if ((d.key === 'sat' || d.key === 'sun') && d.label && d.hours) out[d.key] = { ...dates[d.key], label: d.label, hours: d.hours }
  }
  return out
}
