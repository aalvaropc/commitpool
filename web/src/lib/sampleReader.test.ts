import { describe, expect, it } from 'vitest'
import { readSample } from './sampleReader'

describe('readSample', () => {
  it('reads a yes for one day and a no for the other', () => {
    expect(readSample('Saturday works for me, Sunday I can’t.')).toEqual({ sat: 'available', sun: 'unavailable', limits: {}, ask: null })
  })

  it('lets a later condition on one day override "either day" and keeps its time', () => {
    expect(readSample('either day, but Sat only if we finish before six')).toEqual({
      sat: 'conditional',
      sun: 'available',
      limits: { sat: { type: 'end_before', hour: 18 } },
      ask: null,
    })
  })

  it('reads a no and a specific yes in one sentence', () => {
    expect(readSample('I can’t do Saturday; Sunday morning is perfect.')).toEqual({ sat: 'unavailable', sun: 'available', limits: {}, ask: null })
  })

  it('reads "no good" as a no', () => {
    expect(readSample('Count me in on Saturday. Sunday is no good for me.')).toEqual({ sat: 'available', sun: 'unavailable', limits: {}, ask: null })
  })

  it('reads "both days" as a yes for both', () => {
    expect(readSample('Both days work for me.')).toEqual({ sat: 'available', sun: 'available', limits: {}, ask: null })
  })

  it('does not guess when someone is vague, and asks which hours', () => {
    expect(readSample('maybe? ideally in the afternoon')).toEqual({ sat: 'unknown', sun: 'unknown', limits: {}, ask: 'Which hours in the afternoon work for you?' })
  })

  it('downgrades a yes that comes with a vague time of day', () => {
    expect(readSample('I can make it either day, ideally in the afternoon.')).toEqual({
      sat: 'unknown',
      sun: 'unknown',
      limits: {},
      ask: 'Which hours in the afternoon work for you?',
    })
  })

  it('stays unknown when no day is named', () => {
    expect(readSample('sounds fun!')).toEqual({ sat: 'unknown', sun: 'unknown', limits: {}, ask: null })
  })

  it.each([
    ['Saturday doesn’t work, Sunday is fine', 'unavailable', 'available'],
    ['Neither Saturday nor Sunday works', 'unavailable', 'unavailable'],
    ['I can’t do Saturday and Sunday', 'unavailable', 'unavailable'],
    ['Sunday, I can’t', 'unknown', 'unavailable'],
    ['Saturday works and Sunday too', 'available', 'available'],
    ['Saturday is a no for me', 'unavailable', 'unknown'],
    ['Saturdays work', 'available', 'unknown'],
    ['the weekend works', 'available', 'available'],
    ['I’m not busy Saturday', 'available', 'unknown'],
    ['Saturday, no problem', 'available', 'unknown'],
    ['not sure about Sunday', 'unknown', 'unknown'],
    ['Saturday maybe', 'unknown', 'unknown'],
    ['maybe Saturday', 'unknown', 'unknown'],
    ['Saturday works out great', 'available', 'unknown'],
    ['I don’t know about Saturday yet', 'unknown', 'unknown'],
    ['Saturday!', 'available', 'unknown'],
  ] as const)('reads %j as Sat %s, Sun %s', (message, sat, sun) => {
    const reading = readSample(message)
    expect([reading.sat, reading.sun]).toEqual([sat, sun])
  })

  it.each([
    ['Both days, but only if it ends by six', { type: 'end_before', hour: 18 }],
    ['Saturday as long as we’re done by 2pm', { type: 'end_before', hour: 14 }],
    ['Saturday only if it starts after 1 PM', { type: 'start_after', hour: 13 }],
    ['Saturday if it’s before noon', { type: 'end_before', hour: 12 }],
    ['Saturday works if we finish by 11:30', { type: 'end_before', hour: 11.5 }],
    ['Saturday, as long as it’s over by 2 p.m.', { type: 'end_before', hour: 14 }],
  ] as const)('keeps the time in %j', (message, limit) => {
    expect(readSample(message).limits.sat).toEqual(limit)
  })

  it('leaves a condition without a time unresolved', () => {
    const reading = readSample('Saturday only if my sister can come')
    expect(reading.sat).toBe('conditional')
    expect(reading.limits).toEqual({})
  })
})
