import { countedBy, type Counted } from './chatScript'
import { withLabels, type SampleDates } from './sampleDates'
import { readSample, type Day, type SampleLimit, type SampleReading, type SampleVerdict } from './sampleReader'

export type ReadBy = 'claude' | 'rules' | 'fallback'

export type Reader = ReadBy | 'prepared'

export const READ_LABEL: Record<Reader, string> = {
  prepared: 'Prepared reading',
  claude: 'Read by Claude',
  rules: 'Simple rules read this one',
  fallback: 'Claude is busy — simple rules read this one',
}

export const LIVE_PATH = '/api/v1/sample-reading'
export const LIVE_TIMEOUT_MS = 13_000

export interface Earlier {
  message: string
  questions: string[]
}

export interface Turn {
  message: string
  earlier: Earlier | null
}

export interface LiveReading {
  by: ReadBy
  reading: SampleReading
  counted: Counted
  question: string | null
  dates: SampleDates
}

interface ServerVerdict {
  verdict: SampleVerdict
  condition: { type: string; time: string }
  counted: boolean
}

interface ServerReading {
  read_by: string
  dates: { key: string; label: string; hours: string }[]
  per_date: Record<Day, ServerVerdict>
  question: string
}

const VERDICTS: SampleVerdict[] = ['available', 'conditional', 'unavailable', 'unknown']

export function readingRequest(turn: Turn): { message: string } {
  return { message: turn.message }
}

function isVerdict(v: unknown): v is ServerVerdict {
  if (typeof v !== 'object' || v === null) return false
  const o = v as Record<string, unknown>
  return VERDICTS.includes(o.verdict as SampleVerdict) && typeof o.counted === 'boolean' && typeof o.condition === 'object' && o.condition !== null
}

function isReading(v: unknown): v is ServerReading {
  if (typeof v !== 'object' || v === null) return false
  const o = v as Record<string, unknown>
  const perDate = o.per_date as Record<string, unknown> | undefined
  return typeof o.read_by === 'string' && Array.isArray(o.dates) && typeof perDate === 'object' && perDate !== null && isVerdict(perDate.sat) && isVerdict(perDate.sun)
}

function limitOf(v: ServerVerdict): SampleLimit | null {
  const { type, time } = v.condition
  const m = /^(\d{2}):(\d{2})$/.exec(time ?? '')
  if (v.verdict !== 'conditional' || (type !== 'end_before' && type !== 'start_after') || !m) return null
  return { type, hour: Number(m[1]) + Number(m[2]) / 60 }
}

function fromServer(data: ServerReading, local: SampleDates): LiveReading {
  const question = typeof data.question === 'string' && data.question.trim() ? data.question.trim() : null
  const reading: SampleReading = { sat: data.per_date.sat.verdict, sun: data.per_date.sun.verdict, limits: {}, ask: question }
  for (const day of ['sat', 'sun'] as const) {
    const limit = limitOf(data.per_date[day])
    if (limit) reading.limits[day] = limit
  }
  return {
    by: data.read_by === 'claude' ? 'claude' : 'rules',
    reading,
    counted: { sat: data.per_date.sat.counted, sun: data.per_date.sun.counted },
    question,
    dates: withLabels(local, data.dates),
  }
}

export function fallback(message: string, local: SampleDates): LiveReading {
  const reading = readSample(message)
  return { by: 'fallback', reading, counted: countedBy(reading), question: reading.ask, dates: local }
}

async function ask(turn: Turn, local: SampleDates, signal: AbortSignal): Promise<LiveReading> {
  try {
    const res = await fetch(LIVE_PATH, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(readingRequest(turn)),
      credentials: 'same-origin',
      cache: 'no-store',
      signal,
    })
    if (res.status !== 200) return fallback(turn.message, local)
    const data: unknown = await res.json()
    return isReading(data) ? fromServer(data, local) : fallback(turn.message, local)
  } catch {
    return fallback(turn.message, local)
  }
}

export async function readLive(turn: Turn, local: SampleDates, timeoutMs = LIVE_TIMEOUT_MS): Promise<LiveReading> {
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  const late = new Promise<LiveReading>((done) => {
    timer = setTimeout(() => {
      controller.abort()
      done(fallback(turn.message, local))
    }, timeoutMs)
  })
  try {
    return await Promise.race([ask(turn, local, controller.signal), late])
  } finally {
    clearTimeout(timer)
  }
}
