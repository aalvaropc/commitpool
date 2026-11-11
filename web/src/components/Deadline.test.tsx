import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import Deadline from './Deadline'

const closesAt = new Date(Date.now() + 36 * 3600 * 1000).toISOString()

describe('Deadline', () => {
  it('counts down while reservations are open', () => {
    expect(renderToStaticMarkup(<Deadline closesAt={closesAt} timeZone="America/New_York" open />)).toContain(' left)')
  })

  it('keeps the closing time but drops the countdown for someone already paid', () => {
    const html = renderToStaticMarkup(<Deadline closesAt={closesAt} timeZone="America/New_York" open counting={false} />)
    expect(html).toContain('Reservations close ')
    expect(html).not.toContain(' left')
  })
})
