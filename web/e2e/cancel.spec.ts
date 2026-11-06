import { approveCheckout, createWorkshop, enrollmentRow, expect, headline, openPayments, sendAvailability, startCheckout, test } from './helpers'

test.describe('withdraw and cancel', () => {
  test('two participants withdraw, then the organizer cancels and the last hold is released', async ({ page: owner, join }) => {
    const workshop = await createWorkshop(owner, 'Pottery evening')
    const participants = []
    for (const name of ['Ada', 'Ben', 'Cleo']) {
      const p = await join(workshop.shareURL, name)
      await sendAvailability(p)
      participants.push(p)
    }
    const [ada, ben, cleo] = participants
    await openPayments(owner, workshop, 3)

    for (const p of [ada, ben]) {
      await startCheckout(p)
      await approveCheckout(p)
    }
    await startCheckout(cleo)
    await cleo.page.goBack()
    await cleo.page.getByRole('button', { name: 'Give up my spot…' }).click()
    await expect(cleo.page.getByText('Your spot goes back to the group. Nothing was held at PayPal.')).toBeVisible()
    await cleo.page.getByRole('button', { name: 'Yes, leave' }).click()
    await expect(cleo.page.getByText('You gave up your spot. Nothing was held at PayPal.')).toBeVisible()

    await ben.page.getByRole('button', { name: 'Leave and release my hold…' }).click()
    await ben.page.getByRole('button', { name: 'Yes, leave' }).click()
    await expect(ben.page.getByText('You left. PayPal releases your hold; you are not charged.')).toBeVisible()

    await owner.reload()
    await expect(enrollmentRow(owner, cleo.name)).toContainText('Gave up the spot')
    await expect(enrollmentRow(owner, ben.name)).toContainText('Gave up the spot — hold released', { timeout: 30_000 })

    await owner.getByRole('button', { name: 'Cancel workshop…' }).click()
    await expect(owner.getByText('PayPal releases 1 hold.')).toBeVisible()
    await owner.getByLabel('Reason (optional)').fill('The studio is closed that week.')
    await owner.getByRole('button', { name: 'Yes, cancel workshop' }).click()

    await expect(headline(owner)).toHaveText(/^Cancelled/, { timeout: 60_000 })
    await expect(enrollmentRow(owner, ada.name)).toContainText('Hold released', { timeout: 60_000 })
    await expect(owner.getByRole('button', { name: 'Cancel workshop…' })).toHaveCount(0)
    await ada.page.reload()
    await expect(headline(ada.page)).toHaveText(/^Cancelled/)
    await expect(ada.page.getByRole('button', { name: /Leave|Give up/ })).toHaveCount(0)
  })
})
