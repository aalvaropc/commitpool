import type { Money, Workshop } from '../api'
import { formatMoney } from './money'
import { durationLabel, formatMoment, windowLabel } from './time'

export interface TermItem {
  label: string
  value: string
  kind?: 'money' | 'number' | 'text'
  span?: boolean
}

export function workshopTerms(w: Pick<Workshop, 'price' | 'minimum' | 'capacity' | 'duration_min'>): TermItem[] {
  return [
    { label: 'Price', value: formatMoney(w.price), kind: 'money' },
    { label: 'Needed to happen', value: String(w.minimum), kind: 'number' },
    { label: 'Spots', value: String(w.capacity), kind: 'number' },
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
  if (t.price) items.push({ label: 'Price', value: formatMoney(t.price), kind: 'money' })
  if (t.durationMin !== undefined) items.push({ label: 'Duration', value: durationLabel(t.durationMin), kind: 'number' })
  if (t.windowMin !== undefined) items.push({ label: 'Time to reserve', value: windowLabel(t.windowMin), kind: 'text' })
  items.push({ label: 'Needed to happen', value: String(t.minimum), kind: 'number' })
  items.push({ label: 'Spots', value: String(t.capacity), kind: 'number' })
  if (t.opensAt) items.push({ label: 'Reservations opened', value: formatMoment(t.opensAt, t.timeZone), kind: 'text' })
  if (t.closesAt) items.push({ label: t.paymentsOpen ? 'Reservations close' : 'Reservations closed', value: formatMoment(t.closesAt, t.timeZone), kind: 'text' })
  if (t.closesAbout) items.push({ label: 'Reservations would close about', value: formatMoment(t.closesAbout, t.timeZone), kind: 'text', span: true })
  return items
}
