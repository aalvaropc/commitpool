import type { CandidateDate, Group } from '../api'
import { count } from './plural'
import { formatDateRange, formatDay, formatTimeRange } from './time'

export interface DateRow {
  id: string
  day: string
  time: string
  label: string
  canCome: number
  best: boolean
  chosen: boolean
}

export function dateRows(dates: CandidateDate[], group: Group, chosenId: string | null, timeZone: string): DateRow[] {
  const shown = chosenId ? dates.filter((d) => d.id === chosenId) : dates
  const most = Math.max(0, ...shown.map((d) => group.can_come[d.id] ?? 0))
  const bestId = !chosenId && group.answered > 0 && most > 0 ? shown.find((d) => (group.can_come[d.id] ?? 0) === most)?.id : undefined
  return shown.map((d) => ({
    id: d.id,
    day: formatDay(d.starts_at, timeZone),
    time: formatTimeRange(d.starts_at, d.ends_at, timeZone),
    label: formatDateRange(d.starts_at, d.ends_at, timeZone),
    canCome: group.can_come[d.id] ?? 0,
    best: d.id === bestId,
    chosen: d.id === chosenId,
  }))
}

export function groupLine(group: Group, needed: number): string {
  const head = group.answered === 0 ? 'Be the first to say when you can come.' : `${count(group.answered, 'person', 'people')} answered so far.`
  return `${head} ${needed} needed to happen.`
}
