import { countedBy, type Counted } from './chatScript'
import { readSample, type Day, type SampleLimit, type SampleReading, type SampleVerdict } from './sampleReader'

export type ReadBy = 'claude' | 'rules' | 'fallback'

export type Reader = ReadBy | 'prepared'

export const READ_LABEL: Record<Reader, string> = {
  prepared: 'Prepared reading',
  claude: 'Read by Claude',
  rules: 'Simple rules read this one',
  fallback: 'Simple rules read this one — Claude wasn’t available',
}

const FOOTNOTE_READING: Record<ReadBy | 'pending', string> = {
  pending: 'The five answers are prepared; yours goes to Claude to read live.',
  claude: 'The five answers are prepared; Claude read yours live.',
  rules: 'The five answers are prepared; simple rules on the server read yours.',
  fallback: 'The five answers are prepared; Claude wasn’t available, so simple rules in your browser read yours.',
}

export function footnote(last: ReadBy | null): string {
  return `${FOOTNOTE_READING[last ?? 'pending']} Payments in this chat are simulated.`
}

export const LIVE_PATH = '/api/v1/sample-reading'
export const LIVE_TIMEOUT_MS = 13_000

export interface Earlier {
  message: string
  questions: string[]
  question_sigs: string[]
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
  questionSig: string
}

interface ServerVerdict {
  verdict: SampleVerdict
  condition: { type: string; time: string }
  counted: boolean
}

interface ServerReading {
  read_by: string
  per_date: Record<Day, ServerVerdict>
  question: string
  question_sig?: string
}

const VERDICTS: SampleVerdict[] = ['available', 'conditional', 'unavailable', 'unknown']

export function readingRequest(turn: Turn): { message: string; earlier?: Earlier } {
  return turn.earlier ? { message: turn.message, earlier: turn.earlier } : { message: turn.message }
}

export function earlierFrom(message: string, read: LiveReading): Earlier {
  return { message, questions: read.question ? [read.question] : [], question_sigs: read.question && read.questionSig ? [read.questionSig] : [] }
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
  return typeof o.read_by === 'string' && typeof perDate === 'object' && perDate !== null && isVerdict(perDate.sat) && isVerdict(perDate.sun)
}

function limitOf(v: ServerVerdict): SampleLimit | null {
  const { type, time } = v.condition
  const m = /^(\d{2}):(\d{2})$/.exec(time ?? '')
  if (v.verdict !== 'conditional' || (type !== 'end_before' && type !== 'start_after') || !m) return null
  return { type, hour: Number(m[1]) + Number(m[2]) / 60 }
}

function fromServer(data: ServerReading): LiveReading {
  const question = typeof data.question === 'string' && data.question.trim() ? data.question : null
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
    questionSig: question && typeof data.question_sig === 'string' ? data.question_sig : '',
  }
}

export function fallback(message: string): LiveReading {
  const reading = readSample(message)
  return { by: 'fallback', reading, counted: countedBy(reading), question: reading.ask, questionSig: '' }
}

async function ask(turn: Turn, signal: AbortSignal): Promise<LiveReading> {
  try {
    const res = await fetch(LIVE_PATH, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(readingRequest(turn)),
      credentials: 'same-origin',
      cache: 'no-store',
      signal,
    })
    if (res.status !== 200) return fallback(turn.message)
    const data: unknown = await res.json()
    return isReading(data) ? fromServer(data) : fallback(turn.message)
  } catch {
    return fallback(turn.message)
  }
}

export async function readLive(turn: Turn, timeoutMs = LIVE_TIMEOUT_MS): Promise<LiveReading> {
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  const late = new Promise<LiveReading>((done) => {
    timer = setTimeout(() => {
      controller.abort()
      done(fallback(turn.message))
    }, timeoutMs)
  })
  try {
    return await Promise.race([ask(turn, controller.signal), late])
  } finally {
    clearTimeout(timer)
  }
}
