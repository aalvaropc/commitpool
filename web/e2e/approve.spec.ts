import type { APIRequestContext } from '@playwright/test'
import { expectAccessible } from './a11y'
import { expect, headline, test } from './helpers'

interface ToolResult<T> {
  isError?: boolean
  structuredContent?: T
  content?: { type: string; text?: string }[]
}

interface Preview {
  approval_id: string
  state: string
  approve_url: string
  price: string
  next: string
}

async function tool<T>(request: APIRequestContext, name: string, args: Record<string, unknown>): Promise<ToolResult<T>> {
  const res = await request.post('/mcp', {
    headers: { Accept: 'application/json, text/event-stream' },
    data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } },
  })
  expect(res.status(), await res.text()).toBe(200)
  const body = (await res.json()) as { result?: ToolResult<T>; error?: { message: string } }
  expect(body.error).toBeUndefined()
  return body.result ?? {}
}

function said(result: ToolResult<unknown>): string {
  return (result.content ?? []).map((c) => c.text ?? '').join('')
}

function daysAhead(days: number): string {
  const d = new Date(Date.now() + days * 86_400_000)
  d.setUTCHours(15, 0, 0, 0)
  return d.toISOString().replace('.000Z', 'Z')
}

async function prepared(request: APIRequestContext, title: string): Promise<{ owner: string; dateId: string; preview: Preview }> {
  const created = await tool<{ owner_link: string; share_link: string }>(request, 'create_workshop', {
    title,
    duration_min: 120,
    timezone: 'America/New_York',
    minimum: 1,
    capacity: 4,
    price: '12.00',
    currency: 'USD',
    date_starts: [daysAhead(7)],
    host_name: 'Maya',
  })
  const owner = created.structuredContent!.owner_link
  const joined = await tool<{ participant_link: string }>(request, 'join_workshop', { share_link: created.structuredContent!.share_link, display_name: 'Ada' })
  await tool(request, 'submit_availability', { participant_link: joined.structuredContent!.participant_link, message: 'Any of the dates works for me.' })
  const status = await tool<{ recommended_date_id: string }>(request, 'get_status', { owner_link: owner })
  const dateId = status.structuredContent!.recommended_date_id
  expect(dateId).not.toBe('')
  const preview = (await tool<Preview>(request, 'preview_open_payments', { owner_link: owner, date_id: dateId })).structuredContent!
  expect(preview.state).toBe('pending')
  return { owner, dateId, preview }
}

test.describe('organizer approval for an assistant', () => {
  test('the assistant prepares, the organizer taps Approve on their own page, then reservations open', async ({ page, request }) => {
    const { owner, dateId, preview } = await prepared(request, 'Approved glazing night')
    expect(preview.approve_url).toBe(`${owner}/approve/${preview.approval_id}`)
    expect(preview.next).toContain('Show these terms to the organizer and ask them to open this link and tap Approve; then call confirm_open_payments.')

    const early = await tool(request, 'confirm_open_payments', { owner_link: owner, approval_id: preview.approval_id })
    expect(early.isError).toBe(true)
    expect(said(early)).toContain('The organizer has not approved yet')

    const apiPath = new URL(preview.approve_url).pathname.replace('/approve/', '/approvals/').replace(/^\/owner\//, '/api/v1/owner/')
    const scripted = await request.post(`${apiPath}/approve`)
    expect(scripted.status()).toBe(403)
    expect((await scripted.json()).error).toBe('approval_needs_page')

    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto(preview.approve_url)
    const ask = page.getByRole('region', { name: 'Your assistant wants to open reservations on these terms:' })
    await expect(ask).toBeVisible()
    await expect(page.getByRole('heading', { level: 1, name: 'Approved glazing night' })).toBeVisible()
    await expect(ask).toContainText('$12')
    await expect(ask).toContainText('24 hours')
    await expect(ask).toContainText('Nothing is charged until enough people reserve.')
    expect(Number(await page.evaluate('document.documentElement.scrollWidth'))).toBeLessThanOrEqual(390)
    for (const name of ['Approve', 'Not now']) {
      const box = await ask.getByRole('button', { name, exact: true }).boundingBox()
      expect(box?.height ?? 0).toBeGreaterThanOrEqual(44)
    }
    await expectAccessible(page, 'the approve page')

    await ask.getByRole('button', { name: 'Not now', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Not approved' })).toBeFocused()
    await page.getByRole('button', { name: 'Review the terms again' }).click()

    await page.route('**/api/v1/owner/*/approvals/*/approve', async (route) => {
      const headers = { ...route.request().headers() }
      delete headers['sec-fetch-site']
      await route.continue({ headers })
    })
    await ask.getByRole('button', { name: 'Approve', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Approved. Your assistant can now open reservations.' })).toBeFocused()
    await expect(page.getByText(/^Your assistant has until .+ to open them; after that it needs your approval again\.$/)).toBeVisible()
    const approvedAgain = (await tool<Preview>(request, 'preview_open_payments', { owner_link: owner, date_id: dateId })).structuredContent!
    expect(approvedAgain).toMatchObject({ approval_id: preview.approval_id, state: 'approved' })
    expect(approvedAgain.next).toContain('The organizer already approved these terms')
    expect(Number(await page.evaluate('document.documentElement.scrollWidth'))).toBeLessThanOrEqual(390)
    await expectAccessible(page, 'the approved page')

    const confirmed = await tool<{ payments_open: boolean; status: string }>(request, 'confirm_open_payments', { owner_link: owner, approval_id: preview.approval_id })
    expect(confirmed.isError, said(confirmed)).toBeFalsy()
    expect(confirmed.structuredContent).toMatchObject({ payments_open: true, status: 'FUNDING' })
    const again = await tool(request, 'confirm_open_payments', { owner_link: owner, approval_id: preview.approval_id })
    expect(said(again)).toContain('already used')

    await page.reload()
    await expect(page.getByRole('heading', { name: 'Reservations are open' })).toBeVisible()
    await page.getByRole('link', { name: 'Go to your organizer page' }).click()
    await expect(headline(page)).toHaveText('Reservations open')
  })

  test('a browser that still cannot approve after one refresh says so instead of looping', async ({ page, request }) => {
    const { preview } = await prepared(request, 'Stubborn browser night')
    let refused = 0
    await page.route('**/api/v1/owner/*/approvals/*/approve', async (route) => {
      refused++
      await route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error: 'approval_needs_page', message: 'Only the organizer can approve, on the approve page in their own browser.' }) })
    })
    await page.goto(preview.approve_url)
    const approve = page.getByRole('button', { name: 'Approve', exact: true })
    await approve.click()
    await expect(page.getByRole('alert')).toHaveText('This page had to refresh before approving. Tap Approve again.')
    await approve.click()
    await expect(page.getByRole('alert')).toContainText('This browser could not confirm the tap came from this page')
    await approve.click()
    await expect(page.getByRole('alert')).toContainText('This browser could not confirm the tap came from this page')
    expect(refused).toBe(3)
    await expect(approve).toBeVisible()
  })
})
