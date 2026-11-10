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
