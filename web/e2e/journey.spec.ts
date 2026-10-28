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

    await openPayments(owner, workshop, 3)
    for (const p of participants) {
      await startCheckout(p)
      await approveCheckout(p)
    }

    await expect(headline(owner)).toHaveText('Workshop confirmed', { timeout: 60_000 })
    await expect(owner.getByText('3 paid seats reached the minimum of 3.')).toBeVisible()
    await expect(paidSeats(owner, 3)).toBeVisible()
    for (const p of participants) {
      await expect(enrollmentRow(owner, p.name)).toContainText('Seat paid')
      await expect(headline(p.page)).toHaveText('Seat confirmed')
    }
  })
})
