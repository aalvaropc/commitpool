import { expectAccessible } from './a11y'
import {
  approveCheckout,
  createWorkshop,
  enrollmentRow,
  expect,
  headline,
  openPayments,
  paidSeats,
  sendAvailability,
  startCheckout,
  test,
} from './helpers'

test.describe('funding journey', () => {
  test('creates workshop, collects availability, opens funding, three participants pay, workshop confirmed', async ({ page: owner, join }) => {
    const workshop = await createWorkshop(owner, 'Bread baking basics')
    const participants = []
    for (const name of ['Ada', 'Ben', 'Cleo']) {
      const p = await join(workshop.shareURL, name)
      await sendAvailability(p)
      participants.push(p)
    }

    const invite = await owner.request.get(workshop.shareURL)
    expect(await invite.text()).toContain('<meta property="og:title" content="Bread baking basics" />')
    const preview = await owner.request.get(workshop.shareURL.replace('/w/', '/api/v1/w/') + '/og.png')
    expect(preview.headers()['content-type']).toBe('image/png')

    await openPayments(owner, workshop, 3)
    const calendar = participants[0].page.getByRole('link', { name: 'Add to calendar' })
    await participants[0].page.reload()
    await expect(calendar).toHaveAttribute('href', /^\/api\/v1\/p\/[\w-]+\/calendar\.ics$/)
    const ics = await participants[0].page.request.get((await calendar.getAttribute('href')) ?? '')
    expect(ics.headers()['content-type']).toBe('text/calendar; charset=utf-8')
    expect(await ics.text()).toContain('SUMMARY:Bread baking basics')
    for (const p of participants) {
      await startCheckout(p)
      await approveCheckout(p)
    }

    await expect(headline(owner)).toHaveText('It’s on', { timeout: 60_000 })
    await expect(owner.getByRole('region', { name: 'Next step' })).toContainText('3 paid — $30 collected.')
    await expect(paidSeats(owner, 3)).toBeVisible()
    const payments = owner.getByRole('region', { name: 'Payments', exact: true })
    await expect(payments).toContainText('$30.00 collected · $2.52 PayPal fees · $27.48 net')
    const ada = enrollmentRow(owner, 'Ada')
    await ada.getByText('PayPal details').click()
    await expect(ada.getByRole('button', { name: 'Copy charge ID for Ada' })).toBeVisible()
    await expect(ada.getByRole('definition').filter({ hasText: /^\$0\.84$/ })).toBeVisible()
    await expect(ada.getByRole('definition').filter({ hasText: /^\$9\.16$/ })).toBeVisible()
    await payments.getByRole('button', { name: 'Verify with PayPal' }).click()
    await expect(payments.getByRole('status').filter({ hasText: 'match PayPal' })).toContainText(/^9 of 9 records match PayPal · checked \d{1,2}:\d{2} [AP]M$/)
    await expectAccessible(owner, 'the organizer page with PayPal details and a verify result')
    for (const p of participants) {
      await expect(enrollmentRow(owner, p.name)).toContainText('Paid')
      await expect(headline(p.page)).toHaveText('You’re in — it’s on')
    }
  })
})
