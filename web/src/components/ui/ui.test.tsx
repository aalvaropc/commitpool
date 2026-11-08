import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { AvatarStack } from './Avatar'
import { Button } from './Button'
import { ConfirmDialog } from './ConfirmDialog'
import { Meter } from './Meter'
import { SegmentedControl } from './SegmentedControl'
import { VerdictChip } from './VerdictChip'

describe('Meter', () => {
  it('says how far the group is from happening, in words', () => {
    const html = renderToStaticMarkup(<Meter capacity={6} needed={3} paid={0} reserved={1} />)
    expect(html).toContain('2 more and it’s on')
    expect(html).toContain('0 paid and 1 reserved of 6 spots; 3 needed to happen.')
  })

  it('keeps only the words for screen readers when the caption is left to the page', () => {
    const html = renderToStaticMarkup(<Meter capacity={6} needed={3} paid={0} reserved={1} caption={false} />)
    expect(html).not.toContain('2 more and it’s on')
    expect(html).not.toContain('0 paid · 1 reserved')
    expect(html).toMatch(/<figcaption class="sr-only">0 paid and 1 reserved of 6 spots; 3 needed to happen.<\/figcaption>/)
  })
})

describe('VerdictChip', () => {
  it('always carries its words, never colour alone', () => {
    const html = renderToStaticMarkup(<VerdictChip verdict="conditional">Only if it ends by 6 PM</VerdictChip>)
    expect(html).toContain('Only if it ends by 6 PM')
    expect(html).toMatch(/<svg[^>]*aria-hidden="true"/)
  })
})

describe('AvatarStack', () => {
  it('names everyone for screen readers and shows how many are hidden', () => {
    const html = renderToStaticMarkup(<AvatarStack names={['Priya Raman', 'Ben Whitaker', 'Lucía Torres']} max={2} label="Priya, Ben and Lucía answered" />)
    expect(html).toContain('aria-label="Priya, Ben and Lucía answered"')
    expect(html).toContain('+1')
    expect(html).toContain('PR')
  })
})

describe('SegmentedControl', () => {
  it('is a group of native radios with the current value checked', () => {
    const html = renderToStaticMarkup(
      <SegmentedControl
        legend="Can you come on Sat, Nov 14?"
        name="sat"
        value="available"
        options={[
          { value: 'available', label: 'Can come' },
          { value: 'unavailable', label: 'Can’t' },
        ]}
        onChange={() => undefined}
      />,
    )
    expect(html).toContain('<legend')
    expect(html).toMatch(/<input id="sat-available" type="radio"[^>]*checked=""[^>]*value="available"/)
    expect(html).toMatch(/<label for="sat-unavailable"/)
  })
})

describe('ConfirmDialog', () => {
  it('is a native dialog named by its title and described by its body', () => {
    const html = renderToStaticMarkup(
      <ConfirmDialog open={false} title="Give up your spot?" confirmLabel="Yes, give it up" onConfirm={() => undefined} onClose={() => undefined}>
        Your spot goes back to the group.
      </ConfirmDialog>,
    )
    expect(html).toMatch(/^<dialog[^>]*aria-labelledby="[^"]+"[^>]*aria-describedby="[^"]+"/)
    expect(html).toContain('Give up your spot?')
    expect(html).toContain('Yes, give it up')
  })
})

describe('Button while busy', () => {
  it('keeps an ordinary button focusable and says it is busy', () => {
    const html = renderToStaticMarkup(
      <Button busy busyLabel="Leaving…">
        Yes, leave
      </Button>,
    )
    expect(html).toContain('aria-disabled="true"')
    expect(html).not.toMatch(/\sdisabled=""/)
    expect(html).toContain('Leaving…')
  })

  it('disables a submit button so Enter cannot send the form twice', () => {
    const html = renderToStaticMarkup(
      <Button type="submit" busy>
        Join the class
      </Button>,
    )
    expect(html).toMatch(/\sdisabled=""/)
  })

  it('shapes every button as a pill', () => {
    for (const size of ['sm', 'md', 'lg'] as const) expect(renderToStaticMarkup(<Button size={size}>Go</Button>)).toContain('rounded-full')
  })
})

describe('Meter caption', () => {
  it('says the split between paid and reserved in words, not only in colour', () => {
    const html = renderToStaticMarkup(<Meter capacity={6} needed={3} paid={1} reserved={2} />)
    expect(html).toContain('1 paid · 2 reserved · 3 needed')
  })

  it('never speaks of reservations while it only shows who can come', () => {
    const html = renderToStaticMarkup(<Meter capacity={6} needed={3} paid={0} reserved={4} interest />)
    expect(html).toContain('aria-hidden="true"')
    expect(html).not.toContain('reserved')
    expect(html).not.toContain('bg-hold')
  })
})
