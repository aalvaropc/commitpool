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

test.describe('below minimum', () => {
  test('below minimum at close shows cancelled with refund tracking', async ({ page: owner, join }) => {
    const workshop = await createWorkshop(owner, 'Watercolour workshop')
    const participants = []
    for (const name of ['Ada', 'Ben', 'Cleo']) {
      const p = await join(workshop.shareURL, name)
      await sendAvailability(p)
      participants.push(p)
    }
    const [ada, ben, cleo] = participants

    await openPayments(owner, workshop, 3)
    for (const p of participants) await startCheckout(p)
    await failNextCharge(owner, cleo.name)

    // Ada and Ben pay; Cleo's charge fails, so two paid seats stay below the minimum of three.
    for (const p of participants) await approveCheckout(p)
    await expect(enrollmentRow(owner, cleo.name)).toContainText('Payment failed — you can try again', { timeout: 60_000 })
    await expect(paidSeats(owner, 2)).toBeVisible()
    await expect(headline(owner)).toHaveText('Payments are open')

    await owner.getByRole('button', { name: /Close payments now/ }).click()

    // The fake refunds within one worker poll, so "Workshop cancelled — refund in progress" can come and go
    // between two reads of the page; the settled outcome is what must show.
    await expect(headline(owner)).toHaveText('Workshop cancelled — refund completed', { timeout: 60_000 })
    await expect(owner.getByText('1 hold released, 2 refunds completed, 0 in progress.')).toBeVisible()
    for (const p of [ada, ben]) await expect(enrollmentRow(owner, p.name)).toContainText('Refund completed')
    await expect(enrollmentRow(owner, cleo.name)).toContainText('Hold released')
    for (const p of [ada, ben]) await expect(headline(p.page)).toHaveText('Workshop cancelled — refund completed')
  })
})
