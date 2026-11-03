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
    // Every checkout starts first: the demo control applies to a payment attempt, which the enrollment creates.
    for (const p of participants) await startCheckout(p)
    await failNextCharge(owner, ben.name)

    // Three holds reach the minimum and are charged; Ben's charge fails, which leaves two paid seats.
    for (const p of [ada, ben, cleo]) await approveCheckout(p)
    await expect(enrollmentRow(owner, ben.name)).toContainText('Payment failed — you can try again', { timeout: 60_000 })
    await expect(paidSeats(owner, 2)).toBeVisible()
    await expect(headline(owner)).toHaveText('Payments are open')
    await expect(headline(ben.page)).toHaveText('Payment failed — you can try again')

    await approveCheckout(dev)
    await expect(headline(owner)).toHaveText('Workshop confirmed', { timeout: 60_000 })
    await expect(paidSeats(owner, 3)).toBeVisible()
    for (const p of [ada, cleo, dev]) await expect(enrollmentRow(owner, p.name)).toContainText('Seat paid')
    await expect(enrollmentRow(owner, ben.name)).toContainText('Payment failed — you can try again')

    // Ben's page stops polling once his own status is final, so it reads the confirmation on a reload.
    await ben.page.reload()
    await expect(headline(ben.page)).toHaveText('Workshop confirmed — payment failed, you can try again')
  })
})
