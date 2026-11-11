import { expectAccessible } from './a11y'
import { expect, headline, test } from './helpers'

const GRACE_REPLY = 'No problem — I swapped Saturday. Sunday I’m still at the market.'

test.describe('one-click demo', () => {
  test('a visitor becomes Iris, and Grace’s answer to Claude opens Saturday', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: 'Try it as Iris' }).first().click()

    await expect(page.getByRole('heading', { level: 1, name: 'Life drawing for beginners' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'You are Iris now' })).toBeVisible()
    await expect(page.getByText('Demo workshop · prepared data')).toBeVisible()
    await expect(page.getByText('6 of 6 answered', { exact: false })).toBeVisible({ timeout: 30_000 })

    const next = page.getByRole('region', { name: 'Next step' })
    await expect(next).toContainText(/Tied: Sat, \w{3} \d{1,2} comes first/)
    await expect(next).toContainText('You need 3.')
    await expect(next).toContainText('Grace Liu’s message needs clarifying.')
    await expect(page.getByRole('button', { name: /Open reservations/ })).toHaveCount(0)

    const suggests = page.getByRole('region', { name: 'Claude suggests' })
    await expect(suggests).toContainText('Grace Liu’s message needs clarifying.')
    await expect(suggests.getByRole('button', { name: 'Copy message for Grace Liu' })).toBeVisible()
    await expect(suggests).toContainText('Hi Grace! The class runs 10 AM–12 PM on Saturday and Sunday')
    await expect(suggests).toContainText('Hannah Kim said only if it ends by 11:30 AM')

    const grid = page.getByRole('region', { name: 'Who can come, by date' })
    await expect(grid).toContainText('Not counted — needs clarifying')
    await expect(grid).toContainText('Tied')

    const guide = page.getByRole('region', { name: 'You are Iris now' })
    await expect(guide).toContainText('Saturday has 2 of the 3 Iris needs. Act as Grace Liu and answer Claude’s question')
    await expect(guide).toContainText("for example: “No problem — I swapped Saturday. Sunday I'm still at the market.”")
    await expectAccessible(page, 'the demo organizer page')

    const ownerURL = page.url()
    const grace = guide.getByRole('link', { name: /Act as Grace Liu/ })
    await page.goto((await grace.getAttribute('href')) ?? '/')
    await expect(page.getByText('You joined as Grace Liu.')).toBeVisible()
    await expect(page.getByText('The class runs 10 AM–12 PM on Saturday and Sunday — can you make either morning, and which?')).toBeVisible()
    const answer = page.getByLabel('Your answer to Claude’s question')
    await expect(answer).toHaveValue('')
    await expectAccessible(page, 'Grace’s page with Claude’s question')
    await answer.fill(GRACE_REPLY)
    await page.getByRole('button', { name: 'Send my answer' }).click()
    await expect(page.getByRole('button', { name: 'Update my availability' })).toBeVisible()
    await expect(page.getByLabel('Your answer to Claude’s question')).toHaveCount(0)

    await page.goto(ownerURL)
    await expect(page.getByRole('region', { name: 'Next step' })).toContainText('works for 3 — you need 3')
    await expect(page.getByRole('button', { name: /Open reservations for Sat/ })).toBeVisible()
    await expect(page.getByRole('region', { name: 'You are Iris now' })).toContainText('Open reservations, then reserve as someone who can come.')

    await page.getByRole('region', { name: 'Next step' }).getByRole('button', { name: /^Open reservations for Sat/ }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Open reservations', exact: true }).click()
    await expect(headline(page)).toHaveText('Reservations open')
    await expect(page.getByRole('region', { name: 'Next step' }).getByRole('heading', { name: '0 of 3 reserved — 3 more and it’s on.' })).toBeFocused()
    await expect(page.getByRole('region', { name: 'You are Iris now' })).toContainText('Reserve as someone who can come: act as Priya Raman and reserve with PayPal.')
    const nudges = page.getByRole('region', { name: 'Claude suggests' })
    await expect(nudges).toContainText('3 people can come but haven’t reserved: ')
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    for (const name of ['Priya Raman', 'Ben Whitaker', 'Grace Liu']) {
      const first = name.split(' ')[0]
      await nudges.getByRole('button', { name: `Copy message for ${name}` }).click()
      await expect(nudges.getByRole('status')).toHaveText(`Copied the message for ${name}. Paste it where you talk to them.`)
      await expect(nudges).toContainText(`Message for ${first} — each one is written for that person`)
      expect(String(await page.evaluate('navigator.clipboard.readText()'))).toMatch(new RegExp(`^Hi ${first}! Reservations for Life drawing for beginners`))
    }
    await expect(page.getByRole('heading', { name: 'Terms people agreed to' })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Who can come, by date' })).not.toContainText('Reservations open')
    await expectAccessible(page, 'the demo organizer page with reservations open')
  })

  test('the organizer link brings the act-as links to another device', async ({ page, browser }) => {
    await page.goto('/')
    await page.getByRole('button', { name: 'Try it as Iris' }).first().click()
    await expect(page.getByRole('heading', { name: 'You are Iris now' })).toBeVisible()
    const links = page.getByRole('link', { name: /^Act as / })
    await expect(links.first()).toBeVisible()
    const names = await links.allTextContents()

    const elsewhere = await browser.newContext()
    try {
      const other = await elsewhere.newPage()
      await other.goto(page.url())
      await expect(other.getByRole('heading', { name: 'You are Iris now' })).toBeVisible()
      await expect(other.getByRole('link', { name: /^Act as / })).toHaveText(names)
    } finally {
      await elsewhere.close()
    }
  })
})
