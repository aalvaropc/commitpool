import { describe, expect, it } from 'vitest'
import { demoParticipants } from './demo'

describe('demo participants', () => {
  it('offers the people the organizer view lists', () => {
    const act_as = [
      { name: 'Priya Raman', participant_url: 'https://cp.test/p/PRI' },
      { name: 'Ben Whitaker', participant_url: 'https://cp.test/p/BEN' },
    ]
    expect(demoParticipants({ act_as })).toEqual([
      { name: 'Priya Raman', path: '/p/PRI' },
      { name: 'Ben Whitaker', path: '/p/BEN' },
    ])
  })

  it('ignores anything that is not a participant link', () => {
    expect(demoParticipants({ act_as: [{ name: 'Mallory', participant_url: 'javascript:alert(1)' }] })).toEqual([])
    expect(demoParticipants({ act_as: [{ name: 'Mallory', participant_url: 'https://cp.test/owner/OWN' }] })).toEqual([])
  })

  it('offers nobody outside a demo workshop', () => {
    expect(demoParticipants({})).toEqual([])
  })
})
