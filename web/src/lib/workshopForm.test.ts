import { describe, expect, it } from 'vitest'
import type { CreateWorkshopResponse, WorkshopDraft } from '../api'
import { blankForm, breakEven, DATES_RULE, dateErrors, firstInvalid, fitsBreakEven, fromDraft, inviteMessage, MAX_DATES, newSlot, nextDay, serverField, toRequest, validateWorkshop, type WorkshopForm } from './workshopForm'

const now = Date.parse('2026-10-27T12:00:00Z')
const form: WorkshopForm = {
  title: 'Life drawing for beginners',
  description: '',
  hostName: 'Iris Calder',
  ownerEmail: '',
  location: 'The Long Room · Red Hook, Brooklyn',
  duration: '120',
  timezone: 'America/New_York',
  minimum: '3',
  capacity: '6',
  price: '45',
  venueCost: '120',
  window: '1440',
  dates: [newSlot('2026-11-03', '15:00'), newSlot('2026-11-04', '10:00')],
}

const plain = (f: WorkshopForm) => f.dates.map(({ date, time }) => ({ date, time }))

const draft = (over: Partial<WorkshopDraft> = {}): WorkshopDraft => ({
  title: '',
  description: '',
  host_name: '',
  location: '',
  timezone: '',
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
    const at = (date: string, time: string, window = '1440') => {
      const slot = newSlot(date, time)
      return validateWorkshop({ ...form, window, dates: [slot] }, now).errors[`date:${slot.id}`]
    }
    expect(at('2026-10-29', '07:59')).toBe(DATES_RULE)
    expect(at('2026-10-29', '08:00')).toBeUndefined()
    expect(at('2026-10-28', '08:30', '15')).toBeUndefined()
  })

  it('checks the money and the people', () => {
    const { errors } = validateWorkshop({ ...form, price: '10.005', minimum: '9', title: ' ', venueCost: 'lots' }, now)
    expect(Object.keys(errors).sort()).toEqual(['capacity', 'price', 'title', 'venueCost'])
  })

  it('takes an optional email for the organizer', () => {
    expect(validateWorkshop({ ...form, ownerEmail: 'iris@example.com' }, now).errors).toEqual({})
    expect(validateWorkshop({ ...form, ownerEmail: 'iris' }, now).errors.ownerEmail).toBe('Write one address, such as iris@example.com, or leave it empty.')
  })

  it('lets the session cost stay empty', () => {
    expect(validateWorkshop({ ...form, venueCost: '' }, now).errors).toEqual({})
  })

  it('needs at least one date, at most ten, each once', () => {
    expect(validateWorkshop({ ...form, dates: [] }, now).errors.dates).toBe('Offer at least one date.')
    const many = Array.from({ length: MAX_DATES + 1 }, (_, i) => newSlot(`2026-11-${String(10 + i).padStart(2, '0')}`, '10:00'))
    expect(validateWorkshop({ ...form, dates: many }, now).errors.dates).toBe('Offer at most 10 dates.')
    const copy = newSlot(form.dates[0].date, form.dates[0].time)
    const twice = validateWorkshop({ ...form, dates: [form.dates[0], copy] }, now).errors
    expect(twice).toEqual({ [`date:${copy.id}`]: 'This date and time is already on the list.' })
  })
})

describe('fromDraft', () => {
  it('fills the form with what Claude read and keeps the rest', () => {
    const { form: got, missing } = fromDraft(
      draft({ title: 'Life drawing for beginners', price: '45.00', venue_cost: '120.00', capacity: 6, duration_min: 120, dates: [{ date: '2026-11-14', start: '10:00' }], missing: ['description', 'minimum'] }),
      blankForm('America/New_York', now),
    )
    expect(got.title).toBe('Life drawing for beginners')
    expect(got.price).toBe('45')
    expect(got.venueCost).toBe('120')
    expect(got.capacity).toBe('6')
    expect(got.minimum).toBe('')
    expect(plain(got)).toEqual([{ date: '2026-11-14', time: '10:00' }])
    expect(missing).toEqual(['description', 'minimum'])
  })

  it('takes the time zone of the place when Claude could tell it', () => {
    const base = blankForm('America/Lima', now)
    expect(fromDraft(draft({ timezone: 'America/New_York' }), base).form.timezone).toBe('America/New_York')
    expect(fromDraft(draft(), base).form.timezone).toBe('America/Lima')
  })

  it('keeps the suggested dates when Claude found none', () => {
    const base = blankForm('America/New_York', now)
    expect(fromDraft(draft({ missing: ['dates'] }), base).form.dates).toEqual(base.dates)
  })
})

describe('date slots', () => {
  it('keep their id while their place in the list changes', () => {
    const a = newSlot('2026-11-14', '10:00')
    const b = newSlot('2026-11-14', '10:00')
    expect(a.id).not.toBe(b.id)
  })

  it('suggest the day after the last one', () => {
    expect(nextDay('2026-11-14')).toBe('2026-11-15')
    expect(nextDay('2026-12-31')).toBe('2027-01-01')
    expect(nextDay('')).toBe('')
  })

  it('keep only the date errors when a draft opens', () => {
    expect(dateErrors({ title: 'x', 'date:d1': 'y', dates: 'z' })).toEqual({ 'date:d1': 'y' })
  })
})

describe('breakEven', () => {
  it('suggests how many people cover the session costs', () => {
    expect(breakEven('45', '120')).toEqual({ seats: 3, text: '3 × $45 = $135 covers your $120 session costs before fees' })
    expect(breakEven('40', '120')).toEqual({ seats: 3, text: '3 × $40 = $120 covers your $120 session costs before fees' })
    expect(breakEven('12.50', '100')).toEqual({ seats: 8, text: '8 × $12.50 = $100 covers your $100 session costs before fees' })
  })

  it('only suggests what the spots allow', () => {
    expect(fitsBreakEven(3, '6')).toBe(true)
    expect(fitsBreakEven(6, '4')).toBe(false)
    expect(fitsBreakEven(6, '')).toBe(true)
    expect(fitsBreakEven(501, '')).toBe(false)
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
    expect(req).toMatchObject({ title: 'Life drawing for beginners', host_name: 'Iris Calder', location: 'The Long Room · Red Hook, Brooklyn', price: '45.00', venue_cost: '120.00', currency: 'USD', minimum: 3, capacity: 6, duration_min: 120, payment_window_min: 1440 })
    expect(toRequest({ ...form, venueCost: ' ' }, []).venue_cost).toBeUndefined()
    expect(toRequest(form, []).owner_email).toBeUndefined()
    expect(toRequest({ ...form, ownerEmail: ' iris@example.com ' }, []).owner_email).toBe('iris@example.com')
  })
})

describe('inviteMessage', () => {
  it('writes a message the organizer can paste in the group chat', () => {
    const created = {
      workshop: { title: 'Life drawing for beginners', location: 'The Long Room · Red Hook, Brooklyn', price: { minor: 4500, currency: 'USD', value: '45.00' }, minimum: 3 },
      share_url: 'https://example.test/w/abc',
    } as CreateWorkshopResponse
    expect(inviteMessage(created)).toBe(
      'I’m planning Life drawing for beginners at The Long Room · Red Hook, Brooklyn. It’s $45 a spot, and it only happens if 3 people reserve; otherwise nobody pays. Tell me when you can come: https://example.test/w/abc',
    )
    expect(inviteMessage({ ...created, workshop: { ...created.workshop, minimum: 1 } })).toContain('only happens if 1 person reserves;')
    expect(inviteMessage({ ...created, workshop: { ...created.workshop, location: '' } })).toContain('I’m planning Life drawing for beginners. It’s')
  })
})

describe('firstInvalid', () => {
  it('follows the order of the fields on the page', () => {
    const [first, second] = form.dates
    expect(firstInvalid({ [`date:${first.id}`]: 'x', price: 'x', capacity: 'x' }, form.dates)).toBe('price')
    expect(firstInvalid({ [`date:${second.id}`]: 'x', [`date:${first.id}`]: 'x' }, form.dates)).toBe(`date:${first.id}`)
    expect(firstInvalid({ window: 'x', dates: 'x' }, form.dates)).toBe('dates')
    expect(firstInvalid({}, form.dates)).toBeNull()
  })
})

describe('serverField', () => {
  it('points a server refusal at the field it is about', () => {
    expect(serverField('invalid_price')).toBe('price')
    expect(serverField('invalid_venue_cost')).toBe('venueCost')
    expect(serverField('invalid_host_name')).toBe('hostName')
    expect(serverField('minimum_above_capacity')).toBe('capacity')
    expect(serverField('invalid_dates')).toBe('dates')
    expect(serverField('internal_error')).toBeNull()
  })
})
