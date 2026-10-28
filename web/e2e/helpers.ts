import { expect, test as base, type BrowserContext, type Locator, type Page } from '@playwright/test'

export { expect }

/** Paid seats every journey's workshop needs; the form's other defaults stand. */
const MINIMUM = 3

/** A message the fake interpreter reads as "can come" on every candidate date. */
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
  /**
   * Joins from the share link in a browser context of its own, so each
   * participant keeps their token in their own sessionStorage.
   */
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
      await page.getByRole('button', { name: 'Join workshop' }).click()
      await expect(page.getByText(`You joined as ${name}.`)).toBeVisible()
      return { name, page }
    })
    await Promise.all(contexts.map((c) => c.close()))
  },
})

/** Creates a workshop through the form and returns the organizer and share links it shows. */
export async function createWorkshop(owner: Page, title: string): Promise<Workshop> {
  await owner.goto('/new')
  await owner.getByLabel('Title', { exact: true }).fill(title)
  await owner.getByLabel('Minimum paid seats').fill(String(MINIMUM))
  await owner.getByRole('button', { name: 'Create workshop' }).click()
  await expect(owner.getByRole('heading', { name: `${title} is ready` })).toBeVisible()
  return {
    ownerURL: await owner.getByRole('textbox', { name: 'Organizer link (keep it private)', exact: true }).inputValue(),
    shareURL: await owner.getByRole('textbox', { name: 'Link for participants', exact: true }).inputValue(),
  }
}

export async function sendAvailability(p: Participant): Promise<void> {
  await p.page.getByLabel('When can you come?').fill(AVAILABLE_ON_EVERY_DATE)
  await p.page.getByRole('button', { name: 'Send my availability' }).click()
  await expect(p.page.getByRole('heading', { name: 'How we read it' })).toBeVisible()
}

/** Opens the organizer page and opens payments on the recommended date, once canCome people can come to it. */
export async function openPayments(owner: Page, workshop: Workshop, canCome: number): Promise<void> {
  await owner.goto(workshop.ownerURL)
  const recommended = owner.getByRole('listitem').filter({ has: owner.getByText('Recommended', { exact: true }) })
  await expect(recommended).toContainText(`${canCome} can come`)
  await recommended.getByRole('button', { name: 'Open payments for this date' }).click()
  await recommended.getByRole('button', { name: 'Open payments now' }).click()
  await expect(owner.getByRole('heading', { name: 'Payments opened' })).toBeVisible()
  await expect(headline(owner)).toHaveText('Payments are open')
}

/** Reloads the participant page, which does not poll before payments open, and follows "Authorize with PayPal" to the fake checkout. */
export async function startCheckout(p: Participant): Promise<void> {
  await p.page.reload()
  await p.page.getByRole('button', { name: 'Authorize with PayPal' }).click()
  await expect(p.page.getByRole('heading', { name: 'Fake PayPal checkout' })).toBeVisible()
}

/** Approves at the fake checkout, which redirects back through /paypal/return, and goes back to the participant page. */
export async function approveCheckout(p: Participant): Promise<void> {
  await p.page.getByRole('button', { name: 'Approve' }).click()
  await expect(p.page.getByRole('heading', { name: 'Back from PayPal' })).toBeVisible()
  await expect(p.page.getByText('Hold authorized', { exact: true })).toBeVisible()
  await p.page.getByRole('link', { name: 'Go to your workshop page' }).click()
  await expect(p.page.getByText(`You joined as ${p.name}.`)).toBeVisible()
}

/** Uses the organizer's demo control so the participant's next charge fails at PayPal. */
export async function failNextCharge(owner: Page, name: string): Promise<void> {
  await enrollmentRow(owner, name).getByRole('button', { name: /Fail next charge/ }).click()
  await expect(owner.getByText(`The next charge for ${name} will fail.`)).toBeVisible()
}

/** The one-line workshop state at the top of the organizer and participant pages. */
export function headline(page: Page): Locator {
  return page.getByRole('region', { name: 'Workshop status' }).getByRole('status')
}

/** The participant's row in the organizer's payments list. */
export function enrollmentRow(owner: Page, name: string): Locator {
  return owner
    .getByRole('region', { name: 'Payments', exact: true })
    .getByRole('listitem')
    .filter({ has: owner.getByText(name, { exact: true }) })
}

/** The paid count in the organizer's seat legend, such as "3 paid". */
export function paidSeats(owner: Page, paid: number): Locator {
  return owner.getByRole('region', { name: 'Seats', exact: true }).getByText(`${paid} paid`, { exact: true })
}
