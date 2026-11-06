import {
  approveCheckout,
  createWorkshop,
  enrollmentRow,
  expect,
  failNextCharge,
  headline,
  openPayments,
  paidSeats,
  sendAvailability,
  startCheckout,
  test,
} from './helpers'

test.describe('induced capture failure', () => {
  test('induced failure keeps workshop funding and shows failed seat', async ({ page: owner, join }) => {
    const workshop = await createWorkshop(owner, 'Pottery for beginners')
    const participants = []
    for (const name of ['Ada', 'Ben', 'Cleo', 'Dev']) {
      const p = await join(workshop.shareURL, name)
      await sendAvailability(p)
      participants.push(p)
    }
    const [ada, ben, cleo, dev] = participants

    await openPayments(owner, workshop, 4)
    for (const p of participants) await startCheckout(p)
    await failNextCharge(owner, ben.name)

    for (const p of [ada, ben, cleo]) await approveCheckout(p)
    await expect(enrollmentRow(owner, ben.name)).toContainText('Payment didn\'t go through — try again', { timeout: 60_000 })
    await expect(paidSeats(owner, 2)).toBeVisible()
    await expect(headline(owner)).toHaveText('Reservations open')
    await expect(headline(ben.page)).toHaveText('Payment didn’t go through — try again')

    await approveCheckout(dev)
    await expect(headline(owner)).toHaveText('It’s on', { timeout: 60_000 })
    await expect(paidSeats(owner, 3)).toBeVisible()
    for (const p of [ada, cleo, dev]) await expect(enrollmentRow(owner, p.name)).toContainText('Paid')
    await expect(enrollmentRow(owner, ben.name)).toContainText('Payment didn\'t go through — try again')

    await ben.page.reload()
    await expect(headline(ben.page)).toHaveText('It’s on — your payment didn’t go through, try again')
  })
})
