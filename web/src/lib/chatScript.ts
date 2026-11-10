import type { SampleDates } from './sampleDates'
import type { Day, SampleReading } from './sampleReader'

export type Counted = Record<Day, boolean>

export interface Voice {
  name: string
  message: string
  reading: SampleReading
}

export const HOST = 'Iris Calder'
export const NEEDED = 3
export const PRICE = 45
export const SESSION_COST = 120
export const CLASS_HOURS = { start: 10, end: 12 }
export const YOU = 'You'

export const CROWD: Voice[] = [
  {
    name: 'Priya Raman',
    message: "Saturday works for me, Sunday I can't.",
    reading: { sat: 'available', sun: 'unavailable', limits: {}, ask: null },
  },
  {
    name: 'Ben Whitaker',
    message: "The first date works if we're done before 6; the second one I'm away.",
    reading: { sat: 'conditional', sun: 'unavailable', limits: { sat: { type: 'end_before', hour: 18 } }, ask: null },
  },
  {
    name: 'Lucía Torres',
    message: 'El sábado no puedo, pero el domingo me viene perfecto.',
    reading: { sat: 'unavailable', sun: 'available', limits: {}, ask: null },
  },
  {
    name: 'Hannah Kim',
    message: "Saturday only if we're done by 11:30 — I have a lunch at noon. Sunday is out.",
    reading: { sat: 'conditional', sun: 'unavailable', limits: { sat: { type: 'end_before', hour: 11.5 } }, ask: null },
  },
  {
    name: 'Tomás Silva',
    message: "Sunday's good. Saturday I'm driving my daughter to a tournament all day.",
    reading: { sat: 'unavailable', sun: 'available', limits: {}, ask: null },
  },
]

export const SUGGESTIONS = ['No problem — Saturday works!', 'El domingo sí puedo', 'The first one, if we’re done by 11']

export const ANSWERS = ['Saturday morning works', 'Sunday morning works', 'Neither morning, sorry']

export interface Read {
  name: string
  counted: Counted
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

export function countedBy(reading: SampleReading): Counted {
  return { sat: fits(reading, 'sat'), sun: fits(reading, 'sun') }
}

export const CROWD_READS: Read[] = CROWD.map((v) => ({ name: v.name, counted: countedBy(v.reading) }))

export function tally(reads: Read[]): Tally {
  return {
    sat: reads.filter((r) => r.counted.sat).length,
    sun: reads.filter((r) => r.counted.sun).length,
  }
}

export function leader(t: Tally): Day {
  return t.sun > t.sat ? 'sun' : 'sat'
}

export function opening(dates: SampleDates): string {
  return `Life drawing for beginners! Saturday or Sunday, ${dates.sat.hours} (${dates.sat.label} or ${dates.sun.label}). The room and model cost me $${SESSION_COST}, so I need ${NEEDED} of you at $${PRICE} each.`
}

export function crowdTally(dates: SampleDates): string {
  const t = tally(CROWD_READS)
  return `${dates.sat.weekday}: ${t.sat} can come. ${dates.sun.weekday}: ${t.sun}. Iris needs ${NEEDED} on one day, so she needs one more — you could be the third.`
}

export interface Outcome {
  key: Day | null
  counts: Tally
  text: string
}

export function outcome(you: Counted, dates: SampleDates, unanswered: boolean): Outcome {
  const counts = tally([...CROWD_READS, { name: YOU, counted: you }])
  const key = leader(counts)
  if (counts[key] >= NEEDED) {
    const day = dates[key]
    const text =
      you.sat && you.sun
        ? `You’re counted for both days, so each has ${NEEDED}. On a tie the earlier date wins: ${day.label}. Iris opens reservations.`
        : `You’re counted for ${day.weekday}: that makes ${counts[key]}, enough for Iris. She opens reservations for ${day.label}.`
    return { key, counts, text }
  }
  const why = unanswered ? 'There’s still a question for you, so you aren’t counted yet.' : 'From what you wrote, you aren’t counted for either day.'
  return {
    key: null,
    counts,
    text: `${why} ${dates.sat.weekday} has ${counts.sat} and ${dates.sun.weekday} has ${counts.sun}: Iris still needs one more, so nothing opens and nobody pays.`,
  }
}

export function reservers(reads: Read[], key: Day): string[] {
  const counted = reads.filter((r) => r.counted[key]).map((r) => r.name)
  return [...counted.filter((n) => n === YOU), ...counted.filter((n) => n !== YOU)]
}
