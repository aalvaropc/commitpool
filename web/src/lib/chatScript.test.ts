import { describe, expect, it } from 'vitest'
import { CROWD, CROWD_READS, SUGGESTIONS, countedBy, crowdTally, fits, leader, opening, outcome, reservers, tally } from './chatScript'
import { sampleDates } from './sampleDates'
import { readSample } from './sampleReader'

const dates = sampleDates(new Date('2026-12-01T12:00:00Z'))
const none = { sat: false, sun: false }

describe('the prepared crowd', () => {
  it('says exactly what the demo cast says, minus Grace', () => {
    expect(CROWD.map((v) => [v.name, v.message])).toEqual([
      ['Priya Raman', "Saturday works for me, Sunday I can't."],
      ['Ben Whitaker', "The first date works if we're done before 6; the second one I'm away."],
      ['Lucía Torres', 'El sábado no puedo, pero el domingo me viene perfecto.'],
      ['Hannah Kim', "Saturday only if we're done by 11:30 — I have a lunch at noon. Sunday is out."],
      ['Tomás Silva', "Sunday's good. Saturday I'm driving my daughter to a tournament all day."],
    ])
  })

  it('counts Ben’s 6 PM limit and not Hannah’s 11:30 one against the 10 AM–12 PM class', () => {
    const counted = Object.fromEntries(CROWD_READS.map((r) => [r.name, r.counted]))
    expect(counted['Ben Whitaker']).toEqual({ sat: true, sun: false })
    expect(counted['Hannah Kim']).toEqual({ sat: false, sun: false })
  })

  it('leaves both days one short', () => {
    expect(tally(CROWD_READS)).toEqual({ sat: 2, sun: 2 })
    expect(crowdTally(dates)).toBe('Saturday: 2 can come. Sunday: 2. Maya needs 3 on one day, so she needs one more — you could be the third.')
  })

  it('names everyone who can reserve on each day', () => {
    expect(reservers(CROWD_READS, 'sat')).toEqual(['Priya Raman', 'Ben Whitaker'])
    expect(reservers(CROWD_READS, 'sun')).toEqual(['Lucía Torres', 'Tomás Silva'])
  })

  it('puts the visitor first among those who reserve', () => {
    expect(reservers([...CROWD_READS, { name: 'You', counted: { sat: true, sun: false } }], 'sat')).toEqual(['You', 'Priya Raman', 'Ben Whitaker'])
  })
})

describe('opening', () => {
  it('takes its dates from the calendar, not from the code', () => {
    expect(opening(dates)).toBe(
      'Wheel-throwing for beginners! Saturday or Sunday, 10 AM–12 PM (Sat, Dec 5 or Sun, Dec 6). The studio costs me $120, so I need 3 of you at $45 each.',
    )
    expect(opening(sampleDates(new Date('2027-01-25T12:00:00Z')))).toContain('(Sat, Jan 30 or Sun, Jan 31)')
  })
})

describe('outcome', () => {
  it('opens Saturday when the visitor is counted on the leading day', () => {
    const o = outcome({ sat: true, sun: false }, dates, false)
    expect(o.key).toBe('sat')
    expect(o.counts).toEqual({ sat: 3, sun: 2 })
    expect(o.text).toBe('You’re counted for Saturday: that makes 3, enough for Maya. She opens reservations for Sat, Dec 5.')
  })

  it('opens Sunday when the visitor is counted on the other day', () => {
    const o = outcome({ sat: false, sun: true }, dates, false)
    expect(o.key).toBe('sun')
    expect(o.text).toContain('Sun, Dec 6')
  })

  it('breaks a tie with the earlier date', () => {
    const o = outcome({ sat: true, sun: true }, dates, false)
    expect(o.key).toBe('sat')
    expect(o.text).toContain('On a tie the earlier date wins: Sat, Dec 5.')
  })

  it('ends honestly when the visitor is not counted', () => {
    const o = outcome(none, dates, false)
    expect(o.key).toBeNull()
    expect(o.text).toBe(
      'From what you wrote, you aren’t counted for either day. Saturday has 2 and Sunday has 2: Maya still needs one more, so nothing opens and nobody pays.',
    )
  })

  it('says a question is still open', () => {
    expect(outcome(none, dates, true).text).toMatch(/^There’s still a question for you/)
  })
})

describe('quick replies', () => {
  it('offers the three answers the landing promises', () => {
    expect(SUGGESTIONS).toEqual(['No problem — Saturday works!', 'El domingo sí puedo', 'The first one, if we’re done by 11'])
  })

  it('a Saturday yes read by the fallback rules makes Saturday three', () => {
    expect(outcome(countedBy(readSample(SUGGESTIONS[0])), dates, false).key).toBe('sat')
  })
})

describe('leader', () => {
  it('prefers the day with more people and the earlier one on a tie', () => {
    expect(leader({ sat: 1, sun: 4 })).toBe('sun')
    expect(leader({ sat: 2, sun: 2 })).toBe('sat')
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
