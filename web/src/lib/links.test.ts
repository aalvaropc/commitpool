import { describe, expect, it } from 'vitest'
import { safeHttps } from './links'

describe('safeHttps', () => {
  it('keeps an https link and normalizes an upper-case scheme', () => {
    expect(safeHttps('https://devpost.com/software/commitpool')).toBe('https://devpost.com/software/commitpool')
    expect(safeHttps('HTTPS://devpost.com/software/commitpool')).toBe('https://devpost.com/software/commitpool')
  })

  it('refuses anything that is not an https link to a host', () => {
    for (const bad of [undefined, '', 'http://devpost.com/x', 'javascript:alert(1)', 'https://:443', 'https://user:pass@devpost.com/x', 'devpost.com/x', 'not a url']) {
      expect(safeHttps(bad)).toBeNull()
    }
  })
})
