import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { countdownShown } from '../lib/status'
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

describe('countdownShown', () => {
  it('hides the countdown only for someone paid in a class that is on', () => {
    expect(countdownShown('CONFIRMED', 'PAID')).toBe(false)
    expect(countdownShown('FUNDING', 'PAID')).toBe(true)
    expect(countdownShown('FUNDING', 'AUTHORIZED')).toBe(true)
    expect(countdownShown('CONFIRMED', 'AUTHORIZED')).toBe(true)
    expect(countdownShown('FUNDING', null)).toBe(true)
  })
})
