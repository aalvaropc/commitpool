import { zonedPlusMinutes, zonedToRFC3339 } from './time'

export interface WorkshopForm {
  title: string
  description: string
  duration: string
  timezone: string
  minimum: string
  capacity: string
  price: string
  window: string
  date1: string
  time1: string
  date2: string
  time2: string
}

export type FormErrors = Partial<Record<keyof WorkshopForm | 'form', string>>

export const DATES_RULE = 'Each date must start at least 24 hours after reservations close.'

const PRICE = /^\d{1,7}(\.\d{1,2})?$/
const DAY_MS = 86400000

const ORDER: (keyof WorkshopForm)[] = ['title', 'description', 'duration', 'price', 'minimum', 'capacity', 'window', 'timezone', 'date1', 'time1', 'date2', 'time2']

export function firstInvalid(errors: FormErrors): keyof WorkshopForm | null {
  return ORDER.find((k) => errors[k]) ?? null
}

const SERVER_FIELDS: Record<string, keyof WorkshopForm> = {
  invalid_title: 'title',
  invalid_description: 'description',
  invalid_duration: 'duration',
  invalid_timezone: 'timezone',
  invalid_minimum: 'minimum',
  invalid_capacity: 'capacity',
  minimum_above_capacity: 'capacity',
  invalid_price: 'price',
  unsupported_currency: 'price',
  invalid_payment_window: 'window',
  invalid_dates: 'date1',
}

export function serverField(code: string): keyof WorkshopForm | null {
  return SERVER_FIELDS[code] ?? null
}

export function validateWorkshop(f: WorkshopForm, now: number): { errors: FormErrors; dates?: { starts_at: string; ends_at: string }[] } {
  const errors: FormErrors = {}
  if (!f.title.trim()) errors.title = 'Give the workshop a title.'
  const duration = Number(f.duration)
  if (!Number.isInteger(duration) || duration < 1 || duration > 1440) errors.duration = 'Use whole minutes between 1 and 1440.'
  const minimum = Number(f.minimum)
  const capacity = Number(f.capacity)
  if (!Number.isInteger(minimum) || minimum < 1) errors.minimum = 'At least 1 person must be needed.'
  if (!Number.isInteger(capacity) || capacity < 1 || capacity > 500) errors.capacity = 'Offer between 1 and 500 spots.'
  else if (!errors.minimum && minimum > capacity) errors.capacity = 'Offer at least as many spots as people needed.'
  if (!PRICE.test(f.price.trim()) || Number(f.price) <= 0) errors.price = 'Write an amount above zero such as 10 or 10.00.'

  const earliest = now + Number(f.window) * 60000 + DAY_MS
  const dates: { starts_at: string; ends_at: string }[] = []
  for (const [d, t, key] of [
    [f.date1, f.time1, 'date1'],
    [f.date2, f.time2, 'date2'],
  ] as const) {
    let starts: string
    try {
      starts = zonedToRFC3339(d, t, f.timezone)
    } catch {
      errors[key] = 'Pick a date and a start time.'
      continue
    }
    if (Date.parse(starts) < earliest) {
      errors[key] = DATES_RULE
      continue
    }
    dates.push({ starts_at: starts, ends_at: Number.isInteger(duration) ? zonedPlusMinutes(starts, duration, f.timezone) : starts })
  }
  if (dates.length === 2 && dates[0].starts_at === dates[1].starts_at) errors.date2 = 'Offer two different dates.'
  return Object.keys(errors).length ? { errors } : { errors, dates }
}
