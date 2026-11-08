import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { SampleReading } from '../../lib/sampleReader'
import { Asks, CommitPoolSays, Reading } from './ChatBubbles'

const reading: SampleReading = { sat: 'available', sun: 'unavailable', limits: {}, ask: null }

describe('Reading', () => {
  it.each([
    ['prepared', 'Prepared reading'],
    ['claude', 'Read by Claude'],
    ['rules', 'Simple rules read this one'],
    ['fallback', 'Simple rules read this one — Claude wasn’t available'],
  ] as const)('labels a %s reading as %j', (by, label) => {
    const html = renderToStaticMarkup(<Reading reading={reading} by={by} />)
    expect(html).toContain(label)
    expect(html).toContain('Sat · Can come')
  })

  it('says Claude is reading only for the visitor’s answer', () => {
    expect(renderToStaticMarkup(<Reading reading={null} by="claude" live />)).toContain('Claude is reading…')
    expect(renderToStaticMarkup(<Reading reading={null} by="prepared" />)).toBe('')
  })
})

describe('signed bubbles', () => {
  it('signs the count as CommitPool and a question by whoever read', () => {
    expect(renderToStaticMarkup(<CommitPoolSays text="2 and 2" />)).toContain('CommitPool')
    expect(renderToStaticMarkup(<CommitPoolSays text="2 and 2" />)).not.toContain('Claude')
    expect(renderToStaticMarkup(<Asks by="claude" text="Which morning?" />)).toContain('Claude')
    expect(renderToStaticMarkup(<Asks by="fallback" text="Which morning?" />)).toContain('Simple rules')
  })
})
