const partsFormats = new Map<string, Intl.DateTimeFormat>()

function zoneFormat(timeZone: string): Intl.DateTimeFormat {
  let f = partsFormats.get(timeZone)
  if (!f) {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone,
      hourCycle: 'h23',
      weekday: 'short',
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
    partsFormats.set(timeZone, f)
  }
  return f
}

interface Wall {
  year: number
  month: number
  day: number
  hour: number
  minute: number
  second: number
  weekday: string
  monthName: string
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

function wallClock(instant: number, timeZone: string): Wall {
  const parts: Record<string, string> = {}
  for (const p of zoneFormat(timeZone).formatToParts(new Date(instant))) parts[p.type] = p.value
  const monthName = parts.month
  return {
    year: Number(parts.year),
    month: MONTHS.indexOf(monthName) + 1,
    day: Number(parts.day),
    hour: Number(parts.hour) % 24,
    minute: Number(parts.minute),
    second: Number(parts.second),
    weekday: parts.weekday,
    monthName,
  }
}

function offsetAt(instant: number, timeZone: string): number {
  const w = wallClock(instant, timeZone)
  const asUTC = Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second)
  return asUTC - Math.floor(instant / 1000) * 1000
}

const pad = (n: number) => String(n).padStart(2, '0')

export function rfc3339InZone(instant: number, timeZone: string): string {
  const offset = offsetAt(instant, timeZone)
  const w = wallClock(instant, timeZone)
  const local = `${w.year}-${pad(w.month)}-${pad(w.day)}T${pad(w.hour)}:${pad(w.minute)}:${pad(w.second)}`
  if (offset === 0) return local + 'Z'
  const sign = offset > 0 ? '+' : '-'
  const total = Math.abs(offset) / 60000
  return `${local}${sign}${pad(Math.floor(total / 60))}:${pad(total % 60)}`
}

export function zonedToRFC3339(date: string, time: string, timeZone: string): string {
  const d = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  const t = /^(\d{2}):(\d{2})$/.exec(time)
  if (!d || !t) throw new Error('Use a date as YYYY-MM-DD and a time as HH:MM.')
  const [y, mo, da, h, mi] = [Number(d[1]), Number(d[2]), Number(d[3]), Number(t[1]), Number(t[2])]
  const wall = Date.UTC(y, mo - 1, da, h, mi)
  const check = new Date(wall)
  if (check.getUTCMonth() !== mo - 1 || check.getUTCDate() !== da || h > 23 || mi > 59) {
    throw new Error('That date or time does not exist.')
  }
  let instant = wall - offsetAt(wall, timeZone)
  const second = offsetAt(instant, timeZone)
  if (wall - second !== instant) instant = wall - second
  return rfc3339InZone(instant, timeZone)
}

export function zonedPlusMinutes(start: string, minutes: number, timeZone: string): string {
  return rfc3339InZone(Date.parse(start) + minutes * 60000, timeZone)
}

function clockText(hour: number, minute: number): string {
  const period = hour < 12 ? 'AM' : 'PM'
  const h = hour % 12 === 0 ? 12 : hour % 12
  return minute === 0 ? `${h} ${period}` : `${h}:${pad(minute)} ${period}`
}

function dayText(w: Wall): string {
  return `${w.weekday}, ${w.monthName} ${w.day}`
}

export function formatDay(iso: string, timeZone: string): string {
  return dayText(wallClock(Date.parse(iso), timeZone))
}

const WEEKDAYS: Record<string, string> = { Mon: 'Monday', Tue: 'Tuesday', Wed: 'Wednesday', Thu: 'Thursday', Fri: 'Friday', Sat: 'Saturday', Sun: 'Sunday' }

export function formatWeekday(iso: string, timeZone: string): string {
  const short = wallClock(Date.parse(iso), timeZone).weekday
  return WEEKDAYS[short] ?? short
}

export function formatClock(iso: string, timeZone: string): string {
  const w = wallClock(Date.parse(iso), timeZone)
  return clockText(w.hour, w.minute)
}

export function formatMoment(iso: string, timeZone: string): string {
  const w = wallClock(Date.parse(iso), timeZone)
  return `${dayText(w)} · ${clockText(w.hour, w.minute)}`
}

export function formatDateRange(startIso: string, endIso: string, timeZone: string): string {
  const s = wallClock(Date.parse(startIso), timeZone)
  const e = wallClock(Date.parse(endIso), timeZone)
  const sameDay = s.year === e.year && s.month === e.month && s.day === e.day
  if (sameDay) return `${dayText(s)} · ${clockText(s.hour, s.minute)}–${clockText(e.hour, e.minute)}`
  return `${dayText(s)} · ${clockText(s.hour, s.minute)} – ${dayText(e)} · ${clockText(e.hour, e.minute)}`
}

export function formatTimeRange(startIso: string, endIso: string, timeZone: string): string {
  const s = wallClock(Date.parse(startIso), timeZone)
  const e = wallClock(Date.parse(endIso), timeZone)
  const sameDay = s.year === e.year && s.month === e.month && s.day === e.day
  return sameDay ? `${clockText(s.hour, s.minute)}–${clockText(e.hour, e.minute)}` : `${clockText(s.hour, s.minute)} – ${clockText(e.hour, e.minute)} next day`
}

export function zoneName(timeZone: string): string {
  const slash = timeZone.lastIndexOf('/')
  if (slash < 0) return timeZone
  return `${timeZone.slice(slash + 1).replaceAll('_', ' ')} time`
}

export function clock12(hhmm: string): string {
  const m = /^(\d{1,2}):(\d{2})$/.exec(hhmm)
  if (!m) return hhmm
  return clockText(Number(m[1]) % 24, Number(m[2]))
}

export function earlier(a: string, b: string): string {
  return Date.parse(a) <= Date.parse(b) ? a : b
}

export function timeLeft(deadline: string, now: number): string | null {
  const ms = Date.parse(deadline) - now
  if (ms <= 0) return null
  if (ms < 60000) return 'less than a minute'
  const minutes = Math.floor(ms / 60000)
  if (minutes < 60) return `${minutes} min`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return minutes % 60 === 0 ? `${hours} h` : `${hours} h ${minutes % 60} min`
  const days = Math.floor(hours / 24)
  return hours % 24 === 0 ? `${days} d` : `${days} d ${hours % 24} h`
}

export function windowLabel(minutes: number): string {
  if (minutes < 1440) return `Demo window: ${minutes} ${minutes === 1 ? 'minute' : 'minutes'}`
  const hours = minutes / 60
  return Number.isInteger(hours) ? `${hours} hours` : `${minutes} minutes`
}

export function durationLabel(minutes: number): string {
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  if (h === 0) return `${m} min`
  const hours = h === 1 ? '1 hour' : `${h} hours`
  return m === 0 ? hours : `${hours} ${m} min`
}

export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

export const TIME_ZONES = [
  'Pacific/Honolulu',
  'America/Anchorage',
  'America/Los_Angeles',
  'America/Denver',
  'America/Phoenix',
  'America/Chicago',
  'America/Mexico_City',
  'America/Bogota',
  'America/Lima',
  'America/New_York',
  'America/Toronto',
  'America/Caracas',
  'America/Santiago',
  'America/Halifax',
  'America/Sao_Paulo',
  'America/Argentina/Buenos_Aires',
  'Atlantic/Azores',
  'UTC',
  'Europe/London',
  'Europe/Lisbon',
  'Africa/Lagos',
  'Europe/Madrid',
  'Europe/Paris',
  'Europe/Berlin',
  'Europe/Rome',
  'Africa/Johannesburg',
  'Africa/Cairo',
  'Europe/Athens',
  'Europe/Istanbul',
  'Africa/Nairobi',
  'Asia/Dubai',
  'Asia/Karachi',
  'Asia/Kolkata',
  'Asia/Dhaka',
  'Asia/Bangkok',
  'Asia/Jakarta',
  'Asia/Singapore',
  'Asia/Shanghai',
  'Asia/Manila',
  'Asia/Tokyo',
  'Asia/Seoul',
  'Australia/Perth',
  'Australia/Sydney',
  'Pacific/Auckland',
]
