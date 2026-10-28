import { describe, expect, it } from 'vitest'
import { tokenFromParticipantURL } from './participant'

describe('tokenFromParticipantURL', () => {
  it('takes the token from the participant URL the server returns', () => {
    expect(tokenFromParticipantURL('http://localhost:8080/p/AbC_123-xyz')).toBe('AbC_123-xyz')
  })

  it('refuses any other address', () => {
    expect(tokenFromParticipantURL('http://localhost:8080/owner/AbC')).toBeNull()
    expect(tokenFromParticipantURL('not a url')).toBeNull()
  })
})
