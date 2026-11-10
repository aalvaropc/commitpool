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
})
