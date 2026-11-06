import type { CandidateDate, Group } from '../api'
import { formatClock, formatDateRange, formatDay, formatTimeRange } from './time'

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

export function groupSummary(group: Group, needed: number): { count: number | null; text: string } {
  if (group.answered === 0) return { count: null, text: `Be the first to say when you can come. ${needed} needed to happen.` }
  return { count: group.answered, text: `${group.answered === 1 ? 'person' : 'people'} answered so far. ${needed} needed to happen.` }
}

export function dateNames(dates: CandidateDate[], timeZone: string): Map<string, string> {
  const days = dates.map((d) => formatDay(d.starts_at, timeZone))
  return new Map(dates.map((d, i) => [d.id, days.filter((x) => x === days[i]).length > 1 ? `${days[i]} at ${formatClock(d.starts_at, timeZone)}` : days[i]]))
}
