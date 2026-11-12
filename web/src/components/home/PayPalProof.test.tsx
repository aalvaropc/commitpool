import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import PayPalProof from './PayPalProof'

describe('PayPal, for real', () => {
  it('names each call under the API that serves it', () => {
    const html = renderToStaticMarkup(<PayPalProof />)
    const row = (api: string) => html.slice(html.indexOf(`>${api}</dt>`), html.indexOf('</div>', html.indexOf(`>${api}</dt>`)))
    expect(row('Orders v2')).toContain('create · authorize')
    expect(row('Payments v2')).toContain('capture · void · refund')
    expect(row('Orders v2')).not.toMatch(/capture|void/)
    expect(row('Webhooks')).toContain('verify-webhook-signature')
  })

  it('shows the Sandbox run of Nov 11, 2026: what Verify found, one spot and the payout', () => {
    const html = renderToStaticMarkup(<PayPalProof />)
    expect(html).toContain('9 of 9 records match PayPal')
    expect(html).toContain('A real PayPal Sandbox run on Nov 11, 2026')
    const record = (kind: string) => html.slice(html.indexOf(`>${kind}</dt>`), html.indexOf('</div>', html.indexOf(`>${kind}</dt>`)))
    expect(record('Order')).toContain('7UV14666HK580221P')
    expect(record('Authorization')).toContain('7D4155031A510524L')
    expect(record('Authorization')).toContain('CAPTURED')
    expect(record('Capture')).toContain('0T125042VN752083V')
    expect(record('Payouts batch')).toContain('L8SSKBUKM5UF6')
    expect(record('Payouts batch')).toContain('Paid out')
    expect(record('Payouts batch')).not.toContain('Match')
    for (const old of ['4 of 4', 'Nov 10, 2026', '8CU0641389306473M', '29F87239ER3603046', '5A184279HG6049222', 'QW74P22YPEA8W', '8K886057PL996893M', '1AS808255V146505P', '82975102TH652782Y', '43G324002P691632L']) {
      expect(html).not.toContain(old)
    }
  })
})
