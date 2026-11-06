import { expectAccessible } from './a11y'
import { createdLinks, expect, test } from './helpers'

test.describe('create a workshop', () => {
  test('Claude drafts the form from a description and the organizer checks it', async ({ page }) => {
    await page.goto('/new')
    await page.getByRole('button', { name: 'Use Maya’s example' }).click()
    await expect(page.getByLabel('Your workshop, in your own words')).toHaveValue(/Maya Okafor/)
    await page.getByRole('button', { name: 'Draft it with Claude' }).click()

    await expect(page.getByRole('heading', { name: 'Check your workshop' })).toBeFocused()
    await expect(page.getByText('Claude drafted this from your description.')).toBeVisible()
    await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Wheel-throwing for beginners')
    await expect(page.getByLabel('Your name')).toHaveValue('Maya Okafor')
    await expect(page.getByLabel('Price per person, USD')).toHaveValue('45')
    await expect(page.getByLabel('What the studio costs you, USD')).toHaveValue('120')
    await expect(page.getByLabel('Spots')).toHaveValue('6')
    await expect(page.getByLabel('Duration in minutes')).toHaveValue('120')
    await expect(page.getByLabel('Time zone')).toHaveValue('America/New_York')
    await expect(page.getByLabel('Day 1')).not.toHaveValue('')
    await expect(page.getByLabel('Starts at').first()).toHaveValue('10:00')
    await expect(page.getByText('3 × $45 = $135 covers your $120 studio.')).toBeVisible()
    await expectAccessible(page, 'the drafted form')

    await page.getByRole('button', { name: 'Create workshop' }).click()
    await expect(page.getByLabel('People needed to happen')).toBeFocused()
    await page.getByRole('button', { name: 'Set people needed to 3' }).click()
    await expect(page.getByLabel('People needed to happen')).toHaveValue('3')
    await expect(page.getByLabel('People needed to happen')).toBeFocused()

    await page.getByRole('button', { name: 'Add another date' }).click()
    await expect(page.getByLabel('Day 3')).toBeFocused()
    await page.getByRole('button', { name: 'Remove day 3' }).click()
    await expect(page.getByLabel('Day 3')).toHaveCount(0)
    await expect(page.getByLabel('Day 2')).toBeFocused()

    await page.getByLabel('Your email').fill('maya@example.test')
    await page.getByRole('button', { name: 'Create workshop' }).click()
    const links = await createdLinks(page, 'Wheel-throwing for beginners')
    await expect(page.getByText('We also sent it to maya@example.test.')).toBeVisible()
    await expect(page.getByRole('textbox', { name: 'Message for your group chat', exact: true })).toHaveValue(
      new RegExp(`only happens if 3 people reserve; otherwise nobody pays\\. Tell me when you can come: ${links.shareURL.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`),
    )
    await expectAccessible(page, 'the created page')

    await page.goto(links.shareURL)
    await expect(page.getByRole('heading', { level: 1, name: 'Wheel-throwing for beginners' })).toBeVisible()
    await expect(page.getByText('Maya Okafor').first()).toBeVisible()
  })

  test('a description that says nothing leaves the form for the organizer', async ({ page }) => {
    await page.goto('/new')
    await page.getByLabel('Your workshop, in your own words').fill('Something fun with friends')
    await page.getByRole('button', { name: 'Draft it with Claude' }).click()
    await expect(page.getByRole('heading', { name: 'Check your workshop' })).toBeFocused()
    await expect(page.getByText('Claude couldn’t tell from your description.').first()).toBeVisible()
    await page.getByRole('button', { name: 'Back to the description' }).click()
    await expect(page.getByLabel('Your workshop, in your own words')).toHaveValue('Something fun with friends')
  })
})
