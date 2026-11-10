import { expectAccessible } from './a11y'
import { approveCheckout, createWorkshop, expect, headline, openPayments, sendAvailability, startCheckout, test } from './helpers'

test.describe('payout', () => {
  test('pays the organizer what people paid, less PayPal fees, when the class starts', async ({ page: owner, join }) => {
    const workshop = await createWorkshop(owner, 'Pottery night', 'iris@example.com')
    const participants = []
    for (const name of ['Ada', 'Ben', 'Cleo']) {
      const p = await join(workshop.shareURL, name)
      await sendAvailability(p)
      participants.push(p)
    }
    await openPayments(owner, workshop, 3)
    for (const p of participants) {
      await startCheckout(p)
      await approveCheckout(p)
    }
    await expect(headline(owner)).toHaveText('It’s on', { timeout: 60_000 })

    const payout = owner.getByRole('region', { name: 'Your payout' })
    await expect(payout.getByRole('status')).toHaveText(/^Paid out when the class starts, /)
    await expect(payout).toContainText('CommitPool sends $27.48 to iris@example.com through PayPal.')
    await expect(payout).toContainText('So far: $30.00 paid by 3 people · $2.52 PayPal fees · $27.48 to you')

    await owner.getByRole('button', { name: /Close reservations now/ }).click()
    const start = owner.getByRole('button', { name: /Start the class now/ })
    await expect(start).toBeVisible()
    await expect(owner.getByText('Demo clock: jumps ahead to the class start')).toBeVisible()
    await start.click()

    await expect(payout.getByRole('status')).toHaveText(/^Paid out \$27\.48 to iris@example\.com · PayPal batch \w+$/, { timeout: 30_000 })
    await expect(payout.getByText('Paid out', { exact: true })).toBeVisible()
    await expect(start).toBeHidden()
    await payout.getByText('PayPal details').click()
    await expect(payout.getByText('Transaction', { exact: true })).toBeVisible()
    await expectAccessible(owner, 'the organizer page once paid out')
  })
})
