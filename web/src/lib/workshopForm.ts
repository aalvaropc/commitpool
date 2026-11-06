import type { CreateWorkshopRequest, CreateWorkshopResponse, DraftField, WorkshopDraft } from '../api'
import { formatMoney } from './money'
import { zonedPlusMinutes, zonedToRFC3339 } from './time'

export interface DateSlot {
  date: string
  time: string
}

export interface WorkshopForm {
  title: string
  description: string
  hostName: string
  location: string
  duration: string
  timezone: string
  minimum: string
  capacity: string
  price: string
  venueCost: string
  window: string
  dates: DateSlot[]
}

export type TextField = Exclude<keyof WorkshopForm, 'dates'>
export type FieldKey = TextField | 'dates' | `date${number}`
export type FormErrors = Partial<Record<FieldKey | 'form', string>>
export type StartsAndEnds = { starts_at: string; ends_at: string }[]

export const MAX_DATES = 10
export const DATES_RULE = 'Each date must start at least 24 hours after reservations close.'

const AMOUNT = /^\d{1,7}(\.\d{1,2})?$/
const DAY_MS = 86400000

const ORDER: FieldKey[] = ['title', 'description', 'hostName', 'location', 'price', 'venueCost', 'minimum', 'capacity', 'duration', 'timezone', 'dates']

const DRAFT_FIELDS: Record<DraftField, FieldKey> = {
  title: 'title',
  description: 'description',
  host_name: 'hostName',
  location: 'location',
  duration_min: 'duration',
  price: 'price',
  venue_cost: 'venueCost',
  minimum: 'minimum',
  capacity: 'capacity',
  dates: 'dates',
}

const SERVER_FIELDS: Record<string, FieldKey> = {
  invalid_title: 'title',
  invalid_description: 'description',
  invalid_host_name: 'hostName',
  invalid_location: 'location',
  invalid_duration: 'duration',
  invalid_timezone: 'timezone',
  invalid_minimum: 'minimum',
  invalid_capacity: 'capacity',
  minimum_above_capacity: 'capacity',
  invalid_price: 'price',
  unsupported_currency: 'price',
  invalid_venue_cost: 'venueCost',
  invalid_payment_window: 'window',
  invalid_dates: 'date0',
}

function isoDay(now: number, offsetDays: number): string {
  const d = new Date(now + offsetDays * DAY_MS)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function blankForm(timezone: string, now: number): WorkshopForm {
  return {
    title: '',
    description: '',
    hostName: '',
    location: '',
    duration: '120',
    timezone,
    minimum: '',
    capacity: '',
    price: '',
    venueCost: '',
    window: '1440',
    dates: [
      { date: isoDay(now, 7), time: '10:00' },
      { date: isoDay(now, 8), time: '10:00' },
    ],
  }
}

const plainAmount = (v: string) => v.replace(/\.00$/, '')
const count = (n: number, fallback: string) => (n > 0 ? String(n) : fallback)

export function fromDraft(draft: WorkshopDraft, base: WorkshopForm): { form: WorkshopForm; missing: FieldKey[] } {
  const form: WorkshopForm = {
    ...base,
    title: draft.title || base.title,
    description: draft.description || base.description,
    hostName: draft.host_name || base.hostName,
    location: draft.location || base.location,
    duration: count(draft.duration_min, base.duration),
    price: draft.price ? plainAmount(draft.price) : base.price,
    venueCost: draft.venue_cost ? plainAmount(draft.venue_cost) : base.venueCost,
    minimum: count(draft.minimum, base.minimum),
    capacity: count(draft.capacity, base.capacity),
    dates: draft.dates.length ? draft.dates.map((d) => ({ date: d.date, time: d.start })) : base.dates,
  }
  return { form, missing: draft.missing.map((f) => DRAFT_FIELDS[f]).filter(Boolean) }
}

const cents = (v: string) => Math.round(Number(v.trim()) * 100)
const usd = (minor: number) => formatMoney({ minor, currency: 'USD' })

export function breakEven(price: string, venueCost: string): { seats: number; text: string } | null {
  if (!AMOUNT.test(price.trim()) || !AMOUNT.test(venueCost.trim())) return null
  const p = cents(price)
  const v = cents(venueCost)
  if (p <= 0 || v <= 0) return null
  const seats = Math.ceil(v / p)
  return { seats, text: `${seats} × ${usd(p)} = ${usd(seats * p)} covers your ${usd(v)} studio` }
}

export function firstInvalid(errors: FormErrors): FieldKey | null {
  for (const key of ORDER) {
    if (errors[key]) return key
    if (key === 'dates') {
      for (let i = 0; i < MAX_DATES + 1; i++) if (errors[`date${i}`]) return `date${i}`
    }
  }
  return errors.window ? 'window' : null
}

export function serverField(code: string): FieldKey | null {
  return SERVER_FIELDS[code] ?? null
}

export function validateWorkshop(f: WorkshopForm, now: number): { errors: FormErrors; dates?: StartsAndEnds } {
  const errors: FormErrors = {}
  if (!f.title.trim()) errors.title = 'Give the workshop a title.'
  const duration = Number(f.duration)
  if (!Number.isInteger(duration) || duration < 1 || duration > 1440) errors.duration = 'Use whole minutes between 1 and 1440.'
  const minimum = Number(f.minimum)
  const capacity = Number(f.capacity)
  if (!f.minimum.trim() || !Number.isInteger(minimum) || minimum < 1) errors.minimum = 'At least 1 person must be needed.'
  if (!f.capacity.trim() || !Number.isInteger(capacity) || capacity < 1 || capacity > 500) errors.capacity = 'Offer between 1 and 500 spots.'
  else if (!errors.minimum && minimum > capacity) errors.capacity = 'Offer at least as many spots as people needed.'
  if (!AMOUNT.test(f.price.trim()) || Number(f.price) <= 0) errors.price = 'Write an amount above zero, such as 45 or 45.50.'
  if (f.venueCost.trim() && !AMOUNT.test(f.venueCost.trim())) errors.venueCost = 'Write an amount such as 120, or leave it empty.'

  if (f.dates.length === 0) errors.dates = 'Offer at least one date.'
  else if (f.dates.length > MAX_DATES) errors.dates = `Offer at most ${MAX_DATES} dates.`

  const earliest = now + Number(f.window) * 60000 + DAY_MS
  const dates: StartsAndEnds = []
  const seen = new Set<string>()
  f.dates.forEach((slot, i) => {
    const key = `date${i}` as const
    let starts: string
    try {
      starts = zonedToRFC3339(slot.date, slot.time, f.timezone)
    } catch {
      errors[key] = 'Pick a day and a start time.'
      return
    }
    if (Date.parse(starts) < earliest) {
      errors[key] = DATES_RULE
      return
    }
    if (seen.has(starts)) {
      errors[key] = 'This date and time is already on the list.'
      return
    }
    seen.add(starts)
    dates.push({ starts_at: starts, ends_at: Number.isInteger(duration) ? zonedPlusMinutes(starts, duration, f.timezone) : starts })
  })
  return Object.keys(errors).length ? { errors } : { errors, dates }
}

const wholeCents = (v: string) => (v.includes('.') ? v.padEnd(v.indexOf('.') + 3, '0') : `${v}.00`)

export function toRequest(f: WorkshopForm, dates: StartsAndEnds): CreateWorkshopRequest {
  const venue = f.venueCost.trim()
  return {
    title: f.title.trim(),
    description: f.description.trim(),
    host_name: f.hostName.trim(),
    location: f.location.trim(),
    duration_min: Number(f.duration),
    timezone: f.timezone,
    minimum: Number(f.minimum),
    capacity: Number(f.capacity),
    price: wholeCents(f.price.trim()),
    venue_cost: venue ? wholeCents(venue) : undefined,
    currency: 'USD',
    payment_window_min: Number(f.window),
    dates,
  }
}

export function inviteMessage(created: Pick<CreateWorkshopResponse, 'workshop' | 'share_url'>): string {
  const w = created.workshop
  const where = w.location ? ` at ${w.location}` : ''
  return `I’m planning ${w.title}${where}. It’s ${formatMoney(w.price)} a spot, and it only happens if ${w.minimum} of us reserve; otherwise nobody pays. Tell me when you can come: ${created.share_url}`
}
