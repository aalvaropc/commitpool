import type { Money, Workshop } from '../api'
import { formatMoney } from './money'
import { durationLabel, formatInZone, windowLabel } from './time'

export interface TermItem {
  label: string
  value: string
  kind?: 'money' | 'number' | 'text'
  span?: boolean
}

export function workshopTerms(w: Pick<Workshop, 'price' | 'minimum' | 'capacity' | 'duration_min'>): TermItem[] {
  return [
    { label: 'Price per seat', value: formatMoney(w.price), kind: 'money' },
    { label: 'Minimum paid seats', value: String(w.minimum), kind: 'number' },
    { label: 'Capacity', value: String(w.capacity), kind: 'number' },
    { label: 'Duration', value: durationLabel(w.duration_min), kind: 'number' },
  ]
}

export interface LockedTermsInput {
  timeZone: string
  dateLabel?: string
  price?: Money
  durationMin?: number
  windowMin?: number
  minimum: number
  capacity: number
  opensAt?: string
  closesAt?: string
  paymentsOpen?: boolean
  closesAbout?: string
}

export function lockedTerms(t: LockedTermsInput): TermItem[] {
  const items: TermItem[] = []
  if (t.dateLabel) items.push({ label: 'Date', value: t.dateLabel, kind: 'text', span: true })
  if (t.price) items.push({ label: 'Price per seat', value: formatMoney(t.price), kind: 'money' })
  if (t.durationMin !== undefined) items.push({ label: 'Duration', value: durationLabel(t.durationMin), kind: 'number' })
  if (t.windowMin !== undefined) items.push({ label: 'Payment window', value: windowLabel(t.windowMin), kind: 'text' })
  items.push({ label: 'Minimum paid seats', value: String(t.minimum), kind: 'number' })
  items.push({ label: 'Capacity', value: String(t.capacity), kind: 'number' })
  if (t.opensAt) items.push({ label: 'Opened', value: formatInZone(t.opensAt, t.timeZone), kind: 'text' })
  if (t.closesAt) items.push({ label: t.paymentsOpen ? 'Closes' : 'Closed', value: formatInZone(t.closesAt, t.timeZone), kind: 'text' })
  if (t.closesAbout) items.push({ label: 'Payments close about', value: formatInZone(t.closesAbout, t.timeZone), kind: 'text', span: true })
  return items
}
