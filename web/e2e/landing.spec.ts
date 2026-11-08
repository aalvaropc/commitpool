import { expectAccessible } from './a11y'
import { expect, test } from './helpers'

test.describe('landing chat', () => {
  test('reads a quick reply live and says who read it', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')
    const chat = page.getByRole('region', { name: 'Example: answers to Maya’s invite' })
    await expect(chat.getByText('Example chat · prepared answers · payments simulated')).toBeVisible()
    await expect(chat.getByText('Prepared reading')).toHaveCount(5)
    await expect(chat.getByText('you could be the third')).toBeVisible()
    await expect(chat.getByText(/Claude is reading/)).toHaveCount(0)

    const sent = page.waitForResponse((r) => r.url().endsWith('/api/v1/sample-reading') && r.request().method() === 'POST')
    await chat.getByRole('button', { name: 'El domingo sí puedo' }).click()
    expect((await sent).status()).toBe(200)
    await expect(chat.getByText('Simple rules read this one', { exact: true })).toBeVisible()
    await expect(chat.getByText('Read by Claude')).toHaveCount(0)
    await expect(chat.getByText(/^Reservations are open for Sat, /)).toBeVisible()
    await expectAccessible(page, 'the landing page after a live reading')
  })
})
