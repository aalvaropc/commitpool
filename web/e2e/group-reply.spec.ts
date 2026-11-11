import { expectAccessible } from './a11y'
import { createWorkshop, expect, sendAvailability, test } from './helpers'

test.describe('Claude’s reply for the group', () => {
  test('once two people answered, the board offers one message for the group chat, with the invite link', async ({ page: owner, join }) => {
    const workshop = await createWorkshop(owner, 'Group reply check')
    await sendAvailability(await join(workshop.shareURL, 'Ada'))
    const grace = await join(workshop.shareURL, 'Grace Liu')
    await grace.page.getByLabel('When can you come?').fill('Count me in, ideally in the afternoon.')
    await grace.page.getByRole('button', { name: 'Send my availability' }).click()

    await owner.goto(workshop.ownerURL)
    const suggests = owner.getByRole('region', { name: 'Claude suggests' })
    const reply = suggests.getByRole('listitem').filter({ hasText: 'Claude’s reply for the group' })
    await expect(reply).toContainText('2 people have answered. Tell the group where things stand.', { timeout: 30_000 })
    await expect(reply).toContainText('Group reply check: thanks for answering, everyone!')
    await expect(reply).toContainText(/works for 1 of you so far/)
    await expect(reply).toContainText('The class needs 2 more people.')
    await expect(reply).toContainText('Grace: The class runs')
    await expect(reply).toContainText(workshop.shareURL)
    await expect(suggests).toContainText('Grace Liu’s message needs clarifying.')
    const all = await suggests.innerText()
    expect(all.indexOf('Claude’s reply for the group')).toBeLessThan(all.indexOf('Grace Liu’s message needs clarifying.'))
    expect(all.toLowerCase()).not.toContain('recommend')

    await owner.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    await reply.getByRole('button', { name: 'Copy message for the group' }).click()
    await expect(reply.getByRole('status')).toHaveText('Copied. Paste it in the group chat.')
    const copied = String(await owner.evaluate('navigator.clipboard.readText()'))
    expect(copied.startsWith('Group reply check: thanks for answering, everyone!')).toBe(true)
    expect(copied.endsWith('\n' + workshop.shareURL)).toBe(true)
    await expectAccessible(owner, 'the organizer page with Claude’s reply for the group')

    await owner.setViewportSize({ width: 390, height: 844 })
    await reply.scrollIntoViewIfNeeded()
    const overflow = Number(await owner.evaluate('document.documentElement.scrollWidth - window.innerWidth'))
    expect(overflow, 'the organizer page scrolls sideways at 390 px').toBeLessThanOrEqual(0)
    const button = await reply.getByRole('button', { name: 'Copy message for the group' }).boundingBox()
    expect(button?.height ?? 0).toBeGreaterThanOrEqual(44)
    await expectAccessible(owner, 'the organizer page with Claude’s reply for the group at 390 px')
  })
})
