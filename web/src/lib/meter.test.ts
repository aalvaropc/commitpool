import { describe, expect, it } from 'vitest'
import { meter } from './meter'

describe('meter', () => {
  it('counts down to the minimum', () => {
    const m = meter({ capacity: 6, needed: 3, paid: 0, reserved: 1 })
    expect(m.caption).toBe('2 more and it’s on')
    expect(m.reached).toBe(false)
    expect(m.reservedPct).toBeCloseTo(100 / 6)
    expect(m.neededPct).toBe(50)
  })

  it('says one more in the singular', () => {
    expect(meter({ capacity: 6, needed: 3, paid: 1, reserved: 1 }).caption).toBe('1 more and it’s on')
  })

  it('waits for the charges once enough people reserved', () => {
    const m = meter({ capacity: 6, needed: 3, paid: 1, reserved: 2 })
    expect(m.caption).toBe('Enough reserved — confirming payments')
    expect(m.reached).toBe(false)
  })

  it('is on once enough people paid', () => {
    const m = meter({ capacity: 6, needed: 3, paid: 4, reserved: 0 })
    expect(m.caption).toBe('It’s on — 4 paid')
    expect(m.reached).toBe(true)
    expect(m.paidPct).toBeCloseTo((4 / 6) * 100)
  })

  it('says full when every spot is taken', () => {
    expect(meter({ capacity: 6, needed: 3, paid: 6, reserved: 0 }).caption).toBe('It’s on — full')
  })

  it('never draws past the capacity', () => {
    const m = meter({ capacity: 4, needed: 2, paid: 3, reserved: 5 })
    expect(m.paidPct + m.reservedPct).toBe(100)
  })

  it('reads the whole state for screen readers', () => {
    expect(meter({ capacity: 6, needed: 3, paid: 1, reserved: 2 }).summary).toBe(
      '1 paid and 2 reserved of 6 spots; 3 needed to happen.',
    )
  })

  it('keeps a zero capacity drawable', () => {
    const m = meter({ capacity: 0, needed: 1, paid: 0, reserved: 0 })
    expect(Number.isFinite(m.neededPct)).toBe(true)
  })
})
