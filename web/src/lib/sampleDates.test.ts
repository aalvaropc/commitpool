import { describe, expect, it } from 'vitest'
import { CLASS_HOURS_WORDS, clockWords, sampleDates, withLabels } from './sampleDates'

const labels = (now: string) => {
  const d = sampleDates(new Date(now))
  return [d.sat.label, d.sun.label]
}

describe('sampleDates', () => {
  it('picks the Saturday at least four days out, like the demo on the server', () => {
    expect(labels('2026-11-02T09:00:00Z')).toEqual(['Sat, Nov 7', 'Sun, Nov 8'])
  })

  it('keeps a Saturday whose 10 AM is still ahead of the earliest moment', () => {
    expect(labels('2026-10-27T13:30:00Z')).toEqual(['Sat, Oct 31', 'Sun, Nov 1'])
  })

  it('skips a Saturday whose 10 AM has passed on New York’s daylight clock', () => {
    expect(labels('2026-10-27T14:30:00Z')).toEqual(['Sat, Nov 7', 'Sun, Nov 8'])
  })

  it('reads the earliest moment on standard time once daylight time ends on 1 Nov 2026', () => {
    expect(labels('2026-11-03T14:30:00Z')).toEqual(['Sat, Nov 7', 'Sun, Nov 8'])
    expect(labels('2026-11-03T15:30:00Z')).toEqual(['Sat, Nov 14', 'Sun, Nov 15'])
  })

  it('crosses into the next month', () => {
    expect(labels('2026-11-26T16:00:00Z')).toEqual(['Sat, Dec 5', 'Sun, Dec 6'])
    expect(labels('2027-01-25T12:00:00Z')).toEqual(['Sat, Jan 30', 'Sun, Jan 31'])
    expect(labels('2027-01-26T16:00:00Z')).toEqual(['Sat, Feb 6', 'Sun, Feb 7'])
  })

  it('stays in the future during judging in December', () => {
    expect(labels('2026-12-01T12:00:00Z')).toEqual(['Sat, Dec 5', 'Sun, Dec 6'])
    expect(labels('2026-12-15T23:00:00Z')).toEqual(['Sat, Dec 26', 'Sun, Dec 27'])
  })

  it('names the days and the class hours', () => {
    const d = sampleDates(new Date('2026-11-02T09:00:00Z'))
    expect(d.sat).toEqual({ key: 'sat', weekday: 'Saturday', label: 'Sat, Nov 7', hours: '10 AM–12 PM' })
    expect(d.sun.weekday).toBe('Sunday')
    expect(CLASS_HOURS_WORDS).toBe('10 AM–12 PM')
  })
})

describe('clockWords', () => {
  it.each([
    [0, '12 AM'],
    [600, '10 AM'],
    [720, '12 PM'],
    [810, '1:30 PM'],
  ])('writes %i minutes as %s', (minutes, words) => {
    expect(clockWords(minutes)).toBe(words)
  })
})

describe('withLabels', () => {
  it('takes the labels the server returned and ignores unknown keys', () => {
    const local = sampleDates(new Date('2026-11-02T09:00:00Z'))
    const next = withLabels(local, [
      { key: 'sat', label: 'Sat, Nov 14', hours: '10 AM–12 PM' },
      { key: 'mon', label: 'Mon, Nov 16', hours: '9 AM–11 AM' },
    ])
    expect(next.sat.label).toBe('Sat, Nov 14')
    expect(next.sun).toEqual(local.sun)
    expect(Object.keys(next)).toEqual(['sat', 'sun'])
  })
})
