import path from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { expectAccessible } from './a11y'
import { createdLinks, createWorkshop, expect, fillTheFormMyself, test } from './helpers'

const fixture = path.join(path.dirname(fileURLToPath(import.meta.url)), 'fixtures', 'whatsapp-ios.txt')

function chatCard(page: Page) {
  return page.getByRole('region', { name: 'Paste your group chat' })
}

async function readingHalfway(page: Page): Promise<() => Promise<void>> {
  const ownerView = /\/api\/v1\/owner\/[^/]+$/
  await page.route(ownerView, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    const response = await route.fetch()
    const view = await response.json()
    if (view.participants.length === 6) {
      const read = new Set(view.participants.slice(0, 2).map((p: { id: string }) => p.id))
      view.availabilities = view.availabilities.filter((a: { participant_id: string }) => read.has(a.participant_id))
      view.participants = view.participants.map((p: { id: string }) => ({ ...p, unread: !read.has(p.id) }))
      view.reading = { pending: 4, total: 6, active: true }
    }
    await route.fulfill({ response, json: view })
  })
  return () => page.unroute(ownerView)
}

test.describe('paste your group chat', () => {
  test('the demo starts from Iris’s chat: Claude reads her six people and Grace needs clarifying', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: 'Try it as Iris' }).first().click()
    await expect(page.getByRole('heading', { level: 1, name: 'Life drawing for beginners' })).toBeVisible()
    await expect(page.getByRole('region', { name: 'You are Iris now' })).toContainText("This is Iris's group chat. Press Read the chat, then Add 6 people.")
    const card = chatCard(page)
    await expect(card.getByRole('textbox', { name: 'Paste your group chat' })).toHaveValue(/\] Iris Calder: Life drawing at The Long Room, Saturday or Sunday 10–12, \$45\. Who's in\?\n/)
    await expect(card).toContainText("Paste the messages, or upload WhatsApp's Export chat (.txt, without media). CommitPool finds who said what; Claude reads each person against your dates.")
    await expect(page.getByRole('region', { name: 'Who can come', exact: true })).toContainText('Paste your group chat above, or share the invite.')
    await expectAccessible(page, 'a new demo with Iris’s group chat')

    await card.getByRole('button', { name: 'Read the chat' }).click()
    await expect(card.getByRole('checkbox')).toHaveCount(7)
    const iris = card.getByRole('checkbox', { name: /^Iris Calder · you/ })
    await expect(iris).not.toBeChecked()
    await expect(iris).toBeDisabled()
    await expect(card.getByRole('checkbox', { checked: true })).toHaveCount(6)
    await expect(card.getByRole('checkbox', { name: /^Lucía Torres “El sábado no puedo/ })).toBeChecked()
    await expectAccessible(page, 'the preview of Iris’s chat')

    const stopFaking = await readingHalfway(page)
    await card.getByRole('button', { name: 'Add 6 people' }).click()
    await expect(page.getByText('Claude is reading the chat… 2 of 6')).toBeVisible()
    await expect(page.getByRole('region', { name: 'Who can come, by date' }).getByText('Claude is reading…')).toHaveCount(4)
    await expectAccessible(page, 'the board while Claude reads the chat')
    await stopFaking()

    await expect(page.getByText('6 of 6 answered', { exact: false })).toBeVisible()
    await expect(page.getByText(/^Claude is reading the chat…/)).toHaveCount(0)
    await expect(page.getByRole('region', { name: 'Paste more of the chat' })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Paste more of the chat' })).toContainText('Added 6 people.')
    const grid = page.getByRole('region', { name: 'Who can come, by date' })
    await expect(grid.getByRole('row').filter({ hasText: 'Grace Liu' })).toContainText('Needs clarifying')
    await expect(grid.getByRole('button', { name: 'Copy their link for Grace Liu' })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Next step' })).toContainText('Grace Liu’s message needs clarifying.')
    await expect(page.getByRole('region', { name: 'You are Iris now' })).toContainText('Act as Grace Liu and answer Claude’s question')
    await expectAccessible(page, 'the demo board after the chat was read')
  })

  test('a real class: three pasted lines become three people, each with their own link', async ({ page: owner }) => {
    const workshop = await createWorkshop(owner, 'Sourdough basics')
    await owner.goto(workshop.ownerURL)
    const card = chatCard(owner)
    await expect(card.getByRole('textbox', { name: 'Paste your group chat' })).toHaveValue('')
    await card.getByRole('button', { name: 'Read the chat' }).click()
    await expect(card.getByRole('alert')).toHaveText('Paste the group chat first.')

    await card
      .getByRole('textbox', { name: 'Paste your group chat' })
      .fill(['Ada: Any of the dates works for me.', 'Ben: Any of the dates works for me.', 'Cleo: Any of the dates works for me.'].join('\n'))
    await card.getByRole('button', { name: 'Read the chat' }).click()
    await expect(card.getByRole('checkbox', { checked: true })).toHaveCount(3)
    await card.getByRole('button', { name: 'Add 3 people' }).click()
    await expect(owner.getByText('3 of 3 answered', { exact: false })).toBeVisible()
    await expect(owner.getByRole('region', { name: 'Next step' })).toContainText('works for 3 — you need 3')

    await owner.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    await owner.getByRole('button', { name: 'Copy their link for Ada' }).click()
    await expect(owner.getByText('Copied Ada’s link.', { exact: false })).toBeVisible()
    const link = String(await owner.evaluate('navigator.clipboard.readText()'))
    expect(link).toMatch(/\/p\/[\w-]+$/)
    const ada = await owner.context().newPage()
    await ada.goto(link)
    await expect(ada.getByText('You joined as Ada.')).toBeVisible()
    const said = ada.getByRole('region', { name: 'What you said in the chat' })
    await expect(said).toContainText('The organizer added this from the group chat:')
    await expect(said).toContainText('Any of the dates works for me.')
    await expect(said).toContainText("If it's wrong, say it again in your own words.")
    await expectAccessible(ada, 'the page of someone added from the chat')
    await ada.close()

    await owner.getByRole('button', { name: 'Remove Cleo' }).click()
    const dialog = owner.getByRole('dialog', { name: 'Remove Cleo?' })
    await expectAccessible(owner, 'the remove dialog')
    await dialog.getByRole('button', { name: 'Remove', exact: true }).click()
    await expect(dialog).toBeHidden()
    await expect(owner.getByText('2 of 2 answered', { exact: false })).toBeVisible()
    await expect(owner.getByRole('button', { name: 'Remove Cleo' })).toHaveCount(0)

    const more = owner.getByRole('region', { name: 'Paste more of the chat' })
    await more.getByText('Paste more of the chat').click()
    await more.getByRole('textbox', { name: 'Paste more of the chat' }).fill('Ada: Sunday works.\nBen: Sunday works.')
    await more.getByRole('button', { name: 'Read the chat' }).click()
    await expect(more.getByRole('checkbox', { name: /^Ada · already here/ })).toBeDisabled()
    await expect(more.getByRole('button', { name: 'Add 0 people' })).toBeDisabled()
  })

  test('uploads a WhatsApp export and leaves the organizer and system lines out', async ({ page: owner }) => {
    await fillTheFormMyself(owner)
    await owner.getByLabel('Title', { exact: true }).fill('Sourdough for two')
    await owner.getByLabel('Your name').fill('Rosa Vidal')
    await owner.getByLabel('Price per person, USD').fill('10')
    await owner.getByLabel('People needed to happen').fill('2')
    await owner.getByLabel('Spots').fill('8')
    await owner.getByRole('button', { name: 'Create workshop' }).click()
    const workshop = await createdLinks(owner, 'Sourdough for two')
    await owner.goto(workshop.ownerURL)

    const card = chatCard(owner)
    const zip = owner.waitForEvent('filechooser')
    await card.getByRole('button', { name: 'Upload the export (.txt)' }).click()
    await (await zip).setFiles({ name: 'WhatsApp Chat - Bread club.zip', mimeType: 'application/zip', buffer: Buffer.from([0x50, 0x4b, 0x03, 0x04]) })
    await expect(card.getByRole('alert')).toHaveText("That's a .zip. Export the chat without media to get a .txt, or paste the messages.")

    const chooser = owner.waitForEvent('filechooser')
    await card.getByRole('button', { name: 'Upload the export (.txt)' }).click()
    await (await chooser).setFiles(fixture)
    await expect(card.getByRole('textbox', { name: 'Paste your group chat' })).toHaveValue(/Kofi Mensah: Any of the dates works for me\./)
    await expect(card.getByRole('alert')).toHaveCount(0)
    await card.getByRole('button', { name: 'Read the chat' }).click()
    await expect(card.getByRole('checkbox')).toHaveCount(3)
    await expect(card.getByRole('checkbox', { name: /^Rosa Vidal · you/ })).not.toBeChecked()
    await expect(card).toContainText('María José 🍞')
    await expect(card).toContainText('lines left out')
    await card.getByRole('button', { name: 'Add 2 people' }).click()
    await expect(owner.getByText('2 of 2 answered', { exact: false })).toBeVisible()
    const grid = owner.getByRole('region', { name: 'Who can come, by date' })
    await expect(grid.getByRole('row').filter({ hasText: 'María José 🍞' })).toContainText('Can bring my own starter too')
    await expect(grid.getByRole('row').filter({ hasText: 'Rosa Vidal' })).toHaveCount(0)
  })
})
