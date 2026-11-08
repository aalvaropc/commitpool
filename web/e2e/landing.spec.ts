import { expectAccessible } from './a11y'
import { expect, test } from './helpers'

test.describe('landing chat', () => {
  test('reads a quick reply live, says who read it and counts it for the right day', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')
    const chat = page.getByRole('region', { name: 'Example: answers to Maya’s invite' })
    await expect(chat.getByText('Example chat · prepared answers · payments simulated')).toBeVisible()
    await expect(chat.getByText('Prepared reading')).toHaveCount(5)
    await expect(chat.getByText('you could be the third')).toBeVisible()
    await expect(chat.getByText(/Claude is reading/)).toHaveCount(0)
    await expect(page.getByText('yours goes to Claude to read live')).toBeVisible()

    const sent = page.waitForResponse((r) => r.url().endsWith('/api/v1/sample-reading') && r.request().method() === 'POST')
    await chat.getByRole('button', { name: 'El domingo sí puedo' }).click()
    expect((await sent).status()).toBe(200)
    await expect(chat.getByText('Simple rules read this one', { exact: true })).toBeVisible()
    await expect(chat.getByText('Read by Claude')).toHaveCount(0)
    await expect(chat.getByText('Sat · Not sure')).toBeVisible()
    await expect(chat.getByText('Sun · Can come').last()).toBeVisible()
    await expect(page.getByText('simple rules on the server read yours')).toBeVisible()
    await expect(chat.getByText(/^You’re counted for Sunday: that makes 3/)).toBeVisible()
    await expect(chat.getByText(/^Reservations are open for Sun, /)).toBeVisible()
    await expectAccessible(page, 'the landing page after a live reading')

    await chat.getByRole('button', { name: /^Reserve · \$45 hold, simulated/ }).click()
    await expect(chat.getByText('It’s on', { exact: true }).last()).toBeVisible()
    await expect(chat.getByText('3 paid · $135 covers the $120 studio.')).toBeVisible()
  })
})
