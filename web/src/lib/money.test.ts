import { describe, expect, it } from 'vitest'
import { formatMoney } from './money'

describe('formatMoney', () => {
  it('formats US dollars from minor units', () => {
    expect(formatMoney({ minor: 1000, currency: 'USD' })).toBe('$10.00')
  })

  it('keeps cents and groups thousands', () => {
    expect(formatMoney({ minor: 5, currency: 'USD' })).toBe('$0.05')
    expect(formatMoney({ minor: 1234567, currency: 'USD' })).toBe('$12,345.67')
  })

  it('uses the currency’s own number of decimals', () => {
    expect(formatMoney({ minor: 500, currency: 'JPY' })).toBe('¥500')
  })
})
