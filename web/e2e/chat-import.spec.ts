import path from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { expectAccessible } from './a11y'
import { createdLinks, createWorkshop, expect, fillTheFormMyself, importDemoChat, test } from './helpers'

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
  return () => page.unrouteAll({ behavior: 'ignoreErrors' })
}

test.describe('the group chat on the first screen', () => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    test(`right after Try it as Iris, Read the chat is visible without scrolling at ${viewport.width} px`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await page.goto('/')
      await page.getByRole('button', { name: 'Try it as Iris' }).first().click()
      const read = chatCard(page).getByRole('button', { name: 'Read the chat' })
      await expect(read).toBeVisible()
      await expect(chatCard(page)).toContainText('This is Iris’s group chat. Press Read the chat, then Add 6 people.')
      expect(Number(await page.evaluate('window.scrollY'))).toBe(0)
      const box = await read.boundingBox()
      expect(box, 'Read the chat has a box').not.toBeNull()
      expect(box!.y + box!.height, `Read the chat ends at ${box!.y + box!.height} px of ${viewport.height}`).toBeLessThanOrEqual(viewport.height)
      await expect(page.getByRole('region', { name: 'Workshop status' })).toHaveText('No answers yet. Paste the group chat, or share the invite.')
    })
  }
})

test.describe('paste your group chat', () => {
  test('the demo starts from Iris’s chat: Claude reads her six people and Grace needs clarifying', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: 'Try it as Iris' }).first().click()
    await expect(page.getByRole('heading', { level: 1, name: 'Life drawing for beginners' })).toBeVisible()
    await expect(page.getByText('You are Iris now', { exact: true })).toBeVisible()
    const card = chatCard(page)
    await expect(card).toContainText('This is Iris’s group chat. Press Read the chat, then Add 6 people.')
    await expect(card.getByRole('textbox', { name: 'Paste your group chat' })).toHaveValue(/\] Iris Calder: Life drawing at The Long Room, Saturday or Sunday 10–12, \$45\. Who's in\?\n/)
    await expect(card).toContainText('Paste the messages, or upload WhatsApp’s Export chat (.txt, without media). CommitPool finds who said what; Claude reads each person against your dates.')
    await expect(page.getByRole('region', { name: 'Who can come', exact: true })).toContainText('Paste your group chat above, or share the invite.')
    await expectAccessible(page, 'a new demo with Iris’s group chat')

    await card.getByRole('button', { name: 'Read the chat' }).click()
    await expect(card.getByRole('checkbox')).toHaveCount(7)
    await expect(card.getByRole('heading', { name: '7 people in the chat' })).toBeFocused()
    const iris = card.getByRole('checkbox', { name: /^Iris Calder · you/ })
    await expect(iris).not.toBeChecked()
    await expect(iris).toBeDisabled()
    await expect(card.getByRole('checkbox', { checked: true })).toHaveCount(6)
    await expect(card.getByRole('checkbox', { name: /^Lucía Torres “El sábado no puedo/ })).toBeChecked()
    await expectAccessible(page, 'the preview of Iris’s chat')

    const stopFaking = await readingHalfway(page)
    try {
      await card.getByRole('button', { name: 'Add 6 people' }).click()
      await expect(page.getByText('Claude is reading the chat… 2 of 6')).toBeVisible()
      await expect(page.getByRole('heading', { name: 'Who can come', exact: true })).toBeFocused()
      await expect(page.getByRole('region', { name: 'Who can come, by date' }).getByText('Claude is reading…')).toHaveCount(4)
      await expect(page.getByRole('region', { name: 'You are Iris now' })).toContainText('Claude is reading each person’s messages against Iris’s dates.')
      await expectAccessible(page, 'the board while Claude reads the chat')
    } finally {
      await stopFaking()
    }

    await expect(page.getByText('6 of 6 answered', { exact: false })).toBeVisible()
    await expect(page.getByText(/^Claude is reading the chat…/)).toHaveCount(0)
    const more = page.getByRole('region', { name: 'Paste more of the chat' })
    await expect(more).toBeVisible()
    await expect(more.getByRole('textbox')).toBeHidden()
    const grid = page.getByRole('region', { name: 'Who can come, by date' })
    await expect(grid.getByRole('row').filter({ hasText: 'Grace Liu' })).toContainText('Needs clarifying')
    await expect(grid.getByRole('button', { name: 'Copy their link for Grace Liu' })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Next step' })).toContainText('Grace Liu’s message needs clarifying.')
    await expect(page.getByRole('region', { name: 'You are Iris now' })).toContainText(
      'Copy Claude’s reply for the group. Grace answered in the chat. Paste the updated chat, or act as Grace to answer on her page.',
    )
    await expect(page.getByRole('region', { name: 'Claude suggests' }).getByRole('listitem').filter({ hasText: 'Claude’s reply for the group' })).toContainText('Copy message for the group')
    await expectAccessible(page, 'the demo board after the chat was read')
  })

  test('pasting Iris’s chat again with Grace’s answer reads only her new message, at 390 px', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/')
    await page.getByRole('button', { name: 'Try it as Iris' }).first().click()
    await importDemoChat(page)
    await expect(page.getByText('6 of 6 answered', { exact: false })).toBeVisible()
    const guide = page.getByRole('region', { name: 'You are Iris now' })
    await expect(guide).toContainText('Grace answered in the chat. Paste the updated chat, or act as Grace to answer on her page.')
    await expect(guide.getByRole('link', { name: /Act as Grace Liu/ })).toBeVisible()

    const more = page.getByRole('region', { name: 'Paste more of the chat' })
    await more.getByText('Paste more of the chat').click()
    const box = more.getByRole('textbox', { name: 'Paste more of the chat' })
    await expect(box).toHaveValue(/\n\[[^\]]+, 7:52:07\u202fPM\] Grace Liu: No problem — I swapped Saturday\. Sunday I'm still at the market\.$/)
    await expect(more).toContainText('Grace’s answer is now the last line of Iris’s chat. Press Read the chat, then Read new messages from 1 person.')
    await expectAccessible(page, 'the demo board with Iris’s updated chat')

    await more.getByRole('button', { name: 'Read the chat' }).click()
    await expect(more.getByRole('heading', { name: '7 people in the chat' })).toBeFocused()
    const grace = more.getByRole('checkbox', { name: /^Grace Liu · new messages “No problem — I swapped Saturday\./ })
    await expect(grace).toBeChecked()
    await expect(grace).toBeEnabled()
    await expect(more.getByRole('checkbox', { name: /^Priya Raman · already here/ })).toBeDisabled()
    await expect(more.getByRole('checkbox', { checked: true })).toHaveCount(1)
    const read = more.getByRole('button', { name: 'Read new messages from 1 person' })
    const box44 = await read.boundingBox()
    expect(box44?.height ?? 0).toBeGreaterThanOrEqual(44)
    const overflow = Number(await page.evaluate('document.documentElement.scrollWidth - window.innerWidth'))
    expect(overflow, 'the board scrolls sideways at 390 px').toBeLessThanOrEqual(0)
    await expectAccessible(page, 'the preview of Iris’s updated chat')

    await read.click()
    await expect(page.getByRole('heading', { name: 'Who can come', exact: true })).toBeFocused()
    await expect(page.getByRole('region', { name: 'Next step' })).toContainText('works for 3 — you need 3')
    await expect(guide).toContainText('Open reservations, then reserve as someone who can come.')
    const card = page.getByRole('list', { name: 'People' }).getByRole('listitem').filter({ hasText: 'Grace Liu' })
    await expect(card).toContainText('No problem — I swapped Saturday.')
    await expect(card).not.toContainText('Needs clarifying')
    await expect(page.getByRole('region', { name: /^Paste / })).toHaveCount(0)
    await expectAccessible(page, 'the demo board after Grace’s answer from the chat')
  })

  test('a demo emptied after its one chat does not offer the chat again', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('button', { name: 'Try it as Iris' }).first().click()
    await importDemoChat(page)
    await expect(page.getByText('6 of 6 answered', { exact: false })).toBeVisible()
    const token = new URL(page.url()).pathname.split('/').pop() ?? ''
    const view = await (await page.request.get(`/api/v1/owner/${token}`)).json()
    for (const p of view.participants as { id: string }[]) {
      expect((await page.request.delete(`/api/v1/owner/${token}/participants/${p.id}`)).status()).toBe(204)
    }
    await page.reload()
    const used = page.getByRole('region', { name: 'Iris’s group chat' })
    await expect(used).toHaveText(/^This demo already read Iris’s chat\. Start a new demo to read it again\./)
    await expect(page.getByRole('region', { name: 'Workshop status' })).toHaveText('No answers yet. Share the invite.')
    await expect(page.getByRole('region', { name: /^Paste / })).toHaveCount(0)
    await expectAccessible(page, 'an emptied demo board')
    await used.getByRole('link', { name: /Back to Try it as Iris/ }).click()
    await expect(page.getByRole('button', { name: 'Try it as Iris' }).first()).toBeVisible()
  })

  test('a real class: three pasted lines become three people, each with their own link', async ({ page: owner }) => {
    const workshop = await createWorkshop(owner, 'Sourdough basics')
    await owner.goto(workshop.ownerURL)
    const card = chatCard(owner)
    await expect(card.getByRole('textbox', { name: 'Paste your group chat' })).toHaveValue('')
    await expect(owner.getByRole('region', { name: 'Workshop status' })).toHaveText('No answers yet. Paste the group chat, or share the invite.')
    await expect(owner.getByRole('textbox', { name: 'Invite link', exact: true })).toHaveValue(workshop.shareURL)
    await expect(card).not.toContainText('Next in the demo')
    await card.getByRole('button', { name: 'Read the chat' }).click()
    await expect(card.getByRole('alert')).toHaveText('Paste the group chat first.')

    await card
      .getByRole('textbox', { name: 'Paste your group chat' })
      .fill(['Ada: Any of the dates works for me.', 'Ben: Any of the dates works for me.', 'Cleo: Any of the dates works for me.'].join('\n'))
    await card.getByRole('button', { name: 'Read the chat' }).click()
    await expect(card.getByRole('checkbox', { checked: true })).toHaveCount(3)
    await card.getByRole('button', { name: 'Add 3 people' }).click()
    await expect(owner.getByText('3 of 3 answered', { exact: false })).toBeVisible()
    await expect(owner.getByRole('region', { name: 'Next step' })).toContainText(/Tied: .* comes first.*(?<!\d)3 can come on/)

    await owner.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    const adaRow = owner.getByRole('region', { name: 'Who can come, by date' }).getByRole('row').filter({ hasText: 'Ada' })
    await adaRow.getByRole('button', { name: 'Copy their link for Ada' }).click()
    await expect(adaRow.getByRole('status')).toHaveText('Copied. Send it to Ada.')
    await expect(adaRow.getByRole('status')).toHaveCount(0, { timeout: 8000 })
    const link = String(await owner.evaluate('navigator.clipboard.readText()'))
    expect(link).toMatch(/\/p\/[\w-]+$/)
    const ada = await owner.context().newPage()
    await ada.goto(link)
    await expect(ada.getByText('You joined as Ada.')).toBeVisible()
    const said = ada.getByRole('region', { name: 'What you said in the chat' })
    await expect(said).toContainText('The organizer added this from the group chat:')
    await expect(said).toContainText('Any of the dates works for me.')
    await expect(said).toContainText('If it’s wrong, say it again in your own words.')
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
    await more.getByRole('textbox', { name: 'Paste more of the chat' }).fill('Ada: Sunday works.\nDev: Any of the dates works for me.')
    await more.getByRole('button', { name: 'Read the chat' }).click()
    await expect(more.getByRole('heading', { name: '2 people in the chat' })).toBeFocused()
    await expect(more.getByRole('checkbox', { name: /^Ada · already here/ })).toBeDisabled()
    await more.getByRole('button', { name: 'Add 1 person' }).click()
    await expect(owner.getByRole('heading', { name: 'Who can come', exact: true })).toBeFocused()
    await expect(owner.getByText('3 of 3 answered', { exact: false })).toBeVisible()
    await expect(more.getByRole('status')).toHaveText('Added 1 person.')
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
    await expect(card.getByRole('alert')).toHaveText('That’s a .zip. Export the chat without media to get a .txt, or paste the messages.')

    const latin1 = owner.waitForEvent('filechooser')
    await card.getByRole('button', { name: 'Upload the export (.txt)' }).click()
    await (await latin1).setFiles({ name: 'WhatsApp Chat - Bread club.txt', mimeType: 'text/plain', buffer: Buffer.from('Luc\xeda: El s\xe1bado s\xed puedo.', 'latin1') })
    await expect(card.getByRole('alert')).toHaveText('This file isn’t UTF-8 text. Export it again from WhatsApp, or paste the messages.')
    await expect(card.getByRole('textbox', { name: 'Paste your group chat' })).toHaveValue('')

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
