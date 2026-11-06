import { describe, expect, it } from 'vitest'
import type { CreateWorkshopResponse, WorkshopDraft } from '../api'
import { blankForm, breakEven, DATES_RULE, firstInvalid, fromDraft, inviteMessage, MAX_DATES, serverField, toRequest, validateWorkshop, type WorkshopForm } from './workshopForm'

const now = Date.parse('2026-10-27T12:00:00Z')
const form: WorkshopForm = {
  title: 'Wheel-throwing for beginners',
  description: '',
  hostName: 'Maya Okafor',
  location: 'Maya’s studio · Greenpoint, Brooklyn',
  duration: '120',
  timezone: 'America/New_York',
  minimum: '3',
  capacity: '6',
  price: '45',
  venueCost: '120',
  window: '1440',
  dates: [
    { date: '2026-11-03', time: '15:00' },
    { date: '2026-11-04', time: '10:00' },
  ],
}

const draft = (over: Partial<WorkshopDraft> = {}): WorkshopDraft => ({
  title: '',
  description: '',
  host_name: '',
  location: '',
  duration_min: 0,
  price: '',
  venue_cost: '',
  minimum: 0,
  capacity: 0,
  dates: [],
  missing: [],
  ...over,
})

describe('validateWorkshop', () => {
  it('builds every date in the zone, ending after the duration', () => {
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
    const at = (date: string, time: string, window = '1440') => validateWorkshop({ ...form, window, dates: [{ date, time }] }, now).errors.date0
    expect(at('2026-10-29', '07:59')).toBe(DATES_RULE)
    expect(at('2026-10-29', '08:00')).toBeUndefined()
    expect(at('2026-10-28', '08:30', '15')).toBeUndefined()
  })

  it('checks the money and the people', () => {
    const { errors } = validateWorkshop({ ...form, price: '10.005', minimum: '9', title: ' ', venueCost: 'lots' }, now)
    expect(Object.keys(errors).sort()).toEqual(['capacity', 'price', 'title', 'venueCost'])
  })

  it('lets the studio cost stay empty', () => {
    expect(validateWorkshop({ ...form, venueCost: '' }, now).errors).toEqual({})
  })

  it('needs at least one date, at most ten, each once', () => {
    expect(validateWorkshop({ ...form, dates: [] }, now).errors.dates).toBe('Offer at least one date.')
    const many = Array.from({ length: MAX_DATES + 1 }, (_, i) => ({ date: `2026-11-${String(10 + i).padStart(2, '0')}`, time: '10:00' }))
    expect(validateWorkshop({ ...form, dates: many }, now).errors.dates).toBe('Offer at most 10 dates.')
    const twice = validateWorkshop({ ...form, dates: [form.dates[0], form.dates[0]] }, now).errors
    expect(twice).toEqual({ date1: 'This date and time is already on the list.' })
  })
})

describe('fromDraft', () => {
  it('fills the form with what Claude read and keeps the rest', () => {
    const { form: got, missing } = fromDraft(
      draft({ title: 'Wheel-throwing for beginners', price: '45.00', venue_cost: '120.00', capacity: 6, duration_min: 120, dates: [{ date: '2026-11-14', start: '10:00' }], missing: ['description', 'minimum'] }),
      blankForm('America/New_York', now),
    )
    expect(got.title).toBe('Wheel-throwing for beginners')
    expect(got.price).toBe('45')
    expect(got.venueCost).toBe('120')
    expect(got.capacity).toBe('6')
    expect(got.minimum).toBe('')
    expect(got.dates).toEqual([{ date: '2026-11-14', time: '10:00' }])
    expect(missing).toEqual(['description', 'minimum'])
  })

  it('keeps the suggested dates when Claude found none', () => {
    const base = blankForm('America/New_York', now)
    expect(fromDraft(draft({ missing: ['dates'] }), base).form.dates).toEqual(base.dates)
  })
})

describe('breakEven', () => {
  it('suggests how many people cover the studio', () => {
    expect(breakEven('45', '120')).toEqual({ seats: 3, text: '3 × $45 = $135 covers your $120 studio' })
    expect(breakEven('40', '120')).toEqual({ seats: 3, text: '3 × $40 = $120 covers your $120 studio' })
    expect(breakEven('12.50', '100')).toEqual({ seats: 8, text: '8 × $12.50 = $100 covers your $100 studio' })
  })

  it('stays quiet without both amounts', () => {
    expect(breakEven('45', '')).toBeNull()
    expect(breakEven('', '120')).toBeNull()
    expect(breakEven('45', '0')).toBeNull()
  })
})

describe('toRequest', () => {
  it('sends the new fields and whole cents', () => {
    const { dates } = validateWorkshop(form, now)
    const req = toRequest(form, dates ?? [])
    expect(req).toMatchObject({ title: 'Wheel-throwing for beginners', host_name: 'Maya Okafor', location: 'Maya’s studio · Greenpoint, Brooklyn', price: '45.00', venue_cost: '120.00', currency: 'USD', minimum: 3, capacity: 6, duration_min: 120, payment_window_min: 1440 })
    expect(toRequest({ ...form, venueCost: ' ' }, []).venue_cost).toBeUndefined()
  })
})

describe('inviteMessage', () => {
  it('writes a message the organizer can paste in the group chat', () => {
    const created = {
      workshop: { title: 'Wheel-throwing for beginners', location: 'Maya’s studio · Greenpoint, Brooklyn', price: { minor: 4500, currency: 'USD', value: '45.00' }, minimum: 3 },
      share_url: 'https://example.test/w/abc',
    } as CreateWorkshopResponse
    expect(inviteMessage(created)).toBe(
      'I’m planning Wheel-throwing for beginners at Maya’s studio · Greenpoint, Brooklyn. It’s $45 a spot, and it only happens if 3 of us reserve; otherwise nobody pays. Tell me when you can come: https://example.test/w/abc',
    )
    expect(inviteMessage({ ...created, workshop: { ...created.workshop, location: '' } })).toContain('I’m planning Wheel-throwing for beginners. It’s')
  })
})

describe('firstInvalid', () => {
  it('follows the order of the fields on the page', () => {
    expect(firstInvalid({ date1: 'x', price: 'x', capacity: 'x' })).toBe('price')
    expect(firstInvalid({ date3: 'x', date1: 'x' })).toBe('date1')
    expect(firstInvalid({ window: 'x', dates: 'x' })).toBe('dates')
    expect(firstInvalid({})).toBeNull()
  })
})

describe('serverField', () => {
  it('points a server refusal at the field it is about', () => {
    expect(serverField('invalid_price')).toBe('price')
    expect(serverField('invalid_venue_cost')).toBe('venueCost')
    expect(serverField('invalid_host_name')).toBe('hostName')
    expect(serverField('minimum_above_capacity')).toBe('capacity')
    expect(serverField('invalid_dates')).toBe('date0')
    expect(serverField('internal_error')).toBeNull()
  })
})
