import { describe, expect, it } from 'vitest'
import { earlier, formatInZone, timeLeft, windowLabel, zonedPlusMinutes, zonedToRFC3339 } from './time'

describe('zonedToRFC3339', () => {
  it('reads a wall-clock time in the workshop’s zone', () => {
    expect(zonedToRFC3339('2026-11-14', '15:00', 'America/Lima')).toBe('2026-11-14T15:00:00-05:00')
    expect(zonedToRFC3339('2026-11-14', '15:00', 'Asia/Kolkata')).toBe('2026-11-14T15:00:00+05:30')
    expect(zonedToRFC3339('2026-11-14', '15:00', 'UTC')).toBe('2026-11-14T15:00:00Z')
  })

  it('follows daylight saving time', () => {
    expect(zonedToRFC3339('2026-07-01', '09:30', 'Europe/Madrid')).toBe('2026-07-01T09:30:00+02:00')
    expect(zonedToRFC3339('2026-12-01', '09:30', 'Europe/Madrid')).toBe('2026-12-01T09:30:00+01:00')
  })

  it('moves a time skipped by a daylight saving change one hour away', () => {
    expect(zonedToRFC3339('2026-03-08', '02:30', 'America/New_York')).toBe('2026-03-08T01:30:00-05:00')
    expect(zonedToRFC3339('2026-03-29', '02:30', 'Europe/Madrid')).toBe('2026-03-29T03:30:00+02:00')
  })

  it('refuses a malformed date or time', () => {
    expect(() => zonedToRFC3339('2026-13-01', '09:30', 'UTC')).toThrow()
    expect(() => zonedToRFC3339('2026-12-01', '9:3', 'UTC')).toThrow()
  })
})

describe('zonedPlusMinutes', () => {
  it('adds a duration to an instant and writes it in the zone', () => {
    expect(zonedPlusMinutes('2026-11-14T15:00:00-05:00', 120, 'America/Lima')).toBe('2026-11-14T17:00:00-05:00')
  })

  it('crosses a daylight saving change by elapsed time', () => {
    expect(zonedPlusMinutes('2026-10-25T01:30:00+02:00', 120, 'Europe/Madrid')).toBe('2026-10-25T02:30:00+01:00')
  })
})

describe('formatInZone', () => {
  it('names the zone after the local date and time', () => {
    expect(formatInZone('2026-11-14T20:00:00Z', 'America/Lima')).toBe('Sat 14 Nov 2026, 15:00 America/Lima')
  })
})

describe('timeLeft', () => {
  const now = Date.parse('2026-10-27T12:00:00Z')

  it('counts hours and minutes down to a deadline', () => {
    expect(timeLeft('2026-10-28T11:40:00Z', now)).toBe('23 h 40 min')
    expect(timeLeft('2026-10-27T12:04:30Z', now)).toBe('4 min')
    expect(timeLeft('2026-10-27T12:00:20Z', now)).toBe('less than a minute')
    expect(timeLeft('2026-10-30T13:00:00Z', now)).toBe('3 d 1 h')
  })

  it('is null once the deadline has passed', () => {
    expect(timeLeft('2026-10-27T11:59:59Z', now)).toBeNull()
  })
})

describe('windowLabel', () => {
  it('labels a window shorter than a day as a demo window', () => {
    expect(windowLabel(15)).toBe('Demo window: 15 minutes')
    expect(windowLabel(1)).toBe('Demo window: 1 minute')
    expect(windowLabel(1440)).toBe('24 hours')
  })
})

describe('earlier', () => {
  it('picks the instant that comes first', () => {
    expect(earlier('2026-10-27T21:12:00Z', '2026-10-27T20:57:00Z')).toBe('2026-10-27T20:57:00Z')
    expect(earlier('2026-10-27T20:00:00-05:00', '2026-10-28T02:00:00Z')).toBe('2026-10-27T20:00:00-05:00')
  })
})
