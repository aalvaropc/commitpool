import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium, expect } from '@playwright/test'

const here = path.dirname(fileURLToPath(import.meta.url))
const repo = path.resolve(here, '..', '..')
const WEBP_LIMIT = 200 * 1024
const PHONE = { width: 390, height: 844 }
const PHONE_SCREEN = { width: 390, height: 792 }
const GRACE_REPLY = 'No problem — I swapped Saturday. Sunday I’m still at the market.'
const usage = 'usage: node web/scripts/capture.mjs <base-url> [--shots dir] [--docs dir] [--png dir] [--no-docs]'

function parse(argv) {
  const out = { base: '', shots: path.join(repo, 'web/src/assets/shots'), docs: path.join(repo, 'docs/images'), png: '', withDocs: true }
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i]
    const value = () => {
      const v = argv[++i]
      if (!v) throw new Error(`${flag} needs a value\n${usage}`)
      return path.resolve(v)
    }
    if (flag === '--shots') out.shots = value()
    else if (flag === '--docs') out.docs = value()
    else if (flag === '--png') out.png = value()
    else if (flag === '--no-docs') out.withDocs = false
    else if (!flag.startsWith('--') && !out.base) out.base = flag.replace(/\/+$/, '')
    else throw new Error(`unknown argument ${flag}\n${usage}`)
  }
  if (!/^https?:\/\/[^/]+$/.test(out.base)) throw new Error(usage)
  return out
}

function step(text) {
  console.log(`capture: ${text}`)
}

function toWebp(png, out, maxWidth) {
  for (const quality of [82, 76, 70, 64, 58]) {
    execFileSync('magick', [png, '-resize', `${maxWidth}x>`, '-strip', '-quality', String(quality), '-define', 'webp:method=6', out])
    const size = fs.statSync(out).size
    if (size <= WEBP_LIMIT) {
      step(`${path.relative(repo, out)} ${Math.round(size / 1024)} KB at quality ${quality}`)
      return
    }
  }
  throw new Error(`${out} stays over ${WEBP_LIMIT / 1024} KB`)
}

function toDocPng(png, out, maxWidth) {
  execFileSync('magick', [png, '-resize', `${maxWidth}x>`, '-strip', '-colors', '256', '-define', 'png:compression-level=9', out])
  step(`${path.relative(repo, out)} ${Math.round(fs.statSync(out).size / 1024)} KB`)
}

async function settle(page) {
  await page.evaluate(() => document.fonts.ready)
  await page.evaluate('new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done)))')
}

async function windowAt(page, locator, file, { above, below, size = PHONE_SCREEN }) {
  await locator.scrollIntoViewIfNeeded()
  await settle(page)
  const box = await locator.boundingBox()
  const scroll = await page.evaluate(() => window.scrollY)
  const total = await page.evaluate(() => document.documentElement.scrollHeight)
  const top = below === undefined ? box.y + scroll - above : box.y + scroll + box.height + below - size.height
  const y = Math.max(0, Math.min(top, total - size.height))
  await page.screenshot({ path: file, fullPage: true, clip: { x: 0, y, width: size.width, height: size.height } })
}

async function union(page, locators, file, pad = 0) {
  await settle(page)
  const boxes = []
  for (const l of locators) {
    await l.scrollIntoViewIfNeeded()
    const box = await l.boundingBox()
    const scroll = await page.evaluate(() => window.scrollY)
    boxes.push({ x: box.x, y: box.y + scroll, right: box.x + box.width, bottom: box.y + scroll + box.height })
  }
  const x = Math.min(...boxes.map((b) => b.x)) - pad
  const y = Math.min(...boxes.map((b) => b.y)) - pad
  const width = Math.max(...boxes.map((b) => b.right)) + pad - x
  const height = Math.max(...boxes.map((b) => b.bottom)) + pad - y
  await page.screenshot({ path: file, fullPage: true, clip: { x, y, width, height } })
}

async function main() {
  const opts = parse(process.argv.slice(2))
  const work = opts.png || fs.mkdtempSync(path.join(os.tmpdir(), 'commitpool-capture-'))
  fs.mkdirSync(work, { recursive: true })
  fs.mkdirSync(opts.shots, { recursive: true })
  if (opts.withDocs) fs.mkdirSync(opts.docs, { recursive: true })
  const png = (name) => path.join(work, `${name}.png`)

  const browser = await chromium.launch()
  try {
    const desk = await browser.newContext({ baseURL: opts.base, viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, reducedMotion: 'reduce', colorScheme: 'dark' })
    const phone = await browser.newContext({ baseURL: opts.base, viewport: PHONE, deviceScaleFactor: 2, isMobile: true, hasTouch: true, reducedMotion: 'reduce', colorScheme: 'dark' })
    const owner = await desk.newPage()
    owner.setDefaultTimeout(60_000)

    if (opts.withDocs) {
      step('landing')
      const landing = await browser.newContext({ baseURL: opts.base, viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce', colorScheme: 'dark' })
      const page = await landing.newPage()
      await page.goto('/')
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
      await expect(page.getByRole('group', { name: 'Quick replies' })).toBeVisible({ timeout: 30_000 })
      await settle(page)
      await page.screenshot({ path: png('landing') })
      await landing.close()
      toDocPng(png('landing'), path.join(opts.docs, 'landing.png'), 1280)
    }

    step('Try it as Iris')
    await owner.goto('/')
    await owner.getByRole('button', { name: 'Try it as Iris' }).first().click()
    await expect(owner.getByRole('heading', { name: 'You are Iris now' })).toBeVisible()
    step('reading Iris’s group chat')
    const chat = owner.getByRole('region', { name: 'Paste your group chat' })
    await expect(chat.getByRole('textbox', { name: 'Paste your group chat' })).toHaveValue(/\] Iris Calder: /)
    await chat.getByRole('button', { name: 'Read the chat' }).click()
    await chat.getByRole('button', { name: 'Add 6 people' }).click()
    await expect(owner.getByRole('region', { name: 'Paste more of the chat' })).toBeVisible()
    step('waiting for the six answers')
    await expect(owner.getByText('6 of 6 answered', { exact: false })).toBeVisible({ timeout: 240_000 })
    await expect(owner.getByText('Claude is writing why this date works…')).toHaveCount(0, { timeout: 120_000 })
    await owner.reload()
    await expect(owner.getByText('6 of 6 answered', { exact: false })).toBeVisible()
    const ownerURL = owner.url()
    const grid = owner.locator('section[aria-labelledby="grid-title"]')

    step('board')
    await grid.scrollIntoViewIfNeeded()
    await settle(owner)
    await grid.screenshot({ path: png('board') })
    toWebp(png('board'), path.join(opts.shots, 'board.webp'), 1600)
    if (opts.withDocs) toDocPng(png('board'), path.join(opts.docs, 'organizer-board.png'), 1152)

    if (opts.withDocs) {
      step('invite on a phone')
      const share = await owner.getByRole('textbox', { name: 'Invite link', exact: true }).first().inputValue()
      const invite = await phone.newPage()
      await invite.goto(share)
      await expect(invite.getByRole('heading', { level: 1, name: 'Life drawing for beginners' })).toBeVisible()
      await settle(invite)
      await invite.screenshot({ path: png('invite') })
      toDocPng(png('invite'), path.join(opts.docs, 'invite-phone.png'), 780)
      await invite.close()
    }

    step('Grace answers Claude')
    const graceURL = await owner.getByRole('link', { name: /Act as Grace Liu/ }).first().getAttribute('href')
    const grace = await phone.newPage()
    grace.setDefaultTimeout(60_000)
    await grace.goto(graceURL)
    await expect(grace.getByText('You joined as Grace Liu.')).toBeVisible()
    const answer = grace.getByLabel('Your answer to Claude’s question')
    await expect(answer).toBeVisible()
    await answer.fill(GRACE_REPLY)
    await answer.blur()
    await windowAt(grace, grace.getByRole('button', { name: 'Send my answer' }), png('grace'), { below: 120 })
    toWebp(png('grace'), path.join(opts.shots, 'grace.webp'), 600)
    await grace.getByRole('button', { name: 'Send my answer' }).click()
    await expect(grace.getByRole('button', { name: 'Update my availability' })).toBeVisible({ timeout: 120_000 })

    step('Saturday reaches 3')
    await owner.goto(ownerURL)
    const next = owner.getByRole('region', { name: 'Next step' })
    await expect(next).toContainText('works for 3 — you need 3', { timeout: 120_000 })
    await expect(owner.getByText('Claude is writing why this date works…')).toHaveCount(0, { timeout: 120_000 })
    await expect(owner.getByText('Claude is rewording this message…')).toHaveCount(0, { timeout: 120_000 })
    if (opts.withDocs) {
      await owner.reload()
      await expect(next).toContainText('works for 3 — you need 3')
      await union(owner, [owner.locator('section[aria-labelledby="suggests-title"]'), owner.locator('section[aria-labelledby="claude-title"]')], png('suggests'))
      toDocPng(png('suggests'), path.join(opts.docs, 'claude-suggests.png'), 776)
    }

    step('open reservations')
    await next.getByRole('button', { name: /^Open reservations for / }).click()
    await owner.getByRole('dialog').getByRole('button', { name: 'Open reservations', exact: true }).click()
    await expect(owner.getByRole('heading', { name: 'Terms people agreed to' })).toBeVisible()

    step('Priya’s reservation')
    const priyaURL = await owner.getByRole('link', { name: /Act as Priya Raman/ }).first().getAttribute('href')
    const priya = await phone.newPage()
    await priya.goto(priyaURL)
    await expect(priya.getByRole('button', { name: 'Reserve with PayPal' })).toBeVisible()
    await windowAt(priya, priya.getByRole('button', { name: 'Reserve with PayPal' }), png('reserve'), { below: 40 })
    toWebp(png('reserve'), path.join(opts.shots, 'reserve.webp'), 600)

    if (opts.withDocs) {
      step('a drafted workshop')
      const draft = await desk.newPage()
      await draft.goto('/new')
      await draft.getByRole('button', { name: 'Use Iris’s example' }).click()
      await draft.getByRole('button', { name: 'Draft it with Claude' }).click()
      await expect(draft.getByRole('heading', { name: 'Check your workshop' })).toBeFocused({ timeout: 120_000 })
      await draft.evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
      const top = draft.getByText('Step 2 of 2', { exact: true })
      const money = draft.getByLabel('Spots')
      await union(draft, [top, money], png('draft'), 24)
      toDocPng(png('draft'), path.join(opts.docs, 'create-draft.png'), 800)
    }
  } finally {
    await browser.close()
  }
  step(`PNGs kept in ${work}`)
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err)
  process.exit(1)
})
