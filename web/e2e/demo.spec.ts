import { expectAccessible } from './a11y'
import { expect, test } from './helpers'

test.describe('one-click demo', () => {
  test('a visitor becomes Maya and watches the answers arrive', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: 'Try it as Maya' }).first().click()

    await expect(page.getByRole('heading', { level: 1, name: 'Wheel-throwing for beginners' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'You are Maya now' })).toBeVisible()
    await expect(page.getByText('Demo workshop · prepared data')).toBeVisible()
    await expect(page.getByText('6 of 6 answered', { exact: false })).toBeVisible({ timeout: 30_000 })
    await expect(page.getByRole('region', { name: 'Next step' })).toContainText('works for 4 — you need 3')
    const suggests = page.getByRole('region', { name: 'Claude suggests' })
    await expect(suggests).toContainText('Grace Liu’s message needs clarifying.')
    await expect(suggests.getByRole('button', { name: 'Copy message for Grace Liu' })).toBeVisible()
    await expect(suggests).toContainText('Hi Grace!')
    await expectAccessible(page, 'the demo organizer page')

    const priya = page.getByRole('link', { name: /Act as Priya Raman/ })
    await expect(priya).toBeVisible()
    await page.goto((await priya.getAttribute('href')) ?? '/')
    await expect(page.getByText('You joined as Priya Raman.')).toBeVisible()
  })
})
