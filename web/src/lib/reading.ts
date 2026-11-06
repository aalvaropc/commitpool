import type { DateVerdict, Interpretation } from '../api'
import { clock12 } from './time'

export function verdictText(v: DateVerdict): string {
  switch (v.verdict) {
    case 'available':
      return 'Can come'
    case 'unavailable':
      return 'Can’t come'
    case 'conditional':
      if (v.condition.type === 'end_before') return `Only if it ends by ${clock12(v.condition.time)}`
      if (v.condition.type === 'start_after') return `Only if it starts at ${clock12(v.condition.time)} or later`
      return 'Only with a condition'
    default:
      return 'Not sure'
  }
}

export function blockerText(blocker: string): string {
  if (blocker === 'unavailable') return 'can’t come'
  if (blocker === 'unknown') return 'not sure'
  if (blocker === 'needs clarification') return 'message needs clarifying'
  const ends = /^ends after (\d{2}:\d{2})$/.exec(blocker)
  if (ends) return `only if it ends by ${clock12(ends[1])}`
  const starts = /^starts before (\d{2}:\d{2})$/.exec(blocker)
  if (starts) return `only if it starts at ${clock12(starts[1])} or later`
  return blocker
}

export function saveButton(dirty: boolean, needsClarification: boolean): { enabled: boolean; label: string } {
  if (dirty) return { enabled: true, label: 'Save corrections' }
  if (needsClarification) return { enabled: true, label: 'Confirm reading' }
  return { enabled: false, label: 'Save corrections' }
}

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
