import type { DateVerdict, Interpretation } from '../api'

export function verdictText(v: DateVerdict): string {
  switch (v.verdict) {
    case 'available':
      return 'Can come'
    case 'unavailable':
      return 'Can’t come'
    case 'conditional':
      if (v.condition.type === 'end_before') return `Only if it ends by ${v.condition.time}`
      if (v.condition.type === 'start_after') return `Only if it starts at ${v.condition.time} or later`
      return 'Only with a condition'
    default:
      return 'Not sure'
  }
}

/** Why a participant does not count for a date, from the server's blocker. */
export function blockerText(blocker: string): string {
  if (blocker === 'unavailable') return 'can’t come'
  if (blocker === 'unknown') return 'not sure'
  if (blocker === 'needs clarification') return 'message needs clarifying'
  const ends = /^ends after (\d{2}:\d{2})$/.exec(blocker)
  if (ends) return `only if it ends by ${ends[1]}`
  const starts = /^starts before (\d{2}:\d{2})$/.exec(blocker)
  if (starts) return `only if it starts at ${starts[1]} or later`
  return blocker
}

/**
 * The reading a participant saves: their verdict for each date, a condition
 * only on a conditional verdict, and their choices as the answer to every
 * open question, so the reading no longer needs clarification.
 */
export function correctedReading(base: Interpretation, perDate: DateVerdict[]): Interpretation {
  return {
    ...base,
    per_date: perDate.map((v) => ({
      ...v,
      condition: v.verdict === 'conditional' ? v.condition : { type: 'none', time: '' },
    })),
    ambiguities: [],
    needs_clarification: false,
  }
}
