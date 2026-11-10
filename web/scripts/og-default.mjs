import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { chromium } from '@playwright/test'

const here = path.dirname(fileURLToPath(import.meta.url))
const template = path.join(here, 'og-default.html')
const out = path.resolve(process.argv[2] ?? path.join(here, '..', 'public', 'og-default.png'))

const browser = await chromium.launch()
try {
  const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 })
  await page.goto(pathToFileURL(template).href)
  await page.evaluate(() => document.fonts.ready)
  const loaded = await page.evaluate(() => document.fonts.check('800 118px "Bricolage Grotesque"'))
  if (!loaded) throw new Error('Bricolage Grotesque did not load; the card would fall back to a system face')
  await page.screenshot({ path: out })
} finally {
  await browser.close()
}
execFileSync('magick', [out, '-strip', '-define', 'png:compression-level=9', out])
console.log(`og-default: wrote ${path.relative(process.cwd(), out)}`)
