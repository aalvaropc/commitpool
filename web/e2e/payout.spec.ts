import type { BrowserContext } from '@playwright/test'
import { expectAccessible } from './a11y'
import { approveCheckout, createWorkshop, expect, headline, importDemoChat, openPayments, sendAvailability, startCheckout, test } from './helpers'

test.describe('payout', () => {
  test('pays the organizer what people paid, less PayPal fees, when the class starts', async ({ page: owner, join }) => {
    const workshop = await createWorkshop(owner, 'Pottery night', 'iris@example.com')
    const participants = []
    for (const name of ['Ada', 'Ben', 'Cleo']) {
      const p = await join(workshop.shareURL, name)
      await sendAvailability(p)
      participants.push(p)
    }
    await openPayments(owner, workshop, 3)
    for (const p of participants) {
      await startCheckout(p)
      await approveCheckout(p)
    }
    await expect(headline(owner)).toHaveText('It’s on', { timeout: 60_000 })

    const payout = owner.getByRole('region', { name: 'Your payout' })
    await expect(payout.getByRole('status')).toHaveText(/^Paid out when the class starts, /)
    await expect(payout).toContainText('CommitPool sends $27.48 to iris@example.com through PayPal.')
    await expect(payout).toContainText('So far: $30.00 paid by 3 people · $2.52 PayPal fees · $27.48 to you')

    await owner.getByRole('button', { name: /Close reservations now/ }).click()
    const cancel = owner.getByRole('button', { name: 'Cancel workshop…' })
    await expect(cancel).toBeVisible()
    const start = owner.getByRole('button', { name: /Start the class now/ })
    await expect(start).toBeVisible()
    await expect(owner.getByText('Demo clock: jumps ahead to the class start')).toBeVisible()
    await start.click()

    await expect(payout.getByRole('status')).toHaveText(/^Paid out \$27\.48 to iris@example\.com · PayPal batch \w+$/, { timeout: 30_000 })
    await expect(payout.getByText('Paid out', { exact: true })).toBeVisible()
    await expect(start).toBeHidden()
    await expect(cancel).toBeHidden()
    await payout.getByText('PayPal details').click()
    await expect(payout.getByText('Transaction', { exact: true })).toBeVisible()
    await expectAccessible(owner, 'the organizer page once paid out')
  })

  test('the demo guide leads from It’s on to the payout', async ({ page: owner, browser }) => {
    await owner.goto('/')
    await owner.getByRole('button', { name: 'Try it as Iris' }).first().click()
    await importDemoChat(owner)
    await expect(owner.getByText('6 of 6 answered', { exact: false })).toBeVisible({ timeout: 30_000 })
    const ownerURL = owner.url()
    const guide = owner.getByRole('region', { name: 'You are Iris now' })
    const contexts: BrowserContext[] = []
    const actAs = async (name: string) => {
      const context = await browser.newContext()
      contexts.push(context)
      const page = await context.newPage()
      await page.goto((await guide.getByRole('link', { name: new RegExp(`Act as ${name}`) }).getAttribute('href')) ?? '/')
      await expect(page.getByText(`You joined as ${name}.`)).toBeVisible()
      return page
    }
    try {
      const grace = await actAs('Grace Liu')
      await grace.getByLabel('Your answer to Claude’s question').fill('No problem — I swapped Saturday. Sunday I’m still at the market.')
      await grace.getByRole('button', { name: 'Send my answer' }).click()
      await expect(grace.getByRole('button', { name: 'Update my availability' })).toBeVisible()

      await owner.goto(ownerURL)
      await owner.getByRole('region', { name: 'Next step' }).getByRole('button', { name: /^Open reservations for Sat/ }).click()
      await owner.getByRole('dialog').getByRole('button', { name: 'Open reservations', exact: true }).click()
      await expect(headline(owner)).toHaveText('Reservations open')

      for (const name of ['Priya Raman', 'Ben Whitaker', 'Grace Liu']) {
        const page = name === 'Grace Liu' ? grace : await actAs(name)
        await startCheckout({ name, page })
        await approveCheckout({ name, page })
      }
      await expect(headline(owner)).toHaveText('It’s on', { timeout: 60_000 })

      await expect(guide).toContainText('Close reservations now to end the reservation window.')
      await expect(owner.getByRole('heading', { name: 'Demo controls' })).toHaveCount(0)
      await guide.getByRole('button', { name: /Close reservations now/ }).click()
      await expect(guide).toContainText('Start the class now. The demo clock jumps to the start and CommitPool pays Iris.')
      await expect(guide).not.toContainText('Reservations closed.')
      await expect(guide.getByRole('button', { name: /Close reservations now/ })).toHaveCount(0)
      await expectAccessible(owner, 'the demo guide pointing at Start the class now')
      await owner.setViewportSize({ width: 390, height: 844 })
      const start = guide.getByRole('button', { name: /Start the class now/ })
      expect((await start.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(44)
      expect(await owner.evaluate('document.documentElement.scrollWidth <= window.innerWidth')).toBe(true)
      await start.click()

      await expect(headline(owner)).toHaveText(/^Class started · \$\d+\.\d{2} paid out to Iris$/, { timeout: 30_000 })
      await expect(owner.getByRole('region', { name: 'Workshop status' })).toContainText(/PayPal batch \w+ · \w+/)
      await expect(guide.getByRole('button', { name: /Start the class now/ })).toHaveCount(0)
      await expect(guide).not.toContainText('The class has started')
      await expect(guide).toContainText('That’s the whole loop: the chat read, the date found, the holds charged and the money paid out to Iris.')
      await expect(guide.getByRole('link', { name: 'Try it as Iris' })).toHaveAttribute('href', '/')
      await expectAccessible(owner, 'the demo organizer page once paid out')
    } finally {
      await Promise.all(contexts.map((c) => c.close()))
    }
  })
})
