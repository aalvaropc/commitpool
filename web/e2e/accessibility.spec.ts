import { expectAccessible } from './a11y'
import { createWorkshop, expect, fillTheFormMyself, sendAvailability, test } from './helpers'

test.describe('accessibility', () => {
  test('every page a person reaches has no serious accessibility problem', async ({ page, join }) => {
    await page.goto('/')
    await expect(page.getByRole('button', { name: 'Try it as Iris' }).first()).toBeVisible()
    await expectAccessible(page, 'the landing page')

    await page.goto('/new')
    await expectAccessible(page, 'the describe step')

    await fillTheFormMyself(page)
    await expectAccessible(page, 'the create form')

    const workshop = await createWorkshop(page, 'Accessible life drawing')
    await expectAccessible(page, 'the created page')

    await page.goto(workshop.shareURL)
    await expectAccessible(page, 'the invite page')

    const ada = await join(workshop.shareURL, 'Ada')
    await sendAvailability(ada)
    await expectAccessible(ada.page, 'the participant page')

    await page.goto(workshop.ownerURL)
    await expect(page.getByRole('region', { name: 'Workshop status' })).toBeVisible()
    await expectAccessible(page, 'the organizer page')
  })
})
