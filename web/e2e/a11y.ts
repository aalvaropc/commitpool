import AxeBuilder from '@axe-core/playwright'
import { expect, type Page } from '@playwright/test'

const WCAG = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']

export async function expectAccessible(page: Page, where: string): Promise<void> {
  const result = await new AxeBuilder({ page }).withTags(WCAG).analyze()
  const serious = result.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')
  const report = serious.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`)
  expect(report, `accessibility problems on ${where}`).toEqual([])
}
