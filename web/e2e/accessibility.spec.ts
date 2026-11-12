import { expectAccessible } from './a11y'
import type { Page } from '@playwright/test'
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

test.describe('landing at phone and desktop width', () => {
  for (const viewport of [
    { width: 390, height: 844 },
    { width: 1440, height: 900 },
  ]) {
    test(`the landing at ${viewport.width} px passes axe, fits the width and keeps 44 px targets`, async ({ page }) => {
      await page.route('https://i.ytimg.com/**', (route) => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="480" height="360"/>' }))
      await page.setViewportSize(viewport)
      await page.emulateMedia({ reducedMotion: 'reduce' })
      await page.goto('/')
      await expect(page.getByRole('button', { name: 'Try it as Iris' }).first()).toBeVisible()
      await page.getByRole('region', { name: 'Example: answers to Iris’s invite' }).scrollIntoViewIfNeeded()
      await expect(page.getByRole('group', { name: 'Quick replies' })).toBeVisible()
      await expect(page.getByText('Pick one or write your own, in any language.')).toBeVisible()
      await expectAccessible(page, `the landing page at ${viewport.width} px`)

      const overflow = Number(await page.evaluate('document.documentElement.scrollWidth - window.innerWidth'))
      expect(overflow, 'the landing scrolls sideways').toBeLessThanOrEqual(0)

      const small = (await page.evaluate(`[...document.querySelectorAll('header a, header button, main a, main button, main summary, main input')]
        .filter((el) => el.offsetParent !== null && !el.closest('[aria-hidden="true"]'))
        .map((el) => ({ el, box: el.getBoundingClientRect() }))
        .filter(({ box }) => box.height < 44 || box.width < 44)
        .map(({ el, box }) => el.tagName.toLowerCase() + ' ' + (el.getAttribute('aria-label') || el.textContent || '').trim().slice(0, 40) + ' ' + Math.round(box.width) + 'x' + Math.round(box.height))`)) as string[]
      expect(small, 'targets smaller than 44 px').toEqual([])
    })
  }
})

async function fitsAPhone(page: Page, where: string): Promise<void> {
  await expectAccessible(page, where)
  const overflow = Number(await page.evaluate('document.documentElement.scrollWidth - window.innerWidth'))
  expect(overflow, `${where} scrolls sideways`).toBeLessThanOrEqual(0)
  const small = (await page.evaluate(`[...document.querySelectorAll('main a, main button, main summary, main input, main textarea')]
    .filter((el) => el.offsetParent !== null && !el.closest('[aria-hidden="true"]') && !(el.matches('a') && el.closest('p')))
    .map((el) => ({ el, box: (el.matches('input[type=checkbox]') ? el.closest('label') ?? el : el).getBoundingClientRect() }))
    .filter(({ box }) => box.height < 44 || box.width < 44)
    .map(({ el, box }) => el.tagName.toLowerCase() + ' ' + (el.getAttribute('aria-label') || el.textContent || '').trim().slice(0, 40) + ' ' + Math.round(box.width) + 'x' + Math.round(box.height))`)) as string[]
  expect(small, `targets smaller than 44 px on ${where}`).toEqual([])
}

test.describe('the demo board at phone width', () => {
  test('Iris’s chat, its preview and the board at 390 px pass axe, fit the width and keep 44 px targets', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')
    await page.getByRole('button', { name: 'Try it as Iris' }).first().click()
    const card = page.getByRole('region', { name: 'Paste your group chat' })
    await expect(card.getByRole('textbox', { name: 'Paste your group chat' })).toHaveValue(/Iris Calder/)
    await fitsAPhone(page, 'a new demo at 390 px')

    await card.getByRole('button', { name: 'Read the chat' }).click()
    await expect(card.getByRole('checkbox')).toHaveCount(7)
    await fitsAPhone(page, 'the chat preview at 390 px')

    await card.getByRole('button', { name: 'Add 6 people' }).click()
    await expect(page.getByText('6 of 6 answered', { exact: false })).toBeVisible()
    await expect(page.getByRole('list', { name: 'People' }).getByRole('button', { name: 'Copy their link for Grace Liu' })).toBeVisible()
    await fitsAPhone(page, 'the demo board at 390 px')
  })
})
