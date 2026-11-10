import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import MoneyFlow, { moneyExample } from './MoneyFlow'

describe('the money example', () => {
  it('charges 3.49% + $0.49 on each $45 capture, rounded half up', () => {
    expect(moneyExample()).toEqual({ collected: 13_500, fees: 618, net: 12_882 })
    expect(moneyExample(10, 1)).toEqual({ collected: 1_000, fees: 84, net: 916 })
  })

  it('prints the same figures in the ledger and the receipt', () => {
    const html = renderToStaticMarkup(<MoneyFlow />)
    for (const text of ['$45 held', '$135 charged', '$0 for anyone', '$128.82 paid out', '$135.00 collected', '$6.18 PayPal fees', '$128.82 net']) {
      expect(html).toContain(text)
    }
  })
})
