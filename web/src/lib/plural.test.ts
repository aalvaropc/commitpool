import { describe, expect, it } from 'vitest'
import { count } from './plural'

describe('count', () => {
  it('puts the noun in the singular only for one', () => {
    expect(count(1, 'paid seat')).toBe('1 paid seat')
    expect(count(3, 'paid seat')).toBe('3 paid seats')
    expect(count(0, 'refund')).toBe('0 refunds')
  })

  it('takes an irregular plural', () => {
    expect(count(1, 'person', 'people')).toBe('1 person')
    expect(count(2, 'person', 'people')).toBe('2 people')
  })
})
