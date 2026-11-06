import { describe, expect, it } from 'vitest'
import { formatMoney } from './money'

describe('formatMoney', () => {
  it('formats whole US dollars without cents', () => {
    expect(formatMoney({ minor: 1000, currency: 'USD' })).toBe('$10')
    expect(formatMoney({ minor: 4550, currency: 'USD' })).toBe('$45.50')
  })

  it('keeps cents and groups thousands', () => {
    expect(formatMoney({ minor: 5, currency: 'USD' })).toBe('$0.05')
    expect(formatMoney({ minor: 1234567, currency: 'USD' })).toBe('$12,345.67')
  })

  it('uses the currency’s own number of decimals', () => {
    expect(formatMoney({ minor: 500, currency: 'JPY' })).toBe('¥500')
  })
})
