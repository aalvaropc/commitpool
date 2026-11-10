import { expect, test as base, type BrowserContext, type Locator, type Page } from '@playwright/test'

export { expect }

const MINIMUM = 3

const AVAILABLE_ON_EVERY_DATE = 'Any of the dates works for me.'

export interface Workshop {
  ownerURL: string
  shareURL: string
}

export interface Participant {
  name: string
  page: Page
}

export const test = base.extend<{
  join: (shareURL: string, name: string) => Promise<Participant>
}>({
  join: async ({ browser }, use) => {
    const contexts: BrowserContext[] = []
    await use(async (shareURL, name) => {
      const context = await browser.newContext()
      contexts.push(context)
      const page = await context.newPage()
      await page.goto(shareURL)
      await page.getByLabel('Your name').fill(name)
      await page.getByRole('button', { name: 'Join the class' }).click()
      await expect(page.getByText(`You joined as ${name}.`)).toBeVisible()
      return { name, page }
    })
    await Promise.all(contexts.map((c) => c.close()))
  },
})

export async function fillTheFormMyself(owner: Page): Promise<void> {
  await owner.goto('/new')
  await owner.getByRole('button', { name: 'Fill in the form myself' }).click()
  await expect(owner.getByRole('heading', { name: 'Check your workshop' })).toBeFocused()
}

export async function createdLinks(owner: Page, title: string): Promise<Workshop> {
  await expect(owner.getByRole('heading', { name: `${title} is ready` })).toBeVisible()
  return {
    ownerURL: await owner.getByRole('textbox', { name: 'Organizer link (keep it private)', exact: true }).inputValue(),
    shareURL: await owner.getByRole('textbox', { name: 'Invite link', exact: true }).inputValue(),
  }
}

export async function createWorkshop(owner: Page, title: string, payoutEmail?: string): Promise<Workshop> {
  await fillTheFormMyself(owner)
  await owner.getByLabel('Title', { exact: true }).fill(title)
  await owner.getByLabel('Price per person, USD').fill('10')
  await owner.getByLabel('People needed to happen').fill(String(MINIMUM))
  await owner.getByLabel('Spots').fill('8')
  if (payoutEmail) await owner.getByLabel('PayPal email for your payout').fill(payoutEmail)
  await owner.getByRole('button', { name: 'Create workshop' }).click()
  return createdLinks(owner, title)
}

export async function sendAvailability(p: Participant): Promise<void> {
  await p.page.getByLabel('When can you come?').fill(AVAILABLE_ON_EVERY_DATE)
  await p.page.getByRole('button', { name: 'Send my availability' }).click()
  await expect(p.page.getByRole('heading', { name: 'How we read it' })).toBeVisible()
}

export async function openPayments(owner: Page, workshop: Workshop, canCome: number): Promise<void> {
  await owner.goto(workshop.ownerURL)
  const next = owner.getByRole('region', { name: 'Next step' })
  await expect(next).toContainText(`works for ${canCome}`)
  await next.getByRole('button', { name: /^Open reservations for / }).click()
  await owner.getByRole('dialog').getByRole('button', { name: 'Open reservations', exact: true }).click()
  await expect(owner.getByRole('heading', { name: 'Terms people agreed to' })).toBeVisible()
  await expect(headline(owner)).toHaveText('Reservations open')
}

export async function startCheckout(p: Participant): Promise<void> {
  await p.page.reload()
  await p.page.getByRole('button', { name: 'Reserve with PayPal' }).click()
  await expect(p.page.getByRole('heading', { name: 'Fake PayPal checkout' })).toBeVisible()
}

export async function approveCheckout(p: Participant): Promise<void> {
  await p.page.getByRole('button', { name: 'Approve' }).click()
  await expect(p.page.getByRole('heading', { name: 'Back from PayPal' })).toBeVisible()
  await expect(p.page.getByText('Spot reserved', { exact: true })).toBeVisible()
  await p.page.getByRole('link', { name: 'Go to your workshop page' }).click()
  await expect(p.page.getByText(`You joined as ${p.name}.`)).toBeVisible()
}

export async function failNextCharge(owner: Page, name: string): Promise<void> {
  await enrollmentRow(owner, name).getByRole('button', { name: /Fail next charge/ }).click()
  await expect(owner.getByText(`The next charge for ${name} will fail.`)).toBeVisible()
}

export function headline(page: Page): Locator {
  return page.getByRole('region', { name: 'Workshop status' }).getByRole('status')
}

export function enrollmentRow(owner: Page, name: string): Locator {
  return owner
    .getByRole('region', { name: 'Payments', exact: true })
    .getByRole('listitem')
    .filter({ has: owner.getByText(name, { exact: true }) })
}

export function paidSeats(owner: Page, paid: number): Locator {
  return owner
    .getByRole('region', { name: 'Spots', exact: true })
    .locator('dl')
    .filter({ has: owner.locator('dt', { hasText: /^Paid$/ }) })
    .locator('dd', { hasText: new RegExp(`^${paid}$`) })
}
