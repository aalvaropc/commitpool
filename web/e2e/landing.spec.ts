import { expectAccessible } from './a11y'
import { expect, test } from './helpers'

test.describe('landing chat', () => {
  test('reads a quick reply live, says who read it and counts it for the right day', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')
    await expect(page.getByRole('heading', { level: 1, name: 'Find the date that fills the class.' })).toBeVisible()
    const chat = page.getByRole('region', { name: 'Example: answers to Iris’s invite' })
    await expect(page.getByText('Paste your group chat. Claude reads everyone’s maybes and asks when it matters.')).toBeVisible()
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
    await expect(chat.getByText('3 paid · $135 before PayPal fees covers your $120 session costs.')).toBeVisible()
  })
})

const VIDEO = 'https://www.youtube-nocookie.com/embed/aB3_x-9Zk0Q'

test.describe('landing proof', () => {
  test.beforeEach(async ({ page }) => {
    await page.route('https://www.youtube-nocookie.com/**', (route) => route.fulfill({ contentType: 'text/html', body: '<!doctype html><title>Demo video</title>' }))
    await page.route('https://i.ytimg.com/**', (route) => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="480" height="360"/>' }))
    await page.emulateMedia({ reducedMotion: 'reduce' })
  })

  test('the headline, the sourced figures, the proof and the judge path', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { level: 1, name: 'Find the date that fills the class.' })).toBeVisible()

    const figures = page.getByRole('region', { name: 'Why a hold, not a poll' })
    await expect(figures).toContainText('~37%')
    await expect(figures).toContainText('~17%')
    await expect(figures).toContainText('419,100')
    await expect(figures).toContainText('388,400')
    await expect(figures.getByRole('link', { name: /PheedLoop Event Data Lab, 2026/ })).toHaveAttribute('href', 'https://pheedloop.com/blog/event-data-lab-report-5')
    await expect(figures.getByRole('link', { name: /teachers/ })).toHaveAttribute('href', 'https://www.onetonline.org/link/summary/25-3021.00')
    await expect(figures.getByRole('link', { name: /instructors/ })).toHaveAttribute('href', 'https://www.onetonline.org/link/summary/39-9031.00')

    const see = page.getByRole('region', { name: 'See it work.' })
    for (const img of await see.getByRole('img').all()) {
      await expect(img).toHaveAttribute('alt', /.{40,}/)
      await expect(img).toHaveAttribute('loading', 'lazy')
    }

    const money = page.getByRole('region', { name: 'The money comes last.' })
    await expect(money).toContainText('$135.00 collected · $8.19 PayPal fees · $126.81 net')
    await expect(money).toContainText('$126.81 sent')

    const judges = page.getByRole('region', { name: 'For judges.' })
    const paths = judges.getByRole('listitem')
    await expect(paths).toHaveCount(2)
    await expect(paths.first()).toHaveText('30 seconds: type an answer in the chat above.')
    await expect(paths.last()).toContainText('About 4 minutes: Try it as Iris, read her group chat, then 3 PayPal checkouts. PayPal remembers your login after the first.')
    await expect(judges.getByRole('link', { name: 'Code on GitHub' })).toHaveAttribute('href', 'https://github.com/aalvaropc/commitpool')
    await expect(judges.getByRole('link', { name: 'Devpost' })).toHaveAttribute('href', 'https://devpost.com/software/commitpool-e2e')
    await expect(page.getByRole('navigation', { name: 'Project links' }).getByRole('link', { name: 'Code on GitHub' })).toBeVisible()
    await expect(judges.getByRole('heading', { name: 'PayPal Sandbox buyer' })).toBeVisible()
    await expect(judges).toContainText('judge-buyer@example.com')
    await expect(judges).not.toContainText('e2e-sandbox-password')
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    await judges.getByRole('button', { name: 'Copy the Sandbox buyer password' }).click()
    await expect(judges.getByRole('status').filter({ hasText: 'Password copied.' })).toHaveCount(1)
    expect(String(await page.evaluate('navigator.clipboard.readText()'))).toBe('e2e-sandbox-password')

    await expect(judges.locator('iframe')).toHaveCount(0)
    await expect(judges.getByRole('button', { name: 'Watch the demo video' }).locator('img')).toHaveAttribute('src', 'https://i.ytimg.com/vi/aB3_x-9Zk0Q/hqdefault.jpg')
    await judges.getByRole('button', { name: 'Watch the demo video' }).click()
    const video = judges.locator('iframe')
    await expect(video).toBeFocused()
    await expect(video).toHaveAttribute('src', `${VIDEO}?autoplay=1`)
    await expect(video).toHaveAttribute('loading', 'lazy')
    await expect(video).toHaveAttribute('referrerpolicy', 'strict-origin-when-cross-origin')
    await expect(video).toHaveAttribute('title', /^CommitPool demo video/)

    const truth = judges.getByRole('table')
    await expect(truth.getByRole('row', { name: /^PayPal Sandbox/ })).toContainText('Real Sandbox calls')
    await expect(truth.getByRole('row', { name: /Claude on Amazon Bedrock/ })).toContainText('Real')
    await expect(truth.getByRole('row', { name: /Demo participants/ })).toContainText('Prepared')
    await expect(truth.getByRole('row', { name: /Close reservations now/ })).toContainText('Accelerated clock')
    await expect(truth.getByRole('row', { name: /Fail next charge/ })).toContainText('Simulated failure')
    await expect(truth.getByRole('row', { name: /Emails/ })).toContainText('Logged, not sent')

    const apis = page.getByRole('region', { name: 'PayPal, for real.' })
    await expect(apis).toContainText('create · authorize')
    await expect(apis).toContainText('capture · void · refund')
    await expect(apis).toContainText('9 of 9 records match PayPal')
    await expect(apis).toContainText('QW74P22YPEA8W')
  })

  test('without a video or a buyer login the judge path points to the testing instructions', async ({ page }) => {
    await page.route('**/api/v1/demo', (route) => (route.request().method() === 'GET' ? route.fulfill({ json: { workshops: true } }) : route.fallback()))
    await page.goto('/')
    const judges = page.getByRole('region', { name: 'For judges.' })
    await expect(judges).toContainText('The PayPal Sandbox buyer login for the 4-minute path is in the testing instructions on Devpost.')
    await expect(judges.getByRole('heading', { name: 'PayPal Sandbox buyer' })).toHaveCount(0)
    await expect(judges.getByRole('button', { name: /^Copy/ })).toHaveCount(0)
    await expect(judges.locator('iframe')).toHaveCount(0)
    await expect(judges.getByRole('button', { name: 'Watch the demo video' })).toHaveCount(0)
    await expect(judges.getByRole('link', { name: 'Devpost' })).toHaveCount(0)
    await expect(judges.getByRole('link', { name: 'Code on GitHub' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Try it as Iris' }).first()).toBeVisible()
  })
})
