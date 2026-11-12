import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import MoneyFlow, { SANDBOX_RUN } from './MoneyFlow'

const textOf = (html: string) => html.replaceAll(/<[^>]*>/g, '')

describe('the money example', () => {
  it('is the Sandbox run of Nov 11, 2026 as PayPal charged it, not a fee formula', () => {
    expect(SANDBOX_RUN).toEqual({ date: 'Nov 11, 2026', collected: 13_500, fees: 819, net: 12_681 })
    expect(SANDBOX_RUN.collected - SANDBOX_RUN.fees).toBe(SANDBOX_RUN.net)
  })

  it('prints the same figures in the ledger and the receipt', () => {
    const text = textOf(renderToStaticMarkup(<MoneyFlow />))
    for (const want of ['$45 held', '$135 charged', '$0 for anyone', '$126.81 sent', '$135.00 collected · $8.19 PayPal fees · $126.81 net', 'real PayPal Sandbox run on Nov 11, 2026']) {
      expect(text).toContain(want)
    }
    for (const old of ['$6.18', '$128.82', '3.49%', 'Nov 10, 2026']) {
      expect(text).not.toContain(old)
    }
  })
})
