import { expectAccessible } from './a11y'
import { approveCheckout, createWorkshop, expect, headline, openPayments, sendAvailability, startCheckout, test } from './helpers'

test.describe('deadline rescue', () => {
  test('a short class in its last hours: Claude suggests one extension, the organizer approves, everyone sees the new deadline', async ({ page: owner, join }) => {
    const workshop = await createWorkshop(owner, 'Pottery wheel taster')
    const participants = []
    for (const name of ['Ada', 'Ben', 'Cleo']) {
      const p = await join(workshop.shareURL, name)
      await sendAvailability(p)
      participants.push(p)
    }
    const [ada, ben] = participants

    await openPayments(owner, workshop, 3)
    for (const p of [ada, ben]) {
      await startCheckout(p)
      await approveCheckout(p)
    }
    await expect(ada.page.getByText('Latest possible deadline:')).toBeVisible()

    await owner.reload()
    const suggests = owner.getByRole('region', { name: 'Claude suggests' })
    await expect(suggests.getByRole('button', { name: /^Extend to / })).toHaveCount(0)
    await owner.getByRole('button', { name: /Skip to the last 2 hours/ }).click()
    await expect(owner.getByText('Reservations now close in 2 hours.')).toBeVisible()

    const extend = suggests.getByRole('button', { name: /^Extend to / })
    await expect(extend).toBeVisible({ timeout: 30_000 })
    await expect(suggests).toContainText('1 more is needed')
    await expect(suggests).toContainText('Claude’s message for the group, once you extend')
    await expect(suggests).toContainText('has more time: reservations now stay open until')
    await expectAccessible(owner, 'the organizer page with a rescue suggestion')
    const newDeadline = ((await extend.textContent()) ?? '').replace(/^Extend to /, '')

    await extend.click()
    const dialog = owner.getByRole('dialog')
    await expect(dialog).toContainText('You can do this only once.')
    await expect(dialog).toContainText('Everyone who left an email is told the new deadline.')
    await expect(dialog).toContainText('Anyone can still leave and release their hold until charging starts.')
    await expectAccessible(owner, 'the extend dialog')
    await dialog.getByRole('button', { name: `Extend to ${newDeadline}` }).click()
    await expect(dialog).toBeHidden()

    const moved = owner.getByText(new RegExp(`^Deadline extended from .+ to ${newDeadline.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`))
    await expect(moved).toBeVisible()
    await expect(owner.getByRole('region', { name: 'Next step' })).toContainText(newDeadline)
    await expect(suggests.getByRole('button', { name: /^Extend to / })).toHaveCount(0)
    await expect(suggests).toContainText(`by ${newDeadline}`.split(' · ')[0])

    await ada.page.reload()
    await expect(ada.page.getByText(/^Deadline extended from .+ to /)).toBeVisible()
    await expect(ada.page.getByRole('region', { name: 'Workshop status' })).toContainText(newDeadline)
    await expect(ada.page.getByText('It was already extended once.', { exact: false })).toBeVisible()
    await expectAccessible(ada.page, 'the participant page after an extension')

    await owner.getByRole('button', { name: /Close reservations now/ }).click()
    await expect(headline(owner)).toHaveText(/^Cancelled/, { timeout: 60_000 })
    await expect(moved).toBeHidden()
  })
})
