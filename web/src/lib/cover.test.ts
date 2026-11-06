import { describe, expect, it } from 'vitest'
import { cover } from './cover'

describe('cover', () => {
  it('draws the same shapes for the same workshop', () => {
    expect(cover('a1b2c3')).toEqual(cover('a1b2c3'))
  })

  it('draws different shapes for different workshops', () => {
    expect(cover('a1b2c3')).not.toEqual(cover('z9y8x7'))
  })

  it('keeps every ring partly inside the frame', () => {
    for (const id of ['', 'x', 'workshop-1', 'workshop-2', 'f0e1d2c3b4a5']) {
      for (const r of cover(id).rings) {
        expect(r.cx).toBeGreaterThanOrEqual(0)
        expect(r.cx).toBeLessThanOrEqual(100)
        expect(r.cy).toBeGreaterThanOrEqual(0)
        expect(r.cy).toBeLessThanOrEqual(100)
        expect(r.r).toBeGreaterThan(r.width)
      }
    }
  })
})
