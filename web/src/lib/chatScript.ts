import type { Day, SampleReading } from './sampleReader'

export interface Voice {
  name: string
  message: string
}

export const HOST = 'Maya Okafor'
export const NEEDED = 3
export const PRICE = 45
export const STUDIO = 120
export const CLASS_HOURS = { start: 10, end: 12 }

export const OPENING = 'Wheel-throwing for beginners! Sat Nov 14 or Sun Nov 15, 10 AM–12 PM. The studio costs me $120, so I need 3 of you at $45 each.'

export const CROWD: Voice[] = [
  { name: 'Priya Raman', message: 'Saturday works for me, Sunday I can’t.' },
  { name: 'Ben Whitaker', message: 'either day, but Sat only if we finish before six' },
  { name: 'Lucía Torres', message: 'I can’t do Saturday; Sunday morning is perfect.' },
  { name: 'Hannah Kim', message: 'Count me in on Saturday. Sunday is no good for me.' },
  { name: 'Tomás Silva', message: 'Both days work for me.' },
]

export const SUGGESTIONS = ['Saturday works for me', 'Both days, but only if it ends by six', 'maybe? ideally in the afternoon']

export interface Read {
  name: string
  reading: SampleReading
}

export interface Tally {
  sat: number
  sun: number
}

export function fits(reading: SampleReading, day: Day): boolean {
  if (reading.ask) return false
  const verdict = reading[day]
  if (verdict === 'available') return true
  const limit = reading.limits[day]
  if (verdict !== 'conditional' || !limit) return false
  return limit.type === 'end_before' ? CLASS_HOURS.end <= limit.hour : CLASS_HOURS.start >= limit.hour
}

export function tally(reads: Read[]): Tally {
  return {
    sat: reads.filter((r) => fits(r.reading, 'sat')).length,
    sun: reads.filter((r) => fits(r.reading, 'sun')).length,
  }
}

export interface Recommendation {
  day: 'Sat, Nov 14' | 'Sun, Nov 15'
  key: 'sat' | 'sun'
  count: number
  enough: boolean
  text: string
}

export function recommend(t: Tally): Recommendation {
  const key = t.sun > t.sat ? 'sun' : 'sat'
  const day = key === 'sat' ? 'Sat, Nov 14' : 'Sun, Nov 15'
  const count = t[key]
  const enough = count >= NEEDED
  return {
    day,
    key,
    count,
    enough,
    text: enough ? `${day} works for ${count} — you need ${NEEDED}.` : `${count} can come on ${day} — you need ${NEEDED}. Keep the answers coming.`,
  }
}

export function reservers(reads: Read[], key: Day): string[] {
  return reads.filter((r) => fits(r.reading, key)).map((r) => r.name)
}
