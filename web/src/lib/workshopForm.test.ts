import { describe, expect, it } from 'vitest'
import { DATES_RULE, firstInvalid, serverField, validateWorkshop, type WorkshopForm } from './workshopForm'

const now = Date.parse('2026-10-27T12:00:00Z')
const form: WorkshopForm = {
  title: 'Wheel-throwing basics',
  description: '',
  duration: '120',
  timezone: 'America/Lima',
  minimum: '3',
  capacity: '8',
  price: '10',
  window: '1440',
  date1: '2026-11-03',
  time1: '15:00',
  date2: '2026-11-04',
  time2: '10:00',
}

describe('validateWorkshop', () => {
  it('builds both dates in the zone, ending after the duration', () => {
    const { errors, dates } = validateWorkshop(form, now)
    expect(errors).toEqual({})
    expect(dates).toEqual([
      { starts_at: '2026-11-03T15:00:00-05:00', ends_at: '2026-11-03T17:00:00-05:00' },
      { starts_at: '2026-11-04T10:00:00-05:00', ends_at: '2026-11-04T12:00:00-05:00' },
    ])
  })

  it('states the opening rule up front', () => {
    expect(DATES_RULE).toBe('Each date must start at least 24 hours after reservations close.')
  })

  it('refuses a date that starts less than 24 h after the window would close', () => {
    expect(validateWorkshop({ ...form, date1: '2026-10-29', time1: '06:59' }, now).errors.date1).toBe(DATES_RULE)
    expect(validateWorkshop({ ...form, date1: '2026-10-29', time1: '07:00' }, now).errors.date1).toBeUndefined()
    expect(validateWorkshop({ ...form, window: '15', date1: '2026-10-28', time1: '07:30' }, now).errors.date1).toBeUndefined()
  })

  it('checks the money and the seats', () => {
    const { errors } = validateWorkshop({ ...form, price: '10.005', minimum: '9', title: ' ' }, now)
    expect(Object.keys(errors).sort()).toEqual(['capacity', 'price', 'title'])
  })
})

describe('firstInvalid', () => {
  it('follows the order of the fields on the page', () => {
    expect(firstInvalid({ date2: 'x', price: 'x', capacity: 'x' })).toBe('price')
    expect(firstInvalid({ date1: 'x' })).toBe('date1')
    expect(firstInvalid({})).toBeNull()
  })
})

describe('serverField', () => {
  it('points a server refusal at the field it is about', () => {
    expect(serverField('invalid_price')).toBe('price')
    expect(serverField('minimum_above_capacity')).toBe('capacity')
    expect(serverField('invalid_dates')).toBe('date1')
    expect(serverField('internal_error')).toBeNull()
  })
})
