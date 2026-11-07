import { describe, expect, it } from 'vitest'
import { CROWD, fits, recommend, reservers, tally, type Read } from './chatScript'
import { readSample } from './sampleReader'

const reads: Read[] = CROWD.map((v) => ({ name: v.name, reading: readSample(v.message) }))

describe('the opening conversation', () => {
  it('counts who can come each day, including a condition the class meets', () => {
    expect(tally(reads)).toEqual({ sat: 4, sun: 3 })
  })

  it('recommends Saturday and says there are enough', () => {
    const r = recommend(tally(reads))
    expect(r.key).toBe('sat')
    expect(r.enough).toBe(true)
    expect(r.text).toBe('Sat, Nov 14 works for 4 — you need 3.')
  })

  it('names everyone who can reserve on the day', () => {
    expect(reservers(reads, 'sat')).toEqual(['Priya Raman', 'Ben Whitaker', 'Hannah Kim', 'Tomás Silva'])
  })

  it('keeps Saturday when the visitor makes Sunday a tie, as the server picks the earlier date', () => {
    const withVisitor = [...reads, { name: 'You', reading: readSample('Sunday works for me') }]
    expect(tally(withVisitor)).toEqual({ sat: 4, sun: 4 })
    expect(recommend(tally(withVisitor)).key).toBe('sat')
  })
})

describe('fits', () => {
  it('counts a condition the 10 AM–12 PM class meets', () => {
    expect(fits(readSample('Saturday if it ends by 2pm'), 'sat')).toBe(true)
  })

  it('does not count a condition the class breaks', () => {
    expect(fits(readSample('Saturday only if it starts after 1 PM'), 'sat')).toBe(false)
  })

  it('does not count a condition it cannot check', () => {
    expect(fits(readSample('Saturday only if my sister can come'), 'sat')).toBe(false)
  })

  it('does not count anyone who still has to answer a question', () => {
    expect(fits(readSample('I can make it either day, ideally in the afternoon.'), 'sat')).toBe(false)
  })

  it('does not count a no', () => {
    expect(fits(readSample('I can’t make either day'), 'sat')).toBe(false)
  })

  it.each(['Saturday works but I have to leave by 11', 'Saturday, only in the evening', 'Saturday is bad', 'Saturday if it ends before 25'])(
    'does not count %j',
    (message) => {
      expect(fits(readSample(message), 'sat')).toBe(false)
    },
  )

  it('counts a limit the class keeps without a condition word', () => {
    expect(fits(readSample('Saturday is fine from 9'), 'sat')).toBe(true)
  })
})

describe('recommend', () => {
  it('prefers the day with more people', () => {
    expect(recommend({ sat: 1, sun: 4 }).key).toBe('sun')
  })

  it('asks for more answers when nobody has enough yet', () => {
    const r = recommend({ sat: 2, sun: 1 })
    expect(r.enough).toBe(false)
    expect(r.text).toContain('Keep the answers coming')
  })
})
