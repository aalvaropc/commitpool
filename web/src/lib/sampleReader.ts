export type SampleVerdict = 'available' | 'conditional' | 'unavailable' | 'unknown'

export type Day = 'sat' | 'sun'

export interface SampleLimit {
  type: 'end_before' | 'start_after'
  hour: number
}

export interface SampleReading {
  sat: SampleVerdict
  sun: SampleVerdict
  limits: Partial<Record<Day, SampleLimit>>
  ask: string | null
}

const DAYS: Day[] = ['sat', 'sun']

const UNSURE = /\b(maybe|not sure|unsure|might|perhaps|possibly|probably|don't know|no idea|we'll see|tbd)\b/
const FINE = /\b(not busy|not a problem|no problem|no worries|no plans)\b/
const NEGATIVE =
  /\b(can't|cannot|can not|won't|wouldn't|couldn't|don't|doesn't|isn't|aren't|not|no|nope|neither|nor|never|busy|away|unavailable|out of town|bad|wedding|plans|trip|appointment|booked|swamped|(?:i'm|im|i am|i'll be|ill be) (?:out|working|traveling|travelling)|i work)\b/
const CONDITION = /\b(only if|as long as|so long as|if|unless|before|after|until|till|provided|depends)\b/
const POSITIVE =
  /\b(works?|fine|good|great|perfect|free|can|could|yes|yeah|yep|ok|okay|sure|down|available|love|happy|lovely|awesome|amazing|(?:i'm|im|i am|count me|i'll be|ill be) in|(?:i'll|ill|i will|i can) be there)\b/
const SOFT = /\b(ideally|prefer|rather)\b/
const OFF_HOURS = /\b(afternoon|evening|night)s?\b/
const CLOCK = /\d/
const LIMIT =
  /\b(before|by|until|till|after|from)\s+(\d{1,2})(?::(\d{2}))?|\b(before|by|until|till|after|from)\s+(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|noon|midday|midnight)\b/
const MERIDIEM = /^\s*([ap])m\b/
const DAY_PAIR = /\b(sat(?:urday)?s?|sun(?:day)?s?)\s+and\s+(sat(?:urday)?s?|sun(?:day)?s?)\b/g
const FILLER = /\b(sat(urday)?s?|sun(day)?s?|either|both|any|every|days?|weekend|whichever|whenever|too|also|as well|or|on|the|for me|then|same|please|this|next)\b/g
const WORD_HOURS: Record<string, number> = {
  one: 1,
  two: 2,
  three: 3,
  four: 4,
  five: 5,
  six: 6,
  seven: 7,
  eight: 8,
  nine: 9,
  ten: 10,
  eleven: 11,
  twelve: 12,
}

function clausesOf(message: string): string[] {
  return message
    .toLowerCase()
    .replace(/[’‘]/g, "'")
    .replace(/\b([ap])\.m\.?/g, '$1m')
    .replace(DAY_PAIR, '$1 & $2')
    .split(/[.;!?,]|:(?!\d)|\bbut\b|\band\b/)
    .map((c) => c.trim())
    .filter(Boolean)
}

function daysIn(clause: string): Day[] {
  if (/\b(either|both|any|every)\s+days?\b|\bwhichever\b|\bwhenever\b|\bweekend\b/.test(clause)) return [...DAYS]
  const found: Day[] = []
  if (/\bsat(urday)?s?\b/.test(clause)) found.push('sat')
  if (/\bsun(day)?s?\b/.test(clause)) found.push('sun')
  return found
}

function bare(clause: string): boolean {
  return clause.replace(FILLER, '').replace(/[^a-z]/g, '') === ''
}

function verdictOf(clause: string): SampleVerdict | null {
  if (UNSURE.test(clause)) return 'unknown'
  if (FINE.test(clause)) return 'available'
  if (NEGATIVE.test(clause)) return 'unavailable'
  if (CONDITION.test(clause) || LIMIT.test(clause)) return 'conditional'
  if (POSITIVE.test(clause)) return 'available'
  return null
}

function limitOf(clause: string): SampleLimit | null {
  const match = LIMIT.exec(clause)
  if (!match) return null
  const word = match[1] ?? match[4]
  const raw = match[2] ?? match[5]
  const type = word === 'after' || word === 'from' ? 'start_after' : 'end_before'
  if (raw === 'midnight') return { type, hour: type === 'end_before' ? 23 + 59 / 60 : 0 }
  const meridiem = MERIDIEM.exec(clause.slice(match.index + match[0].length))?.[1]
  let hour: number
  if (raw === 'noon' || raw === 'midday') hour = 12
  else {
    hour = WORD_HOURS[raw] ?? Number(raw)
    if (meridiem === 'p' && hour < 12) hour += 12
    else if (meridiem === 'a' && hour === 12) hour = 0
    else if (!meridiem && hour >= 1 && hour <= 7) hour += 12
  }
  const minutes = match[3] ? Number(match[3]) : 0
  if (hour > 23 || minutes > 59) return null
  return { type, hour: hour + minutes / 60 }
}

export function readSample(message: string): SampleReading {
  const reading: SampleReading = { sat: 'unknown', sun: 'unknown', limits: {}, ask: null }
  const set = (day: Day, verdict: SampleVerdict, limit: SampleLimit | null) => {
    reading[day] = verdict
    if (verdict === 'conditional' && limit) reading.limits[day] = limit
    else delete reading.limits[day]
  }
  let pending: Day[] = []
  let previous: { verdict: SampleVerdict; limit: SampleLimit | null } | null = null
  for (const clause of clausesOf(message)) {
    const days = daysIn(clause)
    const verdict = verdictOf(clause)
    const limit = verdict === 'conditional' ? limitOf(clause) : null
    const window = CLOCK.test(clause) || verdict === 'unavailable' ? undefined : OFF_HOURS.exec(clause)?.[1]
    if (window) reading.ask = `Which hours in the ${window} work for you?`
    if (days.length > 0) {
      pending = pending.filter((d) => !days.includes(d))
      if (window) {
        for (const d of days) set(d, 'unknown', null)
        previous = { verdict: 'unknown', limit: null }
      } else if (verdict) {
        for (const d of days) set(d, verdict, limit)
        previous = { verdict, limit }
      } else if (!bare(clause)) {
        for (const d of days) set(d, 'unknown', null)
      } else if (previous) {
        for (const d of days) set(d, previous.verdict, previous.limit)
      } else {
        pending.push(...days)
      }
      continue
    }
    if (window || UNSURE.test(clause) || SOFT.test(clause)) {
      for (const d of pending) set(d, 'unknown', null)
      pending = []
      for (const d of DAYS) if (reading[d] === 'available') set(d, 'unknown', null)
      continue
    }
    if (pending.length > 0 && verdict) {
      for (const d of pending) set(d, verdict, limit)
      previous = { verdict, limit }
      pending = []
      continue
    }
    if (verdict === 'conditional') {
      for (const d of DAYS) if (reading[d] === 'available') set(d, 'conditional', limit)
    }
  }
  for (const d of pending) set(d, 'available', null)
  return reading
}
